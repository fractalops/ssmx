package state

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestUpsertAndGetCachedInstances(t *testing.T) {
	db := openTestDB(t)

	instances := []CachedInstance{
		{InstanceID: "i-001", Name: "web-prod", State: "running", SSMStatus: "ok", PrivateIP: "10.0.0.1", AgentVersion: "3.2", Region: "us-east-1", Profile: "default"},
		{InstanceID: "i-002", Name: "worker-01", State: "running", SSMStatus: "ok", PrivateIP: "10.0.0.2", AgentVersion: "3.2", Region: "us-east-1", Profile: "default"},
	}

	if err := UpsertInstances(db, instances); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := GetCachedInstances(db, "default", "us-east-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(got))
	}
	if got[0].Name != "web-prod" {
		t.Errorf("expected web-prod, got %s", got[0].Name)
	}
}

func TestGetCachedInstances_EmptyOnCacheMiss(t *testing.T) {
	db := openTestDB(t)

	got, err := GetCachedInstances(db, "default", "us-east-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 instances on cold cache, got %d", len(got))
	}
}

func TestUpsertInstances_UpdatesExisting(t *testing.T) {
	db := openTestDB(t)

	first := []CachedInstance{
		{InstanceID: "i-001", Name: "web-prod", State: "running", SSMStatus: "ok", PrivateIP: "10.0.0.1", Region: "us-east-1", Profile: "default"},
	}
	if err := UpsertInstances(db, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second := []CachedInstance{
		{InstanceID: "i-001", Name: "web-prod", State: "stopped", SSMStatus: "unknown", PrivateIP: "10.0.0.1", Region: "us-east-1", Profile: "default"},
	}
	if err := UpsertInstances(db, second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := GetCachedInstances(db, "default", "us-east-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(got))
	}
	if got[0].State != "stopped" {
		t.Errorf("expected state=stopped after update, got %s", got[0].State)
	}
}
