package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

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

func newCmdWithFormatFlag() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().StringVar(&flagFormat, "format", "table", "output format")
	return cmd
}

func TestRejectFormatIfSet_DefaultIsNotRejected(t *testing.T) {
	cmd := newCmdWithFormatFlag()
	// Flag not explicitly set — Changed("format") is false.
	if err := rejectFormatIfSet(cmd); err != nil {
		t.Errorf("expected no error when --format not set, got: %v", err)
	}
}

func TestRejectFormatIfSet_ExplicitValueRejected(t *testing.T) {
	cmd := newCmdWithFormatFlag()
	_ = cmd.Flags().Set("format", "json") // marks it as Changed
	if err := rejectFormatIfSet(cmd); err == nil {
		t.Error("expected error when --format explicitly set on unsupported mode")
	}
}

func TestRejectFormatIfSet_ExplicitTableRejected(t *testing.T) {
	// Even --format table (same as default) should be rejected when explicitly
	// passed to an unsupported mode — the intent is unambiguous.
	cmd := newCmdWithFormatFlag()
	_ = cmd.Flags().Set("format", "table")
	if err := rejectFormatIfSet(cmd); err == nil {
		t.Error("expected error when --format table explicitly set on unsupported mode")
	}
}

func TestValidateFormat_AllowedPasses(t *testing.T) {
	flagFormat = "json"
	if err := validateFormat("table", "json"); err != nil {
		t.Errorf("expected no error for 'json', got: %v", err)
	}
	flagFormat = "table"
	if err := validateFormat("table", "json"); err != nil {
		t.Errorf("expected no error for 'table', got: %v", err)
	}
}

func TestValidateFormat_UnknownFails(t *testing.T) {
	flagFormat = "tsv"
	if err := validateFormat("table", "json"); err == nil {
		t.Error("expected error for 'tsv' when not allowed")
	}
}

func TestValidateFormat_TSVAllowedForList(t *testing.T) {
	flagFormat = "tsv"
	if err := validateFormat("table", "json", "tsv"); err != nil {
		t.Errorf("expected no error for tsv when explicitly allowed, got: %v", err)
	}
}

func TestBuildDryRunPlan_IncludesAlwaysTrueWarnings(t *testing.T) {
	wf := &workflow.Workflow{
		Name: "risky",
		Steps: map[string]*workflow.Step{
			"start":  {Shell: "echo start"},
			"debug":  {Shell: "echo debug", Always: true, Needs: []string{"start"}},
			"deploy": {Shell: "echo deploy", Needs: []string{"debug"}},
		},
	}
	plan, err := buildDryRunPlan(wf, nil)
	if err != nil {
		t.Fatalf("buildDryRunPlan: %v", err)
	}
	if len(plan.Warnings) == 0 {
		t.Error("expected warnings for risky always: true pattern, got none")
	}
	// Verify warnings are included in JSON marshaling.
	b, _ := json.Marshal(plan)
	if !strings.Contains(string(b), "warnings") {
		t.Errorf("expected 'warnings' key in JSON, got: %s", string(b))
	}
}

func TestWriteWorkflowInfo_ShowsAlwaysTrueWarning(t *testing.T) {
	wf := &workflow.Workflow{
		Name: "risky",
		Steps: map[string]*workflow.Step{
			"start":  {Shell: "echo start"},
			"debug":  {Shell: "echo debug", Always: true, Needs: []string{"start"}},
			"deploy": {Shell: "echo deploy", Needs: []string{"debug"}},
		},
	}
	var buf bytes.Buffer
	writeWorkflowInfo(&buf, wf)
	out := buf.String()
	if !strings.Contains(out, "warnings:") {
		t.Errorf("expected 'warnings:' section in workflow info, got:\n%s", out)
	}
	if !strings.Contains(out, `"deploy"`) {
		t.Errorf("expected 'deploy' mentioned in warning, got:\n%s", out)
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
