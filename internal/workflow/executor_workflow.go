package workflow

import (
	"context"
	"fmt"
	"strings"
)

// runWorkflowStep executes a workflow: step by loading and running the named
// sub-workflow recursively on the same instance. The parent's ExprContext is
// used only to resolve with: values; the sub-workflow runs with a fresh scope.
//
// Resolved sub-workflow outputs are returned as StepResult.Outputs so that
// the parent workflow can reference them via ${{ steps.<name>.outputs.<key> }}.
func runWorkflowStep(ctx context.Context, e *Engine, step *Step, name string, parentCtx ExprContext, opts RunOptions) (*StepResult, error) {
	wfName := step.Workflow

	// Cycle detection: refuse if the sub-workflow is already on the call stack.
	for _, ancestor := range e.callStack {
		if ancestor == wfName {
			path := strings.Join(append(e.callStack, wfName), " → ")
			return nil, fmt.Errorf("step %q: workflow cycle detected: %s", name, path)
		}
	}

	// Load the sub-workflow via the injectable loader.
	subWf, err := e.loader(wfName)
	if err != nil {
		return nil, fmt.Errorf("step %q: loading workflow %q: %w", name, wfName, err)
	}

	// Validate that all with: keys are declared inputs of the sub-workflow.
	for k := range step.With {
		if _, ok := subWf.Inputs[k]; !ok {
			return nil, fmt.Errorf("step %q: with: key %q is not declared in workflow %q inputs", name, k, wfName)
		}
	}

	// Build the inputs map, resolving any ${{ }} expressions in with: values
	// against the parent context.
	withInputs := make(map[string]string, len(step.With))
	for k, v := range step.With {
		raw := fmt.Sprintf("%v", v)
		resolved, err := Resolve(raw, parentCtx)
		if err != nil {
			return nil, fmt.Errorf("step %q: resolving with: %q: %w", name, k, err)
		}
		withInputs[k] = resolved
	}

	// Create a child engine with the sub-workflow name appended to the call stack.
	child := e.newChild(wfName)

	// Run the sub-workflow in a fresh scope.
	subOpts := RunOptions{
		Inputs:    withInputs,
		DryRun:    opts.DryRun,
		Stderr:    opts.Stderr,
		NoSpinner: opts.NoSpinner,
	}
	outputs, runErr := child.Run(ctx, subWf, subOpts)
	if runErr != nil {
		return &StepResult{Success: false, ExitCode: 1, Outputs: map[string]string{}}, runErr
	}

	return &StepResult{
		Success: true,
		Outputs: outputs,
	}, nil
}
