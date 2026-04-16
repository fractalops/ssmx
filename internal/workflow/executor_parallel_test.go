package workflow

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
)

func TestRunParallelStep_AllSucceed(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 0}
	e := makeTestEngine(runner, func(_ string) (*Workflow, error) { return nil, nil })
	step := &Step{
		Parallel: map[string]*Step{
			"fetch-a": {Shell: "curl -fsSL http://a -o /tmp/a"},
			"fetch-b": {Shell: "curl -fsSL http://b -o /tmp/b"},
		},
	}
	var buf bytes.Buffer
	result, err := runParallelStep(context.Background(), e, step, "fetch-all", ExprContext{}, RunOptions{Stderr: &buf}, &buf, false)
	if err != nil {
		t.Fatalf("runParallelStep: %v", err)
	}
	if !result.Success {
		t.Error("expected success when all sub-steps pass")
	}
}

func TestRunParallelStep_FailureReturnsError(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 1}
	e := makeTestEngine(runner, func(_ string) (*Workflow, error) { return nil, nil })
	step := &Step{
		Parallel: map[string]*Step{
			"a": {Shell: "exit 1"},
			"b": {Shell: "exit 1"},
		},
	}
	var buf bytes.Buffer
	result, err := runParallelStep(context.Background(), e, step, "do-both", ExprContext{}, RunOptions{Stderr: &buf}, &buf, false)
	if err == nil {
		t.Error("expected error when sub-steps fail")
	}
	if result.Success {
		t.Error("expected failure result")
	}
	if !strings.Contains(err.Error(), "of 2 sub-steps failed") {
		t.Errorf("error = %q, expected 'N of 2 sub-steps failed'", err.Error())
	}
}

func TestRunParallelStep_ContinueAll_BothSubStepsRun(t *testing.T) {
	// Verify continue-all: all sub-steps run even when one fails.
	var mu sync.Mutex
	callCount := 0
	runner := &countingShellRunner{
		fn: func() (string, string, int, error) {
			mu.Lock()
			callCount++
			mu.Unlock()
			return "", "", 1, nil // all fail
		},
	}
	e := makeTestEngine(runner, func(_ string) (*Workflow, error) { return nil, nil })
	step := &Step{
		Parallel: map[string]*Step{
			"a": {Shell: "exit 1"},
			"b": {Shell: "exit 1"},
		},
	}
	var buf bytes.Buffer
	_, err := runParallelStep(context.Background(), e, step, "do-both", ExprContext{}, RunOptions{Stderr: &buf}, &buf, false)
	if err == nil {
		t.Error("expected error")
	}
	mu.Lock()
	count := callCount
	mu.Unlock()
	if count != 2 {
		t.Errorf("runner called %d times, want 2 (continue-all: both sub-steps must run)", count)
	}
}

func TestEngine_ParallelStepInWorkflow(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 0}
	e := &Engine{
		instanceID: "i-0abc",
		runner:     runner,
		callStack:  []string{},
		loader:     func(_ string) (*Workflow, error) { return nil, nil },
		docAliases: map[string]string{},
	}
	wf := &Workflow{
		Name: "test",
		Steps: map[string]*Step{
			"fetch": {
				Parallel: map[string]*Step{
					"fetch-a": {Shell: "curl -fsSL http://a -o /tmp/a"},
					"fetch-b": {Shell: "curl -fsSL http://b -o /tmp/b"},
				},
			},
			"verify": {
				Shell: "echo done",
				Needs: []string{"fetch"},
			},
		},
	}
	var buf bytes.Buffer
	if _, err := e.Run(context.Background(), wf, RunOptions{Stderr: &buf}); err != nil {
		t.Fatalf("Run with parallel step: %v", err)
	}
}
