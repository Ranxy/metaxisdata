package pg

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"

	"github.com/jackc/pgx/v5/pgconn"
)

// A dropped database is PostgreSQL's invalid_catalog_name. Every other failure —
// a permission error in particular — must keep its own meaning, because only
// "the database does not exist" is evidence that it left the instance.
func TestDatabaseNotExistError(t *testing.T) {
	t.Parallel()

	notFound := databaseNotExistError("app", &pgconn.PgError{Code: invalidCatalogName})
	require.Equal(t, common.NotFound, common.ErrorCode(notFound))
	require.Contains(t, notFound.Error(), `database "app" not found`)

	denied := databaseNotExistError("app", &pgconn.PgError{Code: "42501"})
	require.Equal(t, common.Internal, common.ErrorCode(denied))

	plain := errors.New("connection refused")
	require.Equal(t, plain, databaseNotExistError("app", plain))
}
