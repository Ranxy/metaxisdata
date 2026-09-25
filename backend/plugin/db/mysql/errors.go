package mysql

import (
	"errors"

	"github.com/Ranxy/metaxisdata/backend/common"

	"github.com/go-sql-driver/mysql"
)

// badDBError is MySQL's ER_BAD_DB_ERROR: the database named by the connection or
// the query does not exist. ER_DBACCESS_DENIED_ERROR (1044) means the opposite —
// the database exists but this user may not see it — so only 1049 is mapped.
const badDBError = 1049

// databaseNotExistError maps the engine's own "this database does not exist"
// error onto common.NotFound. Opening a driver is lazy, so a dropped database
// fails on the first query with this error rather than the caller's own catalog
// check; a caller that mirrors the target needs to tell it apart from a
// permission or connectivity failure, which is not evidence the database is gone.
func databaseNotExistError(databaseName string, err error) error {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == badDBError {
		return common.Errorf(common.NotFound, "database %q not found", databaseName)
	}
	return err
}
