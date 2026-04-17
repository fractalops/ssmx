package workflow

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"golang.org/x/sync/errgroup"
	"golang.org/x/term"
)

// Engine executes workflows against a single target instance.
type Engine struct {
	cfg        aws.Config
	instanceID string
	name       string // EC2 Name tag — exposed as ${{ target.name }}
	privateIP  string // private IP — exposed as ${{ target.private_ip }}
	region     string
	profile    string
	runner     shellRunner                     // injectable for tests; set by New
	callStack  []string                        // workflow names on current call path; detects cycles
	loader     func(string) (*Workflow, error) // injectable; defaults to Load
	docAliases map[string]string               // SSM doc alias → full doc name
}

// RunOptions configures a workflow execution.
type RunOptions struct {
	Inputs    map[string]string // from --param key=value flags
	DryRun    bool
	Stderr    io.Writer // status output; defaults to os.Stderr
	NoSpinner bool      // disable animated spinner (e.g. for sub-workflow runs)
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
func New(cfg aws.Config, instanceID, name, privateIP, region, profile string, docAliases map[string]string) *Engine {
	return &Engine{
		cfg:        cfg,
		instanceID: instanceID,
		name:       name,
		privateIP:  privateIP,
		region:     region,
		profile:    profile,
		runner:     &awsShellRunner{cfg: cfg},
		callStack:  []string{},
		loader:     Load,
		docAliases: docAliases,
	}
}

// newChild returns an Engine for running a sub-workflow. It copies the
// current engine's configuration and appends workflowName to the call stack.
// A fresh slice is allocated so parent and child stacks are independent.
func (e *Engine) newChild(workflowName string) *Engine {
	stack := make([]string, len(e.callStack)+1)
	copy(stack, e.callStack)
	stack[len(e.callStack)] = workflowName
	return &Engine{
		cfg:        e.cfg,
		instanceID: e.instanceID,
		name:       e.name,
		privateIP:  e.privateIP,
		region:     e.region,
		profile:    e.profile,
		runner:     e.runner,
		callStack:  stack,
		loader:     e.loader,
		docAliases: e.docAliases,
	}
}

// Run executes wf against the engine's target instance. It validates inputs,
// builds the DAG, and executes step levels concurrently. The workflow continues
// through all levels even on failure (to allow always: cleanup steps), then
// returns the first step error encountered. On success, the resolved workflow
// outputs are returned (empty map when wf.Outputs is not defined).
func (e *Engine) Run(ctx context.Context, wf *Workflow, opts RunOptions) (map[string]string, *RunSummary, error) {
	w := opts.Stderr
	if w == nil {
		w = os.Stderr
	}

	inputs, err := wf.ApplyInputs(opts.Inputs)
	if err != nil {
		return nil, nil, err
	}

	exprCtx := ExprContext{
		Inputs:  inputs,
		Secrets: map[string]string{},
		Env:     wf.Env,
		Steps:   map[string]*StepResult{},
		Target:  TargetInfo{InstanceID: e.instanceID, Name: e.name, PrivateIP: e.privateIP},
	}
	if exprCtx.Env == nil {
		exprCtx.Env = map[string]string{}
	}

	levels, err := Levels(wf.Steps)
	if err != nil {
		return nil, nil, err
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

	summary := &RunSummary{
		Workflow: wf.Name,
		Instance: e.instanceID,
		Success:  firstErr == nil,
		Steps:    make([]StepSummary, 0),
	}
	if firstErr != nil {
		summary.Error = firstErr.Error()
	}
	// Collect step summaries in DAG execution order (level by level,
	// alphabetical within each level) so that JSON output is stable across runs.
	for _, level := range levels {
		for _, name := range level {
			sr, ok := exprCtx.Steps[name]
			if !ok {
				continue
			}
			summary.Steps = append(summary.Steps, StepSummary{
				Name:    name,
				Success: sr.Success,
				Skipped: sr.Skipped,
				Exit:    sr.ExitCode,
				Stdout:  sr.Stdout,
				Stderr:  sr.Stderr,
			})
		}
	}

	if firstErr != nil {
		return nil, summary, firstErr
	}
	if len(wf.Outputs) == 0 {
		summary.Outputs = map[string]string{}
		return map[string]string{}, summary, nil
	}
	resolved := make(map[string]string, len(wf.Outputs))
	for k, expr := range wf.Outputs {
		v, err := Resolve(expr, exprCtx)
		if err != nil {
			return nil, summary, fmt.Errorf("workflow output %q: %w", k, err)
		}
		resolved[k] = v
	}
	summary.Outputs = resolved
	return resolved, summary, nil
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

	// Spinner is only used for single-step levels — \r rewrites from concurrent
	// goroutines would corrupt output. For multi-step TTY levels we still stream
	// output, but protect all writes with a shared mutex so lines don't interleave.
	isTTY := isTerminalWriter(w)
	useSpinner := isTTY && len(stepNames) == 1 && !opts.NoSpinner

	stepW := w
	if isTTY && len(stepNames) > 1 {
		stepW = &lockedWriter{w: w}
	}

	g, gctx := errgroup.WithContext(ctx)
	for _, name := range stepNames {
		name := name
		step := wf.Steps[name]
		snap := *exprCtx       // copy struct fields (Inputs, Env, Target, etc.)
		snap.Steps = snapSteps // replace with stable snapshot
		// Goroutines return nil; errors are sent via ch. This allows all steps
		// in a level to complete even if one errors, which is required for
		// always: cleanup steps.
		g.Go(func() error {
			res, skip, err := e.runStep(gctx, step, name, snap, failedSteps, opts, stepW, isTTY, useSpinner)
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
			// A skipped step blocks its dependents just like a failed step,
			// so that skips cascade correctly through the DAG.
			failedSteps[o.name] = true
			exprCtx.Steps[o.name] = &StepResult{Success: false, Skipped: true}
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

const maxFailedOutputLines = 50

// printStepOutput prints stdout and stderr from a failed step inline, indented
// 6 spaces. Truncates at maxFailedOutputLines total lines and notes truncation.
func printStepOutput(w io.Writer, stdout, stderr string) {
	var lines []string
	if stdout != "" {
		lines = append(lines, strings.Split(strings.TrimRight(stdout, "\n"), "\n")...)
	}
	if stderr != "" {
		lines = append(lines, strings.Split(strings.TrimRight(stderr, "\n"), "\n")...)
	}
	truncated := false
	if len(lines) > maxFailedOutputLines {
		lines = lines[:maxFailedOutputLines]
		truncated = true
	}
	for _, l := range lines {
		fmt.Fprintf(w, "      %s\n", l)
	}
	if truncated {
		fmt.Fprintf(w, "      ... (output truncated at %d lines)\n", maxFailedOutputLines)
	}
}

// runStep executes a single step. Returns (result, skipped, error).
// isTTY reflects the original output writer (before any mutex wrapping).
// useSpinner should only be true for single-step TTY levels.
func (e *Engine) runStep(ctx context.Context, step *Step, name string, exprCtx ExprContext, failedSteps map[string]bool, opts RunOptions, w io.Writer, isTTY bool, useSpinner bool) (*StepResult, bool, error) {
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
		if step.SSMDoc != "" {
			resolvedDoc := step.SSMDoc
			aliasNote := ""
			if alias, ok := e.docAliases[step.SSMDoc]; ok {
				resolvedDoc = alias
				aliasNote = fmt.Sprintf(" (alias %q → %q)", step.SSMDoc, resolvedDoc)
			}
			paramParts := make([]string, 0, len(step.Params))
			for k, v := range step.Params {
				rv, _ := Resolve(v, exprCtx)
				paramParts = append(paramParts, k+"="+rv)
			}
			sort.Strings(paramParts)
			paramsStr := ""
			if len(paramParts) > 0 {
				paramsStr = " [" + strings.Join(paramParts, ", ") + "]"
			}
			fmt.Fprintf(w, "  %s  %s  [dry-run] ssm-doc: %s%s%s\n", ansi(isTTY, ansiDim, "·"), name, resolvedDoc, aliasNote, paramsStr)
		} else {
			script := step.Shell
			if step.Workflow != "" {
				script = step.Workflow
			}
			resolved, _ := Resolve(script, exprCtx)
			fmt.Fprintf(w, "  %s  %s  [dry-run] %s: %s\n", ansi(isTTY, ansiDim, "·"), name, step.Kind(), resolved)
		}
		return &StepResult{Success: true}, false, nil
	}

	// Set up output streaming. stopSpinner is always callable (no-op when not active).
	var progress io.Writer
	stopSpinner := func() {}
	if useSpinner {
		// Single-step TTY level: animated spinner, output stops it on first write.
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
		progress = newProgressWriter(w, stopSpinner)
	} else {
		fmt.Fprintf(w, "  %s  %s  %s\n", ansi(isTTY, ansiDim, "⠋"), name, ansi(isTTY, ansiDim, "running..."))
		if isTTY {
			// Multi-step TTY level: stream output without spinner.
			// w is already mutex-wrapped by runLevel so concurrent writes are safe.
			progress = newProgressWriter(w, func() {})
		} else {
			progress = newLabeledWriter(w, name)
		}
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
			printStepOutput(w, result.Stdout, result.Stderr)
		}
		return result, false, nil
	case "workflow":
		stopSpinner()
		result, err := runWorkflowStep(ctx, e, step, name, exprCtx, opts)
		if err != nil {
			return nil, false, err
		}
		if result.Success {
			fmt.Fprintf(w, "  %s  %s\n", ansi(isTTY, ansiGreen, "✓"), name)
		} else {
			fmt.Fprintf(w, "  %s  %s  failed\n", ansi(isTTY, ansiRed, "✗"), name)
		}
		return result, false, nil
	case "ssm-doc":
		stopSpinner()
		result, err := runSSMDocStep(ctx, e, step, name, exprCtx, progress)
		if err != nil {
			return nil, false, err
		}
		if result.Success {
			fmt.Fprintf(w, "  %s  %s\n", ansi(isTTY, ansiGreen, "✓"), name)
		} else {
			fmt.Fprintf(w, "  %s  %s  exit code %d\n", ansi(isTTY, ansiRed, "✗"), name, result.ExitCode)
			printStepOutput(w, result.Stdout, result.Stderr)
		}
		return result, false, nil
	case "parallel":
		stopSpinner()
		result, err := runParallelStep(ctx, e, step, name, exprCtx, opts, w, isTTY)
		if err != nil {
			return nil, false, err
		}
		if result.Success {
			fmt.Fprintf(w, "  %s  %s\n", ansi(isTTY, ansiGreen, "✓"), name)
		} else {
			fmt.Fprintf(w, "  %s  %s  failed\n", ansi(isTTY, ansiRed, "✗"), name)
		}
		return result, false, nil
	default:
		return nil, false, fmt.Errorf("step %q: kind %q is not supported in this version", name, step.Kind())
	}
}
