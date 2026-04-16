package workflow

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	awsclient "github.com/fractalops/ssmx/internal/aws"
)

func TestFleetEngine_RunsAllInstances(t *testing.T) {
	var mu sync.Mutex
	callCount := 0
	runner := &countingShellRunner{
		fn: func() (string, string, int, error) {
			mu.Lock()
			callCount++
			mu.Unlock()
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
	if _, err := fe.Run(context.Background(), wf, RunOptions{Stderr: &buf}); err != nil {
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
	_, err := fe.Run(context.Background(), wf, RunOptions{Stderr: &buf})
	if err == nil {
		t.Error("expected error when instances fail")
	}
	out := buf.String()
	if !strings.Contains(out, "0 / 2 succeeded") {
		t.Errorf("expected failure summary, got:\n%s", out)
	}
}

func TestFleetEngine_SummaryCollected(t *testing.T) {
	runner := &countingShellRunner{
		fn: func() (string, string, int, error) {
			return "out", "", 0, nil
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
		Steps: map[string]*Step{"s": {Shell: "echo hi"}},
	}
	var buf bytes.Buffer
	fleet, err := fe.Run(context.Background(), wf, RunOptions{Stderr: &buf})
	if err != nil {
		t.Fatalf("FleetEngine.Run: %v", err)
	}
	if fleet == nil {
		t.Fatal("expected non-nil FleetRunSummary")
	}
	if fleet.Total != 2 {
		t.Errorf("Total = %d, want 2", fleet.Total)
	}
	if fleet.Succeeded != 2 {
		t.Errorf("Succeeded = %d, want 2", fleet.Succeeded)
	}
	if fleet.Failed != 0 {
		t.Errorf("Failed = %d, want 0", fleet.Failed)
	}
	if len(fleet.Instances) != 2 {
		t.Errorf("len(Instances) = %d, want 2", len(fleet.Instances))
	}
	for _, inst := range fleet.Instances {
		if !inst.Success {
			t.Errorf("instance %q: Success = false, want true", inst.Instance)
		}
	}
}

func TestPrefixWriter_PrependsPrefix(t *testing.T) {
	var mw mutexWriter
	var underlying bytes.Buffer
	mw.w = &underlying

	pw := &prefixWriter{w: &mw, prefix: "[web-1]  "}
	_, _ = pw.Write([]byte("hello\nworld\n"))

	out := underlying.String()
	if !strings.Contains(out, "[web-1]  hello") {
		t.Errorf("expected prefix on first line, got:\n%s", out)
	}
	if !strings.Contains(out, "[web-1]  world") {
		t.Errorf("expected prefix on second line, got:\n%s", out)
	}
}
