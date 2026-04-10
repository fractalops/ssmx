package ssh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultSSHUser(t *testing.T) {
	cases := []struct {
		platform string
		want     string
	}{
		{"Amazon Linux", "ec2-user"},
		{"Amazon Linux 2", "ec2-user"},
		{"Ubuntu", "ubuntu"},
		{"Ubuntu 22.04", "ubuntu"},
		{"Debian", "admin"},
		{"Debian GNU/Linux", "admin"},
		{"CentOS Linux", "ec2-user"},
		{"Red Hat Enterprise Linux", "ec2-user"},
		{"SUSE Linux", "ec2-user"},
		{"Windows Server 2022", "Administrator"},
		{"", "ec2-user"}, // unknown → safe default
	}
	for _, tc := range cases {
		got := DefaultSSHUser(tc.platform)
		if got != tc.want {
			t.Errorf("DefaultSSHUser(%q) = %q, want %q", tc.platform, got, tc.want)
		}
	}
}

func TestDefaultKeyPath_ReturnsEmpty_WhenNoneExist(t *testing.T) {
	// Point home to a temp dir with no .ssh keys.
	t.Setenv("HOME", t.TempDir())
	got := DefaultKeyPath()
	if got != "" {
		t.Errorf("expected empty path when no keys exist, got %q", got)
	}
}

func TestLoadOrGenerateKey_GeneratesKeyIfMissing(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ssh_key")

	pubKey, resolved, err := LoadOrGenerateKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrGenerateKey: %v", err)
	}
	if resolved != keyPath {
		t.Errorf("resolved = %q, want %q", resolved, keyPath)
	}
	if !strings.HasPrefix(pubKey, "ssh-ed25519 ") {
		t.Errorf("pubKey does not look like ed25519: %q", pubKey)
	}

	// Private key must exist with mode 0600.
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("private key not created: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("private key mode = %o, want 0600", info.Mode().Perm())
	}

	// Public key must exist with mode 0644.
	info, err = os.Stat(keyPath + ".pub")
	if err != nil {
		t.Fatalf("public key not created: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("public key mode = %o, want 0644", info.Mode().Perm())
	}
}

func TestLoadOrGenerateKey_ReusesExistingKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ssh_key")

	pub1, _, err := LoadOrGenerateKey(keyPath)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	pub2, _, err := LoadOrGenerateKey(keyPath)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if pub1 != pub2 {
		t.Error("second call regenerated the key instead of reusing it")
	}
}

func TestLoadOrGenerateKey_EmptyPath_UsesDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	pubKey, resolved, err := LoadOrGenerateKey("")
	if err != nil {
		t.Fatalf("LoadOrGenerateKey: %v", err)
	}
	if !strings.Contains(resolved, ".ssmx") {
		t.Errorf("default path %q should contain .ssmx", resolved)
	}
	if !strings.HasPrefix(pubKey, "ssh-ed25519 ") {
		t.Errorf("pubKey does not look like ed25519: %q", pubKey)
	}
}
