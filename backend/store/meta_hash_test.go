package store

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"

	"github.com/Ranxy/metaxisdata/backend/common"
)

func buildTableMetaWithStats(name string, rowCount, dataSize, indexSize, dataFree int64, columns ...string) *storepb.StoredMetadata {
	metaCols := make([]*storepb.ColumnMetadata, 0, len(columns))
	for _, col := range columns {
		metaCols = append(metaCols, &storepb.ColumnMetadata{Name: col})
	}
	return &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_TableMetadata{
			TableMetadata: &storepb.TableMetadata{
				Name:      name,
				Columns:   metaCols,
				RowCount:  rowCount,
				DataSize:  dataSize,
				IndexSize: indexSize,
				DataFree:  dataFree,
			},
		},
	}
}

func TestNormalizeMetadataForHashZeroesTableStats(t *testing.T) {
	t.Parallel()

	original := buildTableMetaWithStats("mytable", 1000, 65536, 32768, 1024, "id", "name")
	normalized := normalizeMetadataForHash(original)

	// The original is not mutated: the un-normalized statistics are what gets
	// persisted for display.
	require.Equal(t, int64(1000), original.GetTableMetadata().RowCount)
	require.Equal(t, int64(65536), original.GetTableMetadata().DataSize)

	require.Equal(t, int64(0), normalized.GetTableMetadata().RowCount)
	require.Equal(t, int64(0), normalized.GetTableMetadata().DataSize)
	require.Equal(t, int64(0), normalized.GetTableMetadata().IndexSize)
	require.Equal(t, int64(0), normalized.GetTableMetadata().DataFree)

	require.Equal(t, "mytable", normalized.GetTableMetadata().Name)
	require.Len(t, normalized.GetTableMetadata().Columns, 2)
}

func TestNormalizeMetadataForHashZeroesSequenceLastValue(t *testing.T) {
	t.Parallel()

	original := &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_SequenceMetadata{
			SequenceMetadata: &storepb.SequenceMetadata{Name: "seq1", LastValue: "42"},
		},
	}
	normalized := normalizeMetadataForHash(original)

	require.Equal(t, "42", original.GetSequenceMetadata().LastValue)
	require.Equal(t, "", normalized.GetSequenceMetadata().LastValue)
	require.Equal(t, "seq1", normalized.GetSequenceMetadata().Name)
}

func TestCalcMetaHashIgnoresTableStats(t *testing.T) {
	t.Parallel()

	hashA, err := CalcMetaHash(buildTableMetaWithStats("t", 100, 200, 300, 400, "id"))
	require.NoError(t, err)
	hashB, err := CalcMetaHash(buildTableMetaWithStats("t", 999, 888, 777, 666, "id"))
	require.NoError(t, err)
	require.Equal(t, hashA, hashB)

	hashC, err := CalcMetaHash(buildTableMetaWithStats("t", 100, 200, 300, 400, "id", "name"))
	require.NoError(t, err)
	require.NotEqual(t, hashA, hashC, "a schema change must change the hash")
}

// TestCalcMetaHashMatchesDeterministicEncoding guards the hash encoding itself:
// hashing must use the deterministic binary encoding, never protojson output.
// protobuf-go randomizes protojson whitespace per binary build (internal/detrand
// seeds from the executable), so a protojson-based hash changed on every rebuild
// and forced the syncer to rewrite the whole metadata registry on each restart.
func TestCalcMetaHashMatchesDeterministicEncoding(t *testing.T) {
	t.Parallel()

	meta := buildTableMetaWithStats("t", 100, 200, 300, 400, "id", "name")
	stable, err := proto.MarshalOptions{Deterministic: true}.Marshal(normalizeMetadataForHash(meta))
	require.NoError(t, err)
	want := sha256.Sum256(stable)

	got, err := CalcMetaHash(meta)
	require.NoError(t, err)
	require.Equal(t, want[:], got)
}

// TestCalcStoreMetaHashIsSelfConsistent asserts the contract callers rely on:
// hashing the persisted JSON reproduces the stored hash, even though the JSON
// keeps the volatile statistics that the hash normalizes away.
func TestCalcStoreMetaHashIsSelfConsistent(t *testing.T) {
	t.Parallel()

	meta := buildTableMetaWithStats("t", 100, 200, 300, 400, "id", "name")
	metadataBytes, metaHash, err := CalcStoreMetaHash(meta)
	require.NoError(t, err)

	persisted := &storepb.StoredMetadata{}
	require.NoError(t, common.ProtojsonUnmarshaler.Unmarshal(metadataBytes, persisted))
	require.Equal(t, int64(100), persisted.GetTableMetadata().RowCount)

	roundTripped, err := CalcMetaHash(persisted)
	require.NoError(t, err)
	require.Equal(t, metaHash, roundTripped)
}
