package workflow

import (
	"os"
	"path/filepath"
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
