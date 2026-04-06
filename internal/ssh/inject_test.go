package ssh

import "testing"

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
