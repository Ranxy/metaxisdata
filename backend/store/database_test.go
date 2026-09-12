package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func ptr[T any](value T) *T {
	return &value
}

func TestBuildListDatabaseQueryScopesToLiveRowsByDefault(t *testing.T) {
	query, args := buildListDatabaseQuery(&FindDatabaseMessage{
		InstanceID:   ptr("instance_1"),
		DatabaseName: ptr("db1"),
	})

	// ShowDeleted is the default, so both the instance and the database must be
	// filtered to live rows.
	require.Contains(t, query, "instance.deleted = $3")
	require.Contains(t, query, "db.deleted = $4")
	require.Contains(t, query, "db.instance = $1")
	require.Contains(t, query, "LOWER(db.name) = LOWER($2)")
	require.Equal(t, []any{"instance_1", "db1", false, false}, args)
	requirePlaceholdersMatchArgs(t, query, args)
}

func TestBuildListDatabaseQueryShowDeletedDropsDeletionGuards(t *testing.T) {
	query, args := buildListDatabaseQuery(&FindDatabaseMessage{
		InstanceID:  ptr("instance_1"),
		ShowDeleted: true,
	})

	require.NotContains(t, query, "instance.deleted =")
	require.NotContains(t, query, "db.deleted =")
	require.Equal(t, []any{"instance_1"}, args)
	requirePlaceholdersMatchArgs(t, query, args)
}

func TestBuildListDatabaseQueryCaseInsensitiveName(t *testing.T) {
	query, args := buildListDatabaseQuery(&FindDatabaseMessage{
		DatabaseName: ptr("DB1"),
	})
	require.Contains(t, query, "LOWER(db.name) = LOWER($1)")
	require.Equal(t, []any{"DB1", false, false}, args)

	query, args = buildListDatabaseQuery(&FindDatabaseMessage{
		DatabaseName:    ptr("DB1"),
		IsCaseSensitive: true,
	})
	require.Contains(t, query, "db.name = $1")
	require.NotContains(t, query, "LOWER(db.name)")
	require.Equal(t, []any{"DB1", false, false}, args)
}

func TestBuildListDatabaseQueryScopePredicates(t *testing.T) {
	query, args := buildListDatabaseQuery(&FindDatabaseMessage{
		EffectiveEnvironmentID: ptr("environments/prod"),
		InstanceID:             ptr("instance_1"),
		Engine:                 ptr(storepb.Engine_MYSQL),
		Limit:                  ptr(50),
		Offset:                 ptr(100),
	})

	require.Contains(t, query, "COALESCE(\n\t\t\tdb.environment,\n\t\t\tinstance.environment\n\t\t) = $1")
	require.Contains(t, query, "db.instance = $2")
	require.Contains(t, query, "instance.metadata->>'engine' = $3")
	require.Contains(t, query, " LIMIT 50")
	require.Contains(t, query, " OFFSET 100")
	require.Equal(t, []any{"environments/prod", "instance_1", "MYSQL", false, false}, args)
	requirePlaceholdersMatchArgs(t, query, args)
}

func TestBuildListDatabaseQueryNumbersFilterPlaceholdersAroundItsOwnArgs(t *testing.T) {
	// The filter already carries its own placeholders and args, so everything
	// appended afterwards must be renumbered by len(filter.Args).
	query, args := buildListDatabaseQuery(&FindDatabaseMessage{
		Filter: &ListResourceFilter{
			Where: "db.name <> $1",
			Args:  []any{"ignored"},
		},
		InstanceID: ptr("instance_1"),
	})

	require.Contains(t, query, "db.name <> $1")
	require.Contains(t, query, "db.instance = $2")
	require.Equal(t, []any{"ignored", "instance_1", false, false}, args)
	requirePlaceholdersMatchArgs(t, query, args)
}

// The engine filter compared the protojson text column against the enum's
// number, so it never matched; the bound value must be the enum name.
func TestBuildListDatabaseQueryComparesTheEngineName(t *testing.T) {
	t.Parallel()

	query, args := buildListDatabaseQuery(&FindDatabaseMessage{
		Engine: ptr(storepb.Engine_MYSQL),
	})
	require.Contains(t, query, "instance.metadata->>'engine' = $1")
	require.Equal(t, []any{"MYSQL", false, false}, args)
	requirePlaceholdersMatchArgs(t, query, args)
}
