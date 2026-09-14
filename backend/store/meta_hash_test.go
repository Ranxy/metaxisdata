package store

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
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

func mustCalcMetaHash(t *testing.T, meta *storepb.StoredMetadata) []byte {
	t.Helper()
	hash, err := CalcMetaHash(meta)
	require.NoError(t, err)
	return hash
}

func mustHex(t *testing.T, b []byte) string {
	t.Helper()
	return hex.EncodeToString(b)
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

	hashA := mustCalcMetaHash(t, buildTableMetaWithStats("t", 100, 200, 300, 400, "id"))
	hashB := mustCalcMetaHash(t, buildTableMetaWithStats("t", 999, 888, 777, 666, "id"))
	require.Equal(t, hashA, hashB)

	hashC := mustCalcMetaHash(t, buildTableMetaWithStats("t", 100, 200, 300, 400, "id", "name"))
	require.NotEqual(t, hashA, hashC, "a schema change must change the fingerprint")
}

// TestCalcMetaHashRepeatedOrderMatters pins that repeated fields are ordered
// content: the same columns in a different order are a different table.
func TestCalcMetaHashRepeatedOrderMatters(t *testing.T) {
	t.Parallel()

	hashA := mustCalcMetaHash(t, buildTableMetaWithStats("t", 0, 0, 0, 0, "id", "name"))
	hashB := mustCalcMetaHash(t, buildTableMetaWithStats("t", 0, 0, 0, 0, "name", "id"))
	require.NotEqual(t, hashA, hashB)
}

// TestCalcMetaHashMapOrderIsCanonical pins that map iteration order, which Go
// randomizes, cannot leak into the fingerprint.
func TestCalcMetaHashMapOrderIsCanonical(t *testing.T) {
	t.Parallel()

	build := func(attributes map[string]string) *storepb.StoredMetadata {
		return &storepb.StoredMetadata{
			Type: &storepb.StoredMetadata_ManualSqlMetadata{
				ManualSqlMetadata: &storepb.ManualSQLMetadata{Name: "m", Attributes: attributes},
			},
		}
	}

	hashA := mustCalcMetaHash(t, build(map[string]string{"a": "1", "b": "2", "c": "3", "d": "4"}))
	for range 20 {
		hashB := mustCalcMetaHash(t, build(map[string]string{"d": "4", "c": "3", "b": "2", "a": "1"}))
		require.Equal(t, hashA, hashB)
	}

	changed := mustCalcMetaHash(t, build(map[string]string{"a": "1", "b": "2", "c": "3", "d": "changed"}))
	require.NotEqual(t, hashA, changed, "a changed map value must change the fingerprint")
}

// TestCalcMetaHashIgnoresUnknownFields pins that a row written by a different
// schema version (one carrying fields this build does not know) fingerprints the
// same as the known content. This is the failure mode proto.MarshalOptions
// {Deterministic: true} documents as unstable.
func TestCalcMetaHashIgnoresUnknownFields(t *testing.T) {
	t.Parallel()

	known := buildTableMetaWithStats("t", 0, 0, 0, 0, "id")
	knownBytes, err := proto.Marshal(known)
	require.NoError(t, err)

	// Append a varint field 999, which StoredMetadata does not define.
	withUnknownBytes := protowire.AppendTag(knownBytes, 999, protowire.VarintType)
	withUnknownBytes = protowire.AppendVarint(withUnknownBytes, 42)

	withUnknown := &storepb.StoredMetadata{}
	require.NoError(t, proto.Unmarshal(withUnknownBytes, withUnknown))
	require.NotEmpty(t, withUnknown.ProtoReflect().GetUnknown())

	require.Equal(t, mustCalcMetaHash(t, known), mustCalcMetaHash(t, withUnknown))
}

// TestCalcStoreMetaHashIsSelfConsistent asserts the contract callers rely on:
// hashing the persisted JSON reproduces the stored fingerprint, even though the
// JSON keeps the volatile statistics that the fingerprint normalizes away.
func TestCalcStoreMetaHashIsSelfConsistent(t *testing.T) {
	t.Parallel()

	meta := buildTableMetaWithStats("t", 100, 200, 300, 400, "id", "name")
	metadataBytes, metaHash, err := CalcStoreMetaHash(meta)
	require.NoError(t, err)

	persisted := &storepb.StoredMetadata{}
	require.NoError(t, common.ProtojsonUnmarshaler.Unmarshal(metadataBytes, persisted))
	require.Equal(t, int64(100), persisted.GetTableMetadata().RowCount)

	require.Equal(t, metaHash, mustCalcMetaHash(t, persisted))
}

// TestCalcMetaHashGolden pins the exact fingerprint of a fixed fixture. The
// encoding is private to this package, so this test is the tripwire: any
// accidental change to the canonical spec, or to a protobuf library behavior it
// relies on, fails here instead of silently rewriting the whole registry in
// production.
func TestCalcMetaHashGolden(t *testing.T) {
	t.Parallel()

	fixture := &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_TableMetadata{TableMetadata: &storepb.TableMetadata{
			Name:      "orders",
			Engine:    "InnoDB",
			Collation: "utf8mb4_general_ci",
			RowCount:  1234,
			Columns: []*storepb.ColumnMetadata{
				{Name: "id", Position: 1, Type: "bigint", Nullable: false},
				{Name: "created_at", Position: 2, Type: "timestamp", Default: "now()"},
			},
			Indexes: []*storepb.IndexMetadata{
				{Name: "pk_orders", Expressions: []string{"id"}, Primary: true},
			},
		}},
	}
	manual := &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_ManualSqlMetadata{ManualSqlMetadata: &storepb.ManualSQLMetadata{
			Name:       "report",
			SqlText:    "SELECT 1",
			Tags:       []string{"daily", "finance"},
			Attributes: map[string]string{"owner": "data", "tier": "gold"},
		}},
	}

	require.Equal(t, "26fb1c1b8f5bc620ee2a7b7459e879474c515db2d06980df374574838835c65b", mustHex(t, mustCalcMetaHash(t, fixture)))
	require.Equal(t, "b2798ed060e10d3f0c89a8d1cc311de2a20ad401d99becff7e618d4ea53c1b01", mustHex(t, mustCalcMetaHash(t, manual)))
}
