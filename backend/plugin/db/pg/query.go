package pg

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

var _ db.ReadOnlyQueryer = (*Driver)(nil)

// QueryReadOnly implements db.ReadOnlyQueryer on the PostgreSQL driver.
// Connections opened with ConnectionContext.ReadOnly already carry
// default_transaction_read_only=true and the transaction itself is read-only as
// well, so either guard refuses a write.
func (d *Driver) QueryReadOnly(ctx context.Context, query string, args []any, opts db.QueryOptions) (*db.QueryResult, error) {
	var hooks db.ReadOnlyQueryHooks
	timeoutMillis := opts.Timeout.Milliseconds()
	if opts.Timeout > 0 && timeoutMillis > 0 {
		hooks.Prepare = func(ctx context.Context, tx *sql.Tx) error {
			// SET does not accept bind parameters; the value is an integer
			// derived from a Duration, so formatting it is safe. SET LOCAL keeps
			// the limit on this transaction and leaves the pooled connection
			// clean.
			_, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", timeoutMillis))
			return err
		}
	}
	return db.RunReadOnlyQuery(ctx, d.db, query, args, opts, hooks)
}
