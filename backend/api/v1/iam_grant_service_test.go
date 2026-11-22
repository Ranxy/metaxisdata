package v1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/resource"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestParseIamGrantResource(t *testing.T) {
	t.Parallel()

	resourceType, resourceID, err := parseIamGrantResource("instances/inst-1")
	require.NoError(t, err)
	require.Equal(t, resource.TypeInstance.String(), resourceType)
	require.Equal(t, "inst-1", resourceID)

	resourceType, resourceID, err = parseIamGrantResource("instances/inst-1/databases/sales")
	require.NoError(t, err)
	require.Equal(t, resource.TypeDatabase.String(), resourceType)
	require.Equal(t, common.BuildMetaGUID("inst-1", "sales"), resourceID)

	// A database name containing the GUID separator round-trips through the
	// grant record and back to the resource name.
	escaped := common.BuildMetaGUID("inst-1", "a;b")
	resourceType, resourceID, err = parseIamGrantResource("instances/inst-1/databases/a;b")
	require.NoError(t, err)
	require.Equal(t, resource.TypeDatabase.String(), resourceType)
	require.Equal(t, escaped, resourceID)

	for _, invalid := range []string{"", "instances/inst-1/databases/sales/extra", "databases/sales", "instances"} {
		_, _, err := parseIamGrantResource(invalid)
		require.Error(t, err, "scope %q must be refused", invalid)
	}
}

func TestFormatIamGrantResource(t *testing.T) {
	t.Parallel()

	require.Equal(t, "instances/inst-1", formatIamGrantResource(&store.IamGrantMessage{
		ResourceType: resource.TypeInstance.String(),
		ResourceID:   "inst-1",
	}))
	require.Equal(t, "instances/inst-1/databases/sales", formatIamGrantResource(&store.IamGrantMessage{
		ResourceType: resource.TypeDatabase.String(),
		ResourceID:   common.BuildMetaGUID("inst-1", "sales"),
	}))
	require.Equal(t, "a;b;c;d", formatIamGrantResource(&store.IamGrantMessage{
		ResourceType: resource.TypeTable.String(),
		ResourceID:   "a;b;c;d",
	}))
}

func TestConvertIamGrant(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 11, 22, 6, 0, 0, 0, time.UTC)
	createdBy := int32(7)
	view := convertIamGrant(&store.IamGrantMessage{
		ID:           42,
		ResourceType: resource.TypeDatabase.String(),
		ResourceID:   common.BuildMetaGUID("inst-1", "sales"),
		Role:         "roles/qualityOwner",
		Member:       "users/7",
		Condition:    "request.time > timestamp('2000-01-01T00:00:00Z')",
		CreatedBy:    &createdBy,
		CreatedAt:    createdAt,
	})
	require.Equal(t, "iamGrants/42", view.GetName())
	require.Equal(t, "instances/inst-1/databases/sales", view.GetResource())
	require.Equal(t, "roles/qualityOwner", view.GetRole())
	require.Equal(t, "users/7", view.GetMember())
	require.Equal(t, "request.time > timestamp('2000-01-01T00:00:00Z')", view.GetCondition().GetExpression())
	require.Equal(t, "users/7", view.GetCreatedBy())
	require.Equal(t, createdAt, view.GetCreateTime().AsTime())

	require.Nil(t, convertIamGrant(nil))
}
