package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	awsclient "github.com/fractalops/ssmx/internal/aws"
	"github.com/fractalops/ssmx/internal/config"
	"github.com/fractalops/ssmx/internal/health"
	"github.com/fractalops/ssmx/internal/tui"
)

// runHealth resolves target, runs all health checks, and streams results to stdout.
func runHealth(cmd *cobra.Command, target string) error {
	ctx := context.Background()

	awsCfg, err := awsclient.NewConfig(ctx, flagProfile, flagRegion)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	inst, err := resolveTarget(ctx, cmd, awsCfg, cfg, target)
	if err != nil {
		return err
	}
	if inst == nil {
		return nil // user cancelled picker
	}

	isTTY := term.IsTerminal(int(os.Stdout.Fd())) //nolint:gosec // uintptr→int conversion is safe here: value is a small syscall return

	header := fmt.Sprintf("ssmx health: %s (%s)  %s", nameOrID(inst), inst.InstanceID, awsCfg.Region)
	if isTTY {
		fmt.Printf("\n%s\n\n", tui.StyleBold.Render(header))
	} else {
		fmt.Printf("\n%s\n\n", header)
	}

	ch := health.Run(ctx, awsCfg, inst)
	printResults(ch, isTTY)

	return nil
}

// printResults ranges over ch, printing a section header whenever the section
// changes, then a glyph + label + optional detail for each Result.
func printResults(ch <-chan health.Result, isTTY bool) {
	var currentSection string
	errors, warnings := 0, 0

	for r := range ch {
		if r.Section != currentSection {
			if currentSection != "" {
				fmt.Println() // blank line between sections
			}
			currentSection = r.Section
			if isTTY {
				fmt.Println(tui.StyleHeader.Render(currentSection))
			} else {
				fmt.Println(currentSection)
			}
		}

		glyph := r.Severity.Glyph()
		if isTTY {
			glyph = r.Severity.Style().Render(glyph)
		}

		line := fmt.Sprintf("  %s  %s", glyph, r.Label)
		if r.Detail != "" {
			if isTTY {
				line += "  " + tui.StyleDim.Render(r.Detail)
			} else {
				line += "  (" + r.Detail + ")"
			}
		}
		fmt.Println(line)

		switch r.Severity {
		case health.SeverityError:
			errors++
		case health.SeverityWarn:
			warnings++
		}
	}

	fmt.Println()

	var summary string
	var summaryStyle lipgloss.Style
	switch {
	case errors > 0:
		summary = fmt.Sprintf("Result: %d error(s) — session will not connect (see ✗ items)", errors)
		summaryStyle = tui.StyleError
	case warnings > 0:
		summary = fmt.Sprintf("Result: %d warning(s) — session should connect (see ! items)", warnings)
		summaryStyle = tui.StyleWarning
	default:
		summary = "Result: all checks passed"
		summaryStyle = tui.StyleSuccess
	}

	if isTTY {
		fmt.Println(summaryStyle.Render(summary))
	} else {
		fmt.Println(summary)
	}
	fmt.Println()
}
