package cmd

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"
	awsclient "github.com/fractalops/ssmx/internal/aws"
	"github.com/fractalops/ssmx/internal/config"
	"github.com/fractalops/ssmx/internal/preflight"
	"github.com/fractalops/ssmx/internal/session"
	"github.com/fractalops/ssmx/internal/tui"
)

// parseForward parses a -L flag value into a ForwardSpec.
// Accepted formats:
//   - "8080"                  → local 8080 → localhost:8080
//   - "8080:localhost:8080"   → local 8080 → localhost:8080
//   - "5432:db.internal:5432" → local 5432 → db.internal:5432
func parseForward(s string) (session.ForwardSpec, error) {
	parts := strings.SplitN(s, ":", 3)
	var local, host, remote string

	switch len(parts) {
	case 1:
		// Short form "-L 8080" — forward local 8080 to the instance's own port 8080.
		local = parts[0]
		host = "localhost"
		remote = parts[0]
	case 3:
		local, host, remote = parts[0], parts[1], parts[2]
	default:
		// Two-part strings like "8080:9090" are ambiguous (missing host), so reject them.
		return session.ForwardSpec{}, fmt.Errorf("invalid -L format %q: use port, or localPort:host:remotePort", s)
	}

	if err := validatePort(local); err != nil {
		return session.ForwardSpec{}, fmt.Errorf("invalid local port in %q: %w", s, err)
	}
	if err := validatePort(remote); err != nil {
		return session.ForwardSpec{}, fmt.Errorf("invalid remote port in %q: %w", s, err)
	}
	if host == "" {
		return session.ForwardSpec{}, fmt.Errorf("invalid -L format %q: host must not be empty", s)
	}
	return session.ForwardSpec{LocalPort: local, RemoteHost: host, RemotePort: remote}, nil
}

func validatePort(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("%q is not a valid port (1-65535)", s)
	}
	return nil
}

func runForward(cmd *cobra.Command, target string, forwards []session.ForwardSpec, persist bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

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

	inst, err := resolveTarget(ctx, cmd, awsCfg, cfg, target)
	if err != nil {
		return err
	}
	if inst == nil {
		return nil // user cancelled
	}

	if inst.SSMStatus == "offline" {
		fmt.Fprintf(os.Stderr, "%s  %s (%s) is not reachable via SSM\n",
			tui.StyleWarning.Render("!"), inst.Name, inst.InstanceID,
		)
		return &errOffline{inst}
	}

	var wg sync.WaitGroup
	// Buffered so goroutines can write without blocking after wg.Wait returns.
	errc := make(chan error, len(forwards))

	// Each -L rule gets its own goroutine; they all run concurrently against the
	// same SSM session (the plugin multiplexes them independently).
	for _, fwd := range forwards {
		fwd := fwd
		wg.Add(1)
		go func() {
			defer wg.Done()
			errc <- runSingleForward(ctx, awsCfg, inst.InstanceID, region, profile, fwd, persist)
		}()
	}

	wg.Wait()
	close(errc)

	// Return the first non-nil error.
	for err := range errc {
		if err != nil {
			return err
		}
	}
	return nil
}

func runSingleForward(
	ctx context.Context,
	cfg aws.Config,
	instanceID, region, profile string,
	fwd session.ForwardSpec,
	persist bool,
) error {
	label := fmt.Sprintf("%s → %s:%s", fwd.LocalPort, fwd.RemoteHost, fwd.RemotePort)
	backoff := 2 * time.Second

	for {
		err := session.Forward(ctx, cfg, instanceID, region, profile, fwd)

		// Clean exit (Ctrl-C or context cancelled) — not a reconnect case.
		if ctx.Err() != nil {
			return nil
		}

		if !persist {
			return err
		}

		// --persist: log the drop and wait before reconnecting.
		// Jitter spreads reconnect storms when multiple forwards drop at once.
		jitter := time.Duration(rand.Intn(500)) * time.Millisecond
		fmt.Fprintf(os.Stderr, "  %s  %s dropped — reconnecting in %s\n",
			tui.StyleWarning.Render("↺"), label, backoff+jitter,
		)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff + jitter):
		}

		// Exponential backoff, capped at 30 s.
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}
