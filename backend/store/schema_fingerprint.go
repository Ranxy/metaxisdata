package store

import (
	"crypto/sha256"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// schemaFingerprintDomain versions the schema fingerprint spec. Changing what
// CalcSchemaFingerprint encodes — adding a column field, covering another object
// type — means bumping the suffix here: every stored fingerprint then becomes a
// different value, which is an explicit, reviewable change instead of an
// accidental one. The schema syncer would rewrite every row once.
const schemaFingerprintDomain = "metaxisdata.schema.fingerprint.v1\x00"

// CalcSchemaFingerprint returns the engine-independent fingerprint of the shape a
// data-quality check is written against: the ordered columns (name, type,
// nullability), the primary key, and for a view its SQL body.
//
// It is derived from the stored metadata, not from the engine's DDL, so every
// engine has it — including MySQL and PostgreSQL, whose DDL the syncer never
// fetches. Two engines that describe the same table with the same columns, the
// same primary key and the same view body produce the same fingerprint.
//
// Only what changes the meaning of a check is encoded. Comments, defaults,
// character sets, collations, non-primary indexes, view dependency lists and the
// volatile statistics (row count, sizes) are excluded on purpose: a comment edit
// must not read as a structure change. Column order is encoded by the order of
// the repeated field, which is the registry's order.
//
// A view's definition is included because it is that object's structure: its
// columns can stay the same while it starts reading a different query.
//
// ok is false for object types that carry no shape (columns, databases,
// schemas, sequences, functions, procedures); the caller stores nothing for
// those.
func CalcSchemaFingerprint(meta *storepb.StoredMetadata) (fingerprint []byte, ok bool) {
	reduced := schemaFingerprintMetadata(meta)
	if reduced == nil {
		return nil, false
	}

	encoded := encodeCanonicalMessage([]byte(schemaFingerprintDomain), reduced.ProtoReflect())
	sum := sha256.Sum256(encoded)
	return sum[:], true
}

// schemaFingerprintMetadata reduces metadata to exactly the fields the
// fingerprint covers. It rebuilds the messages instead of cloning and clearing:
// the canonical encoder skips unset fields, so a field that is left out here
// cannot leak into the fingerprint.
func schemaFingerprintMetadata(meta *storepb.StoredMetadata) *storepb.StoredMetadata {
	switch {
	case meta.GetTableMetadata() != nil:
		table := meta.GetTableMetadata()
		return &storepb.StoredMetadata{
			Type: &storepb.StoredMetadata_TableMetadata{TableMetadata: &storepb.TableMetadata{
				Columns: fingerprintColumns(table.GetColumns()),
				Indexes: fingerprintPrimaryKey(table.GetIndexes()),
			}},
		}
	case meta.GetExternalTableMetadata() != nil:
		// An external table has columns but no index metadata.
		table := meta.GetExternalTableMetadata()
		return &storepb.StoredMetadata{
			Type: &storepb.StoredMetadata_ExternalTableMetadata{ExternalTableMetadata: &storepb.ExternalTableMetadata{
				Columns: fingerprintColumns(table.GetColumns()),
			}},
		}
	case meta.GetViewMetadata() != nil:
		view := meta.GetViewMetadata()
		return &storepb.StoredMetadata{
			Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{
				Definition: view.GetDefinition(),
				Columns:    fingerprintColumns(view.GetColumns()),
			}},
		}
	case meta.GetMaterializedViewMetadata() != nil:
		view := meta.GetMaterializedViewMetadata()
		return &storepb.StoredMetadata{
			Type: &storepb.StoredMetadata_MaterializedViewMetadata{MaterializedViewMetadata: &storepb.MaterializedViewMetadata{
				Definition: view.GetDefinition(),
				Columns:    fingerprintColumns(view.GetColumns()),
				Indexes:    fingerprintPrimaryKey(view.GetIndexes()),
			}},
		}
	default:
		return nil
	}
}

// fingerprintColumns keeps only the column fields a check depends on, in the
// registry's order.
func fingerprintColumns(columns []*storepb.ColumnMetadata) []*storepb.ColumnMetadata {
	reduced := make([]*storepb.ColumnMetadata, 0, len(columns))
	for _, column := range columns {
		if column == nil {
			continue
		}
		reduced = append(reduced, &storepb.ColumnMetadata{
			Name:     column.GetName(),
			Type:     column.GetType(),
			Nullable: column.GetNullable(),
		})
	}
	return reduced
}

// fingerprintPrimaryKey keeps the primary-key indexes only, and only their
// ordered key expressions: a constraint renamed or re-commented is not a
// structure change, while a primary key that gains, loses or reorders a column
// is.
func fingerprintPrimaryKey(indexes []*storepb.IndexMetadata) []*storepb.IndexMetadata {
	primary := make([]*storepb.IndexMetadata, 0, 1)
	for _, index := range indexes {
		if index == nil || !index.GetPrimary() {
			continue
		}
		primary = append(primary, &storepb.IndexMetadata{
			Expressions: append([]string(nil), index.GetExpressions()...),
		})
	}
	return primary
}
