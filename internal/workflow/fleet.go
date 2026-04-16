package workflow

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"

	awsclient "github.com/fractalops/ssmx/internal/aws"
)

// NewFleetEngineWithConfig creates a FleetEngine for production use. Each
// per-instance Engine is created from the provided AWS config, region, and profile.
func NewFleetEngineWithConfig(cfg aws.Config, instances []awsclient.Instance, maxConcurrency int, region, profile string, docAliases map[string]string) *FleetEngine {
	return &FleetEngine{
		instances:      instances,
		maxConcurrency: maxConcurrency,
		region:         region,
		profile:        profile,
		newEngine: func(instanceID string) *Engine {
			return New(cfg, instanceID, region, profile, docAliases)
		},
	}
}

// FleetEngine runs a workflow concurrently against multiple EC2 instances.
// Each instance gets its own Engine; output is prefixed with the instance name
// so concurrent lines stay identifiable.
type FleetEngine struct {
	instances      []awsclient.Instance
	maxConcurrency int
	region         string
	profile        string
	// newEngine is injectable for tests. Production code sets it via
	// NewFleetEngineWithConfig. Must not be nil when Run is called.
	newEngine func(instanceID string) *Engine
}

// Run executes wf against every instance in the fleet concurrently.
// Spinners are always disabled (NoSpinner=true) to prevent \r corruption.
// Output is prefixed with the instance name. A summary line is printed after
// all instances complete.
func (fe *FleetEngine) Run(ctx context.Context, wf *Workflow, opts RunOptions) error {
	w := opts.Stderr
	if w == nil {
		w = os.Stderr
	}
	isTTY := isTerminalWriter(w)

	// shared mutex writer — all prefixWriters batch-write through this so
	// lines from different instances do not interleave.
	mw := &mutexWriter{w: w}

	type result struct {
		name string
		err  error
	}
	results := make([]result, len(fe.instances))

	var sem chan struct{}
	if fe.maxConcurrency > 0 {
		sem = make(chan struct{}, fe.maxConcurrency)
	}

	var wg sync.WaitGroup
	for i, inst := range fe.instances {
		i, inst := i, inst
		wg.Add(1)
		if sem != nil {
			sem <- struct{}{}
		}
		go func() {
			defer wg.Done()
			if sem != nil {
				defer func() { <-sem }()
			}

			label := inst.Name
			if label == "" {
				label = inst.InstanceID
			}
			pw := &prefixWriter{
				w:      mw,
				prefix: fmt.Sprintf("  [%-20s]  ", label),
			}

			eng := fe.newEngine(inst.InstanceID)
			instOpts := RunOptions{
				Inputs:    opts.Inputs,
				DryRun:    opts.DryRun,
				Stderr:    pw,
				NoSpinner: true,
			}
			_, err := eng.Run(ctx, wf, instOpts)
			results[i] = result{name: label, err: err}
		}()
	}
	wg.Wait()

	// Summary line.
	failed := 0
	var failNames []string
	for _, r := range results {
		if r.err != nil {
			failed++
			failNames = append(failNames, r.name)
		}
	}
	total := len(fe.instances)
	succeeded := total - failed

	if failed == 0 {
		fmt.Fprintf(mw, "%s\n", ansi(isTTY, ansiGreen,
			fmt.Sprintf("  %d / %d succeeded", succeeded, total)))
		return nil
	}
	msg := fmt.Sprintf("  %d / %d succeeded  (%d failed: %s)",
		succeeded, total, failed, strings.Join(failNames, ", "))
	fmt.Fprintf(mw, "%s\n", ansi(isTTY, ansiRed, msg))
	return fmt.Errorf("%d of %d instances failed", failed, total)
}

// mutexWriter serialises concurrent writes so lines from different goroutines
// do not interleave. It is local to fleet.go; lockedWriter in spinner.go
// serves the same purpose for single-instance concurrent levels.
type mutexWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (mw *mutexWriter) Write(b []byte) (int, error) {
	mw.mu.Lock()
	defer mw.mu.Unlock()
	return mw.w.Write(b)
}

// prefixWriter prepends a fixed string to every line in each Write call.
// Lines within a single Write are batched into one call to the underlying
// writer so the entire prefix block is written atomically.
type prefixWriter struct {
	w      io.Writer
	prefix string
}

func (pw *prefixWriter) Write(b []byte) (int, error) {
	content := strings.TrimRight(string(b), "\n")
	if content == "" {
		return len(b), nil
	}
	var out strings.Builder
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		fmt.Fprintf(&out, "%s%s\n", pw.prefix, line)
	}
	_, err := io.WriteString(pw.w, out.String())
	return len(b), err
}
