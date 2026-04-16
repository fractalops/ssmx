package workflow

import (
	"reflect"
	"strings"
	"testing"
)

func TestLevels_SingleStep(t *testing.T) {
	steps := map[string]*Step{"a": {Shell: "echo a"}}
	levels, err := Levels(steps)
	if err != nil {
		t.Fatalf("Levels: %v", err)
	}
	if len(levels) != 1 || !reflect.DeepEqual(levels[0], []string{"a"}) {
		t.Errorf("got %v, want [[a]]", levels)
	}
}

func TestLevels_LinearChain(t *testing.T) {
	steps := map[string]*Step{
		"a": {Shell: "echo a"},
		"b": {Shell: "echo b", Needs: []string{"a"}},
		"c": {Shell: "echo c", Needs: []string{"b"}},
	}
	levels, err := Levels(steps)
	if err != nil {
		t.Fatalf("Levels: %v", err)
	}
	if len(levels) != 3 {
		t.Fatalf("want 3 levels, got %d: %v", len(levels), levels)
	}
	if !reflect.DeepEqual(levels[0], []string{"a"}) {
		t.Errorf("level 0 = %v, want [a]", levels[0])
	}
	if !reflect.DeepEqual(levels[1], []string{"b"}) {
		t.Errorf("level 1 = %v, want [b]", levels[1])
	}
	if !reflect.DeepEqual(levels[2], []string{"c"}) {
		t.Errorf("level 2 = %v, want [c]", levels[2])
	}
}

func TestLevels_ParallelFanOut(t *testing.T) {
	// a, b, c have no deps — all in level 0; d needs all three.
	steps := map[string]*Step{
		"a": {Shell: "echo a"},
		"b": {Shell: "echo b"},
		"c": {Shell: "echo c"},
		"d": {Shell: "echo d", Needs: []string{"a", "b", "c"}},
	}
	levels, err := Levels(steps)
	if err != nil {
		t.Fatalf("Levels: %v", err)
	}
	if len(levels) != 2 {
		t.Fatalf("want 2 levels, got %d: %v", len(levels), levels)
	}
	// Level 0 contains a, b, c (sorted).
	if !reflect.DeepEqual(levels[0], []string{"a", "b", "c"}) {
		t.Errorf("level 0 = %v, want [a b c]", levels[0])
	}
	if !reflect.DeepEqual(levels[1], []string{"d"}) {
		t.Errorf("level 1 = %v, want [d]", levels[1])
	}
}

func TestLevels_Diamond(t *testing.T) {
	// a → b, a → c, b + c → d
	steps := map[string]*Step{
		"a": {Shell: "echo a"},
		"b": {Shell: "echo b", Needs: []string{"a"}},
		"c": {Shell: "echo c", Needs: []string{"a"}},
		"d": {Shell: "echo d", Needs: []string{"b", "c"}},
	}
	levels, err := Levels(steps)
	if err != nil {
		t.Fatalf("Levels: %v", err)
	}
	if len(levels) != 3 {
		t.Fatalf("want 3 levels, got %d: %v", len(levels), levels)
	}
	if !reflect.DeepEqual(levels[0], []string{"a"}) {
		t.Errorf("level 0 = %v, want [a]", levels[0])
	}
	if !reflect.DeepEqual(levels[1], []string{"b", "c"}) {
		t.Errorf("level 1 = %v, want [b c]", levels[1])
	}
	if !reflect.DeepEqual(levels[2], []string{"d"}) {
		t.Errorf("level 2 = %v, want [d]", levels[2])
	}
}

func TestLevels_CycleDetected(t *testing.T) {
	steps := map[string]*Step{
		"a": {Shell: "echo a", Needs: []string{"b"}},
		"b": {Shell: "echo b", Needs: []string{"a"}},
	}
	_, err := Levels(steps)
	if err == nil {
		t.Error("expected cycle error")
	} else if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error should mention cycle, got: %v", err)
	}
}

func TestLevels_SelfDependency(t *testing.T) {
	steps := map[string]*Step{
		"a": {Shell: "echo a", Needs: []string{"a"}},
	}
	_, err := Levels(steps)
	if err == nil {
		t.Error("expected error for self-dependency")
	} else if !strings.Contains(err.Error(), "itself") {
		t.Errorf("error should mention self-dependency, got: %v", err)
	}
}

func TestLevels_UndefinedDependency(t *testing.T) {
	steps := map[string]*Step{
		"a": {Shell: "echo a", Needs: []string{"ghost"}},
	}
	_, err := Levels(steps)
	if err == nil {
		t.Error("expected undefined dep error")
	} else if !strings.Contains(err.Error(), "undefined") {
		t.Errorf("error should mention undefined, got: %v", err)
	}
}

func TestLevels_EmptySteps(t *testing.T) {
	levels, err := Levels(map[string]*Step{})
	if err != nil {
		t.Fatalf("Levels on empty steps: %v", err)
	}
	if len(levels) != 0 {
		t.Errorf("expected empty levels, got %v", levels)
	}
}

func TestAlwaysTrueWarnings_NoWarningsWhenNoAlwaysSteps(t *testing.T) {
	steps := map[string]*Step{
		"a": {Shell: "echo a"},
		"b": {Shell: "echo b", Needs: []string{"a"}},
		"c": {Shell: "echo c", Needs: []string{"b"}},
	}
	if w := AlwaysTrueWarnings(steps); len(w) != 0 {
		t.Errorf("expected no warnings, got %v", w)
	}
}

func TestAlwaysTrueWarnings_SafePattern(t *testing.T) {
	// cleanup is always: true and depends on start.
	// verify depends on both start and cleanup — this is safe.
	steps := map[string]*Step{
		"start":   {Shell: "echo start"},
		"cleanup": {Shell: "echo cleanup", Always: true, Needs: []string{"start"}},
		"verify":  {Shell: "echo verify", Needs: []string{"start", "cleanup"}},
	}
	if w := AlwaysTrueWarnings(steps); len(w) != 0 {
		t.Errorf("expected no warnings for safe pattern, got %v", w)
	}
}

func TestAlwaysTrueWarnings_RiskyPattern(t *testing.T) {
	// debug is always: true and depends on start.
	// next depends on debug but NOT on start — risky.
	steps := map[string]*Step{
		"start": {Shell: "echo start"},
		"debug": {Shell: "echo debug", Always: true, Needs: []string{"start"}},
		"next":  {Shell: "echo next", Needs: []string{"debug"}},
	}
	w := AlwaysTrueWarnings(steps)
	if len(w) == 0 {
		t.Error("expected warning for risky pattern, got none")
	}
	if !strings.Contains(w[0], `"next"`) {
		t.Errorf("warning should mention step 'next', got: %s", w[0])
	}
	if !strings.Contains(w[0], `"debug"`) {
		t.Errorf("warning should mention always-step 'debug', got: %s", w[0])
	}
	if !strings.Contains(w[0], `"start"`) {
		t.Errorf("warning should mention missing predecessor 'start', got: %s", w[0])
	}
}

func TestAlwaysTrueWarnings_AlwaysStepSkippedForSelf(t *testing.T) {
	// An always: true step depending on another always: true step should not
	// be flagged — both run unconditionally, so there is no hidden skip risk.
	steps := map[string]*Step{
		"start":   {Shell: "echo start"},
		"cleanup": {Shell: "echo cleanup", Always: true, Needs: []string{"start"}},
		"purge":   {Shell: "echo purge", Always: true, Needs: []string{"cleanup"}},
	}
	if w := AlwaysTrueWarnings(steps); len(w) != 0 {
		t.Errorf("expected no warnings when downstream is also always: true, got %v", w)
	}
}

func TestAlwaysTrueWarnings_PartialDepsRisky(t *testing.T) {
	// always-step A depends on [p1, p2].
	// downstream D depends on A and p1 but not p2 — still risky for p2.
	steps := map[string]*Step{
		"p1":      {Shell: "echo p1"},
		"p2":      {Shell: "echo p2"},
		"always-a": {Shell: "echo a", Always: true, Needs: []string{"p1", "p2"}},
		"d":       {Shell: "echo d", Needs: []string{"always-a", "p1"}},
	}
	w := AlwaysTrueWarnings(steps)
	if len(w) == 0 {
		t.Error("expected warning when d is missing p2, got none")
	}
	if !strings.Contains(w[0], `"p2"`) {
		t.Errorf("warning should mention missing predecessor p2, got: %s", w[0])
	}
}
