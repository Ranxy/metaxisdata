package resource

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
)

func TestTypeNamesRoundTrip(t *testing.T) {
	t.Parallel()

	for _, tt := range []Type{
		TypeWorkspace, TypeInstance, TypeDatabase, TypeSchema, TypeTable, TypeView,
		TypeExternalTable, TypeColumn, TypeQualityRule, TypeQualityExecution,
		TypeQualityIssue, TypeQualityContract, TypeQualitySuggestion, TypeColumnProfile,
		TypeColumnLabel, TypeManualSQL, TypeApplication, TypeApplicationSQL, TypeQualityAgent,
	} {
		parsed, ok := ParseType(tt.String())
		require.Truef(t, ok, "type %v does not round-trip", tt)
		require.Equalf(t, tt, parsed, "type %v round-trips to %v", tt, parsed)
	}

	require.Equal(t, "unspecified", TypeUnspecified.String())
	parsed, ok := ParseType("")
	require.True(t, ok)
	require.Equal(t, TypeUnspecified, parsed)
	_, ok = ParseType("not_a_type")
	require.False(t, ok)
}

func TestRefValid(t *testing.T) {
	t.Parallel()

	require.False(t, Ref{}.Valid())
	require.True(t, WorkspaceRef().Valid())
	require.False(t, Ref{Type: TypeWorkspace, ID: "x"}.Valid())
	require.False(t, Ref{Type: TypeTable}.Valid())
	require.True(t, NewRef(TypeTable, "i;db;t").Valid())
	require.True(t, NewRef(TypeTable, "i;db;t").Equal(NewRef(TypeTable, "i;db;t")))
	require.False(t, NewRef(TypeTable, "i;db;t").Equal(NewRef(TypeView, "i;db;t")))
	require.Equal(t, "workspace", WorkspaceRef().String())
	require.Equal(t, "table:i;db;t", NewRef(TypeTable, "i;db;t").String())
}

func TestMetadataAncestors(t *testing.T) {
	t.Parallel()

	workspace := WorkspaceRef()
	for _, tt := range []struct {
		name string
		ref  Ref
		want []Ref
	}{
		{
			name: "instance",
			ref:  NewRef(TypeInstance, "i"),
			want: []Ref{NewRef(TypeInstance, "i"), workspace},
		},
		{
			name: "database",
			ref:  NewRef(TypeDatabase, "i;db"),
			want: []Ref{NewRef(TypeDatabase, "i;db"), NewRef(TypeInstance, "i"), workspace},
		},
		{
			name: "schema",
			ref:  NewRef(TypeSchema, "i;db;public"),
			want: []Ref{
				NewRef(TypeSchema, "i;db;public"),
				NewRef(TypeDatabase, "i;db"),
				NewRef(TypeInstance, "i"),
				workspace,
			},
		},
		{
			name: "table without a schema segment",
			ref:  NewRef(TypeTable, "i;db;orders"),
			want: []Ref{
				NewRef(TypeTable, "i;db;orders"),
				NewRef(TypeDatabase, "i;db"),
				NewRef(TypeInstance, "i"),
				workspace,
			},
		},
		{
			name: "table with a schema segment",
			ref:  NewRef(TypeView, "i;db;public;orders"),
			want: []Ref{
				NewRef(TypeView, "i;db;public;orders"),
				NewRef(TypeSchema, "i;db;public"),
				NewRef(TypeDatabase, "i;db"),
				NewRef(TypeInstance, "i"),
				workspace,
			},
		},
		{
			name: "column of a table without a schema segment",
			ref:  NewRef(TypeColumn, "i;db;orders;id"),
			want: []Ref{
				NewRef(TypeColumn, "i;db;orders;id"),
				NewRef(TypeTable, "i;db;orders"),
				NewRef(TypeDatabase, "i;db"),
				NewRef(TypeInstance, "i"),
				workspace,
			},
		},
		{
			name: "column of a table with a schema segment",
			ref:  NewRef(TypeColumn, "i;db;public;orders;id"),
			want: []Ref{
				NewRef(TypeColumn, "i;db;public;orders;id"),
				NewRef(TypeTable, "i;db;public;orders"),
				NewRef(TypeSchema, "i;db;public"),
				NewRef(TypeDatabase, "i;db"),
				NewRef(TypeInstance, "i"),
				workspace,
			},
		},
		{
			name: "manual SQL under a schema",
			ref:  NewRef(TypeManualSQL, common.BuildMetaGUID("i", "db", "public", "manualSQL:1")),
			want: []Ref{
				NewRef(TypeManualSQL, common.BuildMetaGUID("i", "db", "public", "manualSQL:1")),
				NewRef(TypeSchema, common.BuildMetaGUID("i", "db", "public")),
				NewRef(TypeDatabase, "i;db"),
				NewRef(TypeInstance, "i"),
				workspace,
			},
		},
		{
			name: "application SQL with an empty schema",
			ref:  NewRef(TypeApplicationSQL, common.BuildMetaGUID("i", "db", "", "applicationSQL:1")),
			want: []Ref{
				NewRef(TypeApplicationSQL, common.BuildMetaGUID("i", "db", "", "applicationSQL:1")),
				NewRef(TypeSchema, common.BuildMetaGUID("i", "db", "")),
				NewRef(TypeDatabase, "i;db"),
				NewRef(TypeInstance, "i"),
				workspace,
			},
		},
		{
			name: "names containing the separator keep their escaped prefixes",
			ref:  NewRef(TypeColumn, common.BuildMetaGUID("i", "db", "public", "we;ird", "id")),
			want: []Ref{
				NewRef(TypeColumn, common.BuildMetaGUID("i", "db", "public", "we;ird", "id")),
				NewRef(TypeTable, common.BuildMetaGUID("i", "db", "public", "we;ird")),
				NewRef(TypeSchema, common.BuildMetaGUID("i", "db", "public")),
				NewRef(TypeDatabase, common.BuildMetaGUID("i", "db")),
				NewRef(TypeInstance, common.BuildMetaGUID("i")),
				workspace,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, MetadataAncestors(tt.ref))
		})
	}
}

func TestMetadataAncestorsRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	require.Nil(t, MetadataAncestors(NewRef(TypeQualityRule, "rule-1")))
	require.Nil(t, MetadataAncestors(NewRef(TypeWorkspace, "")))
	require.Nil(t, MetadataAncestors(NewRef(TypeTable, "i;db")))
	require.Nil(t, MetadataAncestors(NewRef(TypeDatabase, "i")))
	require.Nil(t, MetadataAncestors(NewRef(TypeColumn, "i;db;orders")))
	require.Nil(t, MetadataAncestors(NewRef(TypeManualSQL, "i;db")))
	require.Nil(t, MetadataAncestors(NewRef(TypeApplicationSQL, "i;db;public;table;extra")))
}
