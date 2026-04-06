package cmd

import (
	"context"
	"fmt"
	"os"

	awsclient "github.com/fractalops/ssmx/internal/aws"
	"github.com/fractalops/ssmx/internal/config"
	"github.com/fractalops/ssmx/internal/preflight"
	"github.com/fractalops/ssmx/internal/session"
	sshpkg "github.com/fractalops/ssmx/internal/ssh"
)

// runProxy is the backend for the --proxy ProxyCommand.
// It injects an ephemeral SSH key then opens AWS-StartSSHSession,
// piping stdin/stdout as the SSH transport.
//
// instanceID and user come from the ProxyCommand's %h and %r substitutions:
//   ProxyCommand ssmx --proxy %h %r
func runProxy(instanceID, user string) error {
	ctx := context.Background()

	if err := preflight.Run(ctx, flagProfile, flagRegion); err != nil {
		return err
	}

	awsCfg, err := awsclient.NewConfig(ctx, flagProfile, flagRegion)
	if err != nil {
		return err
	}
	region := awsCfg.Region
	profile := flagProfile
	if profile == "" {
		profile = "default"
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Resolve SSH user: explicit from ProxyCommand %r, or guess from PlatformName.
	if user == "" {
		// Look up PlatformName from instance list.
		instances, err := awsclient.ListInstances(ctx, awsCfg, nil)
		if err == nil {
			ssmInfo, _ := awsclient.ListManagedInstances(ctx, awsCfg)
			awsclient.MergeSSMInfo(instances, ssmInfo)
			for _, inst := range instances {
				if inst.InstanceID == instanceID {
					user = sshpkg.DefaultSSHUser(inst.PlatformName)
					break
				}
			}
		}
		if user == "" {
			user = "ec2-user" // safe fallback
		}
	}

	// Load or generate the SSH key to inject.
	pubKey, _, err := sshpkg.LoadOrGenerateKey(cfg.SSHKeyPath)
	if err != nil {
		return fmt.Errorf("loading SSH key: %w", err)
	}

	// Inject key — runs Python script on instance via send-command.
	if err := sshpkg.InjectKey(ctx, awsCfg, instanceID, user, pubKey); err != nil {
		fmt.Fprintf(os.Stderr, "warning: key injection failed: %v\n", err)
		// Don't abort — SSH will fail with an auth error which is clearer.
	}

	return session.SSHProxy(ctx, awsCfg, instanceID, region, profile)
}
