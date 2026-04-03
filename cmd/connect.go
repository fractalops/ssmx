package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	awsclient "github.com/fractalops/ssmx/internal/aws"
	"github.com/fractalops/ssmx/internal/config"
	"github.com/fractalops/ssmx/internal/preflight"
	"github.com/fractalops/ssmx/internal/resolver"
	"github.com/fractalops/ssmx/internal/session"
	"github.com/fractalops/ssmx/internal/tui"
)

func runExec(cmd *cobra.Command, target string, remoteCmd []string) error {
	ctx := context.Background()
	if flagTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, flagTimeout)
		defer cancel()
	}

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

	inst, err := resolver.Resolve(target, instances, cfg.Aliases)
	if err != nil {
		var ambig *resolver.ErrAmbiguous
		if errors.As(err, &ambig) {
			fmt.Fprintf(cmd.ErrOrStderr(), "%q is ambiguous (%d matches) — select one:\n", target, len(ambig.Matches))
			inst, err = tui.RunPicker(ambig.Matches)
			if err != nil {
				return err
			}
			if inst == nil {
				return nil
			}
		} else {
			return err
		}
	}

	if inst.SSMStatus == "offline" {
		fmt.Printf("%s  %s (%s) is not reachable via SSM (status: %s)\n",
			tui.StyleWarning.Render("!"),
			inst.Name, inst.InstanceID, inst.SSMStatus,
		)
		fmt.Printf("  Run %s to investigate\n", tui.StyleBold.Render("ssmx diagnose "+inst.InstanceID))
		return nil
	}

	command := strings.Join(remoteCmd, " ")

	fmt.Printf("%s  Running on %s (%s)...\n",
		tui.StyleSuccess.Render("→"),
		tui.StyleBold.Render(nameOrID(inst)),
		inst.InstanceID,
	)

	return session.Exec(ctx, awsCfg, inst.InstanceID, region, profile, command)
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

	if err := session.Connect(ctx, awsCfg, target.InstanceID, region, profile); err != nil {
		return err
	}

	return postSessionBookmark(target, cfg)
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

// postSessionBookmark auto-bookmarks the instance after a session ends.
// If it's already bookmarked, does nothing. If it's new, saves it with the
// Name tag as default and offers a rename prompt.
func postSessionBookmark(inst *awsclient.Instance, cfg *config.Config) error {
	// Check if already bookmarked (any alias points to this instance ID).
	for _, id := range cfg.Aliases {
		if id == inst.InstanceID {
			return nil // already known
		}
	}

	// Auto-save with the Name tag (or instance ID if unnamed) as the key.
	defaultName := inst.Name
	if defaultName == "" {
		defaultName = inst.InstanceID
	}

	fmt.Println()
	fmt.Printf("%s  Bookmarked as %s\n", tui.StyleSuccess.Render("✓"), tui.StyleBold.Render(defaultName))

	var rename string
	if err := huh.NewInput().
		Title("Rename bookmark?").
		Placeholder("enter to keep \"" + defaultName + "\"").
		Value(&rename).
		Run(); err != nil || rename == "" {
		// Keep default name.
		return config.SetAlias(defaultName, inst.InstanceID)
	}

	return config.SetAlias(rename, inst.InstanceID)
}
