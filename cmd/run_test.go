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
