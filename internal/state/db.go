package state

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fractalops/ssmx/internal/config"
	_ "modernc.org/sqlite" // imported for SQLite driver side effects
)

// Open returns an open SQLite database at ~/.ssmx/state.db, creating it and
// running migrations if it doesn't exist.
func Open() (*sql.DB, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, fmt.Errorf("resolving config dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating state dir: %w", err)
	}

	path := filepath.Join(dir, "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening state db: %w", err)
	}

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrating state db: %w", err)
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	ctx := context.Background()

	// Create ancillary tables that have never changed schema.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS session_history (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			instance_id TEXT NOT NULL,
			name        TEXT NOT NULL DEFAULT '',
			profile     TEXT NOT NULL DEFAULT '',
			region      TEXT NOT NULL DEFAULT '',
			connected_at INTEGER NOT NULL,
			count       INTEGER NOT NULL DEFAULT 1
		);
		CREATE TABLE IF NOT EXISTS fix_history (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			instance_id  TEXT NOT NULL,
			action_type  TEXT NOT NULL,
			resource_ids TEXT NOT NULL DEFAULT '',
			prev_state   TEXT NOT NULL DEFAULT '',
			created_at   INTEGER NOT NULL
		);
	`); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	// instance_cache is managed separately so we can evolve its schema.
	return migrateInstanceCache(ctx, db)
}

// migrateInstanceCache ensures instance_cache exists with the correct schema.
//
// Schema history:
//
//	v1 — instance_id TEXT PRIMARY KEY (sole key)
//	v2 — PRIMARY KEY (instance_id, profile, region) composite key
//
// New installs land on v2 directly. Existing v1 installs are rebuilt in-place;
// the cache is ephemeral (5-minute TTL) so data loss is inconsequential.
func migrateInstanceCache(ctx context.Context, db *sql.DB) error {
	// Count how many columns are part of the PRIMARY KEY.
	// v1 has 1 PK column; v2 has 3.
	rows, err := db.QueryContext(ctx, "PRAGMA table_info(instance_cache)")
	if err != nil {
		return fmt.Errorf("checking instance_cache schema: %w", err)
	}
	pkCount := 0
	exists := false
	for rows.Next() {
		exists = true
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scanning table_info: %w", err)
		}
		if pk > 0 {
			pkCount++
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("closing table_info cursor: %w", err)
	}

	if !exists {
		// Fresh install: create directly with v2 schema.
		if _, err = db.ExecContext(ctx, instanceCacheCreateDDL); err != nil {
			return fmt.Errorf("creating instance_cache table: %w", err)
		}
		return nil
	}

	if pkCount == 3 {
		// Already on v2; nothing to do.
		return nil
	}

	// v1 detected: rebuild with composite PK.
	// Data loss is acceptable — the cache has a 5-minute TTL.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning instance_cache migration: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	for _, stmt := range []string{
		instanceCacheNewDDL,
		`INSERT OR IGNORE INTO instance_cache_new SELECT * FROM instance_cache`,
		`DROP TABLE instance_cache`,
		`ALTER TABLE instance_cache_new RENAME TO instance_cache`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrating instance_cache to composite PK: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing instance_cache migration: %w", err)
	}

	// Add columns that predate platform_name / availability_zone (no-ops on v2).
	if err := addColumnIfMissing(ctx, db, "instance_cache", "platform_name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return addColumnIfMissing(ctx, db, "instance_cache", "availability_zone", "TEXT NOT NULL DEFAULT ''")
}

// instanceCacheCreateDDL creates instance_cache with the v2 composite PK for new installs.
const instanceCacheCreateDDL = `
	CREATE TABLE instance_cache (
		instance_id       TEXT NOT NULL,
		name              TEXT NOT NULL DEFAULT '',
		state             TEXT NOT NULL DEFAULT '',
		ssm_status        TEXT NOT NULL DEFAULT '',
		private_ip        TEXT NOT NULL DEFAULT '',
		agent_version     TEXT NOT NULL DEFAULT '',
		region            TEXT NOT NULL DEFAULT '',
		profile           TEXT NOT NULL DEFAULT '',
		cached_at         INTEGER NOT NULL DEFAULT 0,
		platform_name     TEXT NOT NULL DEFAULT '',
		availability_zone TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (instance_id, profile, region)
	)
`

// instanceCacheNewDDL creates a staging table used during v1→v2 migration.
const instanceCacheNewDDL = `
	CREATE TABLE instance_cache_new (
		instance_id       TEXT NOT NULL,
		name              TEXT NOT NULL DEFAULT '',
		state             TEXT NOT NULL DEFAULT '',
		ssm_status        TEXT NOT NULL DEFAULT '',
		private_ip        TEXT NOT NULL DEFAULT '',
		agent_version     TEXT NOT NULL DEFAULT '',
		region            TEXT NOT NULL DEFAULT '',
		profile           TEXT NOT NULL DEFAULT '',
		cached_at         INTEGER NOT NULL DEFAULT 0,
		platform_name     TEXT NOT NULL DEFAULT '',
		availability_zone TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (instance_id, profile, region)
	)
`

func addColumnIfMissing(ctx context.Context, db *sql.DB, table, column, def string) error {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return fmt.Errorf("querying table_info for %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scanning table_info row for %s: %w", table, err)
		}
		if name == column {
			return nil // already present
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating table_info rows for %s: %w", table, err)
	}
	if _, err := db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+def); err != nil {
		return fmt.Errorf("adding column %s to %s: %w", column, table, err)
	}
	return nil
}
