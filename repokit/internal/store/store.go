package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Store manages the SQLite database for the repokit index.
type Store struct {
	db     *sql.DB
	dbPath string
}

// Open creates or opens the SQLite database at the given path.
func Open(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating database directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	s := &Store{db: db, dbPath: dbPath}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying sql.DB for advanced queries.
func (s *Store) DB() *sql.DB {
	return s.db
}

// BeginTx starts a transaction and returns a TxStore that uses it.
func (s *Store) BeginTx(ctx context.Context) (*TxStore, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	return &TxStore{tx: tx, store: s}, nil
}

// TxStore wraps a Store with an active transaction.
type TxStore struct {
	tx    *sql.Tx
	store *Store
}

// Commit commits the transaction.
func (ts *TxStore) Commit() error {
	return ts.tx.Commit()
}

// Rollback rolls back the transaction.
func (ts *TxStore) Rollback() error {
	return ts.tx.Rollback()
}

// Tx returns the underlying transaction for direct use.
func (ts *TxStore) Tx() *sql.Tx {
	return ts.tx
}

// Checkpoint forces a WAL checkpoint for better read performance after bulk writes.
func (s *Store) Checkpoint() error {
	_, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

func (s *Store) migrate() error {
	// Check current schema version
	var currentVersion string
	err := s.db.QueryRow("SELECT value FROM meta WHERE key = 'schema_version'").Scan(&currentVersion)
	if err == nil && currentVersion == schemaVersion {
		return nil // already up to date
	}

	// Run schema creation (all IF NOT EXISTS, safe to re-run)
	statements := strings.Split(schemaSQL, ";")
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("executing migration statement: %w\nSQL: %s", err, stmt)
		}
	}

	// Set schema version
	_, err = s.db.Exec(
		"INSERT OR REPLACE INTO meta (key, value) VALUES ('schema_version', ?)",
		schemaVersion,
	)
	return err
}
