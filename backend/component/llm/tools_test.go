package llm

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestBuildContextFromMetadataPairsGUIDsWithObjects(t *testing.T) {
	t.Parallel()

	metas := []*storepb.StoredMetadata{
		{Type: &storepb.StoredMetadata_TableMetadata{TableMetadata: &storepb.TableMetadata{
			Name: "users",
			Columns: []*storepb.ColumnMetadata{
				{Name: "id", Type: "int", Nullable: false, Comment: "primary key"},
			},
			Indexes: []*storepb.IndexMetadata{{Name: "users_pkey"}},
		}}},
		{Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{
			Name:       "user_order_view",
			Definition: "SELECT 1",
		}}},
	}

	context := BuildContextFromMetadata(metas, []string{
		"instance_1;db1;public;users",
		"instance_1;db1;public;user_order_view",
	})

	require.Len(t, context.Objects, 2)

	users := context.Objects[0]
	require.Equal(t, "instance_1;db1;public;users", users.GUID)
	require.Equal(t, "users", users.Name)
	require.Equal(t, "db1", users.DBName)
	require.Equal(t, "public", users.SchemaName)
	require.Equal(t, storepb.MetaType_TABLE, users.MetaType)
	require.Equal(t, []ColumnInfo{{Name: "id", Type: "int", Nullable: false, Comment: "primary key"}}, users.Columns)
	require.Equal(t, []string{"users_pkey"}, users.Indexes)

	view := context.Objects[1]
	require.Equal(t, storepb.MetaType_VIEW, view.MetaType)
	require.Equal(t, "SELECT 1", view.SQLText)
}

func TestBuildContextFromMetadataSkipsUnsupportedObjects(t *testing.T) {
	t.Parallel()

	context := BuildContextFromMetadata([]*storepb.StoredMetadata{
		{},
		{Type: &storepb.StoredMetadata_SchemaMetadata{SchemaMetadata: &storepb.SchemaMetadata{Name: "public"}}},
	}, []string{"instance_1;db1;public;whatever", "instance_1;db1;;public"})

	require.Empty(t, context.Objects)
}

func TestBuildContextFromMetadataToleratesMissingGUID(t *testing.T) {
	t.Parallel()

	// A shorter GUID list must not panic or misattribute a GUID to the wrong
	// object: the object without a GUID is still converted, with an empty one.
	context := BuildContextFromMetadata([]*storepb.StoredMetadata{
		{Type: &storepb.StoredMetadata_FunctionMetadata{FunctionMetadata: &storepb.FunctionMetadata{
			Name:       "f",
			Definition: "SELECT 1",
		}}},
		{Type: &storepb.StoredMetadata_ProcedureMetadata{ProcedureMetadata: &storepb.ProcedureMetadata{
			Name:       "p",
			Definition: "SELECT 2",
		}}},
	}, []string{"instance_1;db1;public;f"})

	require.Len(t, context.Objects, 2)
	require.Equal(t, "instance_1;db1;public;f", context.Objects[0].GUID)
	require.Equal(t, "", context.Objects[1].GUID)
	require.Equal(t, "p", context.Objects[1].Name)
}

func TestExplainSQLToolsAreFunctionDefinitions(t *testing.T) {
	t.Parallel()

	tools := ExplainSQLTools()
	require.Len(t, tools, 2)

	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		require.Equal(t, "function", tool.Type)
		require.NotEmpty(t, tool.Function.Name)
		require.NotEmpty(t, tool.Function.Description)
		parameters, ok := tool.Function.Parameters.(map[string]any)
		require.True(t, ok)
		require.Equal(t, "object", parameters["type"])
		require.NotEmpty(t, parameters["required"])
		names = append(names, tool.Function.Name)
	}
	require.Equal(t, []string{"get_object_schema", "search_objects"}, names)
}
