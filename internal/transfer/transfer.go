package transfer

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

// Direction indicates which end of the copy is remote.
type Direction int

const (
	LocalToRemote Direction = iota // zero value — default when Direction is not set
	RemoteToLocal
)

// CopySpec describes a file copy operation.
type CopySpec struct {
	LocalPath  string
	RemotePath string
	Direction  Direction
	Recursive  bool
	User       string // remote OS user (e.g. ec2-user, ubuntu)
	KeyPath    string // private key path for SSH auth
	Profile    string // AWS profile, forwarded to --proxy if non-empty
	Region     string // AWS region, forwarded to --proxy
}

// proxyConn wraps a subprocess's stdin/stdout as a net.Conn.
// Reads come from the subprocess stdout; writes go to subprocess stdin.
type proxyConn struct {
	io.Reader
	io.WriteCloser
	cmd *exec.Cmd
}

func (p *proxyConn) Close() error {
	_ = p.WriteCloser.Close()
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	return nil
}

func (p *proxyConn) LocalAddr() net.Addr                { return dummyAddr{} }
func (p *proxyConn) RemoteAddr() net.Addr               { return dummyAddr{} }
func (p *proxyConn) SetDeadline(_ time.Time) error      { return nil }
func (p *proxyConn) SetReadDeadline(_ time.Time) error  { return nil }
func (p *proxyConn) SetWriteDeadline(_ time.Time) error { return nil }

type dummyAddr struct{}

func (dummyAddr) Network() string { return "ssm" }
func (dummyAddr) String() string  { return "ssm" }

// loadSigner reads an OpenSSH private key from keyPath and returns a gossh.Signer.
func loadSigner(keyPath string) (gossh.Signer, error) {
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("reading private key %s: %w", keyPath, err)
	}
	signer, err := gossh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}
	return signer, nil
}

// dialProxy spawns the current binary with --proxy <instanceID> <user> and returns
// a net.Conn backed by the subprocess's stdin/stdout. The proxy handles EC2
// Instance Connect key push and the SSM WebSocket session before forwarding
// SSH traffic.
func dialProxy(ctx context.Context, instanceID, user, profile, region string) (net.Conn, error) {
	ssmxPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolving binary path: %w", err)
	}

	var args []string
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	if region != "" {
		args = append(args, "--region", region)
	}
	args = append(args, "--proxy", instanceID, user)

	cmd := exec.CommandContext(ctx, ssmxPath, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting proxy: %w", err)
	}
	return &proxyConn{Reader: stdout, WriteCloser: stdin, cmd: cmd}, nil
}

// dialSSH dials SSH over conn using the given signer and returns a ready SSH client.
func dialSSH(conn net.Conn, instanceID, user string, signer gossh.Signer) (*gossh.Client, error) {
	sshConn, chans, reqs, err := gossh.NewClientConn(conn, instanceID, &gossh.ClientConfig{
		User:            user,
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(signer)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec // SSM sessions are already authenticated via IAM
	})
	if err != nil {
		return nil, fmt.Errorf("SSH handshake with %s: %w", instanceID, err)
	}
	return gossh.NewClient(sshConn, chans, reqs), nil
}

// Copy transfers files to or from instanceID using SFTP over an SSM SSH session.
func Copy(ctx context.Context, instanceID string, spec CopySpec) error {
	signer, err := loadSigner(spec.KeyPath)
	if err != nil {
		return err
	}

	conn, err := dialProxy(ctx, instanceID, spec.User, spec.Profile, spec.Region)
	if err != nil {
		return err
	}
	defer conn.Close()

	sshClient, err := dialSSH(conn, instanceID, spec.User, signer)
	if err != nil {
		return err
	}
	defer sshClient.Close()

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return fmt.Errorf("SFTP client: %w", err)
	}
	defer sftpClient.Close()

	switch spec.Direction {
	case LocalToRemote:
		return upload(sftpClient, spec.LocalPath, spec.RemotePath, spec.Recursive)
	case RemoteToLocal:
		return download(sftpClient, spec.RemotePath, spec.LocalPath, spec.Recursive)
	default:
		panic(fmt.Sprintf("transfer: unknown Direction %d", spec.Direction))
	}
}

// upload copies localPath (file or directory) to remotePath on the SFTP server.
func upload(client *sftp.Client, localPath, remotePath string, recursive bool) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if !recursive {
			return fmt.Errorf("%s is a directory — use -r to copy recursively", localPath)
		}
		return uploadDir(client, localPath, remotePath)
	}
	return uploadFile(client, localPath, remotePath)
}

func uploadFile(client *sftp.Client, localPath, remotePath string) error {
	if strings.HasSuffix(remotePath, "/") {
		remotePath = remotePath + filepath.Base(localPath)
	}
	if err := client.MkdirAll(path.Dir(remotePath)); err != nil {
		return fmt.Errorf("creating remote dir: %w", err)
	}
	src, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := client.Create(remotePath)
	if err != nil {
		return fmt.Errorf("creating remote file %s: %w", remotePath, err)
	}
	defer func() { _ = dst.Close() }()
	_, err = io.Copy(dst, src)
	return err
}

func uploadDir(client *sftp.Client, localDir, remoteDir string) error {
	return filepath.Walk(localDir, func(localPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(localDir, localPath)
		remotePath := remoteDir + "/" + filepath.ToSlash(rel)
		if info.IsDir() {
			return client.MkdirAll(remotePath)
		}
		return uploadFile(client, localPath, remotePath)
	})
}

// download copies remotePath (file or directory) from the SFTP server to localPath.
func download(client *sftp.Client, remotePath, localPath string, recursive bool) error {
	info, err := client.Stat(remotePath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if !recursive {
			return fmt.Errorf("%s is a directory — use -r to copy recursively", remotePath)
		}
		return downloadDir(client, remotePath, localPath)
	}
	return downloadFile(client, remotePath, localPath)
}

func downloadFile(client *sftp.Client, remotePath, localPath string) error {
	if strings.HasSuffix(localPath, "/") || isLocalDir(localPath) {
		localPath = filepath.Join(localPath, path.Base(remotePath))
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	src, err := client.Open(remotePath)
	if err != nil {
		return fmt.Errorf("opening remote file %s: %w", remotePath, err)
	}
	defer func() { _ = src.Close() }()
	dst, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()
	_, err = io.Copy(dst, src)
	return err
}

func downloadDir(client *sftp.Client, remoteDir, localDir string) error {
	walker := client.Walk(remoteDir)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return err
		}
		rel, _ := filepath.Rel(remoteDir, walker.Path())
		target := filepath.Join(localDir, filepath.FromSlash(rel))
		if walker.Stat().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		} else {
			if err := downloadFile(client, walker.Path(), target); err != nil {
				return err
			}
		}
	}
	return nil
}

func isLocalDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
