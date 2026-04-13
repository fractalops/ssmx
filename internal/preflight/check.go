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
func Run(ctx context.Context, profile, region string) error {
	// 1. AWS credentials.
	if _, err := awsclient.NewConfig(ctx, profile, region); err != nil {
		return fmt.Errorf("%w\n\nRun `aws configure` to set up credentials", err)
	}
	fmt.Fprintln(os.Stderr, tui.StyleSuccess.Render("ok")+"  AWS credentials configured")

	// 2. Region.
	if region == "" {
		cfg, _ := config.Load()
		if cfg != nil {
			region = cfg.DefaultRegion
		}
	}
	if region != "" {
		fmt.Fprintf(os.Stderr, "%s  Region: %s\n", tui.StyleSuccess.Render("ok"), region)
	} else {
		fmt.Fprintln(os.Stderr, tui.StyleWarning.Render("?")+"  No default region set (use -r or set default_region in ~/.ssmx/config.yaml)")
	}

	// 3. Session Manager plugin.
	if PluginInstalled() {
		fmt.Fprintln(os.Stderr, tui.StyleSuccess.Render("ok")+"  Session Manager plugin installed")
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
