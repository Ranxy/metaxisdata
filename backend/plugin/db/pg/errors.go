package pg

import (
	"errors"

	"github.com/Ranxy/metaxisdata/backend/common"

	"github.com/jackc/pgx/v5/pgconn"
)

// invalidCatalogName is PostgreSQL's SQLSTATE for "database does not exist".
const invalidCatalogName = "3D000"

// databaseNotExistError maps the engine's own "this database does not exist"
// error onto common.NotFound. Opening a driver is lazy, so a dropped database
// fails on the first query with a connect error carrying this SQLSTATE rather
// than the caller's own catalog check; a caller that mirrors the target needs
// to tell that apart from a permission or connectivity failure, which is not
// evidence that the database is gone.
func databaseNotExistError(databaseName string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == invalidCatalogName {
		return common.Errorf(common.NotFound, "database %q not found", databaseName)
	}
	return err
}
