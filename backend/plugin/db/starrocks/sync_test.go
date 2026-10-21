package starrocks

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

// SyncInstance's system-database exclusion is engine-aware: StarRocks's
// read-only `sys` metadatabase and Doris's engine-owned `__internal_schema`
// (audit log and other internal tables) must be excluded so they are not
// imported as user databases. Each name is engine-specific, so neither engine
// drops the other's: Doris has no `sys`, StarRocks has no `__internal_schema`,
// and excluding either there would hide a user database of that name.
func TestSystemDatabaseExclusion(t *testing.T) {
	t.Parallel()

	sr := (&Driver{dbType: storepb.Engine_STARROCKS}).systemDatabaseExclusion()
	require.Contains(t, sr, "'sys'", "StarRocks sync must exclude the sys metadatabase")
	require.Contains(t, sr, "'information_schema'")
	require.Contains(t, sr, "'_statistics_'")
	require.NotContains(t, sr, "'__internal_schema'", "StarRocks must not exclude Doris's internal schema")

	doris := (&Driver{dbType: storepb.Engine_DORIS}).systemDatabaseExclusion()
	require.NotContains(t, doris, "'sys'", "Doris must not exclude sys (StarRocks-only)")
	require.Contains(t, doris, "'information_schema'")
	require.Contains(t, doris, "'_statistics_'")
	require.Contains(t, doris, "'__internal_schema'", "Doris sync must exclude the __internal_schema metadatabase")
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

// Doris and StarRocks report a BITMAP or HLL column's default as the raw
// serialized empty value, a single 0x00 byte. Metadata is stored in a PostgreSQL
// jsonb column, which rejects U+0000 with SQLSTATE 22P05 and takes the whole
// database's sync batch down, so the driver must drop the byte instead.
func TestSetColumnMetadataDefaultStripsNUL(t *testing.T) {
	t.Parallel()

	bitmap := &storepb.ColumnMetadata{Type: "bitmap"}
	setColumnMetadataDefault(bitmap, sql.NullString{String: "\x00", Valid: true}, false, "")
	require.Equal(t, "", bitmap.Default)

	// An ordinary default is preserved exactly.
	literal := &storepb.ColumnMetadata{Type: "varchar(10)"}
	setColumnMetadataDefault(literal, sql.NullString{String: "hello", Valid: true}, true, "")
	require.Equal(t, "hello", literal.Default)
}

func TestSanitizeCatalogString(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", sanitizeCatalogString("\x00"))
	require.Equal(t, "BASE TABLE", sanitizeCatalogString("BASE TABLE\x00"))
	require.Equal(t, "ab", sanitizeCatalogString("a\x00b"))
	require.Equal(t, "plain", sanitizeCatalogString("plain"))
}
