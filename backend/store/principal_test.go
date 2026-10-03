package store

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
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

// RecordLastLogin stamps the profile with a targeted JSONB write, so the key it
// sets has to be the one protojson produces for UserProfile.last_login_time.
// Renaming the proto field without this constant would silently stop recording
// logins instead of failing.
func TestUserProfileLastLoginKeyMatchesProtojson(t *testing.T) {
	t.Parallel()

	payload, err := protojson.Marshal(&storepb.UserProfile{LastLoginTime: timestamppb.New(time.Now())})
	require.NoError(t, err)
	require.Contains(t, string(payload), `"`+userProfileLastLoginKey+`"`)
}
