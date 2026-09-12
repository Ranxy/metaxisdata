package store

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestIsUniqueViolation(t *testing.T) {
	t.Parallel()

	// pgx is the driver the store actually uses.
	require.True(t, isUniqueViolation(&pgconn.PgError{Code: "23505"}))
	require.True(t, isUniqueViolation(errors.Wrap(&pgconn.PgError{Code: "23505"}, "insert user")))
	require.False(t, isUniqueViolation(&pgconn.PgError{Code: "23503"}))

	// lib/pq types may reach the helper through shared code.
	require.True(t, isUniqueViolation(&pq.Error{Code: "23505"}))
	require.False(t, isUniqueViolation(&pq.Error{Code: "23503"}))

	require.False(t, isUniqueViolation(errors.New("boom")))
}
