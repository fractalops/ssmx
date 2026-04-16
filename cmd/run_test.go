package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fractalops/ssmx/internal/workflow"
)

func TestWriteWorkflowInfo_Outputs(t *testing.T) {
	wf := &workflow.Workflow{
		Name:    "deploy",
		Version: "1.0.0",
		Outputs: map[string]string{"slot": "${{ steps.determine-slot.outputs.next }}"},
		Steps: map[string]*workflow.Step{
			"run": {Shell: "echo hi"},
		},
	}
	var buf bytes.Buffer
	writeWorkflowInfo(&buf, wf)
	out := buf.String()
	if !strings.Contains(out, "outputs:") {
		t.Errorf("expected 'outputs:' section, got:\n%s", out)
	}
	if !strings.Contains(out, "slot") {
		t.Errorf("expected 'slot' output key, got:\n%s", out)
	}
}

func TestWriteWorkflowInfo_AlwaysAndTimeout(t *testing.T) {
	wf := &workflow.Workflow{
		Name:    "cleanup",
		Version: "1.0.0",
		Steps: map[string]*workflow.Step{
			"teardown": {Shell: "rm -rf /tmp/x", Always: true, Timeout: "30s"},
		},
	}
	var buf bytes.Buffer
	writeWorkflowInfo(&buf, wf)
	out := buf.String()
	if !strings.Contains(out, "always") {
		t.Errorf("expected 'always' tag, got:\n%s", out)
	}
	if !strings.Contains(out, "timeout:30s") {
		t.Errorf("expected 'timeout:30s' tag, got:\n%s", out)
	}
}

func TestFormatRunSummaryJSON(t *testing.T) {
	s := &workflow.RunSummary{
		Workflow: "deploy",
		Instance: "i-123",
		Success:  false,
		Error:    "step \"run\" failed (exit code 1)",
		Steps: []workflow.StepSummary{
			{Name: "run", Success: false, Exit: 1, Stdout: "some output\n", Stderr: "error line\n"},
		},
	}
	out, err := formatRunSummaryJSON(s)
	if err != nil {
		t.Fatalf("formatRunSummaryJSON error: %v", err)
	}
	if !strings.Contains(out, `"success": false`) {
		t.Errorf("expected success:false in output, got:\n%s", out)
	}
	if !strings.Contains(out, `"workflow": "deploy"`) {
		t.Errorf("expected workflow name, got:\n%s", out)
	}
}

func TestBuildDryRunPlan_Basic(t *testing.T) {
	wf := &workflow.Workflow{
		Name:    "deploy",
		Version: "1.0.0",
		Inputs: map[string]*workflow.Input{
			"env": {Type: "string", Required: true},
		},
		Steps: map[string]*workflow.Step{
			"prepare": {Shell: "echo ${{ inputs.env }}"},
			"deploy":  {Shell: "echo deploying", Needs: []string{"prepare"}},
		},
	}
	plan, err := buildDryRunPlan(wf, map[string]string{"env": "prod"})
	if err != nil {
		t.Fatalf("buildDryRunPlan error: %v", err)
	}
	if plan.Workflow != "deploy" {
		t.Errorf("Workflow = %q, want deploy", plan.Workflow)
	}
	if len(plan.Steps) != 2 {
		t.Errorf("len(Steps) = %d, want 2", len(plan.Steps))
	}
	// prepare should be level 1
	var prepareLvl int
	for _, s := range plan.Steps {
		if s.Name == "prepare" {
			prepareLvl = s.Level
		}
	}
	if prepareLvl != 1 {
		t.Errorf("prepare level = %d, want 1", prepareLvl)
	}
}
