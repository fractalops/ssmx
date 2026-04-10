package cmd

import (
	"context"
	"fmt"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	awsclient "github.com/fractalops/ssmx/internal/aws"
	"github.com/fractalops/ssmx/internal/config"
	"github.com/fractalops/ssmx/internal/preflight"
	"github.com/fractalops/ssmx/internal/session"
	"github.com/fractalops/ssmx/internal/state"
	sshpkg "github.com/fractalops/ssmx/internal/ssh"
)

// runProxy is the backend for the hidden --proxy flag, invoked by scp as ProxyCommand.
// It sends an ephemeral SSH public key via EC2 Instance Connect,
// then opens AWS-StartSSHSession, piping stdin/stdout as the SSH transport.
//
// instanceID and user come from the ProxyCommand's %h and %r substitutions:
//
//	ProxyCommand ssmcp --proxy %h %r
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
		user = resolveSSHUser(ctx, awsCfg, instanceID, profile, region)
	}

	// Load or generate the SSH key.
	pubKey, _, err := sshpkg.LoadOrGenerateKey(cfg.SSHKeyPath)
	if err != nil {
		return fmt.Errorf("loading SSH key: %w", err)
	}

	// Get the instance's availability zone — required by SendSSHPublicKey.
	az, err := resolveAZ(ctx, awsCfg, instanceID, profile, region)
	if err != nil {
		return fmt.Errorf("resolving availability zone: %w", err)
	}

	// Push the public key via EC2 Instance Connect (60-second TTL).
	if err := awsclient.SendSSHPublicKey(ctx, awsCfg, instanceID, az, user, pubKey); err != nil {
		return fmt.Errorf("sending SSH public key: %w", err)
	}

	return session.SSHProxy(ctx, awsCfg, instanceID, region, profile)
}

// resolveSSHUser looks up the PlatformName for instanceID (cache first, then
// live API) and returns the default SSH user for that platform.
func resolveSSHUser(ctx context.Context, awsCfg awssdk.Config, instanceID, profile, region string) string {
	if db, err := state.Open(); err == nil {
		cached, _ := state.GetCachedInstances(db, profile, region)
		_ = db.Close()
		for _, c := range cached {
			if c.InstanceID == instanceID {
				return sshpkg.DefaultSSHUser(c.PlatformName)
			}
		}
	}
	ssmClient := ssm.NewFromConfig(awsCfg)
	out, err := ssmClient.DescribeInstanceInformation(ctx, &ssm.DescribeInstanceInformationInput{
		Filters: []ssmtypes.InstanceInformationStringFilter{
			{Key: awssdk.String("InstanceIds"), Values: []string{instanceID}},
		},
	})
	if err == nil && len(out.InstanceInformationList) > 0 {
		return sshpkg.DefaultSSHUser(awssdk.ToString(out.InstanceInformationList[0].PlatformName))
	}
	return "ec2-user"
}

// resolveAZ returns the availability zone for instanceID. Checks the SQLite
// cache first; falls back to DescribeInstances if not found or AZ is empty.
func resolveAZ(ctx context.Context, awsCfg awssdk.Config, instanceID, profile, region string) (string, error) {
	if db, err := state.Open(); err == nil {
		cached, _ := state.GetCachedInstances(db, profile, region)
		_ = db.Close()
		for _, c := range cached {
			if c.InstanceID == instanceID && c.AvailabilityZone != "" {
				return c.AvailabilityZone, nil
			}
		}
	}
	client := ec2.NewFromConfig(awsCfg)
	out, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		return "", fmt.Errorf("describe-instances: %w", err)
	}
	for _, r := range out.Reservations {
		for _, i := range r.Instances {
			if i.Placement != nil && i.Placement.AvailabilityZone != nil {
				return awssdk.ToString(i.Placement.AvailabilityZone), nil
			}
		}
	}
	return "", fmt.Errorf("availability zone not found for instance %s", instanceID)
}
