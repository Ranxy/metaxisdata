package starrocks

import (
	"context"

	"github.com/Ranxy/metaxisdata/backend/plugin/db"
	dbmysql "github.com/Ranxy/metaxisdata/backend/plugin/db/mysql"
)

var _ db.ReadOnlyQueryer = (*Driver)(nil)

// QueryReadOnly implements db.ReadOnlyQueryer on the StarRocks/Doris driver.
// These engines do not all implement read-only transactions, so the read-only
// guarantee is the SELECT-only statement the compiler produced plus a read-only
// account; RunReadOnlyQuery passes ReadOnly and falls back when it is refused.
func (d *Driver) QueryReadOnly(ctx context.Context, query string, args []any, opts db.QueryOptions) (*db.QueryResult, error) {
	return db.RunReadOnlyQuery(ctx, d.db, query, args, opts, dbmysql.ReadOnlyQueryHooks(opts.Timeout))
}
