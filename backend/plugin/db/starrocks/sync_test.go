package starrocks

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

// SyncInstance's system-database exclusion is engine-aware: StarRocks's
// read-only `sys` metadatabase must be excluded so it is not imported as a user
// database, while Doris has no `sys` and excluding it there would hide a user
// database named sys.
func TestSystemDatabaseExclusion(t *testing.T) {
	t.Parallel()

	sr := (&Driver{dbType: storepb.Engine_STARROCKS}).systemDatabaseExclusion()
	require.Contains(t, sr, "'sys'", "StarRocks sync must exclude the sys metadatabase")
	require.Contains(t, sr, "'information_schema'")
	require.Contains(t, sr, "'_statistics_'")

	doris := (&Driver{dbType: storepb.Engine_DORIS}).systemDatabaseExclusion()
	require.NotContains(t, doris, "'sys'", "Doris must not exclude sys (StarRocks-only)")
	require.Contains(t, doris, "'information_schema'")
}

// StarRocks reports materialized views in information_schema.tables with
// TABLE_TYPE='VIEW' and omits them from information_schema.views, while Doris
// reports them as 'BASE TABLE'. Membership in the materialized-view set is the
// authoritative signal.
func TestIsMaterializedView(t *testing.T) {
	t.Parallel()

	mvMap := map[db.TableKey]*storepb.MaterializedViewMetadata{
		{Schema: "", Table: "mv_async"}: {Name: "mv_async"},
	}
	mvKey := db.TableKey{Schema: "", Table: "mv_async"}

	// StarRocks reports the MV as VIEW; Doris as BASE TABLE; both map to an MV.
	require.True(t, isMaterializedView(viewTableType, mvKey, mvMap))
	require.True(t, isMaterializedView(baseTableType, mvKey, mvMap))
	require.True(t, isMaterializedView(materializedViewType, mvKey, mvMap))

	// A name absent from the MV set is never a materialized view.
	require.False(t, isMaterializedView(viewTableType, db.TableKey{Table: "v_regular"}, mvMap))
	require.False(t, isMaterializedView(baseTableType, db.TableKey{Table: "sales"}, mvMap))
}

// StarRocks synchronous rollups are listed in
// information_schema.materialized_views with REFRESH_TYPE='ROLLUP' but, unlike
// async materialized views, have no information_schema.tables row (they are
// rollup indexes on the base table). Only confirmed rollups are excluded; async
// refresh types are kept so a future refresh type is not silently dropped. The
// query coalesces SQL NULL to the empty string via IFNULL (database/sql cannot
// scan NULL into a string), so a NULL refresh type reaches this check as the
// empty string, which must also be kept.
func TestIsSyncRollup(t *testing.T) {
	t.Parallel()

	require.True(t, isSyncRollup("ROLLUP"))
	require.False(t, isSyncRollup("ASYNC"))
	require.False(t, isSyncRollup("MANUAL"))
	require.False(t, isSyncRollup(""))
}
