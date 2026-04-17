package workflow

import (
	"context"
	"fmt"
	"io"
	"time"
)

// runSSMDocStep executes an ssm-doc: step. It resolves param expressions against
// the parent ExprContext, expands any doc alias, dispatches via SSM SendCommand,
// and waits for completion. Produces stdout/stderr/exitCode in the same shape as
// shell steps; the outputs: block uses ${{ stdout }} and ${{ exitCode }}.
func runSSMDocStep(ctx context.Context, e *Engine, step *Step, name string, exprCtx ExprContext, progress io.Writer) (*StepResult, error) {
	// Expand doc alias if present; use name as-is otherwise.
	docName := step.SSMDoc
	if alias, ok := e.docAliases[docName]; ok {
		docName = alias
	}
	docAlias := ""
	if _, ok := e.docAliases[step.SSMDoc]; ok {
		docAlias = step.SSMDoc
	}

	// Resolve expressions in params values.
	resolvedParams := make(map[string]string, len(step.Params))
	for k, v := range step.Params {
		resolved, err := Resolve(v, exprCtx)
		if err != nil {
			return nil, fmt.Errorf("step %q: resolving param %q: %w", name, k, err)
		}
		resolvedParams[k] = resolved
	}

	// Parse optional step-level timeout.
	var timeoutSecs int32
	if step.Timeout != "" {
		d, err := time.ParseDuration(step.Timeout)
		if err != nil {
			return nil, fmt.Errorf("parsing timeout %q: %w", step.Timeout, err)
		}
		timeoutSecs = int32(d.Seconds())
	}

	cmdID, err := e.runner.sendDocCommand(ctx, e.instanceID, docName, resolvedParams, timeoutSecs)
	if err != nil {
		return nil, fmt.Errorf("sending doc command: %w", err)
	}

	stdout, stderr, exitCode, err := e.runner.waitForShellCommand(ctx, e.instanceID, cmdID, progress)
	if err != nil {
		return nil, fmt.Errorf("waiting for doc command: %w", err)
	}

	res := &StepResult{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
		Success:  exitCode == 0,
		DocName:  docName,
		DocAlias: docAlias,
	}

	if len(step.Outputs) > 0 {
		outputCtx := exprCtx
		outputCtx.CurrentStdout = stdout
		outputCtx.CurrentExitCode = exitCode
		res.Outputs = make(map[string]string, len(step.Outputs))
		for oName, expr := range step.Outputs {
			v, err := Resolve(expr, outputCtx)
			if err != nil {
				return nil, fmt.Errorf("resolving output %q: %w", oName, err)
			}
			res.Outputs[oName] = v
		}
	}

	return res, nil
}
