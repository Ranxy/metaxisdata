package mssql

import (
	"context"

	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

var _ db.ReadOnlyQueryer = (*Driver)(nil)

// QueryReadOnly implements db.ReadOnlyQueryer on the MSSQL driver. MSSQL has no
// read-only transaction (its driver refuses sql.TxOptions.ReadOnly), so
// RunReadOnlyQuery falls back to a plain transaction: the read-only guarantee
// is the SELECT-only statement the compiler produced plus a read-only account,
// as the design records.
func (d *Driver) QueryReadOnly(ctx context.Context, query string, args []any, opts db.QueryOptions) (*db.QueryResult, error) {
	return db.RunReadOnlyQuery(ctx, d.db, query, args, opts, db.ReadOnlyQueryHooks{})
}
