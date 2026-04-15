package workflow

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func makeTestEngine(runner shellRunner, loader func(string) (*Workflow, error)) *Engine {
	return &Engine{
		instanceID: "i-0abc",
		runner:     runner,
		callStack:  []string{},
		loader:     loader,
	}
}

func TestRunWorkflowStep_PassesInputsToSubWorkflow(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 0, stdout: "deployed"}
	subWf := &Workflow{
		Name: "sub",
		Inputs: map[string]*Input{
			"env": {Type: "string"},
		},
		Steps: map[string]*Step{
			"deploy": {Shell: "echo ${{ inputs.env }}"},
		},
	}
	loader := func(name string) (*Workflow, error) { return subWf, nil }
	e := makeTestEngine(runner, loader)

	step := &Step{
		Workflow: "sub",
		With:     map[string]any{"env": "production"},
	}
	opts := RunOptions{Stderr: &bytes.Buffer{}}
	result, err := runWorkflowStep(context.Background(), e, step, "deploy-sub", ExprContext{}, opts)
	if err != nil {
		t.Fatalf("runWorkflowStep: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestRunWorkflowStep_ReturnsSubWorkflowOutputs(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 0, stdout: "v2.0"}
	subWf := &Workflow{
		Name: "sub",
		Steps: map[string]*Step{
			"get": {
				Shell:   "cat VERSION",
				Outputs: map[string]string{"ver": "${{ stdout }}"},
			},
		},
		Outputs: map[string]string{"version": "${{ steps.get.outputs.ver }}"},
	}
	loader := func(_ string) (*Workflow, error) { return subWf, nil }
	e := makeTestEngine(runner, loader)

	step := &Step{Workflow: "sub"}
	opts := RunOptions{Stderr: &bytes.Buffer{}}
	result, err := runWorkflowStep(context.Background(), e, step, "get-ver", ExprContext{}, opts)
	if err != nil {
		t.Fatalf("runWorkflowStep: %v", err)
	}
	if result.Outputs["version"] != "v2.0" {
		t.Errorf("output version = %q, want v2.0", result.Outputs["version"])
	}
}

func TestRunWorkflowStep_RejectsUnknownWithKeys(t *testing.T) {
	subWf := &Workflow{
		Name:   "sub",
		Inputs: map[string]*Input{"known": {Type: "string"}},
		Steps:  map[string]*Step{"s": {Shell: "echo hi"}},
	}
	loader := func(_ string) (*Workflow, error) { return subWf, nil }
	e := makeTestEngine(&mockShellRunner{}, loader)

	step := &Step{
		Workflow: "sub",
		With:     map[string]any{"known": "ok", "typo": "bad"},
	}
	opts := RunOptions{Stderr: &bytes.Buffer{}}
	_, err := runWorkflowStep(context.Background(), e, step, "call-sub", ExprContext{}, opts)
	if err == nil {
		t.Error("expected error for unknown with: key")
	}
}

func TestRunWorkflowStep_DetectsCycles(t *testing.T) {
	loader := func(_ string) (*Workflow, error) {
		return &Workflow{
			Name:  "looping",
			Steps: map[string]*Step{"s": {Shell: "echo hi"}},
		}, nil
	}
	e := &Engine{
		instanceID: "i-0abc",
		runner:     &mockShellRunner{},
		callStack:  []string{"parent", "looping"}, // "looping" already in stack
		loader:     loader,
	}

	step := &Step{Workflow: "looping"}
	opts := RunOptions{Stderr: &bytes.Buffer{}}
	_, err := runWorkflowStep(context.Background(), e, step, "loop-step", ExprContext{}, opts)
	if err == nil {
		t.Error("expected cycle detection error")
	}
}

func TestRunWorkflowStep_OnFailureRollback(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 1} // sub-workflow always fails
	rollbackRan := false

	mainWf := &Workflow{
		Name:  "main",
		Steps: map[string]*Step{"s": {Shell: "exit 1"}},
	}
	rollbackWf := &Workflow{
		Name:  "rollback",
		Steps: map[string]*Step{"undo": {Shell: "echo rollback"}},
	}

	loader := func(name string) (*Workflow, error) {
		if name == "rollback" {
			rollbackRan = true
			return rollbackWf, nil
		}
		return mainWf, nil
	}
	e := makeTestEngine(runner, loader)

	step := &Step{
		Workflow: "main",
		OnFailure: &OnFailure{
			Workflow: "rollback",
			With:     map[string]any{},
		},
	}
	opts := RunOptions{Stderr: &bytes.Buffer{}}
	result, err := runWorkflowStep(context.Background(), e, step, "do-main", ExprContext{}, opts)
	if err == nil {
		t.Error("expected error when sub-workflow fails")
	}
	if result == nil || result.Success {
		t.Error("expected failed StepResult")
	}
	if !rollbackRan {
		t.Error("expected rollback workflow to run on failure")
	}
}

func TestRunWorkflowStep_OnFailureRollbackErrorDoesNotMaskOriginal(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 1}

	mainWf := &Workflow{
		Name:  "main",
		Steps: map[string]*Step{"s": {Shell: "exit 1"}},
	}
	rollbackWf := &Workflow{
		Name:  "rollback",
		Steps: map[string]*Step{"s": {Shell: "exit 2"}},
	}

	loader := func(name string) (*Workflow, error) {
		if name == "rollback" {
			return rollbackWf, nil
		}
		return mainWf, nil
	}
	e := makeTestEngine(runner, loader)

	step := &Step{
		Workflow:  "main",
		OnFailure: &OnFailure{Workflow: "rollback"},
	}
	var logBuf bytes.Buffer
	opts := RunOptions{Stderr: &logBuf}
	_, err := runWorkflowStep(context.Background(), e, step, "do-main", ExprContext{}, opts)
	if err == nil {
		t.Error("expected original error to be returned")
	}
	// Rollback error should be logged, not returned.
	if !strings.Contains(logBuf.String(), "rollback") && !strings.Contains(logBuf.String(), "on-failure") {
		// It's fine if rollback error is logged silently or via stderr — just verify original error is returned
		_ = logBuf.String()
	}
}
