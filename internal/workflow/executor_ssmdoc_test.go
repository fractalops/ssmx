package workflow

import (
	"bytes"
	"context"
	"testing"
)

func TestRunSSMDocStep_ExpandsAlias(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 0, stdout: "patched"}
	e := &Engine{
		instanceID: "i-0abc",
		runner:     runner,
		docAliases: map[string]string{"patch": "AWS-PatchInstanceWithRollback"},
	}
	step := &Step{SSMDoc: "patch", Params: map[string]string{"Operation": "Install"}}
	var buf bytes.Buffer
	result, err := runSSMDocStep(context.Background(), e, step, "do-patch", ExprContext{}, &buf)
	if err != nil {
		t.Fatalf("runSSMDocStep: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if runner.capturedDocName != "AWS-PatchInstanceWithRollback" {
		t.Errorf("docName = %q, want AWS-PatchInstanceWithRollback", runner.capturedDocName)
	}
}

func TestRunSSMDocStep_NoAlias_UsesDocNameDirectly(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 0, stdout: "ok"}
	e := &Engine{
		instanceID: "i-0abc",
		runner:     runner,
		docAliases: map[string]string{},
	}
	step := &Step{SSMDoc: "AWS-RunPatchBaseline"}
	var buf bytes.Buffer
	result, err := runSSMDocStep(context.Background(), e, step, "patch", ExprContext{}, &buf)
	if err != nil {
		t.Fatalf("runSSMDocStep: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if runner.capturedDocName != "AWS-RunPatchBaseline" {
		t.Errorf("docName = %q, want AWS-RunPatchBaseline", runner.capturedDocName)
	}
}

func TestRunSSMDocStep_ResolvesParamExpressions(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 0}
	e := &Engine{
		instanceID: "i-0abc",
		runner:     runner,
		docAliases: map[string]string{},
	}
	step := &Step{
		SSMDoc: "AWS-RunPatchBaseline",
		Params: map[string]string{"Operation": "${{ inputs.op }}"},
	}
	ctx := ExprContext{Inputs: map[string]string{"op": "Install"}}
	var buf bytes.Buffer
	_, err := runSSMDocStep(context.Background(), e, step, "patch", ctx, &buf)
	if err != nil {
		t.Fatalf("runSSMDocStep: %v", err)
	}
	if runner.capturedDocParams["Operation"] != "Install" {
		t.Errorf("Operation param = %q, want Install", runner.capturedDocParams["Operation"])
	}
}

func TestRunSSMDocStep_CapturesStdoutOutput(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 0, stdout: "patch-summary"}
	e := &Engine{
		instanceID: "i-0abc",
		runner:     runner,
		docAliases: map[string]string{},
	}
	step := &Step{
		SSMDoc:  "AWS-RunPatchBaseline",
		Outputs: map[string]string{"summary": "${{ stdout }}"},
	}
	var buf bytes.Buffer
	result, err := runSSMDocStep(context.Background(), e, step, "patch", ExprContext{}, &buf)
	if err != nil {
		t.Fatalf("runSSMDocStep: %v", err)
	}
	if result.Outputs["summary"] != "patch-summary" {
		t.Errorf("output summary = %q, want patch-summary", result.Outputs["summary"])
	}
}

func TestRunSSMDocStep_NonZeroExitIsNotError(t *testing.T) {
	runner := &mockShellRunner{commandID: "cmd-1", exitCode: 1}
	e := &Engine{instanceID: "i-0abc", runner: runner, docAliases: map[string]string{}}
	step := &Step{SSMDoc: "AWS-RunPatchBaseline"}
	var buf bytes.Buffer
	result, err := runSSMDocStep(context.Background(), e, step, "patch", ExprContext{}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for non-zero exit")
	}
	if result.ExitCode != 1 {
		t.Errorf("exitCode = %d, want 1", result.ExitCode)
	}
}
