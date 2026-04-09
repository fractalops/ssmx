package transfer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	KeyPath    string // private key path; if empty, -i is omitted
	Profile    string // AWS profile, forwarded to --proxy if non-empty
	Region     string // AWS region, forwarded to --proxy
}

// buildScpArgs constructs the scp argument list for the given spec.
// ssmxPath is the absolute path to the running binary, used as the ProxyCommand.
// Extracted for testability — Copy calls this then execs scp.
func buildScpArgs(ssmxPath, instanceID string, spec CopySpec) []string {
	proxy := fmt.Sprintf("%q", ssmxPath)
	if spec.Profile != "" {
		proxy += " --profile " + spec.Profile
	}
	if spec.Region != "" {
		proxy += " --region " + spec.Region
	}
	proxy += " --proxy %h %r"

	remote := fmt.Sprintf("%s@%s:%s", spec.User, instanceID, spec.RemotePath)

	var src, dst string
	switch spec.Direction {
	case LocalToRemote:
		src, dst = spec.LocalPath, remote
	case RemoteToLocal:
		src, dst = remote, spec.LocalPath
	default:
		panic(fmt.Sprintf("transfer: unknown Direction %d", spec.Direction))
	}

	args := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ProxyCommand=" + proxy,
	}
	if spec.KeyPath != "" {
		args = append(args, "-i", spec.KeyPath)
	}
	if spec.Recursive {
		args = append(args, "-r")
	}
	return append(args, src, dst)
}

// Copy transfers files to or from instanceID using scp over an SSM SSH proxy.
// It shells out to scp with a ProxyCommand that invokes the current binary with
// --proxy, which handles EC2 Instance Connect key push and the SSM SSH session.
func Copy(ctx context.Context, instanceID string, spec CopySpec) error {
	ssmxPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving binary path: %w", err)
	}

	scpPath, err := exec.LookPath("scp")
	if err != nil {
		return fmt.Errorf("scp not found — install openssh-client: %w", err)
	}

	args := buildScpArgs(ssmxPath, instanceID, spec)
	cmd := exec.CommandContext(ctx, scpPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
