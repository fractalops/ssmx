package ssh

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DefaultSSHUser returns the default SSH username for the given SSM PlatformName.
// Falls back to "ec2-user" for unknown platforms.
func DefaultSSHUser(platformName string) string {
	p := strings.ToLower(platformName)
	switch {
	case strings.Contains(p, "ubuntu"):
		return "ubuntu"
	case strings.Contains(p, "debian"):
		return "admin"
	case strings.Contains(p, "windows"):
		return "Administrator"
	default:
		return "ec2-user" // Amazon Linux, CentOS, RHEL, SUSE, unknown
	}
}

// DefaultKeyPath returns the first existing standard SSH public key path,
// checking id_ed25519, id_rsa, id_ecdsa in order. Returns "" if none exist.
func DefaultKeyPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, name := range []string{"id_ed25519", "id_rsa", "id_ecdsa"} {
		path := filepath.Join(home, ".ssh", name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// LoadOrGenerateKey returns the public key contents for keyPath.
// If keyPath is "", it generates a new ed25519 keypair at
// ~/.ssmx/ssh_key and returns the public key. If the key already
// exists at that path, it is reused without regenerating.
func LoadOrGenerateKey(keyPath string) (pubKey string, resolvedPath string, err error) {
	if keyPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", err
		}
		keyPath = filepath.Join(home, ".ssmx", "ssh_key")
	}

	pubPath := keyPath + ".pub"

	// Generate if private key doesn't exist.
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
			return "", "", fmt.Errorf("creating key dir: %w", err)
		}
		cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-f", keyPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", "", fmt.Errorf("ssh-keygen: %w\n%s", err, out)
		}
	}

	data, err := os.ReadFile(pubPath)
	if err != nil {
		return "", "", fmt.Errorf("reading public key %s: %w", pubPath, err)
	}
	return strings.TrimSpace(string(data)), keyPath, nil
}
