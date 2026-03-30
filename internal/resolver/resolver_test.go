package resolver

import (
	"errors"
	"testing"

	awsclient "github.com/fractalops/ssmx/internal/aws"
)

var testInstances = []awsclient.Instance{
	{InstanceID: "i-001", Name: "web-prod"},
	{InstanceID: "i-002", Name: "web-staging"},
	{InstanceID: "i-003", Name: "worker-01"},
	{InstanceID: "i-004", Name: ""},
}

func TestResolve_ExactAlias(t *testing.T) {
	aliases := map[string]string{"prod": "i-001"}
	got, err := Resolve("prod", testInstances, aliases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.InstanceID != "i-001" {
		t.Errorf("expected i-001, got %s", got.InstanceID)
	}
}

func TestResolve_ExactNameTag(t *testing.T) {
	got, err := Resolve("web-prod", testInstances, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.InstanceID != "i-001" {
		t.Errorf("expected i-001, got %s", got.InstanceID)
	}
}

func TestResolve_CaseInsensitiveNameTag(t *testing.T) {
	got, err := Resolve("WEB-PROD", testInstances, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.InstanceID != "i-001" {
		t.Errorf("expected i-001, got %s", got.InstanceID)
	}
}

func TestResolve_PrefixMatch(t *testing.T) {
	got, err := Resolve("worker", testInstances, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.InstanceID != "i-003" {
		t.Errorf("expected i-003, got %s", got.InstanceID)
	}
}

func TestResolve_AmbiguousPrefix(t *testing.T) {
	_, err := Resolve("web", testInstances, nil)
	var ambig *ErrAmbiguous
	if !errors.As(err, &ambig) {
		t.Fatalf("expected ErrAmbiguous, got %v", err)
	}
	if len(ambig.Matches) != 2 {
		t.Errorf("expected 2 matches, got %d", len(ambig.Matches))
	}
}

func TestResolve_InstanceID(t *testing.T) {
	got, err := Resolve("i-004", testInstances, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.InstanceID != "i-004" {
		t.Errorf("expected i-004, got %s", got.InstanceID)
	}
}

func TestResolve_NotFound(t *testing.T) {
	_, err := Resolve("nonexistent", testInstances, nil)
	var notFound *ErrNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
