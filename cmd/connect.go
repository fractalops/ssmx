package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/charmbracelet/huh"
	awsclient "github.com/fractalops/ssmx/internal/aws"
	"github.com/fractalops/ssmx/internal/config"
	"github.com/fractalops/ssmx/internal/preflight"
	"github.com/fractalops/ssmx/internal/resolver"
	"github.com/fractalops/ssmx/internal/session"
	"github.com/fractalops/ssmx/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	defaultProfile   = "default"
	ssmStatusOffline = "offline"
)

// errOffline is returned when the target instance is not reachable via SSM.
type errOffline struct{ inst *awsclient.Instance }

func (e *errOffline) Error() string {
	return fmt.Sprintf("%s (%s) is not reachable via SSM", e.inst.Name, e.inst.InstanceID)
}

func runExec(cmd *cobra.Command, target string, remoteCmd []string) error {
	ctx := context.Background()
	if len(remoteCmd) == 0 {
		return fmt.Errorf("no command specified after --")
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
		profile = defaultProfile
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

	if inst.SSMStatus == ssmStatusOffline {
		fmt.Fprintf(os.Stderr, "%s  %s (%s) is not reachable via SSM\n",
			tui.StyleWarning.Render("!"), inst.Name, inst.InstanceID,
		)
		fmt.Fprintf(os.Stderr, "  Run %s to investigate\n", tui.StyleBold.Render("ssmx "+inst.InstanceID+" --health"))
		return &errOffline{inst}
	}

	command := strings.Join(remoteCmd, " ")

	fmt.Fprintf(os.Stderr, "%s  Running on %s (%s)...\n",
		tui.StyleSuccess.Render("→"),
		tui.StyleBold.Render(nameOrID(inst)),
		inst.InstanceID,
	)

	// Apply timeout only to the plugin execution, not preflight/listing.
	execCtx := ctx
	if flagTimeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, flagTimeout)
		defer cancel()
	}

	return session.Exec(execCtx, awsCfg, inst.InstanceID, region, profile, command)
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
		profile = defaultProfile
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
		target, err = resolveTarget(ctx, cmd, awsCfg, cfg, args[0])
		if err != nil {
			return err
		}
		if target == nil {
			return nil // user cancelled picker
		}
	}

	if target.SSMStatus == ssmStatusOffline {
		fmt.Fprintf(os.Stderr, "%s  %s (%s) is not reachable via SSM\n",
			tui.StyleWarning.Render("!"), target.Name, target.InstanceID,
		)
		fmt.Fprintf(os.Stderr, "  Run %s to investigate\n", tui.StyleBold.Render("ssmx "+target.InstanceID+" --health"))
		return &errOffline{target}
	}

	fmt.Fprintf(os.Stderr, "%s  Connecting to %s (%s)...\n",
		tui.StyleSuccess.Render("→"),
		tui.StyleBold.Render(nameOrID(target)),
		target.InstanceID,
	)

	// Save terminal state before handing off to session-manager-plugin, which
	// puts the terminal in raw mode. Restore it before showing the bookmark
	// prompt so huh doesn't get garbled input.
	termFd := int(os.Stdin.Fd()) //nolint:gosec // uintptr→int conversion is safe here: value is a small syscall return
	oldState, err := term.GetState(termFd)
	if err != nil {
		oldState = nil
	}

	sessionErr := session.Connect(ctx, awsCfg, target.InstanceID, region, profile)

	if oldState != nil {
		_ = term.Restore(termFd, oldState)
	}

	if sessionErr != nil {
		return sessionErr
	}

	return postSessionBookmark(target, cfg)
}

// resolveTarget lists instances, resolves target string, and opens a picker on
// ambiguity. Returns nil, nil if the user cancels the picker.
func resolveTarget(ctx context.Context, cmd *cobra.Command, awsCfg aws.Config, cfg *config.Config, target string) (*awsclient.Instance, error) {
	instances, err := awsclient.ListInstances(ctx, awsCfg, nil)
	if err != nil {
		return nil, fmt.Errorf("listing instances: %w", err)
	}
	ssmInfo, _ := awsclient.ListManagedInstances(ctx, awsCfg)
	awsclient.MergeSSMInfo(instances, ssmInfo)

	inst, err := resolver.Resolve(target, instances, cfg.Aliases)
	if err != nil {
		var ambig *resolver.ErrAmbiguous
		if errors.As(err, &ambig) {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%q is ambiguous (%d matches) — select one:\n", target, len(ambig.Matches))
			return tui.RunPicker(ambig.Matches)
		}
		return nil, err
	}
	return inst, nil
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

	defaultName := inst.Name
	if defaultName == "" {
		defaultName = inst.InstanceID
	}

	var rename string
	err := huh.NewInput().
		Title("Save this instance as a bookmark?").
		Placeholder(defaultName).
		Value(&rename).
		Run()
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil
		}
		return err
	}

	name := defaultName
	if rename != "" {
		name = rename
	}
	if err := config.SetAlias(name, inst.InstanceID); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s  Bookmarked as %s\n", tui.StyleSuccess.Render("✓"), tui.StyleBold.Render(name))
	return nil
}
