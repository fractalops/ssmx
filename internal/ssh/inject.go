package ssh

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsclient "github.com/fractalops/ssmx/internal/aws"
)

// injectScript is the Python 3 script that safely appends an ephemeral SSH
// public key to ~/.ssh/authorized_keys on the remote instance, then removes
// it after 15 seconds. It is sent base64-encoded to avoid JSON escaping issues.
//
// Placeholders __USER__ and __PUBKEY__ are replaced before encoding.
const injectScript = `import pwd, os, fcntl, sys, time
user    = "__USER__"
pubkey  = "__PUBKEY__"
keydata = pubkey.split()[1]
try:
    pw = pwd.getpwnam(user)
except KeyError:
    sys.exit(f"user {user!r} not found")
uid, gid, home = pw.pw_uid, pw.pw_gid, pw.pw_dir
ssh_dir = os.path.join(home, ".ssh")
ak      = os.path.join(ssh_dir, "authorized_keys")
os.makedirs(ssh_dir, mode=0o700, exist_ok=True)
os.chown(ssh_dir, uid, gid)
with open(ak, "a+") as fh:
    fcntl.flock(fh, fcntl.LOCK_EX)
    os.chmod(ak, 0o600)
    os.chown(ak, uid, gid)
    fh.seek(0)
    if any(keydata in line for line in fh):
        sys.exit(0)
    fh.write(pubkey + "\n")
if os.fork() == 0:
    os.setsid()
    time.sleep(15)
    try:
        with open(ak, "r+") as fh:
            fcntl.flock(fh, fcntl.LOCK_EX)
            kept = [l for l in fh if keydata not in l]
            fh.seek(0)
            fh.truncate()
            fh.writelines(kept)
    except Exception:
        pass
    os._exit(0)
`

// InjectKey injects pubKey into the remote instance's ~/.ssh/authorized_keys
// for user via send-command. The key self-destructs after 15 seconds.
// Blocks until the command succeeds or times out (10 seconds).
func InjectKey(ctx context.Context, cfg aws.Config, instanceID, user, pubKey string) error {
	script := strings.ReplaceAll(injectScript, "__USER__", user)
	script = strings.ReplaceAll(script, "__PUBKEY__", pubKey)

	encoded := base64.StdEncoding.EncodeToString([]byte(script))
	// The instance runs: python3 -c "$(printf '<b64>' | base64 -d)"
	command := fmt.Sprintf(`python3 -c "$(printf '%s' | base64 -d)"`, encoded)

	cmdID, err := awsclient.SendCommand(ctx, cfg, instanceID, command)
	if err != nil {
		return fmt.Errorf("injecting SSH key: %w", err)
	}
	return awsclient.PollCommandInvocation(ctx, cfg, instanceID, cmdID, 10*time.Second)
}

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
