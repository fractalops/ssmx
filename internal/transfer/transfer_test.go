package transfer

import (
	"slices"
	"testing"
)

func TestBuildScpArgs_LocalToRemote(t *testing.T) {
	spec := CopySpec{
		LocalPath:  "./file.txt",
		RemotePath: "/tmp/",
		Direction:  LocalToRemote,
		User:       "ec2-user",
		KeyPath:    "/home/user/.ssmx/ssh_key",
	}
	got := buildScpArgs("/usr/local/bin/ssmcp", "i-0abc123def", spec)
	want := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ProxyCommand=/usr/local/bin/ssmcp --proxy %h %r",
		"-i", "/home/user/.ssmx/ssh_key",
		"./file.txt",
		"ec2-user@i-0abc123def:/tmp/",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestBuildScpArgs_RemoteToLocal(t *testing.T) {
	spec := CopySpec{
		LocalPath:  "./logs/",
		RemotePath: "/var/log/app.log",
		Direction:  RemoteToLocal,
		User:       "ubuntu",
		KeyPath:    "/home/user/.ssmx/ssh_key",
	}
	got := buildScpArgs("/usr/local/bin/ssmcp", "i-0abc123def", spec)
	want := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ProxyCommand=/usr/local/bin/ssmcp --proxy %h %r",
		"-i", "/home/user/.ssmx/ssh_key",
		"ubuntu@i-0abc123def:/var/log/app.log",
		"./logs/",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestBuildScpArgs_Recursive(t *testing.T) {
	spec := CopySpec{
		LocalPath:  "./dist/",
		RemotePath: "/srv/app/",
		Direction:  LocalToRemote,
		User:       "ec2-user",
		Recursive:  true,
	}
	got := buildScpArgs("/usr/local/bin/ssmcp", "i-0abc123def", spec)
	want := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ProxyCommand=/usr/local/bin/ssmcp --proxy %h %r",
		"-r",
		"./dist/",
		"ec2-user@i-0abc123def:/srv/app/",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestBuildScpArgs_WithProfile(t *testing.T) {
	spec := CopySpec{
		LocalPath:  "./file.txt",
		RemotePath: "/tmp/",
		Direction:  LocalToRemote,
		User:       "ec2-user",
		Profile:    "staging",
		Region:     "eu-west-1",
	}
	got := buildScpArgs("/usr/local/bin/ssmcp", "i-0abc123def", spec)
	want := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ProxyCommand=/usr/local/bin/ssmcp --profile staging --region eu-west-1 --proxy %h %r",
		"./file.txt",
		"ec2-user@i-0abc123def:/tmp/",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestBuildScpArgs_NoKeyPath(t *testing.T) {
	spec := CopySpec{
		LocalPath:  "./file.txt",
		RemotePath: "/tmp/",
		Direction:  LocalToRemote,
		User:       "ec2-user",
		KeyPath:    "",
	}
	got := buildScpArgs("/usr/local/bin/ssmcp", "i-0abc123def", spec)
	for i, arg := range got {
		if arg == "-i" {
			t.Errorf("expected no -i flag, found at index %d", i)
		}
	}
}
