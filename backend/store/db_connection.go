package store

import (
	"context"
	"database/sql"
	"sync"
	"time"

	// Registers the "pgx" driver used by sql.Open below.
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/pkg/errors"
)

const (
	// maxOpenConnsCap keeps a single server from claiming too much of the
	// database's connection budget.
	maxOpenConnsCap = 50
	// maxIdleConns keeps a pool of warm connections for bursts; the database/sql
	// default of 2 forces frequent reconnects under concurrency.
	maxIdleConns = 10
	// connMaxLifetime recycles connections so a stale one behind a NAT or a
	// connection pooler (PgBouncer) is not reused indefinitely.
	connMaxLifetime = 30 * time.Minute
	// connMaxIdleTime releases connections that have been idle for a while.
	connMaxIdleTime = 5 * time.Minute
)

type DBConnectionManager struct {
	db      *sql.DB
	pgURL   string
	init    sync.Once
	initErr error
}

func NewDBConnectionManager(pgURL string) *DBConnectionManager {
	return &DBConnectionManager{pgURL: pgURL}
}

// Initialize opens the connection pool. It is safe to call more than once and
// from multiple goroutines: without the guard two callers could each open a
// pool and leak one of them.
func (m *DBConnectionManager) Initialize(ctx context.Context) error {
	m.init.Do(func() {
		if m.pgURL == "" {
			m.initErr = errors.New("database URL is not provided")
			return
		}
		db, err := createConnection(ctx, m.pgURL)
		if err != nil {
			m.initErr = err
			return
		}
		m.db = db
	})
	return m.initErr
}

// GetDB returns the current database connection. It is nil until Initialize
// succeeds.
func (m *DBConnectionManager) GetDB() *sql.DB {
	return m.db
}

func (m *DBConnectionManager) Close() error {
	if m.db != nil {
		return m.db.Close()
	}
	return nil
}

func createConnection(ctx context.Context, pgURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", pgURL)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open database connection")
	}

	// Validate connection
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, errors.Wrap(err, "failed to ping database")
	}

	// Configure connection pool
	var maxConns, reservedConns int
	if err := db.QueryRowContext(ctx, `SHOW max_connections`).Scan(&maxConns); err != nil {
		_ = db.Close()
		return nil, errors.Wrap(err, "failed to get max_connections")
	}
	if err := db.QueryRowContext(ctx, `SHOW superuser_reserved_connections`).Scan(&reservedConns); err != nil {
		_ = db.Close()
		return nil, errors.Wrap(err, "failed to get superuser_reserved_connections")
	}

	// A misconfigured server can report max_connections <= reserved, and
	// database/sql treats 0 (or a negative value) as "unlimited", so clamp to at
	// least one.
	db.SetMaxOpenConns(clampMaxOpenConns(maxConns, reservedConns))
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(connMaxLifetime)
	db.SetConnMaxIdleTime(connMaxIdleTime)

	return db, nil
}

// clampMaxOpenConns derives the pool size from the server's connection budget,
// capped and floored at one so it can never be interpreted as "unlimited".
func clampMaxOpenConns(maxConns, reservedConns int) int {
	return max(1, min(maxConns-reservedConns, maxOpenConnsCap))
}
