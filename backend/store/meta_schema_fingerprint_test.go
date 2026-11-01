package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The fingerprint upsert must not churn a row whose shape did not change: the
// sync diffs in Go, and this guard is the safety net for a concurrent writer.
func TestBuildUpsertMetaRegistrySchemaFingerprintQueryGuardsOnFingerprint(t *testing.T) {
	t.Parallel()

	query := buildUpsertMetaRegistrySchemaFingerprintQuery()
	require.Contains(t, query, "ON CONFLICT (guid, object_type) DO UPDATE")
	require.Contains(t, query, "meta_registry_schema_fingerprint.fingerprint IS DISTINCT FROM EXCLUDED.fingerprint")
	require.Contains(t, query, "UNNEST ($1::text[], $2::int[], $3::bytea[])")
}

// Deleting fingerprints must pair guid with object_type: the two independent
// ANY predicates also match every cross combination.
func TestBuildDeleteMetaRegistrySchemaFingerprintByKeyQueryIsPaired(t *testing.T) {
	t.Parallel()

	query := buildDeleteMetaRegistrySchemaFingerprintByKeyQuery()
	require.Contains(t, query, "(guid, object_type::int) IN (SELECT * FROM unnest($1::text[], $2::int[]))")
	require.NotContains(t, query, "ANY($1)")
	require.NotContains(t, query, "ANY($2)")
}

// The sync's write diff only needs the hashes; reading the metadata back would
// defeat it.
func TestBuildListMetaRegistrySchemaFingerprintQueryReadsHashesOnly(t *testing.T) {
	t.Parallel()

	query := buildListMetaRegistrySchemaFingerprintQuery()
	require.Contains(t, query, "SELECT guid, object_type, fingerprint")
	require.NotContains(t, query, "meta_registry_resource")
	require.Contains(t, query, "(guid, object_type::int) IN (SELECT * FROM unnest($1::text[], $2::int[]))")
}

// The data-quality read path resolves both signals in one round trip, and the
// request drives the join so a key with no row is still distinguishable.
func TestBuildListMetaRegistrySchemaSignalQueryJoinsBothSources(t *testing.T) {
	t.Parallel()

	query := buildListMetaRegistrySchemaSignalQuery()
	require.Contains(t, query, "AS requested(guid, object_type)")
	require.Contains(t, query, "LEFT JOIN meta_registry_schema_fingerprint f")
	require.Contains(t, query, "LEFT JOIN meta_registry_resource_schema s")
	require.Contains(t, query, "f.fingerprint")
	require.Contains(t, query, "s.schema_hash")
	// The DDL itself is never part of the signal.
	require.NotContains(t, query, "s.schema,")
	require.NotContains(t, query, "s.schema ")
}
