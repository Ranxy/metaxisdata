package mysql

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"

	"github.com/go-sql-driver/mysql"
)

// A dropped database is ER_BAD_DB_ERROR. ER_DBACCESS_DENIED_ERROR means the
// database exists but this user may not see it, so it must keep its own meaning:
// only "the database does not exist" is evidence that it left the instance.
func TestDatabaseNotExistError(t *testing.T) {
	t.Parallel()

	notFound := databaseNotExistError("app", &mysql.MySQLError{Number: badDBError})
	require.Equal(t, common.NotFound, common.ErrorCode(notFound))
	require.Contains(t, notFound.Error(), `database "app" not found`)

	denied := databaseNotExistError("app", &mysql.MySQLError{Number: 1044})
	require.Equal(t, common.Internal, common.ErrorCode(denied))

	plain := errors.New("connection refused")
	require.Equal(t, plain, databaseNotExistError("app", plain))
}
