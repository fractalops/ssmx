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
	_, execErr := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS instance_cache (
			instance_id   TEXT PRIMARY KEY,
			name          TEXT NOT NULL DEFAULT '',
			state         TEXT NOT NULL DEFAULT '',
			ssm_status    TEXT NOT NULL DEFAULT '',
			private_ip    TEXT NOT NULL DEFAULT '',
			agent_version TEXT NOT NULL DEFAULT '',
			region        TEXT NOT NULL DEFAULT '',
			profile       TEXT NOT NULL DEFAULT '',
			cached_at         INTEGER NOT NULL,
			platform_name     TEXT NOT NULL DEFAULT '',
			availability_zone TEXT NOT NULL DEFAULT ''
		);

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
	`)
	if execErr != nil {
		return fmt.Errorf("running migrations: %w", execErr)
	}
	// Add platform_name to existing installations that predate this column.
	if err := addColumnIfMissing(ctx, db, "instance_cache", "platform_name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return addColumnIfMissing(ctx, db, "instance_cache", "availability_zone", "TEXT NOT NULL DEFAULT ''")
}

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
