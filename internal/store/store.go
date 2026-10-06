// Package store persists configuration, run history and snapshot metadata in SQLite.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"s3freeze/internal/secret"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db  *sql.DB
	box *secret.Box
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id            INTEGER PRIMARY KEY,
	username      TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	created_at    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
	token      TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS storages (
	id         INTEGER PRIMARY KEY,
	name       TEXT NOT NULL,
	type       TEXT NOT NULL,
	endpoint   TEXT NOT NULL DEFAULT '',
	region     TEXT NOT NULL DEFAULT '',
	access_key TEXT NOT NULL DEFAULT '',
	secret_key TEXT NOT NULL DEFAULT '',
	use_ssl    INTEGER NOT NULL DEFAULT 1,
	path_style INTEGER NOT NULL DEFAULT 1,
	local_path TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS jobs (
	id                INTEGER PRIMARY KEY,
	name              TEXT NOT NULL,
	enabled           INTEGER NOT NULL DEFAULT 1,
	schedule          TEXT NOT NULL DEFAULT '',
	source_storage_id INTEGER NOT NULL REFERENCES storages(id),
	source_bucket     TEXT NOT NULL DEFAULT '',
	source_prefix     TEXT NOT NULL DEFAULT '',
	dest_storage_id   INTEGER NOT NULL REFERENCES storages(id),
	dest_bucket       TEXT NOT NULL DEFAULT '',
	dest_prefix       TEXT NOT NULL DEFAULT '',
	compression       INTEGER NOT NULL DEFAULT 1,
	encryption        INTEGER NOT NULL DEFAULT 0,
	passphrase        TEXT NOT NULL DEFAULT '',
	concurrency       INTEGER NOT NULL DEFAULT 4,
	keep_last         INTEGER NOT NULL DEFAULT 7,
	keep_days         INTEGER NOT NULL DEFAULT 0,
	created_at        INTEGER NOT NULL,
	updated_at        INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS runs (
	id             INTEGER PRIMARY KEY,
	job_id         INTEGER REFERENCES jobs(id) ON DELETE CASCADE,
	kind           TEXT NOT NULL,
	status         TEXT NOT NULL,
	detail         TEXT NOT NULL DEFAULT '',
	started_at     INTEGER NOT NULL,
	finished_at    INTEGER,
	objects_total  INTEGER NOT NULL DEFAULT 0,
	objects_done   INTEGER NOT NULL DEFAULT 0,
	objects_failed INTEGER NOT NULL DEFAULT 0,
	bytes_total    INTEGER NOT NULL DEFAULT 0,
	bytes_done     INTEGER NOT NULL DEFAULT 0,
	bytes_uploaded INTEGER NOT NULL DEFAULT 0,
	snapshot_id    TEXT NOT NULL DEFAULT '',
	error          TEXT NOT NULL DEFAULT '',
	log            TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS runs_job ON runs(job_id, id DESC);
CREATE TABLE IF NOT EXISTS snapshots (
	job_id      INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
	id          TEXT NOT NULL,
	run_id      INTEGER NOT NULL DEFAULT 0,
	created_at  INTEGER NOT NULL, -- unix milliseconds
	objects     INTEGER NOT NULL DEFAULT 0,
	size        INTEGER NOT NULL DEFAULT 0,
	added_bytes INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (job_id, id)
);
CREATE INDEX IF NOT EXISTS snapshots_job_time ON snapshots(job_id, created_at DESC);
`

func Open(path string, box *secret.Box) (*Store, error) {
	dsn := path + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	if err := addColumn(db, "storages", "builtin", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	return &Store{db: db, box: box}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping() error { return s.db.Ping() }

func now() int64 { return time.Now().Unix() }

func toTime(v int64) time.Time { return time.Unix(v, 0).UTC() }

func toTimePtr(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := toTime(v.Int64)
	return &t
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// addColumn adds a column to an existing table if it is missing.
func addColumn(db *sql.DB, table, column, def string) error {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	rows.Close()
	_, err = db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, def))
	return err
}
