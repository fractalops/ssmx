package workflow

import (
	"os"
	"testing"
)

func TestResolveWorkflow_ByName_UnknownReturnsError(t *testing.T) {
	_, meta, err := ResolveWorkflow("nonexistent-workflow-xyz", "", nil)
	if err == nil {
		t.Fatal("expected error for unknown workflow name")
	}
	_ = meta
}

func TestResolveWorkflow_ByFile_ParsesWorkflow(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("name: file-wf\nsteps:\n  s:\n    shell: echo hi\n")
	f.Close()

	wf, meta, err := ResolveWorkflow("", f.Name(), nil)
	if err != nil {
		t.Fatalf("ResolveWorkflow: %v", err)
	}
	if wf.Name != "file-wf" {
		t.Errorf("Name = %q, want file-wf", wf.Name)
	}
	if meta.Kind != SourceKindFile {
		t.Errorf("Kind = %q, want %q", meta.Kind, SourceKindFile)
	}
	if meta.Label != f.Name() {
		t.Errorf("Label = %q, want file path", meta.Label)
	}
}

func TestResolveWorkflow_DocRef_SynthesizesWorkflow(t *testing.T) {
	params := map[string]string{"Operation": "Install"}
	wf, meta, err := ResolveWorkflow("doc:AWS-RunPatchBaseline", "", params)
	if err != nil {
		t.Fatalf("ResolveWorkflow: %v", err)
	}
	if wf.Name != "doc:AWS-RunPatchBaseline" {
		t.Errorf("Name = %q, want doc:AWS-RunPatchBaseline", wf.Name)
	}
	if meta.Kind != SourceKindDoc {
		t.Errorf("Kind = %q, want %q", meta.Kind, SourceKindDoc)
	}
	step, ok := wf.Steps["run-doc"]
	if !ok {
		t.Fatal("expected step 'run-doc'")
	}
	if step.SSMDoc != "AWS-RunPatchBaseline" {
		t.Errorf("SSMDoc = %q, want AWS-RunPatchBaseline", step.SSMDoc)
	}
	if step.Params["Operation"] != "Install" {
		t.Errorf("Params[Operation] = %q, want Install", step.Params["Operation"])
	}
}

func TestSynthesizeDocWorkflow_StripPrefix(t *testing.T) {
	wf := SynthesizeDocWorkflow("doc:AWS-RunPatchBaseline", nil)
	step := wf.Steps["run-doc"]
	if step.SSMDoc != "AWS-RunPatchBaseline" {
		t.Errorf("SSMDoc = %q; want AWS-RunPatchBaseline (prefix stripped)", step.SSMDoc)
	}
}

func TestSynthesizeDocWorkflow_ParamsCopied(t *testing.T) {
	params := map[string]string{"Key": "Val"}
	wf := SynthesizeDocWorkflow("doc:AWS-Foo", params)
	// Mutating original should not affect synthesized workflow
	params["Key"] = "mutated"
	if wf.Steps["run-doc"].Params["Key"] != "Val" {
		t.Error("params were not deep-copied; mutation of original affected synthesized workflow")
	}
}

func TestResolveWorkflow_DocAlias_SynthesizesWithAliasName(t *testing.T) {
	wf, meta, err := ResolveWorkflow("doc:patch", "", nil)
	if err != nil {
		t.Fatalf("ResolveWorkflow: %v", err)
	}
	if meta.Kind != SourceKindDoc {
		t.Errorf("Kind = %q, want SourceKindDoc", meta.Kind)
	}
	// SSMDoc should be "patch" — alias resolution happens in the engine at runtime
	if wf.Steps["run-doc"].SSMDoc != "patch" {
		t.Errorf("SSMDoc = %q, want patch", wf.Steps["run-doc"].SSMDoc)
	}
}

func TestResolveWorkflow_EmptyDocName_ReturnsError(t *testing.T) {
	_, _, err := ResolveWorkflow("doc:", "", nil)
	if err == nil {
		t.Fatal("expected error for empty doc name (doc:)")
	}
}

func TestResolveWorkflow_BothEmpty_ReturnsError(t *testing.T) {
	_, _, err := ResolveWorkflow("", "", nil)
	if err == nil {
		t.Fatal("expected error when both run and runFile are empty")
	}
}
