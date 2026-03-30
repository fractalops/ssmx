package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"
	awsclient "github.com/fractalops/ssmx/internal/aws"
	"github.com/fractalops/ssmx/internal/config"
	"github.com/fractalops/ssmx/internal/preflight"
	"github.com/fractalops/ssmx/internal/resolver"
	"github.com/fractalops/ssmx/internal/session"
	"github.com/fractalops/ssmx/internal/tui"
)

var connectCmd = &cobra.Command{
	Use:   "connect [target]",
	Short: "Start an interactive SSM session on an instance",
	Long: `Start an interactive SSM session.

With no target, opens an interactive instance picker.
Target can be an alias, Name tag, Name tag prefix, or instance ID.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runConnect,
}

func init() {
	rootCmd.AddCommand(connectCmd)
}

func runConnect(cmd *cobra.Command, args []string) error {
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

	var target *awsclient.Instance

	if len(args) == 0 {
		target, err = pickInstance(ctx, awsCfg)
		if err != nil {
			return err
		}
		if target == nil {
			return nil // user cancelled
		}
	} else {
		instances, err := awsclient.ListInstances(ctx, awsCfg, nil)
		if err != nil {
			return fmt.Errorf("listing instances: %w", err)
		}
		ssmInfo, _ := awsclient.ListManagedInstances(ctx, awsCfg)
		awsclient.MergeSSMInfo(instances, ssmInfo)

		target, err = resolver.Resolve(args[0], instances, cfg.Aliases)
		if err != nil {
			var ambig *resolver.ErrAmbiguous
			if errors.As(err, &ambig) {
				fmt.Fprintf(cmd.ErrOrStderr(), "%q is ambiguous (%d matches) — select one:\n", args[0], len(ambig.Matches))
				target, err = tui.RunPicker(ambig.Matches)
				if err != nil {
					return err
				}
				if target == nil {
					return nil
				}
			} else {
				return err
			}
		}
	}

	if target.SSMStatus == "offline" {
		fmt.Printf("%s  %s (%s) is not reachable via SSM (status: %s)\n",
			tui.StyleWarning.Render("!"),
			target.Name, target.InstanceID, target.SSMStatus,
		)
		fmt.Printf("  Run %s to investigate\n", tui.StyleBold.Render("ssmx diagnose "+target.InstanceID))
		return nil
	}

	fmt.Printf("%s  Connecting to %s (%s)...\n",
		tui.StyleSuccess.Render("→"),
		tui.StyleBold.Render(nameOrID(target)),
		target.InstanceID,
	)

	return session.Connect(ctx, awsCfg, target.InstanceID, region, profile)
}

func pickInstance(ctx context.Context, cfg aws.Config) (*awsclient.Instance, error) {
	instances, err := awsclient.ListInstances(ctx, cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("listing instances: %w", err)
	}
	ssmInfo, _ := awsclient.ListManagedInstances(ctx, cfg)
	awsclient.MergeSSMInfo(instances, ssmInfo)
	return tui.RunPicker(instances)
}

func nameOrID(inst *awsclient.Instance) string {
	if inst.Name != "" {
		return inst.Name
	}
	return inst.InstanceID
}
