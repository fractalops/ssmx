package transfer

import (
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
