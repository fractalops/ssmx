// Package state manages local SQLite state for instance caching.
package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const cacheTTL = 5 * time.Minute

// CachedInstance is a row from the instance_cache table.
type CachedInstance struct {
	InstanceID       string
	Name             string
	State            string
	SSMStatus        string
	PrivateIP        string
	AgentVersion     string
	Region           string
	Profile          string
	PlatformName     string
	AvailabilityZone string
	CachedAt         time.Time
}

// GetCachedInstances returns all cached instances for the given profile+region
// that were cached within the TTL window.
func GetCachedInstances(ctx context.Context, db *sql.DB, profile, region string) ([]CachedInstance, error) {
	cutoff := time.Now().Add(-cacheTTL).Unix()
	rows, err := db.QueryContext(ctx, `
		SELECT instance_id, name, state, ssm_status, private_ip, agent_version,
		       region, profile, cached_at, platform_name, availability_zone
		FROM instance_cache
		WHERE profile = ? AND region = ? AND cached_at >= ?
		ORDER BY name ASC
	`, profile, region, cutoff)
	if err != nil {
		return nil, fmt.Errorf("querying instance cache: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var instances []CachedInstance
	for rows.Next() {
		var inst CachedInstance
		var cachedAtUnix int64
		if err := rows.Scan(
			&inst.InstanceID, &inst.Name, &inst.State, &inst.SSMStatus,
			&inst.PrivateIP, &inst.AgentVersion, &inst.Region, &inst.Profile,
			&cachedAtUnix, &inst.PlatformName, &inst.AvailabilityZone,
		); err != nil {
			return nil, fmt.Errorf("scanning cached instance row: %w", err)
		}
		inst.CachedAt = time.Unix(cachedAtUnix, 0)
		instances = append(instances, inst)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating cached instance rows: %w", err)
	}
	return instances, nil
}

// UpsertInstances replaces the cached instance list for a profile+region.
func UpsertInstances(ctx context.Context, db *sql.DB, instances []CachedInstance) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning upsert transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO instance_cache
			(instance_id, name, state, ssm_status, private_ip, agent_version,
			 region, profile, cached_at, platform_name, availability_zone)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id) DO UPDATE SET
			name=excluded.name, state=excluded.state, ssm_status=excluded.ssm_status,
			private_ip=excluded.private_ip, agent_version=excluded.agent_version,
			region=excluded.region, profile=excluded.profile, cached_at=excluded.cached_at,
			platform_name=excluded.platform_name, availability_zone=excluded.availability_zone
	`)
	if err != nil {
		return fmt.Errorf("preparing upsert statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	now := time.Now().Unix()
	for _, inst := range instances {
		if _, err := stmt.ExecContext(ctx,
			inst.InstanceID, inst.Name, inst.State, inst.SSMStatus,
			inst.PrivateIP, inst.AgentVersion, inst.Region, inst.Profile,
			now, inst.PlatformName, inst.AvailabilityZone,
		); err != nil {
			return fmt.Errorf("upserting instance %s: %w", inst.InstanceID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing upsert transaction: %w", err)
	}
	return nil
}
