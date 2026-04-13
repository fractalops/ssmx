package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gossh "golang.org/x/crypto/ssh"
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
			return "", "", fmt.Errorf("resolving home directory: %w", err)
		}
		keyPath = filepath.Join(home, ".ssmx", "ssh_key")
	}

	pubPath := keyPath + ".pub"

	// Generate if private key doesn't exist.
	if _, statErr := os.Stat(keyPath); os.IsNotExist(statErr) {
		if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
			return "", "", fmt.Errorf("creating key dir: %w", err)
		}
		if err := generateEd25519Key(keyPath); err != nil {
			return "", "", err
		}
	} else if statErr != nil {
		return "", "", fmt.Errorf("checking key path: %w", statErr)
	}

	data, err := os.ReadFile(pubPath)
	if err != nil {
		return "", "", fmt.Errorf("reading public key %s: %w", pubPath, err)
	}
	return strings.TrimSpace(string(data)), keyPath, nil
}

// generateEd25519Key generates a new ed25519 keypair and writes the private
// key (OpenSSH PEM format, mode 0600) and public key (authorized_keys format,
// mode 0644) to keyPath and keyPath+".pub" respectively.
func generateEd25519Key(keyPath string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generating ed25519 key: %w", err)
	}

	// Private key: OpenSSH PEM format.
	pemBlock, err := gossh.MarshalPrivateKey(priv, "")
	if err != nil {
		return fmt.Errorf("marshaling private key: %w", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(pemBlock), 0o600); err != nil {
		return fmt.Errorf("writing private key: %w", err)
	}

	// Public key: authorized_keys format (e.g. "ssh-ed25519 AAAA...").
	sshPub, err := gossh.NewPublicKey(pub)
	if err != nil {
		return fmt.Errorf("converting public key: %w", err)
	}
	if err := os.WriteFile(keyPath+".pub", gossh.MarshalAuthorizedKey(sshPub), 0o644); err != nil {
		return fmt.Errorf("writing public key: %w", err)
	}

	return nil
}
