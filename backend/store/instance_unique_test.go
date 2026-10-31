package store

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// TestMapCreateInstanceError keeps the duplicate-id contract at the store
// boundary: the instance id is caller-chosen, so a collision must reach the API
// as AlreadyExists rather than as an opaque 500.
func TestMapCreateInstanceError(t *testing.T) {
	t.Parallel()

	err := mapCreateInstanceError("instance-1", &pgconn.PgError{Code: "23505"})
	require.Error(t, err)
	require.Equal(t, common.Conflict, common.ErrorCode(err))
	require.Contains(t, err.Error(), "instance-1")

	other := errors.New("connection reset")
	require.Equal(t, other, mapCreateInstanceError("instance-1", other),
		"a non-uniqueness error must pass through untouched")
}
