package workflow

import (
	"bytes"
	"context"
	"io"
	"testing"
)

// mockShellRunner records calls and returns configured responses.
type mockShellRunner struct {
	commandID        string
	stdout           string
	stderr           string
	exitCode         int
	sendErr          error
	waitErr          error
	capturedCommands []string
	capturedEnv      map[string]string
}

func (m *mockShellRunner) sendShellCommand(_ context.Context, _ string, commands []string, env map[string]string, _ int32) (string, error) {
	m.capturedCommands = commands
	m.capturedEnv = env
	return m.commandID, m.sendErr
}

func (m *mockShellRunner) waitForShellCommand(_ context.Context, _, _ string, _ io.Writer) (string, string, int, error) {
	return m.stdout, m.stderr, m.exitCode, m.waitErr
}

func TestRunShellStep_Success(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", stdout: "v1.4.2", exitCode: 0}
	step := &Step{
		Shell:   "cat /srv/app/VERSION",
		Outputs: map[string]string{"version": "${{ stdout }}"},
	}
	res, err := runShellStep(context.Background(), runner, "i-0abc", step, ExprContext{}, nil)
	if err != nil {
		t.Fatalf("runShellStep: %v", err)
	}
	if !res.Success {
		t.Error("expected success")
	}
	if res.Stdout != "v1.4.2" {
		t.Errorf("stdout = %q, want v1.4.2", res.Stdout)
	}
	if res.Outputs["version"] != "v1.4.2" {
		t.Errorf("outputs.version = %q, want v1.4.2", res.Outputs["version"])
	}
}

func TestRunShellStep_ResolvesInputsInScript(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 0}
	step := &Step{Shell: "deploy ${{ inputs.version }}"}
	ctx := ExprContext{Inputs: map[string]string{"version": "2.0.0"}}

	if _, err := runShellStep(context.Background(), runner, "i-0abc", step, ctx, nil); err != nil {
		t.Fatalf("runShellStep: %v", err)
	}
	found := false
	for _, cmd := range runner.capturedCommands {
		if cmd == "deploy 2.0.0" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'deploy 2.0.0' in commands, got %v", runner.capturedCommands)
	}
}

func TestRunShellStep_NonZeroExitIsNotError(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 1, stderr: "not found"}
	step := &Step{Shell: "which nonexistent"}

	res, err := runShellStep(context.Background(), runner, "i-0abc", step, ExprContext{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure (exitCode=1)")
	}
	if res.ExitCode != 1 {
		t.Errorf("exitCode = %d, want 1", res.ExitCode)
	}
}

func TestRunShellStep_EnvResolution(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 0}
	step := &Step{
		Shell: "echo hi",
		Env:   map[string]string{"DEPLOY_ENV": "${{ inputs.env }}"},
	}
	ctx := ExprContext{Inputs: map[string]string{"env": "production"}}

	if _, err := runShellStep(context.Background(), runner, "i-0abc", step, ctx, nil); err != nil {
		t.Fatalf("runShellStep: %v", err)
	}
	if runner.capturedEnv["DEPLOY_ENV"] != "production" {
		t.Errorf("DEPLOY_ENV = %q, want production", runner.capturedEnv["DEPLOY_ENV"])
	}
}

func TestRunShellStep_PrependsSafetyFlags(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 0}
	step := &Step{Shell: "echo hi"}

	if _, err := runShellStep(context.Background(), runner, "i-0abc", step, ExprContext{}, nil); err != nil {
		t.Fatalf("runShellStep: %v", err)
	}
	if len(runner.capturedCommands) == 0 || runner.capturedCommands[0] != "set -euo pipefail" {
		t.Errorf("first command should be 'set -euo pipefail', got %v", runner.capturedCommands)
	}
}

func TestRunShellStep_OutputsExitCode(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 3}
	step := &Step{
		Shell:   "exit 3",
		Outputs: map[string]string{"code": "${{ exitCode }}"},
	}
	res, err := runShellStep(context.Background(), runner, "i-0abc", step, ExprContext{}, nil)
	if err != nil {
		t.Fatalf("runShellStep: %v", err)
	}
	if res.Outputs["code"] != "3" {
		t.Errorf("outputs.code = %q, want 3", res.Outputs["code"])
	}
}

// countingShellRunner calls fn on each waitForShellCommand call.
type countingShellRunner struct {
	fn func() (string, string, int, error)
}

func (r *countingShellRunner) sendShellCommand(_ context.Context, _ string, _ []string, _ map[string]string, _ int32) (string, error) {
	return "cmd-x", nil
}

func (r *countingShellRunner) waitForShellCommand(_ context.Context, _, _ string, _ io.Writer) (string, string, int, error) {
	return r.fn()
}

func TestEngine_RunSingleStep(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", stdout: "hello", exitCode: 0}
	e := &Engine{instanceID: "i-0abc", runner: runner}
	wf := &Workflow{
		Name:  "test",
		Steps: map[string]*Step{"greet": {Shell: "echo hello"}},
	}
	var buf bytes.Buffer
	if _, err := e.Run(context.Background(), wf, RunOptions{Stderr: &buf}); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestEngine_FailedStepReturnsError(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 1}
	e := &Engine{instanceID: "i-0abc", runner: runner}
	wf := &Workflow{
		Name:  "test",
		Steps: map[string]*Step{"bad": {Shell: "exit 1"}},
	}
	var buf bytes.Buffer
	_, err := e.Run(context.Background(), wf, RunOptions{Stderr: &buf})
	if err == nil {
		t.Error("expected error when step fails")
	}
}

func TestEngine_SkipsStepWhenDependencyFailed(t *testing.T) {
	callCount := 0
	runner := &countingShellRunner{
		fn: func() (string, string, int, error) {
			callCount++
			return "", "", 1, nil // always fail
		},
	}
	e := &Engine{instanceID: "i-0abc", runner: runner}
	wf := &Workflow{
		Name: "test",
		Steps: map[string]*Step{
			"a": {Shell: "exit 1"},
			"b": {Shell: "echo skip", Needs: []string{"a"}},
		},
	}
	var buf bytes.Buffer
	_, _ = e.Run(context.Background(), wf, RunOptions{Stderr: &buf})
	// Only step a should run; b should be skipped.
	if callCount != 1 {
		t.Errorf("runner called %d times, want 1 (step b should be skipped)", callCount)
	}
}

func TestEngine_AlwaysRunsAfterFailure(t *testing.T) {
	callCount := 0
	runner := &countingShellRunner{
		fn: func() (string, string, int, error) {
			callCount++
			if callCount == 1 {
				return "", "", 1, nil // first call (step a) fails
			}
			return "", "", 0, nil // second call (cleanup) succeeds
		},
	}
	e := &Engine{instanceID: "i-0abc", runner: runner}
	wf := &Workflow{
		Name: "test",
		Steps: map[string]*Step{
			"a":       {Shell: "exit 1"},
			"cleanup": {Shell: "rm -f /tmp/lock", Always: true, Needs: []string{"a"}},
		},
	}
	var buf bytes.Buffer
	_, _ = e.Run(context.Background(), wf, RunOptions{Stderr: &buf})
	if callCount != 2 {
		t.Errorf("runner called %d times, want 2 (cleanup should run despite failure)", callCount)
	}
}

func TestEngine_IfConditionSkipsStep(t *testing.T) {
	callCount := 0
	runner := &countingShellRunner{
		fn: func() (string, string, int, error) {
			callCount++
			return "", "", 0, nil
		},
	}
	e := &Engine{instanceID: "i-0abc", runner: runner}
	wf := &Workflow{
		Name: "test",
		Steps: map[string]*Step{
			"check": {Shell: "echo ok"},
			"alert": {
				Shell: "echo alert",
				Needs: []string{"check"},
				If:    "${{ steps.check.exitCode }} != 0",
			},
		},
	}
	var buf bytes.Buffer
	if _, err := e.Run(context.Background(), wf, RunOptions{Stderr: &buf}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// "alert" should be skipped because check exited 0, so "!= 0" is false.
	// Runner should be called exactly once (for "check" only).
	if callCount != 1 {
		t.Errorf("runner called %d times, want 1 (alert should be skipped)", callCount)
	}
}

func TestEngine_DryRunDoesNotCallRunner(t *testing.T) {
	runner := &mockShellRunner{}
	e := &Engine{instanceID: "i-0abc", runner: runner}
	wf := &Workflow{
		Name:  "test",
		Steps: map[string]*Step{"s": {Shell: "echo hi"}},
	}
	var buf bytes.Buffer
	if _, err := e.Run(context.Background(), wf, RunOptions{DryRun: true, Stderr: &buf}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runner.capturedCommands != nil {
		t.Error("dry-run should not call runner")
	}
}

func TestEngine_ValidatesRequiredInput(t *testing.T) {
	runner := &mockShellRunner{}
	e := &Engine{instanceID: "i-0abc", runner: runner}
	wf := &Workflow{
		Name: "test",
		Inputs: map[string]*Input{
			"version": {Type: "string", Required: true},
		},
		Steps: map[string]*Step{"s": {Shell: "echo ${{ inputs.version }}"}},
	}
	var buf bytes.Buffer
	_, err := e.Run(context.Background(), wf, RunOptions{Stderr: &buf})
	if err == nil {
		t.Error("expected error for missing required input")
	}
}

func TestEngine_ReturnsWorkflowOutputs(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", stdout: "v1.4.2", exitCode: 0}
	e := &Engine{instanceID: "i-0abc", runner: runner}
	wf := &Workflow{
		Name: "test",
		Steps: map[string]*Step{
			"get-version": {
				Shell:   "cat VERSION",
				Outputs: map[string]string{"ver": "${{ stdout }}"},
			},
		},
		Outputs: map[string]string{
			"app_version": "${{ steps.get-version.outputs.ver }}",
		},
	}
	var buf bytes.Buffer
	outputs, err := e.Run(context.Background(), wf, RunOptions{Stderr: &buf})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outputs["app_version"] != "v1.4.2" {
		t.Errorf("app_version = %q, want v1.4.2", outputs["app_version"])
	}
}

func TestEngine_ReturnsNilOutputsOnFailure(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 1}
	e := &Engine{instanceID: "i-0abc", runner: runner}
	wf := &Workflow{
		Name:    "test",
		Steps:   map[string]*Step{"bad": {Shell: "exit 1"}},
		Outputs: map[string]string{"result": "ok"},
	}
	var buf bytes.Buffer
	outputs, err := e.Run(context.Background(), wf, RunOptions{Stderr: &buf})
	if err == nil {
		t.Error("expected error")
	}
	if outputs != nil {
		t.Errorf("expected nil outputs on failure, got %v", outputs)
	}
}

func TestEngine_LoaderInjectable(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 0}
	e := &Engine{
		instanceID: "i-0abc",
		runner:     runner,
		callStack:  []string{},
		loader: func(name string) (*Workflow, error) {
			return &Workflow{Name: name, Steps: map[string]*Step{"s": {Shell: "echo ok"}}}, nil
		},
	}
	wf := &Workflow{Name: "parent", Steps: map[string]*Step{"s": {Shell: "echo hi"}}}
	var buf bytes.Buffer
	if _, err := e.Run(context.Background(), wf, RunOptions{Stderr: &buf}); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestEngine_NewChild_AppendsCallStack(t *testing.T) {
	runner := &mockShellRunner{}
	e := &Engine{
		instanceID: "i-0abc",
		runner:     runner,
		callStack:  []string{"parent"},
		loader:     func(_ string) (*Workflow, error) { return nil, nil },
	}
	child := e.newChild("child-wf")
	if len(child.callStack) != 2 {
		t.Fatalf("callStack len = %d, want 2", len(child.callStack))
	}
	if child.callStack[0] != "parent" || child.callStack[1] != "child-wf" {
		t.Errorf("callStack = %v, want [parent child-wf]", child.callStack)
	}
	// Mutating parent's stack must not affect child
	e.callStack = append(e.callStack, "intruder")
	if len(child.callStack) != 2 {
		t.Error("child callStack was mutated when parent changed")
	}
}
