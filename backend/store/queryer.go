package store

import (
	"context"
	"database/sql"
)

// Queryer is the read half of the store's database handle: *sql.DB and *sql.Tx
// both implement it. A read helper takes it so the same SQL can run on the pool
// — a single statement needs no transaction — or inside a caller's transaction,
// without a second copy of the query.
type Queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
