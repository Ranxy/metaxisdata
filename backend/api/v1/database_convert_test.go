package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestConvertStoredMetadataMessageColumnMetadata(t *testing.T) {
	t.Parallel()

	converted := convertStoredMetadataMessage(&storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_ColumnMetadata{
			ColumnMetadata: &storepb.ColumnMetadata{
				Name:        "customer_email",
				Comment:     "contains pii",
				UserComment: "sensitive contact channel",
				Type:        "text",
			},
		},
	})

	require.NotNil(t, converted)
	column := converted.GetColumnMetadata()
	require.NotNil(t, column)
	require.Equal(t, "customer_email", column.Name)
	require.Equal(t, "contains pii", column.Comment)
	require.Equal(t, "sensitive contact channel", column.UserComment)
	require.Equal(t, "text", column.Type)
}

// A page of databases is rendered with one instance lookup: the IDs are
// deduplicated so many databases on one instance do not each cost a query.
func TestDistinctInstanceIDs(t *testing.T) {
	t.Parallel()

	require.Empty(t, distinctInstanceIDs(nil))

	ids := distinctInstanceIDs([]*store.DatabaseMessage{
		{InstanceID: "inst-a", DatabaseName: "db1"},
		{InstanceID: "inst-b", DatabaseName: "db2"},
		{InstanceID: "inst-a", DatabaseName: "db3"},
	})
	require.Equal(t, []string{"inst-a", "inst-b"}, ids)
}

// The conversion no longer resolves the instance itself, so it works on the
// instance the caller batched.
func TestConvertToDatabaseUsesTheGivenInstance(t *testing.T) {
	t.Parallel()

	database := convertToDatabase(&store.DatabaseMessage{
		InstanceID:   "inst-a",
		DatabaseName: "db1",
		Metadata:     &storepb.DatabaseMetadata{Version: "1.2.3"},
	}, &store.InstanceMessage{ResourceID: "inst-a", Metadata: &storepb.Instance{Title: "prod", Engine: storepb.Engine_POSTGRES}})

	require.Equal(t, "instances/inst-a/databases/db1", database.Name)
	require.Equal(t, "1.2.3", database.SchemaVersion)
	require.Equal(t, "instances/inst-a", database.InstanceResource.Name)
}
