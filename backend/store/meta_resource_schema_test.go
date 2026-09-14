package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The upsert must not rewrite a row whose DDL did not change: a full-fetch sync
// passes every object on every run, and an unconditional DO UPDATE would churn
// a dead tuple per object per sync and reset updated_at.
func TestBuildUpsertMetaRegistrySchemaQueryGuardsOnHash(t *testing.T) {
	t.Parallel()

	query := buildUpsertMetaRegistrySchemaQuery()
	require.Contains(t, query, "ON CONFLICT (guid, object_type) DO UPDATE")
	require.Contains(t, query, "meta_registry_resource_schema.schema_hash IS DISTINCT FROM EXCLUDED.schema_hash")
	require.Contains(t, query, "UNNEST ($1::text[], $2::int[], $3::text[], $4::bytea[])")
}

// Deleting DDL rows must pair guid with object_type, like the history queries:
// `guid = ANY($1) AND object_type = ANY($2)` also matches every cross
// combination and would delete unrequested resources.
func TestBuildDeleteMetaRegistrySchemaByKeyQueryIsPaired(t *testing.T) {
	t.Parallel()

	query := buildDeleteMetaRegistrySchemaByKeyQuery()
	require.Contains(t, query, "(guid, object_type::int) IN (SELECT * FROM unnest($1::text[], $2::int[]))")
	require.NotContains(t, query, "ANY($1)")
	require.NotContains(t, query, "ANY($2)")
}

// The hash lookup feeds the write diff, so it must read only the hash (never the
// DDL) and pair guid with object_type like the delete does.
func TestBuildListMetaRegistrySchemaHashQuery(t *testing.T) {
	t.Parallel()

	query := buildListMetaRegistrySchemaHashQuery()
	require.Contains(t, query, "SELECT guid, object_type, schema_hash")
	// "schema," can only match the DDL column in a select list, never
	// schema_hash or the table name; reading the DDL here would defeat the diff.
	require.NotContains(t, query, "schema,")
	require.Contains(t, query, "(guid, object_type::int) IN (SELECT * FROM unnest($1::text[], $2::int[]))")
	require.NotContains(t, query, "ANY($1)")
	require.NotContains(t, query, "ANY($2)")
}
