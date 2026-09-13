package v1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Login records LastLoginTime by patching a clone of the profile. The previous
// implementation rebuilt the column from two fields, which silently dropped any
// other field the profile gained, and it wrote through the profile the store
// caches.
func TestProfileWithLastLogin(t *testing.T) {
	t.Parallel()

	lastChange := timestamppb.New(time.Now().Add(-time.Hour))
	current := &storepb.UserProfile{LastChangePasswordTime: lastChange}

	patched := profileWithLastLogin(current)
	require.NotNil(t, patched.LastLoginTime)
	require.Equal(t, lastChange.AsTime(), patched.LastChangePasswordTime.AsTime())
	require.True(t, patched.LastLoginTime.AsTime().After(time.Now().Add(-time.Minute)))

	// The caller's profile — potentially the cached entry — is untouched.
	require.Nil(t, current.LastLoginTime)
	require.NotSame(t, current, patched)

	// A user with no profile yet gets one.
	require.NotNil(t, profileWithLastLogin(nil).LastLoginTime)
}
