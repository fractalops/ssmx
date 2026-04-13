package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	awsclient "github.com/fractalops/ssmx/internal/aws"
	"github.com/fractalops/ssmx/internal/config"
	"github.com/fractalops/ssmx/internal/preflight"
	"github.com/fractalops/ssmx/internal/resolver"
	sshpkg "github.com/fractalops/ssmx/internal/ssh"
	"github.com/fractalops/ssmx/internal/transfer"
	"github.com/fractalops/ssmx/internal/tui"
	"github.com/spf13/cobra"
)

var (
	flagProfile   string
	flagRegion    string
	flagProxy     bool
	flagRecursive bool
	flagUser      string
)

// errOffline is returned when the target instance is not reachable via SSM.
// The message is already printed before returning; Execute silences re-printing.
type errOffline struct{ name, id string }

func (e *errOffline) Error() string {
	return fmt.Sprintf("%s (%s) is not reachable via SSM", e.name, e.id)
}

var rootCmd = &cobra.Command{
	Use:   "ssmcp SOURCE DEST",
	Short: "Copy files to or from an EC2 instance over SSM",
	Long: `Copy files to or from an EC2 instance via SFTP over an SSM SSH session.

At least one of SOURCE or DEST must be a remote path (host:path).
When both are remote, files are streamed instance-to-instance via tar — no temp files, no open ports.

  ssmcp ./file.txt web-prod:/tmp/
  ssmcp web-prod:/var/log/app.log ./
  ssmcp -r ./dist/ web-prod:/srv/app/
  ssmcp web-prod:/data/ web-staging:/data/

The host is resolved via bookmark alias, Name tag, or instance ID.
EC2 Instance Connect must be available on the target instance.`,
	Args:          cobra.ArbitraryArgs,
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagProxy {
			if len(args) < 2 {
				return fmt.Errorf("--proxy requires <instanceID> <user>")
			}
			return runProxy(args[0], args[1])
		}
		if len(args) != 2 {
			return cmd.Help()
		}
		return runCopy(cmd, args[0], args[1])
	},
}

func runCopy(cmd *cobra.Command, src, dst string) error {
	srcHost, srcPath, srcRemote := parseEndpoint(src)
	dstHost, dstPath, dstRemote := parseEndpoint(dst)

	if !srcRemote && !dstRemote {
		return fmt.Errorf("both SOURCE and DEST are local — at least one must be a remote path (host:path)")
	}

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

	instances, err := awsclient.ListInstances(ctx, awsCfg, nil)
	if err != nil {
		return fmt.Errorf("listing instances: %w", err)
	}
	ssmInfo, _ := awsclient.ListManagedInstances(ctx, awsCfg)
	awsclient.MergeSSMInfo(instances, ssmInfo)

	// Resolve source instance (always present when srcRemote; or dst when only dst is remote).
	target := srcHost
	if !srcRemote {
		target = dstHost
	}

	inst, err := resolver.Resolve(target, instances, cfg.Aliases)
	if err != nil {
		var ambig *resolver.ErrAmbiguous
		if errors.As(err, &ambig) {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%q is ambiguous (%d matches) — select one:\n", target, len(ambig.Matches))
			inst, err = tui.RunPicker(ambig.Matches)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	if inst == nil {
		return nil // user cancelled picker
	}

	if inst.SSMStatus == "offline" {
		fmt.Fprintf(os.Stderr, "%s  %s (%s) is not reachable via SSM\n",
			tui.StyleWarning.Render("!"), inst.Name, inst.InstanceID,
		)
		fmt.Fprintf(os.Stderr, "  Run %s to investigate\n",
			tui.StyleBold.Render("ssmx "+inst.InstanceID+" --health"),
		)
		return &errOffline{inst.Name, inst.InstanceID}
	}

	user := flagUser
	if user == "" {
		user = resolveSSHUser(ctx, awsCfg, inst.InstanceID, profile, region)
	}

	_, keyPath, err := sshpkg.LoadOrGenerateKey(cfg.SSHKeyPath)
	if err != nil {
		return fmt.Errorf("loading SSH key: %w", err)
	}

	// Both-remote: instance-to-instance tar pipe.
	if srcRemote && dstRemote {
		dstInst, err := resolver.Resolve(dstHost, instances, cfg.Aliases)
		if err != nil {
			var ambig *resolver.ErrAmbiguous
			if errors.As(err, &ambig) {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%q is ambiguous (%d matches) — select one:\n", dstHost, len(ambig.Matches))
				dstInst, err = tui.RunPicker(ambig.Matches)
				if err != nil {
					return err
				}
			} else {
				return err
			}
		}
		if dstInst == nil {
			return nil
		}
		if dstInst.SSMStatus == "offline" {
			fmt.Fprintf(os.Stderr, "%s  %s (%s) is not reachable via SSM\n",
				tui.StyleWarning.Render("!"), dstInst.Name, dstInst.InstanceID,
			)
			fmt.Fprintf(os.Stderr, "  Run %s to investigate\n",
				tui.StyleBold.Render("ssmx "+dstInst.InstanceID+" --health"),
			)
			return &errOffline{dstInst.Name, dstInst.InstanceID}
		}
		// tar handles directories recursively by default; Recursive flag is not needed.
		return transfer.CopyRemoteToRemote(ctx, inst.InstanceID, srcPath, dstInst.InstanceID, dstPath,
			transfer.CopySpec{
				User:    user,
				KeyPath: keyPath,
				Profile: flagProfile,
				Region:  region,
			},
		)
	}

	// One remote: standard local↔remote SFTP copy.
	var direction transfer.Direction
	var localPath, remotePath string
	if srcRemote {
		direction = transfer.RemoteToLocal
		localPath = dstPath
		remotePath = srcPath
	} else {
		direction = transfer.LocalToRemote
		localPath = srcPath
		remotePath = dstPath
	}

	return transfer.Copy(ctx, inst.InstanceID, transfer.CopySpec{
		LocalPath:  localPath,
		RemotePath: remotePath,
		Direction:  direction,
		Recursive:  flagRecursive,
		User:       user,
		KeyPath:    keyPath,
		Profile:    flagProfile,
		Region:     region,
	})
}

// parseEndpoint splits a cp argument into (host, path, isRemote).
// Remote paths use the scp convention "host:path".
// Leading /, ./, ../ unambiguously mark a local path.
// A bare name with no colon, or a string starting with colon, is also local.
func parseEndpoint(s string) (host, path string, remote bool) {
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		return "", s, false
	}
	idx := strings.Index(s, ":")
	if idx <= 0 {
		return "", s, false
	}
	return s[:idx], s[idx+1:], true
}

// Execute is the entry point called from ssmcp/main.go.
func Execute(version, buildTime string) {
	rootCmd.Version = version
	if buildTime != "" {
		rootCmd.SetVersionTemplate("ssmcp " + version + " (built " + buildTime + ")\n")
	}

	if err := rootCmd.Execute(); err != nil {
		var offline *errOffline
		if errors.As(err, &offline) {
			// Message already printed; just exit non-zero.
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&flagProfile, "profile", "p", "", "AWS profile to use")
	rootCmd.PersistentFlags().StringVar(&flagRegion, "region", "", "AWS region to use")
	rootCmd.Flags().BoolVar(&flagProxy, "proxy", false, "")
	rootCmd.Flags().BoolVarP(&flagRecursive, "recursive", "r", false, "copy directories recursively")
	rootCmd.Flags().StringVarP(&flagUser, "user", "u", "", "remote SSH user (default: inferred from instance platform)")
	_ = rootCmd.Flags().MarkHidden("proxy")
}
