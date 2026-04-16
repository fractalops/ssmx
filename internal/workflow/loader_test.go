package workflow

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_FindsProjectLocal(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	wfDir := filepath.Join(dir, ".ssmx", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "name: deploy\nsteps:\n  s:\n    shell: echo hi\n"
	if err := os.WriteFile(filepath.Join(wfDir, "deploy.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	wf, err := Load("deploy")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if wf.Name != "deploy" {
		t.Errorf("name = %q, want deploy", wf.Name)
	}
}

func TestLoad_NotFound(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	_ = os.Chdir(dir)

	_, err := Load("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent workflow")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	wfDir := filepath.Join(dir, ".ssmx", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wfDir, "bad.yaml"), []byte(":\tinvalid:yaml:"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	_ = os.Chdir(dir)

	_, err := Load("bad")
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestList_ReturnsWorkflowNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	wfDir := filepath.Join(dir, ".ssmx", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"deploy", "rollback"} {
		content := "name: " + name + "\nsteps:\n  s:\n    shell: echo x\n"
		if err := os.WriteFile(filepath.Join(wfDir, name+".yaml"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	_ = os.Chdir(dir)

	names, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 2 {
		t.Errorf("got %d workflows, want 2: %v", len(names), names)
	}
}

func TestList_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	_ = os.Chdir(dir)

	names, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("expected empty list, got %v", names)
	}
}

func TestLoad_Stdin(t *testing.T) {
	yaml := `
name: stdin-test
version: "1.0.0"
steps:
  run:
    shell: echo hello
`
	old := stdinReader
	stdinReader = bytes.NewReader([]byte(yaml))
	defer func() { stdinReader = old }()

	wf, err := Load("-")
	if err != nil {
		t.Fatalf("Load(\"-\") error: %v", err)
	}
	if wf.Name != "stdin-test" {
		t.Errorf("Name = %q, want stdin-test", wf.Name)
	}
}

func TestLoad_StdinInvalidYAML(t *testing.T) {
	old := stdinReader
	stdinReader = bytes.NewReader([]byte(": invalid: yaml: ["))
	defer func() { stdinReader = old }()

	_, err := Load("-")
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoad_StdinEmpty(t *testing.T) {
	old := stdinReader
	stdinReader = strings.NewReader("")
	defer func() { stdinReader = old }()

	// Empty stdin yields a valid but empty workflow — the important thing is it does not panic.
	_, _ = Load("-")
}

func TestLoadFile_ExplicitPath(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("name: file-wf\nsteps:\n  s:\n    shell: echo hi\n")
	f.Close()

	wf, err := LoadFile(f.Name())
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if wf.Name != "file-wf" {
		t.Errorf("Name = %q, want file-wf", wf.Name)
	}
}

func TestLoadFile_NotFound(t *testing.T) {
	_, err := LoadFile("/nonexistent/path/deploy.yaml")
	if err == nil {
		t.Error("expected error for non-existent path")
	}
}

func TestLoadFile_InvalidYAML(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(":\tinvalid:yaml:")
	f.Close()

	_, err = LoadFile(f.Name())
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoadFile_ValidationError(t *testing.T) {
	// A step with no kind (no shell/ssm-doc/workflow/parallel) fails validation.
	f, err := os.CreateTemp(t.TempDir(), "*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("name: bad-wf\nsteps:\n  s:\n    timeout: 30s\n")
	f.Close()

	_, err = LoadFile(f.Name())
	if err == nil {
		t.Error("expected validation error for step with no kind")
	}
}

func TestLoadFile_Stdin(t *testing.T) {
	yaml := "name: stdin-file-wf\nsteps:\n  s:\n    shell: echo hi\n"
	old := stdinReader
	stdinReader = bytes.NewReader([]byte(yaml))
	defer func() { stdinReader = old }()

	wf, err := LoadFile("-")
	if err != nil {
		t.Fatalf("LoadFile(\"-\"): %v", err)
	}
	if wf.Name != "stdin-file-wf" {
		t.Errorf("Name = %q, want stdin-file-wf", wf.Name)
	}
}
