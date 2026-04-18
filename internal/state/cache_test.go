package state

import (
	"context"
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
	ctx := context.Background()
	db := openTestDB(t)

	instances := []CachedInstance{
		{InstanceID: "i-001", Name: "web-prod", State: "running", SSMStatus: "ok", PrivateIP: "10.0.0.1", AgentVersion: "3.2", Region: "us-east-1", Profile: "default"},
		{InstanceID: "i-002", Name: "worker-01", State: "running", SSMStatus: "ok", PrivateIP: "10.0.0.2", AgentVersion: "3.2", Region: "us-east-1", Profile: "default"},
	}

	if err := UpsertInstances(ctx, db, "default", "us-east-1", instances); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := GetCachedInstances(ctx, db, "default", "us-east-1")
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
	ctx := context.Background()
	db := openTestDB(t)

	got, err := GetCachedInstances(ctx, db, "default", "us-east-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 instances on cold cache, got %d", len(got))
	}
}

func TestUpsertAndGet_PlatformName(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	instances := []CachedInstance{
		{
			InstanceID: "i-001", Name: "web", State: "running",
			SSMStatus: "online", PrivateIP: "10.0.0.1", AgentVersion: "3.2",
			Region: "us-east-1", Profile: "default", PlatformName: "Ubuntu",
		},
	}
	if err := UpsertInstances(ctx, db, "default", "us-east-1", instances); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := GetCachedInstances(ctx, db, "default", "us-east-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 || got[0].PlatformName != "Ubuntu" {
		t.Errorf("expected PlatformName=Ubuntu, got %q", got[0].PlatformName)
	}
}

func TestUpsertInstances_UpdatesExisting(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	first := []CachedInstance{
		{InstanceID: "i-001", Name: "web-prod", State: "running", SSMStatus: "ok", PrivateIP: "10.0.0.1", Region: "us-east-1", Profile: "default"},
	}
	if err := UpsertInstances(ctx, db, "default", "us-east-1", first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second := []CachedInstance{
		{InstanceID: "i-001", Name: "web-prod", State: "stopped", SSMStatus: "unknown", PrivateIP: "10.0.0.1", Region: "us-east-1", Profile: "default"},
	}
	if err := UpsertInstances(ctx, db, "default", "us-east-1", second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := GetCachedInstances(ctx, db, "default", "us-east-1")
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

// TestUpsertInstances_StaleInstanceRemovedOnRefresh verifies that an instance
// no longer present in a fresh AWS listing is removed from the cache rather
// than left to linger until TTL expiry.
func TestUpsertInstances_StaleInstanceRemovedOnRefresh(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	seed := []CachedInstance{
		{InstanceID: "i-001", Name: "web", Region: "us-east-1", Profile: "default"},
		{InstanceID: "i-002", Name: "worker", Region: "us-east-1", Profile: "default"},
	}
	if err := UpsertInstances(ctx, db, "default", "us-east-1", seed); err != nil {
		t.Fatalf("seed upsert: %v", err)
	}

	// Refresh: i-002 has been terminated and is no longer returned by AWS.
	refresh := []CachedInstance{
		{InstanceID: "i-001", Name: "web", Region: "us-east-1", Profile: "default"},
	}
	if err := UpsertInstances(ctx, db, "default", "us-east-1", refresh); err != nil {
		t.Fatalf("refresh upsert: %v", err)
	}

	got, err := GetCachedInstances(ctx, db, "default", "us-east-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 instance after refresh, got %d (stale instance was not removed)", len(got))
	}
	if got[0].InstanceID != "i-001" {
		t.Errorf("expected i-001 to remain, got %s", got[0].InstanceID)
	}
}

// TestUpsertInstances_CrossProfileIsolation verifies that upserting instances
// for profile=B does not overwrite or remove the cached rows for profile=A,
// even when both caches contain the same instance_id.
func TestUpsertInstances_CrossProfileIsolation(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	staging := []CachedInstance{
		{InstanceID: "i-001", Name: "stg-web", Region: "us-east-1", Profile: "staging"},
	}
	if err := UpsertInstances(ctx, db, "staging", "us-east-1", staging); err != nil {
		t.Fatalf("staging upsert: %v", err)
	}

	// prod upserts the same instance_id under a different profile.
	prod := []CachedInstance{
		{InstanceID: "i-001", Name: "prod-web", Region: "us-east-1", Profile: "prod"},
	}
	if err := UpsertInstances(ctx, db, "prod", "us-east-1", prod); err != nil {
		t.Fatalf("prod upsert: %v", err)
	}

	got, err := GetCachedInstances(ctx, db, "staging", "us-east-1")
	if err != nil {
		t.Fatalf("get staging: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected staging cache to have 1 instance, got %d (prod upsert wiped staging row)", len(got))
	}
	if got[0].Name != "stg-web" {
		t.Errorf("staging cache was overwritten by prod upsert: got name=%q, want stg-web", got[0].Name)
	}
}
