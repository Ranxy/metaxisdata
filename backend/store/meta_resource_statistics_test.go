package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func tableMetadataWithStats(name string, rowCount int64) *storepb.StoredMetadata {
	return &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_TableMetadata{
			TableMetadata: &storepb.TableMetadata{
				Name:     name,
				Columns:  []*storepb.ColumnMetadata{{Name: "id"}},
				RowCount: rowCount,
				DataSize: 200,
				DataFree: 50,
			},
		},
	}
}

func TestMetadataStatisticsExtractsTableAndSequenceFields(t *testing.T) {
	t.Parallel()

	table := tableMetadataWithStats("orders", 100)
	require.Equal(t, MetaRegistryStatistics{RowCount: 100, DataSize: 200, DataFree: 50}, MetadataStatistics(table))

	sequence := &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_SequenceMetadata{
			SequenceMetadata: &storepb.SequenceMetadata{Name: "orders_id_seq", LastValue: "1000"},
		},
	}
	require.Equal(t, MetaRegistryStatistics{SequenceLastValue: "1000"}, MetadataStatistics(sequence))

	// A type without statistics has none, so every sync compares it equal to itself.
	view := &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{Name: "v"}},
	}
	require.Equal(t, MetaRegistryStatistics{}, MetadataStatistics(view))
}

// The statistics sit outside the change fingerprint by construction: that is what lets a
// stats refresh write the metadata without a history row or a lineage re-analysis.
func TestStatisticsDoNotMoveTheFingerprint(t *testing.T) {
	t.Parallel()

	_, first, err := CalcStoreMetaHash(tableMetadataWithStats("orders", 100))
	require.NoError(t, err)
	_, second, err := CalcStoreMetaHash(tableMetadataWithStats("orders", 999999))
	require.NoError(t, err)
	require.Equal(t, first, second)
}

// The refresh is a metadata-only UPDATE behind a hash guard: it must never write a history
// row (the operator-visible ledger) and must never rewrite meta_hash or object identity.
func TestMetaRegistryStatisticsUpdateStatementShape(t *testing.T) {
	t.Parallel()

	statement := metaRegistryStatisticsUpdateStatement
	require.Contains(t, statement, "UPDATE meta_registry_resource")
	require.Contains(t, statement, "SET metadata = updated.metadata")
	require.Contains(t, statement, "current.meta_hash = updated.meta_hash")
	require.Contains(t, statement, "UNNEST($1::text[], $2::int[], $3::jsonb[], $4::bytea[])")
	require.NotContains(t, statement, "meta_registry_resource_history")
	require.NotContains(t, statement, "SET metadata = updated.metadata,")
	require.NotContains(t, statement, "INSERT")
	require.NotContains(t, statement, "DELETE")
}

// The read is prefix-scoped and restricted to the two object types that can carry
// statistics, so a sync does not pull every row of the database.
func TestMetaRegistryStatisticsQueryShape(t *testing.T) {
	t.Parallel()

	require.Contains(t, metaRegistryStatisticsQuery, "meta_registry_resource.object_type = ANY")
	require.Contains(t, metaRegistryStatisticsQuery, "#>> '{tableMetadata,rowCount}'")
	require.Contains(t, metaRegistryStatisticsQuery, "#>> '{tableMetadata,dataSize}'")
	require.Contains(t, metaRegistryStatisticsQuery, "#>> '{sequenceMetadata,lastValue}'")
	require.NotContains(t, metaRegistryStatisticsQuery, "meta_registry_resource_history")
	require.False(t, strings.Contains(metaRegistryStatisticsQuery, "SELECT *"),
		"the read must project the statistics only, not the whole row")
}
