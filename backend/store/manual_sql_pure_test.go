package store

import (
	"testing"

	"github.com/stretchr/testify/require"

)

func TestBuildManualSQLGUID(t *testing.T) {
	t.Parallel()

	guid := buildManualSQLGUID("instance_1", "db1", "public", "top-orders")
	require.Equal(t, "instance_1;db1;public;__manual_sql__/top-orders", guid)
	// The manual SQL segment must keep its prefix so it cannot collide with a
	// schema object of the same name.
	require.NotEqual(t, guid, buildManualSQLGUID("instance_1", "db1", "public", "orders"))
}

func TestNormalizeManualSQLTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, nil},
		{"only blanks", []string{"", "   "}, nil},
		{"trims", []string{"  finance  "}, []string{"finance"}},
		{"dedupes case-insensitively keeping the first spelling", []string{"Finance", "finance", "FINANCE"}, []string{"Finance"}},
		{"sorts by normalized key", []string{"zebra", "Alpha", "beta"}, []string{"Alpha", "beta", "zebra"}},
		{"drops blanks among values", []string{" ", "b", "", "a"}, []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, normalizeManualSQLTags(tt.in))
		})
	}
}

func TestNormalizeManualSQLAttributes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   map[string]string
		want map[string]string
	}{
		{"empty", nil, nil},
		{"only blank keys", map[string]string{" ": "v"}, nil},
		{"trims keys and values", map[string]string{" owner ": "  alice "}, map[string]string{"owner": "alice"}},
		{"keeps empty values", map[string]string{"owner": ""}, map[string]string{"owner": ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, normalizeManualSQLAttributes(tt.in))
		})
	}
}

func TestBuildManualSQLStoredMetadata(t *testing.T) {
	t.Parallel()

	metadata := buildManualSQLStoredMetadata(&ManualSQLMessage{
		ManualSQLID:        "top-orders",
		Name:               "Top orders",
		Title:              "Top orders by amount",
		Comment:            "used by the dashboard",
		SQLText:            "SELECT 1",
		SchemaName:         "public",
		InstanceResourceID: "instance_1",
		DatabaseName:       "db1",
		Tags:               []string{" Finance ", "finance"},
		Attributes:         map[string]string{" owner ": " alice "},
	})

	manualSQL := metadata.GetManualSqlMetadata()
	require.NotNil(t, manualSQL)
	require.Equal(t, "top-orders", manualSQL.GetManualSqlId())
	require.Equal(t, "Top orders by amount", manualSQL.GetTitle())
	require.Equal(t, "SELECT 1", manualSQL.GetSqlText())
	require.Equal(t, []string{"Finance"}, manualSQL.GetTags())
	require.Equal(t, map[string]string{"owner": "alice"}, manualSQL.GetAttributes())
	// The instance is stored as a formatted resource name, not a bare ID.
	require.Equal(t, "instances/instance_1", manualSQL.GetInstanceResource())
	require.Equal(t, "db1", manualSQL.GetDatabaseName())
	require.Equal(t, "public", manualSQL.GetSchemaName())
}

func TestBuildManualSQLStoredMetadataNormalizesEmptyCollectionsToNil(t *testing.T) {
	t.Parallel()

	metadata := buildManualSQLStoredMetadata(&ManualSQLMessage{
		ManualSQLID:        "top-orders",
		InstanceResourceID: "instance_1",
		Tags:               []string{" "},
		Attributes:         map[string]string{},
	})

	manualSQL := metadata.GetManualSqlMetadata()
	require.Nil(t, manualSQL.GetTags())
	require.Nil(t, manualSQL.GetAttributes())
}
