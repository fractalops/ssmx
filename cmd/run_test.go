package cmd

import (
	"bytes"
	"encoding/json"
	"os"
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
	writeWorkflowInfo(&buf, wf, map[string]string{})
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
	writeWorkflowInfo(&buf, wf, map[string]string{})
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

// TestFormatRunSummaryJSON_FailureDetailInPayload verifies that failed step
// stdout/stderr are preserved in the JSON summary. This matters because
// --format json suppresses the human-readable status stream (io.Discard),
// making the JSON payload the sole source of failure diagnostics.
func TestFormatRunSummaryJSON_FailureDetailInPayload(t *testing.T) {
	s := &workflow.RunSummary{
		Workflow: "deploy",
		Instance: "i-123",
		Success:  false,
		Error:    "step \"start\" failed (exit code 125)",
		Steps: []workflow.StepSummary{
			{
				Name:    "start",
				Success: false,
				Exit:    125,
				Stdout:  "container name already in use\n",
				Stderr:  "docker: Error response from daemon: Conflict.\n",
			},
		},
	}
	out, err := formatRunSummaryJSON(s)
	if err != nil {
		t.Fatalf("formatRunSummaryJSON error: %v", err)
	}
	// Failure detail must survive in JSON even when no status stream is available.
	if !strings.Contains(out, "container name already in use") {
		t.Errorf("stdout missing from JSON payload:\n%s", out)
	}
	if !strings.Contains(out, "Conflict") {
		t.Errorf("stderr missing from JSON payload:\n%s", out)
	}
	if !strings.Contains(out, `"exit_code": 125`) {
		t.Errorf("exit code missing from JSON payload:\n%s", out)
	}
	if !strings.Contains(out, `"error":`) {
		t.Errorf("top-level error field missing from JSON payload:\n%s", out)
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
	writeWorkflowInfo(&buf, wf, map[string]string{})
	out := buf.String()
	if !strings.Contains(out, "warnings:") {
		t.Errorf("expected 'warnings:' section in workflow info, got:\n%s", out)
	}
	if !strings.Contains(out, `"deploy"`) {
		t.Errorf("expected 'deploy' mentioned in warning, got:\n%s", out)
	}
}

func TestWriteWorkflowInfo_SSMDocStep_ShowsDocAndParams(t *testing.T) {
	wf := &workflow.Workflow{
		Name: "patch",
		Steps: map[string]*workflow.Step{
			"do-patch": {
				SSMDoc: "AWS-RunPatchBaseline",
				Params: map[string]string{"Operation": "Install"},
			},
		},
	}
	aliases := map[string]string{}
	var buf bytes.Buffer
	writeWorkflowInfo(&buf, wf, aliases)
	out := buf.String()
	if !strings.Contains(out, "AWS-RunPatchBaseline") {
		t.Errorf("expected doc name in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Operation=Install") {
		t.Errorf("expected params in output, got:\n%s", out)
	}
}

func TestWriteWorkflowInfo_SSMDocStep_ShowsAliasExpansion(t *testing.T) {
	wf := &workflow.Workflow{
		Name: "patch",
		Steps: map[string]*workflow.Step{
			"do-patch": {SSMDoc: "patch"},
		},
	}
	aliases := map[string]string{"patch": "AWS-PatchInstanceWithRollback"}
	var buf bytes.Buffer
	writeWorkflowInfo(&buf, wf, aliases)
	out := buf.String()
	if !strings.Contains(out, "AWS-PatchInstanceWithRollback") {
		t.Errorf("expected resolved alias target in output, got:\n%s", out)
	}
	if !strings.Contains(out, "patch") {
		t.Errorf("expected alias name in output, got:\n%s", out)
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

// ── --run / --run-file mutual exclusion ──────────────────────────────────────

func TestCheckMutuallyExclusive_BothRunFlags(t *testing.T) {
	old1, old2 := flagRun, flagRunFile
	defer func() { flagRun, flagRunFile = old1, old2 }()
	flagRun = "deploy"
	flagRunFile = "/tmp/deploy.yaml"
	if err := checkMutuallyExclusive(); err == nil {
		t.Error("expected error when both --run and --run-file are set")
	}
}

func TestCheckMutuallyExclusive_OnlyRunIsOK(t *testing.T) {
	old1, old2 := flagRun, flagRunFile
	defer func() { flagRun, flagRunFile = old1, old2 }()
	flagRun = "deploy"
	flagRunFile = ""
	if err := checkMutuallyExclusive(); err != nil {
		t.Errorf("unexpected error with only --run: %v", err)
	}
}

func TestCheckMutuallyExclusive_OnlyRunFileIsOK(t *testing.T) {
	old1, old2 := flagRun, flagRunFile
	defer func() { flagRun, flagRunFile = old1, old2 }()
	flagRun = ""
	flagRunFile = "/tmp/deploy.yaml"
	if err := checkMutuallyExclusive(); err != nil {
		t.Errorf("unexpected error with only --run-file: %v", err)
	}
}

func TestCheckMutuallyExclusive_BothWorkflowInfoFlags(t *testing.T) {
	old1, old2 := flagWorkflowInfo, flagWorkflowInfoFile
	defer func() { flagWorkflowInfo, flagWorkflowInfoFile = old1, old2 }()
	flagWorkflowInfo = "deploy"
	flagWorkflowInfoFile = "/tmp/deploy.yaml"
	if err := checkMutuallyExclusive(); err == nil {
		t.Error("expected error when both --workflow-info and --workflow-info-file are set")
	}
}

// ── parseRootArgs routing for --run-file ─────────────────────────────────────

func callParseRootArgs(run, runFile string, args []string, tags []string) rootArgs {
	return parseRootArgs(
		false, false, false, false, false, false,
		args, -1,
		run, runFile,
		false, nil, "", "", false,
		tags, 0,
	)
}

func TestParseRootArgs_RunFileWithTarget(t *testing.T) {
	ra := callParseRootArgs("", "/tmp/deploy.yaml", []string{"web-prod"}, nil)
	if ra.action != actionRun {
		t.Errorf("action = %v, want actionRun", ra.action)
	}
	if ra.target != "web-prod" {
		t.Errorf("target = %q, want web-prod", ra.target)
	}
}

func TestParseRootArgs_RunFileFleetWithTag(t *testing.T) {
	ra := callParseRootArgs("", "/tmp/deploy.yaml", nil, []string{"env=prod"})
	if ra.action != actionRunFleet {
		t.Errorf("action = %v, want actionRunFleet", ra.action)
	}
}

func TestParseRootArgs_RunFileMissingTarget(t *testing.T) {
	ra := callParseRootArgs("", "/tmp/deploy.yaml", nil, nil)
	if ra.action != actionRunMissingTarget {
		t.Errorf("action = %v, want actionRunMissingTarget", ra.action)
	}
}

func TestParseRootArgs_WorkflowInfoFile(t *testing.T) {
	ra := parseRootArgs(
		false, false, false, false, false, false,
		nil, -1,
		"", "",
		false, nil, "", "/tmp/deploy.yaml", false,
		nil, 0,
	)
	if ra.action != actionWorkflowInfoFile {
		t.Errorf("action = %v, want actionWorkflowInfoFile", ra.action)
	}
	if ra.target != "/tmp/deploy.yaml" {
		t.Errorf("target = %q, want /tmp/deploy.yaml", ra.target)
	}
}

// ── loadActiveWorkflow ───────────────────────────────────────────────────────

func TestLoadActiveWorkflow_UsesRunFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("name: file-deploy\nsteps:\n  s:\n    shell: echo hi\n")
	f.Close()

	old1, old2 := flagRun, flagRunFile
	defer func() { flagRun, flagRunFile = old1, old2 }()
	flagRun = ""
	flagRunFile = f.Name()

	wf, err := loadActiveWorkflow()
	if err != nil {
		t.Fatalf("loadActiveWorkflow: %v", err)
	}
	if wf.Name != "file-deploy" {
		t.Errorf("Name = %q, want file-deploy", wf.Name)
	}
}

func TestLoadActiveWorkflow_RunFileNotFound(t *testing.T) {
	old1, old2 := flagRun, flagRunFile
	defer func() { flagRun, flagRunFile = old1, old2 }()
	flagRun = ""
	flagRunFile = "/nonexistent/deploy.yaml"

	_, err := loadActiveWorkflow()
	if err == nil {
		t.Error("expected error for non-existent --run-file path")
	}
}
