package workflow

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
)

// runParallelStep executes all sub-steps in step.Parallel concurrently.
// All sub-steps run to completion regardless of sibling failures (continue-all).
// Sub-steps share a lockedWriter so their output lines do not interleave;
// each sub-step's output is prefixed with its name.
// Returns an error if any sub-step failed; all sub-steps always complete first.
func runParallelStep(ctx context.Context, e *Engine, step *Step, name string, exprCtx ExprContext, opts RunOptions, w io.Writer, isTTY bool) (*StepResult, error) {
	lw := &lockedWriter{w: w}

	// Stable sub-step order so output is deterministic across runs.
	subNames := make([]string, 0, len(step.Parallel))
	for subName := range step.Parallel {
		subNames = append(subNames, subName)
	}
	sort.Strings(subNames)

	type outcome struct {
		success bool
		err     error
	}
	ch := make(chan outcome, len(subNames))

	var wg sync.WaitGroup
	for _, subName := range subNames {
		subName := subName
		subStep := step.Parallel[subName]
		wg.Add(1)
		go func() {
			defer wg.Done()
			pw := &prefixWriter{
				w:      lw,
				prefix: fmt.Sprintf("  [%s]  ", subName),
			}
			// Sub-steps have no dependency graph between them.
			res, skipped, err := e.runStep(ctx, subStep, subName, exprCtx, map[string]bool{}, opts, pw, isTTY, false)
			if err != nil {
				ch <- outcome{err: err}
				return
			}
			if skipped || res.Success {
				ch <- outcome{success: true}
				return
			}
			ch <- outcome{
				success: false,
				err:     fmt.Errorf("sub-step %q failed (exit code %d)", subName, res.ExitCode),
			}
		}()
	}
	wg.Wait()
	close(ch)

	var firstErr error
	failed := 0
	for o := range ch {
		if !o.success || o.err != nil {
			failed++
			if firstErr == nil {
				firstErr = o.err
			}
		}
	}
	if failed > 0 {
		return &StepResult{Success: false, ExitCode: 1},
			fmt.Errorf("%d of %d sub-steps failed: %w", failed, len(subNames), firstErr)
	}
	return &StepResult{Success: true}, nil
}
