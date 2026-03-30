package state

import (
	"database/sql"
	"time"
)

const cacheTTL = 5 * time.Minute

// CachedInstance is a row from the instance_cache table.
type CachedInstance struct {
	InstanceID   string
	Name         string
	State        string
	SSMStatus    string
	PrivateIP    string
	AgentVersion string
	Region       string
	Profile      string
	CachedAt     time.Time
}

// GetCachedInstances returns all cached instances for the given profile+region
// that were cached within the TTL window.
func GetCachedInstances(db *sql.DB, profile, region string) ([]CachedInstance, error) {
	cutoff := time.Now().Add(-cacheTTL).Unix()
	rows, err := db.Query(`
		SELECT instance_id, name, state, ssm_status, private_ip, agent_version, region, profile, cached_at
		FROM instance_cache
		WHERE profile = ? AND region = ? AND cached_at >= ?
		ORDER BY name ASC
	`, profile, region, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []CachedInstance
	for rows.Next() {
		var inst CachedInstance
		var cachedAtUnix int64
		if err := rows.Scan(
			&inst.InstanceID, &inst.Name, &inst.State, &inst.SSMStatus,
			&inst.PrivateIP, &inst.AgentVersion, &inst.Region, &inst.Profile,
			&cachedAtUnix,
		); err != nil {
			return nil, err
		}
		inst.CachedAt = time.Unix(cachedAtUnix, 0)
		instances = append(instances, inst)
	}
	return instances, rows.Err()
}

// UpsertInstances replaces the cached instance list for a profile+region.
func UpsertInstances(db *sql.DB, instances []CachedInstance) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO instance_cache
			(instance_id, name, state, ssm_status, private_ip, agent_version, region, profile, cached_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id) DO UPDATE SET
			name=excluded.name, state=excluded.state, ssm_status=excluded.ssm_status,
			private_ip=excluded.private_ip, agent_version=excluded.agent_version,
			region=excluded.region, profile=excluded.profile, cached_at=excluded.cached_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, inst := range instances {
		if _, err := stmt.Exec(
			inst.InstanceID, inst.Name, inst.State, inst.SSMStatus,
			inst.PrivateIP, inst.AgentVersion, inst.Region, inst.Profile, now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}
