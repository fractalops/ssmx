package transfer

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// TestLoadSigner_ParsesValidKey verifies that a freshly generated ed25519
// private key can be loaded and parsed by loadSigner.
func TestLoadSigner_ParsesValidKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pemBlock, err := gossh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(pemBlock), 0o600); err != nil {
		t.Fatal(err)
	}

	signer, err := loadSigner(keyPath)
	if err != nil {
		t.Fatalf("loadSigner: %v", err)
	}
	if signer.PublicKey().Type() != "ssh-ed25519" {
		t.Errorf("key type = %q, want ssh-ed25519", signer.PublicKey().Type())
	}
}

// TestLoadSigner_MissingFile verifies that loadSigner returns an error for a
// non-existent key file.
func TestLoadSigner_MissingFile(t *testing.T) {
	_, err := loadSigner("/nonexistent/path/key")
	if err == nil {
		t.Error("expected error for missing key, got nil")
	}
}

// TestProxyConn_ReadWrite verifies that proxyConn correctly routes reads to
// the Reader and writes to the WriteCloser.
func TestProxyConn_ReadWrite(t *testing.T) {
	// pr1/pw1: data written by the test flows into conn.Reader (simulates subprocess stdout)
	pr1, pw1 := io.Pipe()
	// pr2/pw2: data written by conn flows out of the WriteCloser (simulates subprocess stdin)
	pr2, pw2 := io.Pipe()

	conn := &proxyConn{stdout: pr1, stdin: pw2}

	done := make(chan struct{})
	go func() {
		defer close(done)
		pw1.Write([]byte("hello"))
	}()

	buf := make([]byte, 5)
	if _, err := conn.Read(buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf) != "hello" {
		t.Errorf("Read got %q, want %q", buf, "hello")
	}

	go conn.Write([]byte("world"))

	buf2 := make([]byte, 5)
	if _, err := pr2.Read(buf2); err != nil {
		t.Fatalf("pr2.Read: %v", err)
	}
	if string(buf2) != "world" {
		t.Errorf("Write got %q, want %q", buf2, "world")
	}

	// These must not panic.
	_ = conn.LocalAddr()
	_ = conn.RemoteAddr()
	_ = conn.SetDeadline(time.Time{})
	_ = conn.SetReadDeadline(time.Time{})
	_ = conn.SetWriteDeadline(time.Time{})

	pw1.Close()
	pr2.Close()
	<-done
}

// TestProxyConn_Close verifies that calling Close on a proxyConn closes both
// the stdout reader and the stdin writer.
func TestProxyConn_Close(t *testing.T) {
	pr1, _ := io.Pipe()
	_, pw2 := io.Pipe()

	conn := &proxyConn{stdout: pr1, stdin: pw2}

	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// pr1 was closed — reading should fail
	buf := make([]byte, 4)
	_, err := conn.Read(buf)
	if err == nil {
		t.Error("expected error reading from closed conn, got nil")
	}

	// pw2 was closed — writing should fail with io.ErrClosedPipe
	_, err = pw2.Write([]byte("x"))
	if err != io.ErrClosedPipe {
		t.Errorf("expected io.ErrClosedPipe writing to closed stdin, got %v", err)
	}
}

// TestLoadSigner_InvalidPEM verifies that loadSigner returns an error when the
// key file contains data that is not a valid PEM-encoded key.
func TestLoadSigner_InvalidPEM(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "badkey")
	if err := os.WriteFile(keyPath, []byte("not a valid pem key"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadSigner(keyPath)
	if err == nil {
		t.Error("expected error for invalid PEM key, got nil")
	}
}

// TestCopyRemoteToRemote_FailsWithBadKey verifies that CopyRemoteToRemote
// returns an error when the key file does not exist, before touching the network.
func TestCopyRemoteToRemote_FailsWithBadKey(t *testing.T) {
	err := CopyRemoteToRemote(context.Background(),
		"i-0src123", "/srv/app",
		"i-0dst456", "/srv/app",
		CopySpec{
			User:    "ec2-user",
			KeyPath: "/nonexistent/key",
		},
	)
	if err == nil {
		t.Error("expected error for missing key, got nil")
	}
}

// TestShellQuote verifies safe quoting of paths for remote shell commands.
func TestShellQuote(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"simple", "'simple'"},
		{"/path/to/file", "'/path/to/file'"},
		{"has space", "'has space'"},
		{"it's here", "'it'\\''s here'"},
		{"quote'inside'path", "'quote'\\''inside'\\''path'"},
	}
	for _, c := range cases {
		if got := shellQuote(c.in); got != c.want {
			t.Errorf("shellQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestUpload_DirectoryWithoutRecursive verifies that uploading a directory
// without -r returns an error rather than silently succeeding or panicking.
func TestUpload_DirectoryWithoutRecursive(t *testing.T) {
	dir := t.TempDir()
	// upload with a nil sftp client is fine — the directory check happens first.
	err := upload(nil, dir, "/remote/path", false)
	if err == nil {
		t.Fatal("expected error when uploading directory without recursive flag")
	}
}

// TestDownloadDir_CreatesLocalTree verifies that downloadDir reconstructs the
// remote directory structure locally. It uses an in-process sftp server so no
// real SSH connection is required.
func TestUploadDir_CreatesRemoteTree(t *testing.T) {
	// Build a local source tree.
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"root.txt":       "root content",
		"sub/nested.txt": "nested content",
	}
	for rel, data := range files {
		if err := os.WriteFile(filepath.Join(src, rel), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Build an in-process sftp server (uses real OS filesystem).
	remote := t.TempDir()
	sftpClient := newInProcSFTPClient(t)

	if err := uploadDir(sftpClient, src, remote+"/dst"); err != nil {
		t.Fatalf("uploadDir: %v", err)
	}

	// Verify each file landed at the expected remote path.
	for rel, want := range files {
		p := filepath.Join(remote, "dst", rel)
		got, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("remote file %s not found: %v", p, err)
			continue
		}
		if string(got) != want {
			t.Errorf("remote file %s = %q, want %q", p, got, want)
		}
	}
}

// TestDownloadDir_CreatesLocalTree mirrors TestUploadDir using downloadDir.
func TestDownloadDir_CreatesLocalTree(t *testing.T) {
	// Build a remote source tree inside a temp dir backed by in-proc sftp.
	remote := t.TempDir()
	if err := os.MkdirAll(filepath.Join(remote, "src", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"src/a.txt":     "alpha",
		"src/sub/b.txt": "beta",
	}
	for rel, data := range files {
		if err := os.WriteFile(filepath.Join(remote, rel), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	sftpClient := newInProcSFTPClient(t)

	local := t.TempDir()
	if err := downloadDir(sftpClient, remote+"/src", local); err != nil {
		t.Fatalf("downloadDir: %v", err)
	}

	for rel, want := range files {
		// rel is "src/a.txt" — downloadDir strips the remote base ("src") prefix.
		localPath := filepath.Join(local, filepath.FromSlash(rel[len("src/"):]))
		got, err := os.ReadFile(localPath)
		if err != nil {
			t.Errorf("local file %s not found: %v", localPath, err)
			continue
		}
		if string(got) != want {
			t.Errorf("local file %s = %q, want %q", localPath, got, want)
		}
	}
}
