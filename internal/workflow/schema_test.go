package workflow

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParse_MinimalWorkflow(t *testing.T) {
	raw := `
name: deploy
steps:
  stop-app:
    shell: systemctl stop app
`
	var wf Workflow
	if err := yaml.Unmarshal([]byte(raw), &wf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if wf.Name != "deploy" {
		t.Errorf("name = %q, want deploy", wf.Name)
	}
	step, ok := wf.Steps["stop-app"]
	if !ok {
		t.Fatal("step stop-app not found")
	}
	if step.Shell != "systemctl stop app" {
		t.Errorf("shell = %q, want systemctl stop app", step.Shell)
	}
}

func TestParse_StepWithNeeds(t *testing.T) {
	raw := `
name: w
steps:
  a:
    shell: echo a
  b:
    shell: echo b
    needs: [a]
`
	var wf Workflow
	if err := yaml.Unmarshal([]byte(raw), &wf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(wf.Steps["b"].Needs) != 1 || wf.Steps["b"].Needs[0] != "a" {
		t.Errorf("b.needs = %v, want [a]", wf.Steps["b"].Needs)
	}
}

func TestParse_Inputs(t *testing.T) {
	raw := `
name: w
inputs:
  version:
    type: string
    required: true
  env:
    type: string
    default: production
steps:
  s:
    shell: echo hi
`
	var wf Workflow
	if err := yaml.Unmarshal([]byte(raw), &wf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !wf.Inputs["version"].Required {
		t.Error("version.required should be true")
	}
	if wf.Inputs["env"].Default != "production" {
		t.Errorf("env.default = %v, want production", wf.Inputs["env"].Default)
	}
}

func TestValidate_MultipleStepKinds(t *testing.T) {
	wf := &Workflow{
		Name: "w",
		Steps: map[string]*Step{
			"bad": {Shell: "echo x", SSMDoc: "AWS-RunPatchBaseline"},
		},
	}
	if err := wf.Validate(); err == nil {
		t.Error("expected error for step with multiple kinds")
	}
}

func TestValidate_NoStepKind(t *testing.T) {
	wf := &Workflow{
		Name:  "w",
		Steps: map[string]*Step{"empty": {}},
	}
	if err := wf.Validate(); err == nil {
		t.Error("expected error for step with no kind")
	}
}

func TestApplyInputs_RequiredMissing(t *testing.T) {
	wf := &Workflow{
		Name: "w",
		Inputs: map[string]*Input{
			"version": {Type: "string", Required: true},
		},
		Steps: map[string]*Step{"s": {Shell: "echo x"}},
	}
	if _, err := wf.ApplyInputs(nil); err == nil {
		t.Error("expected error for missing required input")
	}
}

func TestApplyInputs_DefaultApplied(t *testing.T) {
	wf := &Workflow{
		Name: "w",
		Inputs: map[string]*Input{
			"env": {Type: "string", Default: "production"},
		},
		Steps: map[string]*Step{"s": {Shell: "echo x"}},
	}
	resolved, err := wf.ApplyInputs(nil)
	if err != nil {
		t.Fatalf("ApplyInputs: %v", err)
	}
	if resolved["env"] != "production" {
		t.Errorf("env = %q, want production", resolved["env"])
	}
}

func TestApplyInputs_ProvidedOverridesDefault(t *testing.T) {
	wf := &Workflow{
		Name: "w",
		Inputs: map[string]*Input{
			"env": {Type: "string", Default: "production"},
		},
		Steps: map[string]*Step{"s": {Shell: "echo x"}},
	}
	resolved, err := wf.ApplyInputs(map[string]string{"env": "staging"})
	if err != nil {
		t.Fatalf("ApplyInputs: %v", err)
	}
	if resolved["env"] != "staging" {
		t.Errorf("env = %q, want staging", resolved["env"])
	}
}

func TestStepKind(t *testing.T) {
	cases := []struct {
		step *Step
		want string
	}{
		{&Step{Shell: "echo"}, "shell"},
		{&Step{SSMDoc: "AWS-RunPatchBaseline"}, "ssm-doc"},
		{&Step{Workflow: "deploy"}, "workflow"},
		{&Step{Parallel: map[string]*Step{"a": {Shell: "echo"}}}, "parallel"},
		{&Step{}, ""},
	}
	for _, c := range cases {
		if got := c.step.Kind(); got != c.want {
			t.Errorf("Kind() = %q, want %q for %+v", got, c.want, c.step)
		}
	}
}

func TestApplyInputs_UnknownKeyReturnsError(t *testing.T) {
	wf := &Workflow{
		Name:  "w",
		Steps: map[string]*Step{"s": {Shell: "echo x"}},
	}
	_, err := wf.ApplyInputs(map[string]string{"unknown": "value"})
	if err == nil {
		t.Error("expected error for unknown input key")
	}
}

func TestValidate_ParallelSubStepNoKind(t *testing.T) {
	wf := &Workflow{
		Name: "w",
		Steps: map[string]*Step{
			"fetch": {
				Parallel: map[string]*Step{
					"bad": {}, // no kind
				},
			},
		},
	}
	if err := wf.Validate(); err == nil {
		t.Error("expected error for parallel sub-step with no kind")
	}
}

func TestValidate_ParallelSubStepMultipleKinds(t *testing.T) {
	wf := &Workflow{
		Name: "w",
		Steps: map[string]*Step{
			"fetch": {
				Parallel: map[string]*Step{
					"bad": {Shell: "echo", SSMDoc: "AWS-RunPatchBaseline"},
				},
			},
		},
	}
	if err := wf.Validate(); err == nil {
		t.Error("expected error for parallel sub-step with multiple kinds")
	}
}

func TestValidate_OnFailureOnNonWorkflowStep_Error(t *testing.T) {
	wf := &Workflow{
		Name: "test",
		Steps: map[string]*Step{
			"s": {
				Shell:     "echo hi",
				OnFailure: &OnFailure{Workflow: "rollback"},
			},
		},
	}
	if err := wf.Validate(); err == nil {
		t.Error("expected error for on-failure on shell step")
	}
}

func TestValidate_OnFailureEmptyWorkflow_Error(t *testing.T) {
	wf := &Workflow{
		Name: "test",
		Steps: map[string]*Step{
			"s": {
				Workflow:  "sub",
				OnFailure: &OnFailure{}, // no Workflow field
			},
		},
	}
	if err := wf.Validate(); err == nil {
		t.Error("expected error for on-failure with empty workflow name")
	}
}

func TestValidate_ParallelSubStep_NestedParallelRejected(t *testing.T) {
	wf := &Workflow{
		Name: "w",
		Steps: map[string]*Step{
			"outer": {
				Parallel: map[string]*Step{
					"inner": {
						Shell: "echo inner",
						Parallel: map[string]*Step{
							"deep": {Shell: "echo deep"},
						},
					},
				},
			},
		},
	}
	if err := wf.Validate(); err == nil {
		t.Error("expected error for nested parallel")
	}
}

func TestValidate_ParallelSubStep_NeedsRejected(t *testing.T) {
	wf := &Workflow{
		Name: "w",
		Steps: map[string]*Step{
			"fetch": {
				Parallel: map[string]*Step{
					"a": {Shell: "echo a", Needs: []string{"b"}},
				},
			},
		},
	}
	if err := wf.Validate(); err == nil {
		t.Error("expected error for parallel sub-step with needs:")
	}
}

func TestValidate_ParallelSubStep_IfRejected(t *testing.T) {
	wf := &Workflow{
		Name: "w",
		Steps: map[string]*Step{
			"fetch": {
				Parallel: map[string]*Step{
					"a": {Shell: "echo a", If: "${{ inputs.flag }}"},
				},
			},
		},
	}
	if err := wf.Validate(); err == nil {
		t.Error("expected error for parallel sub-step with if:")
	}
}

func TestValidate_ParallelSubStep_AlwaysRejected(t *testing.T) {
	wf := &Workflow{
		Name: "w",
		Steps: map[string]*Step{
			"fetch": {
				Parallel: map[string]*Step{
					"a": {Shell: "echo a", Always: true},
				},
			},
		},
	}
	if err := wf.Validate(); err == nil {
		t.Error("expected error for parallel sub-step with always:")
	}
}
