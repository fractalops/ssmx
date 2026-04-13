package preflight

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const pluginBinary = "session-manager-plugin"

// PluginInstalled returns true if session-manager-plugin is on PATH.
func PluginInstalled() bool {
	_, err := exec.LookPath(pluginBinary)
	return err == nil
}

// InstallPlugin attempts to install session-manager-plugin for the current
// platform. Returns an error if automatic installation is not supported.
func InstallPlugin(ctx context.Context) error {
	switch runtime.GOOS {
	case "darwin":
		return installDarwin(ctx)
	case "linux":
		return installLinux(ctx)
	default:
		return fmt.Errorf("automatic install not supported on %s — see https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html", runtime.GOOS)
	}
}

// knownBrewBins are common locations where Homebrew places binaries.
var knownBrewBins = []string{
	"/opt/homebrew/bin", // Apple Silicon
	"/usr/local/bin",    // Intel
}

func installDarwin(ctx context.Context) error {
	// If brew already downloaded the cask pkg, use it directly — no need to
	// run brew again (which triggers a slow index update).
	if pkg := findBrewCaskPkg(); pkg != "" {
		return openPkgInstaller(ctx, pkg)
	}

	// Try brew to download the cask.
	if brewPath, err := exec.LookPath("brew"); err == nil {
		fmt.Print("  Using Homebrew... ")
		cmd := exec.CommandContext(ctx, brewPath, "install", "session-manager-plugin")
		cmd.Env = append(os.Environ(), "HOMEBREW_NO_AUTO_UPDATE=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Println()
			_, _ = os.Stderr.Write(out)
			return fmt.Errorf("brew install session-manager-plugin: %w", err)
		}
		fmt.Println("done")
		if ensurePluginOnPath() == nil {
			return nil
		}
		if pkg := findBrewCaskPkg(); pkg != "" {
			return openPkgInstaller(ctx, pkg)
		}
	}

	// No brew — download the .pkg directly.
	return installDarwinPkg(ctx)
}

// findBrewCaskPkg looks for a downloaded .pkg in the brew Caskroom.
func findBrewCaskPkg() string {
	caskroots := []string{"/opt/homebrew/Caskroom", "/usr/local/Caskroom"}
	for _, base := range caskroots {
		matches, _ := filepath.Glob(filepath.Join(base, "session-manager-plugin", "*", "*.pkg"))
		if len(matches) > 0 {
			return matches[0]
		}
	}
	return ""
}

func installDarwinPkg(ctx context.Context) error {
	pkgURL := "https://s3.amazonaws.com/session-manager-downloads/plugin/latest/mac/session-manager-plugin.pkg"
	if runtime.GOARCH == "arm64" {
		pkgURL = "https://s3.amazonaws.com/session-manager-downloads/plugin/latest/mac_arm64/session-manager-plugin.pkg"
	}

	tmpDir, err := os.MkdirTemp("", "ssmx-plugin-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	pkgPath := filepath.Join(tmpDir, "session-manager-plugin.pkg")
	fmt.Print("  Downloading session-manager-plugin.pkg... ")
	if err := downloadFile(ctx, pkgURL, pkgPath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	fmt.Println("done")

	return openPkgInstaller(ctx, pkgPath)
}

// openPkgInstaller installs a .pkg silently via osascript, which shows a
// single macOS auth dialog and runs installer headlessly.
func openPkgInstaller(ctx context.Context, pkgPath string) error {
	const (
		pluginBin  = "/usr/local/sessionmanagerplugin/bin/session-manager-plugin"
		pluginLink = "/usr/local/bin/session-manager-plugin"
	)

	// Remove quarantine so Gatekeeper doesn't block the pkg.
	_ = exec.CommandContext(ctx, "xattr", "-d", "com.apple.quarantine", pkgPath).Run()

	fmt.Println()
	fmt.Println("  session-manager-plugin is an AWS tool ssmx uses to open secure")
	fmt.Println("  sessions to your EC2 instances. You'll be prompted to allow it.")
	fmt.Println()

	script := fmt.Sprintf(
		`do shell script "installer -pkg %s -target / && ln -sf %s %s" with administrator privileges`,
		pkgPath, pluginBin, pluginLink,
	)
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "User cancelled") {
			return fmt.Errorf("installation cancelled")
		}
		return fmt.Errorf("install failed: %s", strings.TrimSpace(string(out)))
	}

	_ = ensurePluginOnPath()
	return nil
}

// ensurePluginOnPath checks if session-manager-plugin is findable; if not,
// it searches known locations and prepends the first match to PATH.
func ensurePluginOnPath() error {
	if _, err := exec.LookPath(pluginBinary); err == nil {
		return nil
	}
	for _, dir := range knownBrewBins {
		candidate := filepath.Join(dir, pluginBinary)
		if _, err := os.Stat(candidate); err == nil {
			current := os.Getenv("PATH")
			if current != "" {
				_ = os.Setenv("PATH", dir+":"+current)
			} else {
				_ = os.Setenv("PATH", dir)
			}
			return nil
		}
	}
	return fmt.Errorf("session-manager-plugin installed but not found in PATH or known locations — open a new terminal and try again")
}

func downloadFile(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request for %s: %w", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("creating download destination %s: %w", dest, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("writing download to %s: %w", dest, err)
	}
	return nil
}

func installLinux(ctx context.Context) error {
	if _, err := exec.LookPath("dpkg"); err == nil {
		return installLinuxDeb(ctx)
	}
	if _, err := exec.LookPath("rpm"); err == nil {
		return installLinuxRPM(ctx)
	}
	return fmt.Errorf("unsupported Linux distribution — install manually: https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html")
}

func installLinuxDeb(ctx context.Context) error {
	cmds := [][]string{
		{"curl", "--silent", "-o", "/tmp/session-manager-plugin.deb",
			"https://s3.amazonaws.com/session-manager-downloads/plugin/latest/ubuntu_64bit/session-manager-plugin.deb"},
		{"sudo", "dpkg", "-i", "/tmp/session-manager-plugin.deb"},
	}
	for _, args := range cmds {
		if err := exec.CommandContext(ctx, args[0], args[1:]...).Run(); err != nil { //nolint:gosec // installer args are controlled by this binary, not user input
			return fmt.Errorf("running %v: %w", args, err)
		}
	}
	return nil
}

func installLinuxRPM(ctx context.Context) error {
	cmds := [][]string{
		{"curl", "--silent", "-o", "/tmp/session-manager-plugin.rpm",
			"https://s3.amazonaws.com/session-manager-downloads/plugin/latest/linux_64bit/session-manager-plugin.rpm"},
		{"sudo", "yum", "install", "-y", "/tmp/session-manager-plugin.rpm"},
	}
	for _, args := range cmds {
		if err := exec.CommandContext(ctx, args[0], args[1:]...).Run(); err != nil { //nolint:gosec // installer args are controlled by this binary, not user input
			return fmt.Errorf("running %v: %w", args, err)
		}
	}
	return nil
}
