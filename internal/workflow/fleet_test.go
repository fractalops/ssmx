package workflow

import (
	"bytes"
	"context"
	"strings"
	"testing"

	awsclient "github.com/fractalops/ssmx/internal/aws"
)

func TestFleetEngine_RunsAllInstances(t *testing.T) {
	callCount := 0
	runner := &countingShellRunner{
		fn: func() (string, string, int, error) {
			callCount++
			return "", "", 0, nil
		},
	}

	instances := []awsclient.Instance{
		{InstanceID: "i-0001", Name: "web-1"},
		{InstanceID: "i-0002", Name: "web-2"},
	}

	fe := &FleetEngine{
		instances:      instances,
		maxConcurrency: 0,
		newEngine: func(instanceID string) *Engine {
			return &Engine{
				instanceID: instanceID,
				runner:     runner,
				callStack:  []string{},
				loader:     Load,
			}
		},
	}

	wf := &Workflow{
		Name:  "test",
		Steps: map[string]*Step{"s": {Shell: "echo hi"}},
	}
	var buf bytes.Buffer
	if err := fe.Run(context.Background(), wf, RunOptions{Stderr: &buf}); err != nil {
		t.Fatalf("FleetEngine.Run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "2 / 2 succeeded") {
		t.Errorf("expected summary line, got:\n%s", out)
	}
}

func TestFleetEngine_ReportsFailures(t *testing.T) {
	runner := &countingShellRunner{
		fn: func() (string, string, int, error) {
			return "", "", 1, nil // always fail
		},
	}

	instances := []awsclient.Instance{
		{InstanceID: "i-0001", Name: "web-1"},
		{InstanceID: "i-0002", Name: "web-2"},
	}

	fe := &FleetEngine{
		instances: instances,
		newEngine: func(instanceID string) *Engine {
			return &Engine{
				instanceID: instanceID,
				runner:     runner,
				callStack:  []string{},
				loader:     Load,
			}
		},
	}

	wf := &Workflow{
		Name:  "test",
		Steps: map[string]*Step{"s": {Shell: "exit 1"}},
	}
	var buf bytes.Buffer
	err := fe.Run(context.Background(), wf, RunOptions{Stderr: &buf})
	if err == nil {
		t.Error("expected error when instances fail")
	}
	out := buf.String()
	if !strings.Contains(out, "0 / 2 succeeded") {
		t.Errorf("expected failure summary, got:\n%s", out)
	}
}

func TestPrefixWriter_PrependsPrefix(t *testing.T) {
	var mw mutexWriter
	var underlying bytes.Buffer
	mw.w = &underlying

	pw := &prefixWriter{mw: &mw, prefix: "[web-1]  "}
	_, _ = pw.Write([]byte("hello\nworld\n"))

	out := underlying.String()
	if !strings.Contains(out, "[web-1]  hello") {
		t.Errorf("expected prefix on first line, got:\n%s", out)
	}
	if !strings.Contains(out, "[web-1]  world") {
		t.Errorf("expected prefix on second line, got:\n%s", out)
	}
}
