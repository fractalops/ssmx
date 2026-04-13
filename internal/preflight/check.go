// Package preflight verifies ssmx prerequisites before running commands.
package preflight

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	awsclient "github.com/fractalops/ssmx/internal/aws"
	"github.com/fractalops/ssmx/internal/config"
	"github.com/fractalops/ssmx/internal/tui"
)

// Run performs all first-run checks and interactively resolves any failures.
// Returns an error only if a check failure cannot be resolved.
// Success states are silent — only warnings and errors are printed.
func Run(ctx context.Context, profile, region string) error {
	// 1. AWS credentials.
	awsCfg, err := awsclient.NewConfig(ctx, profile, region)
	if err != nil {
		return fmt.Errorf("%w\n\nRun `aws configure` to set up credentials", err)
	}

	// 2. Region — resolved from flag, ~/.aws/config, AWS_DEFAULT_REGION, or ~/.ssmx/config.yaml.
	// Only prompt when no source provides one.
	if awsCfg.Region == "" {
		if ssmxCfg, _ := config.Load(); ssmxCfg != nil && ssmxCfg.DefaultRegion != "" {
			awsCfg.Region = ssmxCfg.DefaultRegion
		}
	}
	if awsCfg.Region == "" {
		var defaultRegion string
		if err := huh.NewInput().
			Title("No AWS region found — enter a default to save (or leave blank to skip)").
			Placeholder("us-east-1").
			Value(&defaultRegion).
			Run(); err == nil && defaultRegion != "" {
			if ssmxCfg, lerr := config.Load(); lerr == nil {
				ssmxCfg.DefaultRegion = defaultRegion
				_ = config.Save(ssmxCfg)
			}
		}
	}

	// 3. Session Manager plugin.
	if PluginInstalled() {
		return nil
	}

	fmt.Fprintln(os.Stderr, tui.StyleError.Render("err")+" Session Manager plugin not found")

	var install bool
	if err := huh.NewConfirm().
		Title("Install session-manager-plugin now?").
		Value(&install).
		Run(); err != nil || !install {
		return fmt.Errorf("session-manager-plugin is required — install it from https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html")
	}

	fmt.Fprint(os.Stderr, "  Installing... ")
	if err := InstallPlugin(ctx); err != nil {
		fmt.Fprintln(os.Stderr)
		return fmt.Errorf("install failed: %w", err)
	}
	fmt.Fprintln(os.Stderr, tui.StyleSuccess.Render("done"))
	return nil
}
