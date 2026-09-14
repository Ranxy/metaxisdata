package postgresql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// TestUnquotedIdentifiersMatchRegistryCase pins PG-FU-3: PostgreSQL folds
// unquoted identifiers to lower case and preserves quoted ones, which is exactly
// how pg_catalog — and therefore the metadata registry — stores them. The runner
// derives a lineage source GUID from the analyzer's names, and GetMetaRegistry
// matches GUIDs exactly, so this alignment is what lets a source node resolve.
//
// See plan/postgresql_omni_parser_migration_plan.md (PG-FU-3 and Appendix C).
func TestUnquotedIdentifiersMatchRegistryCase(t *testing.T) {
	const instance, database = "inst-1", "appdb"

	cases := []struct {
		sql        string
		wantSchema string
		wantTable  string
		wantColumn string
	}{
		{"SELECT ID FROM Users", "", "users", "id"},
		{"SELECT Users.ID FROM Users", "", "users", "id"},
		{"SELECT u.ID FROM Users u", "", "users", "id"},
		{"SELECT ID FROM MySchema.Users", "myschema", "users", "id"},
		{"SELECT users.id FROM users", "", "users", "id"},
		{`SELECT "ID" FROM "Users"`, "", "Users", "ID"},
		{`SELECT "ID" FROM "MySchema"."Users"`, "MySchema", "Users", "ID"},
	}

	for _, tc := range cases {
		t.Run(tc.sql, func(t *testing.T) {
			relations, err := analyzeSQL(tc.sql, nil)
			require.NoError(t, err)

			found := false
			for _, rel := range relations {
				if rel.Target.Table.Name != resultTableName {
					continue
				}
				found = true

				got := model.ObjectIdentifier{
					InstanceID: instance,
					Database:   database,
					Schema:     rel.Source.Table.Schema,
					Name:       rel.Source.Table.Name,
				}
				want := model.ObjectIdentifier{
					InstanceID: instance,
					Database:   database,
					Schema:     tc.wantSchema,
					Name:       tc.wantTable,
				}
				require.Equal(t, want.GUID(), got.GUID(),
					"analyzer-derived source GUID must equal the registry (pg_catalog) GUID")
				require.Equal(t, tc.wantSchema, rel.Source.Table.Schema, "source schema")
				require.Equal(t, tc.wantTable, rel.Source.Table.Name, "source table")
				require.Equal(t, tc.wantColumn, rel.Source.Name, "source column")
			}
			require.True(t, found, "expected a __result__ edge for %q", tc.sql)
		})
	}
}
