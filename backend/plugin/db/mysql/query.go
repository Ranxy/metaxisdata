package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

var _ db.ReadOnlyQueryer = (*Driver)(nil)

// QueryReadOnly implements db.ReadOnlyQueryer on the MySQL driver. The driver
// sends START TRANSACTION READ ONLY, but the MySQL-wire engines do not all
// implement it; RunReadOnlyQuery falls back to a plain transaction there, and a
// read-only account remains the guard.
func (d *Driver) QueryReadOnly(ctx context.Context, query string, args []any, opts db.QueryOptions) (*db.QueryResult, error) {
	return db.RunReadOnlyQuery(ctx, d.db, query, args, opts, ReadOnlyQueryHooks(opts.Timeout))
}

// ReadOnlyQueryHooks returns the hooks that bound a SELECT server-side on the
// MySQL-wire family. A server that does not know MAX_EXECUTION_TIME ignores the
// statement, in which case the client deadline still applies. StarRocks and
// Doris share this helper because they speak the same protocol.
func ReadOnlyQueryHooks(timeout time.Duration) db.ReadOnlyQueryHooks {
	timeoutMillis := timeout.Milliseconds()
	if timeout <= 0 || timeoutMillis <= 0 {
		return db.ReadOnlyQueryHooks{}
	}
	return db.ReadOnlyQueryHooks{
		Prepare: func(ctx context.Context, tx *sql.Tx) error {
			_, _ = tx.ExecContext(ctx, fmt.Sprintf("SET SESSION MAX_EXECUTION_TIME = %d", timeoutMillis))
			return nil
		},
		Cleanup: func(ctx context.Context, tx *sql.Tx) {
			// MAX_EXECUTION_TIME is session-scoped, so leaving it set would cap
			// later queries that reuse this pooled connection.
			_, _ = tx.ExecContext(ctx, "SET SESSION MAX_EXECUTION_TIME = 0")
		},
	}
}
