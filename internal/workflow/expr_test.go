package workflow

import (
	"testing"
)

func TestResolve_Inputs(t *testing.T) {
	ctx := ExprContext{Inputs: map[string]string{"version": "1.4.2"}}
	got, err := Resolve("app-${{ inputs.version }}.tar.gz", ctx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "app-1.4.2.tar.gz" {
		t.Errorf("got %q, want app-1.4.2.tar.gz", got)
	}
}

func TestResolve_Env(t *testing.T) {
	ctx := ExprContext{Env: map[string]string{"REGION": "us-east-1"}}
	got, err := Resolve("region=${{ env.REGION }}", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "region=us-east-1" {
		t.Errorf("got %q", got)
	}
}

func TestResolve_StepsOutputs(t *testing.T) {
	ctx := ExprContext{
		Steps: map[string]*StepResult{
			"get-version": {Outputs: map[string]string{"current": "1.3.0"}},
		},
	}
	got, err := Resolve("${{ steps.get-version.outputs.current }}", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "1.3.0" {
		t.Errorf("got %q, want 1.3.0", got)
	}
}

func TestResolve_StepsSuccess(t *testing.T) {
	ctx := ExprContext{
		Steps: map[string]*StepResult{"smoke": {Success: true}},
	}
	got, err := Resolve("${{ steps.smoke.success }}", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "true" {
		t.Errorf("got %q, want true", got)
	}
}

func TestResolve_StepsExitCode(t *testing.T) {
	ctx := ExprContext{
		Steps: map[string]*StepResult{"s": {ExitCode: 2}},
	}
	got, err := Resolve("${{ steps.s.exitCode }}", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "2" {
		t.Errorf("got %q, want 2", got)
	}
}

func TestResolve_StepsStdout(t *testing.T) {
	ctx := ExprContext{
		Steps: map[string]*StepResult{"s": {Stdout: "hello"}},
	}
	got, err := Resolve("${{ steps.s.stdout }}", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestResolve_Target(t *testing.T) {
	ctx := ExprContext{
		Target: TargetInfo{InstanceID: "i-0abc123", PrivateIP: "10.0.1.5", Name: "web-1"},
	}
	got, err := Resolve("${{ target.instance_id }}:${{ target.private_ip }}", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "i-0abc123:10.0.1.5" {
		t.Errorf("got %q", got)
	}
}

func TestResolve_CurrentStdout(t *testing.T) {
	ctx := ExprContext{CurrentStdout: "v1.4.2"}
	got, err := Resolve("${{ stdout }}", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "v1.4.2" {
		t.Errorf("got %q", got)
	}
}

func TestResolve_CurrentExitCode(t *testing.T) {
	ctx := ExprContext{CurrentExitCode: 42}
	got, err := Resolve("code=${{ exitCode }}", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "code=42" {
		t.Errorf("got %q", got)
	}
}

func TestResolve_NoExpression(t *testing.T) {
	ctx := ExprContext{}
	got, err := Resolve("plain string", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "plain string" {
		t.Errorf("got %q", got)
	}
}

func TestResolve_UnknownInput(t *testing.T) {
	ctx := ExprContext{Inputs: map[string]string{}}
	_, err := Resolve("${{ inputs.missing }}", ctx)
	if err == nil {
		t.Error("expected error for unknown input")
	}
}

func TestResolve_UnknownStep(t *testing.T) {
	ctx := ExprContext{Steps: map[string]*StepResult{}}
	_, err := Resolve("${{ steps.ghost.success }}", ctx)
	if err == nil {
		t.Error("expected error for unknown step")
	}
}

func TestEvalBool_Truthy(t *testing.T) {
	for _, s := range []string{"true", "1"} {
		got, err := EvalBool(s, ExprContext{})
		if err != nil {
			t.Fatalf("EvalBool(%q): %v", s, err)
		}
		if !got {
			t.Errorf("EvalBool(%q) = false, want true", s)
		}
	}
}

func TestEvalBool_Falsy(t *testing.T) {
	for _, s := range []string{"false", "0", ""} {
		got, err := EvalBool(s, ExprContext{})
		if err != nil {
			t.Fatalf("EvalBool(%q): %v", s, err)
		}
		if got {
			t.Errorf("EvalBool(%q) = true, want false", s)
		}
	}
}

func TestEvalBool_NotEqualTrue(t *testing.T) {
	ctx := ExprContext{Steps: map[string]*StepResult{"s": {ExitCode: 1}}}
	got, err := EvalBool("${{ steps.s.exitCode }} != 0", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("1 != 0 should be true")
	}
}

func TestEvalBool_NotEqualFalse(t *testing.T) {
	ctx := ExprContext{Steps: map[string]*StepResult{"s": {ExitCode: 0}}}
	got, err := EvalBool("${{ steps.s.exitCode }} != 0", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("0 != 0 should be false")
	}
}

func TestEvalBool_Equal(t *testing.T) {
	ctx := ExprContext{Inputs: map[string]string{"env": "prod"}}
	got, err := EvalBool("${{ inputs.env }} == prod", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("prod == prod should be true")
	}
}

func TestEvalBool_WithExpressionResolution(t *testing.T) {
	ctx := ExprContext{Steps: map[string]*StepResult{"smoke": {Success: true}}}
	got, err := EvalBool("${{ steps.smoke.success }}", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("steps.smoke.success = true should be truthy")
	}
}
