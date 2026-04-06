package state

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fractalops/ssmx/internal/config"
	_ "modernc.org/sqlite"
)

// Open returns an open SQLite database at ~/.ssmx/state.db, creating it and
// running migrations if it doesn't exist.
func Open() (*sql.DB, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
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
		db.Close()
		return nil, fmt.Errorf("migrating state db: %w", err)
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
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
	if err != nil {
		return err
	}
	// Add platform_name to existing installations that predate this column.
	if err := addColumnIfMissing(db, "instance_cache", "platform_name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return addColumnIfMissing(db, "instance_cache", "availability_zone", "TEXT NOT NULL DEFAULT ''")
}

func addColumnIfMissing(db *sql.DB, table, column, def string) error {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil // already present
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + def)
	return err
}
