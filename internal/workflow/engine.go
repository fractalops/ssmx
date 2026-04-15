package workflow

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"golang.org/x/sync/errgroup"
	"golang.org/x/term"
)

// Engine executes workflows against a single target instance.
type Engine struct {
	cfg        aws.Config
	instanceID string
	region     string
	profile    string
	runner     shellRunner // injectable for tests; set by New
}

// RunOptions configures a workflow execution.
type RunOptions struct {
	Inputs map[string]string // from --param key=value flags
	DryRun bool
	Stderr io.Writer // status output; defaults to os.Stderr
}

// ANSI color codes used for step status output.
const (
	ansiReset  = "\033[0m"
	ansiGreen  = "\033[32m"
	ansiRed    = "\033[31m"
	ansiYellow = "\033[33m"
	ansiDim    = "\033[2m"
)

// ansi wraps s in the given ANSI escape sequence when isTTY is true.
// When isTTY is false the string is returned unchanged so piped output
// stays clean.
func ansi(isTTY bool, code, s string) string {
	if !isTTY {
		return s
	}
	return code + s + ansiReset
}

// isTerminalWriter reports whether w is a file descriptor that refers to a
// terminal. Used to decide whether to show animated spinners.
func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// New creates an Engine targeting instanceID.
func New(cfg aws.Config, instanceID, region, profile string) *Engine {
	return &Engine{
		cfg:        cfg,
		instanceID: instanceID,
		region:     region,
		profile:    profile,
		runner:     &awsShellRunner{cfg: cfg},
	}
}

// Run executes wf against the engine's target instance. It validates inputs,
// builds the DAG, and executes step levels concurrently. The workflow continues
// through all levels even on failure (to allow always: cleanup steps), then
// returns the first step error encountered.
func (e *Engine) Run(ctx context.Context, wf *Workflow, opts RunOptions) error {
	w := opts.Stderr
	if w == nil {
		w = os.Stderr
	}

	inputs, err := wf.ApplyInputs(opts.Inputs)
	if err != nil {
		return err
	}

	exprCtx := ExprContext{
		Inputs:  inputs,
		Secrets: map[string]string{},
		Env:     wf.Env,
		Steps:   map[string]*StepResult{},
		Target:  TargetInfo{InstanceID: e.instanceID},
	}
	if exprCtx.Env == nil {
		exprCtx.Env = map[string]string{}
	}

	levels, err := Levels(wf.Steps)
	if err != nil {
		return err
	}

	failedSteps := map[string]bool{}
	var firstErr error

	for _, level := range levels {
		if err := e.runLevel(ctx, wf, level, &exprCtx, failedSteps, opts, w); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			// Do not return here — allow subsequent always: cleanup steps to run.
		}
	}
	return firstErr
}

// runLevel executes all steps in a DAG level concurrently and collects
// their results. Returns the first step failure error (if any).
func (e *Engine) runLevel(ctx context.Context, wf *Workflow, stepNames []string, exprCtx *ExprContext, failedSteps map[string]bool, opts RunOptions, w io.Writer) error {
	type outcome struct {
		name    string
		result  *StepResult
		err     error
		skipped bool
	}
	ch := make(chan outcome, len(stepNames))

	// Build a stable snapshot of Steps for all goroutines in this level.
	// Shallow-copying the map (not the struct) is enough because StepResult
	// values are never mutated after creation. Without this, all goroutines
	// would share the same live map that the serial post-level loop writes to.
	snapSteps := make(map[string]*StepResult, len(exprCtx.Steps))
	for k, v := range exprCtx.Steps {
		snapSteps[k] = v
	}

	// Use an animated spinner only when writing to a real terminal and the
	// level has a single step. Multi-step levels run concurrently; mixing \r
	// rewrites from several goroutines would corrupt the output.
	isTTY := isTerminalWriter(w)
	useSpinner := isTTY && len(stepNames) == 1

	g, gctx := errgroup.WithContext(ctx)
	for _, name := range stepNames {
		name := name
		step := wf.Steps[name]
		snap := *exprCtx  // copy struct fields (Inputs, Env, Target, etc.)
		snap.Steps = snapSteps // replace with stable snapshot
		// Goroutines return nil; errors are sent via ch. This allows all steps
		// in a level to complete even if one errors, which is required for
		// always: cleanup steps.
		g.Go(func() error {
			res, skip, err := e.runStep(gctx, step, name, snap, failedSteps, opts, w, useSpinner)
			ch <- outcome{name: name, result: res, err: err, skipped: skip}
			return nil
		})
	}
	// g.Wait() return is nil by design (goroutines always return nil; errors go via ch).
	_ = g.Wait()
	close(ch)

	var firstErr error
	for o := range ch {
		if o.skipped {
			continue
		}
		if o.err != nil {
			fmt.Fprintf(w, "  %s  %s  error: %v\n", ansi(isTTY, ansiRed, "✗"), o.name, o.err)
			failedSteps[o.name] = true
			if firstErr == nil {
				firstErr = o.err
			}
			continue
		}
		exprCtx.Steps[o.name] = o.result
		if !o.result.Success {
			failedSteps[o.name] = true
			step := wf.Steps[o.name]
			if step.Always {
				// Log cleanup step failures so operators can see them, but do not
				// let them mask the original error that triggered cleanup.
				fmt.Fprintf(w, "  %s  %s  cleanup failed (exit code %d)\n", ansi(isTTY, ansiYellow, "!"), o.name, o.result.ExitCode)
			} else if firstErr == nil {
				firstErr = fmt.Errorf("step %q failed (exit code %d)", o.name, o.result.ExitCode)
			}
		}
	}
	return firstErr
}

// runStep executes a single step. Returns (result, skipped, error).
// useSpinner enables the animated braille spinner + live stdout streaming;
// it should only be true when w is a TTY and the step's level has one step.
func (e *Engine) runStep(ctx context.Context, step *Step, name string, exprCtx ExprContext, failedSteps map[string]bool, opts RunOptions, w io.Writer, useSpinner bool) (*StepResult, bool, error) {
	isTTY := isTerminalWriter(w)

	// Skip if any dependency failed (unless always: true).
	if !step.Always {
		for _, dep := range step.Needs {
			if failedSteps[dep] {
				fmt.Fprintf(w, "  %s  %s  skipped (dependency %q failed)\n", ansi(isTTY, ansiDim, "—"), name, dep)
				return nil, true, nil
			}
		}
	}

	// Evaluate if: condition.
	if step.If != "" {
		run, err := EvalBool(step.If, exprCtx)
		if err != nil {
			return nil, false, fmt.Errorf("step %q if condition: %w", name, err)
		}
		if !run {
			fmt.Fprintf(w, "  %s  %s  skipped (if: condition false)\n", ansi(isTTY, ansiDim, "—"), name)
			return nil, true, nil
		}
	}

	if opts.DryRun {
		resolved, _ := Resolve(step.Shell, exprCtx)
		fmt.Fprintf(w, "  %s  %s  [dry-run] %s: %s\n", ansi(isTTY, ansiDim, "·"), name, step.Kind(), resolved)
		return &StepResult{Success: true}, false, nil
	}

	// Set up spinner and live-output streaming when on a TTY.
	// stopSpinner is always callable (no-op when spinner is not active).
	var progress io.Writer
	stopSpinner := func() {}
	if useSpinner {
		fmt.Fprintf(w, "  %s  %s  %s", ansi(isTTY, ansiDim, "⠋"), name, ansi(isTTY, ansiDim, "running...")) // no trailing newline — spinner overwrites via \r
		sp := newStepSpinner(w, name, isTTY)
		var stopOnce sync.Once
		stopSpinner = func() {
			stopOnce.Do(func() {
				sp.Stop()
				sp.ClearLine()
			})
		}
		defer stopSpinner() // safety net: ensures stop on error/unsupported-kind paths
		// progressWriter stops the spinner on first write and indents output.
		progress = newProgressWriter(w, stopSpinner)
	} else {
		fmt.Fprintf(w, "  %s  %s  %s\n", ansi(isTTY, ansiDim, "⠋"), name, ansi(isTTY, ansiDim, "running..."))
	}

	switch step.Kind() {
	case "shell":
		result, err := runShellStep(ctx, e.runner, e.instanceID, step, exprCtx, progress)
		// Stop and clear the spinner before printing the final status line.
		// If progressWriter already stopped it (because output arrived), this is a no-op.
		// If the step produced no output (e.g. silent systemctl), this prevents
		// the ✓/✗ line from being appended to the live spinner frame.
		stopSpinner()
		if err != nil {
			return nil, false, err
		}
		if result.Success {
			fmt.Fprintf(w, "  %s  %s\n", ansi(isTTY, ansiGreen, "✓"), name)
		} else {
			fmt.Fprintf(w, "  %s  %s  exit code %d\n", ansi(isTTY, ansiRed, "✗"), name, result.ExitCode)
		}
		return result, false, nil
	default:
		return nil, false, fmt.Errorf("step %q: kind %q is not supported in this version", name, step.Kind())
	}
}
