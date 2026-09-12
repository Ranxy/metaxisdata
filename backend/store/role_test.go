package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// TestMarshalRolePermissionsIsDeterministic pins the payload invariant: equal
// permission sets encode identically regardless of map iteration order, so a
// role update that changes nothing produces no spurious JSONB diff.
func TestMarshalRolePermissionsIsDeterministic(t *testing.T) {
	t.Parallel()

	first, err := marshalRolePermissions(map[permission.Permission]bool{
		permission.UsersDelete:  true,
		permission.InstancesGet: true,
		permission.RolesList:    true,
	})
	require.NoError(t, err)
	second, err := marshalRolePermissions(map[permission.Permission]bool{
		permission.RolesList:    true,
		permission.UsersDelete:  true,
		permission.InstancesGet: true,
	})
	require.NoError(t, err)
	require.Equal(t, string(first), string(second))
	require.Equal(t, `{"permissions":["metaxisdata.instances.get","metaxisdata.roles.list","metaxisdata.users.delete"]}`, string(first))
}

func TestMarshalRolePermissionsEmpty(t *testing.T) {
	t.Parallel()

	payload, err := marshalRolePermissions(nil)
	require.NoError(t, err)
	require.Equal(t, `{}`, string(payload))
}

// TestMarshalRolePermissionsRoundTrip guards the JSONB encoding against the
// protojson field naming the store relies on.
func TestMarshalRolePermissionsRoundTrip(t *testing.T) {
	t.Parallel()

	payload, err := marshalRolePermissions(map[permission.Permission]bool{permission.IAMSetPolicy: true})
	require.NoError(t, err)

	var decoded storepb.RolePermissions
	require.NoError(t, common.ProtojsonUnmarshaler.Unmarshal(payload, &decoded))
	require.Equal(t, []string{permission.IAMSetPolicy}, decoded.GetPermissions())
}

func TestEtagMismatch(t *testing.T) {
	t.Parallel()

	require.False(t, etagMismatch("", ""), "no stored policy: a first write always passes")
	require.False(t, etagMismatch("100", ""), "an empty provided etag skips the check")
	require.False(t, etagMismatch("100", "100"))
	require.True(t, etagMismatch("100", "99"))
	require.True(t, etagMismatch("", "100"))
}
