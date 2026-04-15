package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fractalops/ssmx/internal/workflow"
)

func TestWriteWorkflowInfo_PrintsVersion(t *testing.T) {
	wf := &workflow.Workflow{
		Name:        "deploy",
		Description: "Deploys the app",
		Version:     "1.2.3",
	}
	var buf bytes.Buffer
	writeWorkflowInfo(&buf, wf)
	out := buf.String()
	if !strings.Contains(out, "version: 1.2.3") {
		t.Errorf("expected 'version: 1.2.3' in output, got:\n%s", out)
	}
}

func TestWriteWorkflowInfo_OmitsVersionWhenEmpty(t *testing.T) {
	wf := &workflow.Workflow{Name: "deploy"}
	var buf bytes.Buffer
	writeWorkflowInfo(&buf, wf)
	out := buf.String()
	if strings.Contains(out, "version:") {
		t.Errorf("expected no 'version:' line when version is empty, got:\n%s", out)
	}
}
