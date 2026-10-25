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

// serverSideTimeoutGrace is how much later than the client deadline the
// server-side statement cap is armed.
//
// Both timers derive from QueryOptions.Timeout, so arming them at the same
// instant makes the timeout report a race. A SELECT that reads no table turns a
// MAX_EXECUTION_TIME interrupt into a successful row instead of an error (MySQL
// reports an interrupted SLEEP as 1), so a server cap that fires first hands the
// caller a truncated result where the timeout contract promises a deadline
// error. Arming it later makes the client deadline the authority: it cancels the
// statement's connection and surfaces context.DeadlineExceeded, while the cap
// still bounds the statement's server-side work when the client never observes
// its own deadline.
const serverSideTimeoutGrace = time.Second

// readOnlyServerCapMillis is the MAX_EXECUTION_TIME value for a client-side
// timeout: strictly larger than the client deadline, as serverSideTimeoutGrace
// explains.
func readOnlyServerCapMillis(timeout time.Duration) int64 {
	return timeout.Milliseconds() + serverSideTimeoutGrace.Milliseconds()
}

// ReadOnlyQueryHooks returns the hooks that bound a SELECT server-side on the
// MySQL-wire family. The server cap is only a backstop: the client deadline is
// what reports the timeout, so the cap is armed strictly after it. A server that
// does not know MAX_EXECUTION_TIME ignores the statement, in which case the
// client deadline is the only guard. StarRocks and Doris share this helper
// because they speak the same protocol.
func ReadOnlyQueryHooks(timeout time.Duration) db.ReadOnlyQueryHooks {
	timeoutMillis := timeout.Milliseconds()
	if timeout <= 0 || timeoutMillis <= 0 {
		return db.ReadOnlyQueryHooks{}
	}
	serverTimeoutMillis := readOnlyServerCapMillis(timeout)
	return db.ReadOnlyQueryHooks{
		Prepare: func(ctx context.Context, tx *sql.Tx) error {
			_, _ = tx.ExecContext(ctx, fmt.Sprintf("SET SESSION MAX_EXECUTION_TIME = %d", serverTimeoutMillis))
			return nil
		},
		Cleanup: func(ctx context.Context, tx *sql.Tx) {
			// MAX_EXECUTION_TIME is session-scoped, so leaving it set would cap
			// later queries that reuse this pooled connection.
			_, _ = tx.ExecContext(ctx, "SET SESSION MAX_EXECUTION_TIME = 0")
		},
	}
}
