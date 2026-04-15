package workflow

import (
	"reflect"
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
	}
}

func TestLevels_UndefinedDependency(t *testing.T) {
	steps := map[string]*Step{
		"a": {Shell: "echo a", Needs: []string{"ghost"}},
	}
	_, err := Levels(steps)
	if err == nil {
		t.Error("expected undefined dep error")
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
