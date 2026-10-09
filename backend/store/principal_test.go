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

// A language update writes one profile key, so the key has to be the one
// protojson produces for UserProfile.language for the same reason.
func TestUserProfileLanguageKeyMatchesProtojson(t *testing.T) {
	t.Parallel()

	payload, err := protojson.Marshal(&storepb.UserProfile{Language: "zh-CN"})
	require.NoError(t, err)
	require.Contains(t, string(payload), `"`+userProfileLanguageKey+`"`)
}

// A language on its own must be one JSONB key rather than a whole-column write:
// rewriting the column carries the profile this caller read back into the row,
// which resurrects whatever a concurrent write changed in it — including the
// password change time that retires sessions.
func TestProfileAssignmentsWriteLanguageAsOneKey(t *testing.T) {
	t.Parallel()

	language := "zh-CN"

	t.Run("a language on its own writes one key", func(t *testing.T) {
		t.Parallel()
		set, args, err := profileAssignments(nil, nil, &UpdateUserMessage{Language: &language})
		require.NoError(t, err)
		require.Equal(t, []string{"profile = jsonb_set(profile, ARRAY[$1], to_jsonb($2::text))"}, set)
		require.Equal(t, []any{userProfileLanguageKey, "zh-CN"}, args)
	})

	t.Run("the key and its argument are numbered after the caller's", func(t *testing.T) {
		t.Parallel()
		set, args, err := profileAssignments([]string{"phone = $1"}, []any{"+8613800000000"}, &UpdateUserMessage{Language: &language})
		require.NoError(t, err)
		require.Equal(t, []string{"phone = $1", "profile = jsonb_set(profile, ARRAY[$2], to_jsonb($3::text))"}, set)
		require.Equal(t, []any{"+8613800000000", userProfileLanguageKey, "zh-CN"}, args)
	})

	t.Run("a language beside a whole profile is merged into it", func(t *testing.T) {
		t.Parallel()
		set, args, err := profileAssignments(nil, nil, &UpdateUserMessage{
			Profile:  &storepb.UserProfile{Language: "en-US"},
			Language: &language,
		})
		require.NoError(t, err)
		require.Equal(t, []string{"profile = $1"}, set)
		payload, ok := args[0].([]byte)
		require.True(t, ok)
		require.Contains(t, string(payload), `"`+userProfileLanguageKey+`":"zh-CN"`)
		require.NotContains(t, string(payload), "en-US", "the patch's language wins over the profile's")
	})

	t.Run("neither field writes nothing", func(t *testing.T) {
		t.Parallel()
		set, args, err := profileAssignments(nil, nil, &UpdateUserMessage{})
		require.NoError(t, err)
		require.Empty(t, set)
		require.Empty(t, args)
	})
}
