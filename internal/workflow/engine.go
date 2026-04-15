package workflow

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"golang.org/x/sync/errgroup"
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
			res, skip, err := e.runStep(gctx, step, name, snap, failedSteps, opts, w)
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
			fmt.Fprintf(w, "  ✗  %s  error: %v\n", o.name, o.err)
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
				fmt.Fprintf(w, "  !  %s  cleanup failed (exit code %d)\n", o.name, o.result.ExitCode)
			} else if firstErr == nil {
				firstErr = fmt.Errorf("step %q failed (exit code %d)", o.name, o.result.ExitCode)
			}
		}
	}
	return firstErr
}

// runStep executes a single step. Returns (result, skipped, error).
func (e *Engine) runStep(ctx context.Context, step *Step, name string, exprCtx ExprContext, failedSteps map[string]bool, opts RunOptions, w io.Writer) (*StepResult, bool, error) {
	// Skip if any dependency failed (unless always: true).
	if !step.Always {
		for _, dep := range step.Needs {
			if failedSteps[dep] {
				fmt.Fprintf(w, "  —  %s  skipped (dependency %q failed)\n", name, dep)
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
			fmt.Fprintf(w, "  —  %s  skipped (if: condition false)\n", name)
			return nil, true, nil
		}
	}

	if opts.DryRun {
		resolved, _ := Resolve(step.Shell, exprCtx)
		fmt.Fprintf(w, "  ·  %s  [dry-run] %s: %s\n", name, step.Kind(), resolved)
		return &StepResult{Success: true}, false, nil
	}

	fmt.Fprintf(w, "  ⠋  %s  running...\n", name)

	switch step.Kind() {
	case "shell":
		result, err := runShellStep(ctx, e.runner, e.instanceID, step, exprCtx)
		if err != nil {
			return nil, false, err
		}
		if result.Success {
			fmt.Fprintf(w, "  ✓  %s\n", name)
		} else {
			fmt.Fprintf(w, "  ✗  %s  exit code %d\n", name, result.ExitCode)
		}
		return result, false, nil
	default:
		return nil, false, fmt.Errorf("step %q: kind %q is not supported in this version", name, step.Kind())
	}
}
