// Package store owns the manager's SQLite database: schema migrations,
// settings, rules, the inbox/outbox queues and maintenance (backup, purge).
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store wraps the database handle.
type Store struct {
	DB   *sql.DB
	path string
}

// Open opens (creating if needed) the database at path and applies migrations.
// Use ":memory:" for tests.
func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		dsn = "file:" + filepath.ToSlash(path)
	} else {
		dsn = "file::memory:?cache=shared"
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	dsn += sep + "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite allows one writer; a single connection avoids SQLITE_BUSY between
	// our own goroutines and keeps transactions strictly serialized.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{DB: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.DB.Close() }

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

func (s *Store) migrate() error {
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		name := e.Name()
		v, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("bad migration name %q", name)
		}
		var exists int
		if err := s.DB.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version=?`, v).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := s.DB.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(?,?)`, v, time.Now().UnixMilli()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// IntegrityCheck runs PRAGMA integrity_check and returns an error if the
// database is damaged.
func (s *Store) IntegrityCheck(ctx context.Context) error {
	var res string
	if err := s.DB.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return errors.New("integrity check failed: " + res)
	}
	return nil
}

// Backup writes a consistent copy of the database into dir and keeps the
// newest `keep` backups.
func (s *Store) Backup(ctx context.Context, dir string, keep int) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := filepath.Join(dir, "manager-"+time.Now().Format("20060102-150405")+".db")
	if _, err := s.DB.ExecContext(ctx, `VACUUM INTO ?`, name); err != nil {
		return "", err
	}
	files, _ := filepath.Glob(filepath.Join(dir, "manager-*.db"))
	sort.Strings(files)
	for len(files) > keep {
		os.Remove(files[0])
		files = files[1:]
	}
	return name, nil
}

// LatestBackup returns the newest backup file in dir, or "".
func LatestBackup(dir string) string {
	files, _ := filepath.Glob(filepath.Join(dir, "manager-*.db"))
	if len(files) == 0 {
		return ""
	}
	sort.Strings(files)
	return files[len(files)-1]
}

// NowMs is the current time in unix milliseconds. Overridable in tests.
var NowMs = func() int64 { return time.Now().UnixMilli() }
