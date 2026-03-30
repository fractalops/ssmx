package preflight

import (
	"fmt"
	"os/exec"
	"runtime"
)

const pluginBinary = "session-manager-plugin"

// PluginInstalled returns true if session-manager-plugin is on PATH.
func PluginInstalled() bool {
	_, err := exec.LookPath(pluginBinary)
	return err == nil
}

// InstallPlugin attempts to install session-manager-plugin for the current
// platform. Returns an error if automatic installation is not supported.
func InstallPlugin() error {
	switch runtime.GOOS {
	case "darwin":
		return installDarwin()
	case "linux":
		return installLinux()
	default:
		return fmt.Errorf("automatic install not supported on %s — see https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html", runtime.GOOS)
	}
}

func installDarwin() error {
	if _, err := exec.LookPath("brew"); err == nil {
		cmd := exec.Command("brew", "install", "session-manager-plugin")
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("brew install session-manager-plugin: %w", err)
		}
		return nil
	}
	return fmt.Errorf("homebrew not found — install manually: https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html")
}

func installLinux() error {
	if _, err := exec.LookPath("dpkg"); err == nil {
		return installLinuxDeb()
	}
	if _, err := exec.LookPath("rpm"); err == nil {
		return installLinuxRPM()
	}
	return fmt.Errorf("unsupported Linux distribution — install manually: https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html")
}

func installLinuxDeb() error {
	cmds := [][]string{
		{"curl", "--silent", "-o", "/tmp/session-manager-plugin.deb",
			"https://s3.amazonaws.com/session-manager-downloads/plugin/latest/ubuntu_64bit/session-manager-plugin.deb"},
		{"sudo", "dpkg", "-i", "/tmp/session-manager-plugin.deb"},
	}
	for _, args := range cmds {
		if err := exec.Command(args[0], args[1:]...).Run(); err != nil {
			return fmt.Errorf("running %v: %w", args, err)
		}
	}
	return nil
}

func installLinuxRPM() error {
	cmds := [][]string{
		{"curl", "--silent", "-o", "/tmp/session-manager-plugin.rpm",
			"https://s3.amazonaws.com/session-manager-downloads/plugin/latest/linux_64bit/session-manager-plugin.rpm"},
		{"sudo", "yum", "install", "-y", "/tmp/session-manager-plugin.rpm"},
	}
	for _, args := range cmds {
		if err := exec.Command(args[0], args[1:]...).Run(); err != nil {
			return fmt.Errorf("running %v: %w", args, err)
		}
	}
	return nil
}
