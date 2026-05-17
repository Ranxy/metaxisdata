package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
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
