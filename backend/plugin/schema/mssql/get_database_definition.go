package mssql

import (
	"fmt"
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/schema"
)

func init() {
	schema.RegisterGetTableDefinition(storepb.Engine_MSSQL, GetTableDefinition)
	schema.RegisterGetViewDefinition(storepb.Engine_MSSQL, GetViewDefinition)
	schema.RegisterGetFunctionDefinition(storepb.Engine_MSSQL, GetFunctionDefinition)
	schema.RegisterGetProcedureDefinition(storepb.Engine_MSSQL, GetProcedureDefinition)
}

func GetTableDefinition(schemaName string, table *storepb.TableMetadata, _ []*storepb.SequenceMetadata) (string, error) {
	var buf strings.Builder
	writeTable(&buf, schemaName, table)
	return buf.String(), nil
}

func writeTable(out *strings.Builder, schemaName string, table *storepb.TableMetadata) {
	_, _ = fmt.Fprintf(out, "CREATE TABLE [%s].[%s] (\n", schemaName, table.Name)
	for i, column := range table.Columns {
		if i != 0 {
			_, _ = out.WriteString(",\n")
		}
		writeColumn(out, column)
	}

	for _, key := range table.Indexes {
		if !key.IsConstraint {
			continue
		}

		_, _ = out.WriteString(",\n")
		writeKey(out, key)
	}

	for _, fk := range table.ForeignKeys {
		_, _ = out.WriteString(",\n")
		writeForeignKey(out, fk)
	}

	for _, check := range table.CheckConstraints {
		_, _ = out.WriteString(",\n")
		writeCheck(out, check)
	}
	_, _ = fmt.Fprint(out, "\n);\n\n")

	for _, index := range table.Indexes {
		if index.IsConstraint {
			continue
		}
		writeIndex(out, schemaName, table.Name, index)
	}
}

func writeClusteredColumnStoreIndex(out *strings.Builder, schemaName string, tableName string, index *storepb.IndexMetadata) {
	_, _ = fmt.Fprintf(out, "CREATE CLUSTERED COLUMNSTORE INDEX [%s] ON [%s].[%s];\n\n", index.Name, schemaName, tableName)
}

func writeNonClusteredColumnStoreIndex(out *strings.Builder, schemaName string, tableName string, index *storepb.IndexMetadata) {
	_, _ = fmt.Fprintf(out, "CREATE NONCLUSTERED COLUMNSTORE INDEX [%s] ON [%s].[%s] (\n", index.Name, schemaName, tableName)
	for i, column := range index.Expressions {
		if i != 0 {
			_, _ = out.WriteString(",\n")
		}
		_, _ = fmt.Fprintf(out, "    [%s]", column)
	}
	_, _ = out.WriteString("\n);\n\n")
}

func writeSpatialIndex(out *strings.Builder, schemaName string, tableName string, index *storepb.IndexMetadata) {
	// Use the enhanced spatial index DDL generation
	spatialDDL := generateSpatialIndexDefinition(index, schemaName, tableName)
	_, _ = out.WriteString(spatialDDL)
	_, _ = out.WriteString(";\n\n")
}

func writeNormalIndex(out *strings.Builder, schemaName string, tableName string, index *storepb.IndexMetadata) {
	_, _ = out.WriteString("CREATE")
	if index.Unique {
		_, _ = out.WriteString(" UNIQUE")
	}
	if index.Type != "" {
		_, _ = fmt.Fprintf(out, " %s", index.Type)
	}
	_, _ = fmt.Fprintf(out, " INDEX [%s] ON\n[%s].[%s] (\n", index.Name, schemaName, tableName)
	for i, column := range index.Expressions {
		if i != 0 {
			_, _ = out.WriteString(",\n")
		}
		_, _ = fmt.Fprintf(out, "    [%s]", column)
		if i < len(index.Descending) && index.Descending[i] {
			_, _ = out.WriteString(" DESC")
		} else {
			_, _ = out.WriteString(" ASC")
		}
	}
	_, _ = out.WriteString("\n);\n\n")
}

func writeIndex(out *strings.Builder, schemaName string, tableName string, index *storepb.IndexMetadata) {
	switch strings.ToUpper(index.Type) {
	case "CLUSTERED COLUMNSTORE":
		writeClusteredColumnStoreIndex(out, schemaName, tableName, index)
	case "NONCLUSTERED COLUMNSTORE":
		writeNonClusteredColumnStoreIndex(out, schemaName, tableName, index)
	case "SPATIAL":
		writeSpatialIndex(out, schemaName, tableName, index)
	default:
		writeNormalIndex(out, schemaName, tableName, index)
	}
}

func writeCheck(out *strings.Builder, check *storepb.CheckConstraintMetadata) {
	_, _ = fmt.Fprintf(out, "    CONSTRAINT [%s] CHECK %s", check.Name, check.Expression)
}

func writeForeignKey(out *strings.Builder, fk *storepb.ForeignKeyMetadata) {
	_, _ = fmt.Fprintf(out, "    CONSTRAINT [%s] FOREIGN KEY (", fk.Name)
	for i, column := range fk.Columns {
		if i != 0 {
			_, _ = out.WriteString(", ")
		}
		_, _ = fmt.Fprintf(out, "[%s]", column)
	}
	_, _ = fmt.Fprintf(out, ") REFERENCES [%s].[%s] (", fk.ReferencedSchema, fk.ReferencedTable)
	for i, column := range fk.ReferencedColumns {
		if i != 0 {
			_, _ = out.WriteString(", ")
		}
		_, _ = fmt.Fprintf(out, "[%s]", column)
	}
	_, _ = out.WriteString(")")
	if fk.OnDelete != "" {
		_, _ = fmt.Fprintf(out, " ON DELETE %s", fk.OnDelete)
	}
	if fk.OnUpdate != "" {
		_, _ = fmt.Fprintf(out, " ON UPDATE %s", fk.OnUpdate)
	}
}

func writeKey(out *strings.Builder, key *storepb.IndexMetadata) {
	_, _ = fmt.Fprintf(out, "    CONSTRAINT [%s]", key.Name)
	if key.Primary {
		_, _ = out.WriteString(" PRIMARY KEY")
	} else if key.Unique {
		_, _ = out.WriteString(" UNIQUE")
	}

	if key.Type != "" {
		_, _ = fmt.Fprintf(out, " %s", key.Type)
	}
	_, _ = out.WriteString(" (")
	for i, column := range key.Expressions {
		if i != 0 {
			_, _ = out.WriteString(", ")
		}
		_, _ = fmt.Fprintf(out, "[%s]", column)
		if i < len(key.Descending) && key.Descending[i] {
			_, _ = out.WriteString(" DESC")
		} else {
			_, _ = out.WriteString(" ASC")
		}
	}
	_, _ = out.WriteString(")")
}

func writeColumn(out *strings.Builder, column *storepb.ColumnMetadata) {
	_, _ = fmt.Fprintf(out, "    [%s] %s", column.Name, column.Type)
	if column.IsIdentity {
		_, _ = fmt.Fprintf(out, " IDENTITY(%d,%d)", column.IdentitySeed, column.IdentityIncrement)
	}
	if column.Collation != "" {
		_, _ = fmt.Fprintf(out, " COLLATE %s", column.Collation)
	}
	if column.GetDefault() != "" {
		_, _ = fmt.Fprintf(out, " DEFAULT %s", column.GetDefault())
	}
	if !column.Nullable {
		_, _ = out.WriteString(" NOT NULL")
	}
}

func writeView(out *strings.Builder, _ string, view *storepb.ViewMetadata) {
	// The view definition already contains CREATE VIEW statement
	_, _ = fmt.Fprintf(out, "%s;\n\nGO\n\n", view.Definition)
}

func writeFunction(out *strings.Builder, _ string, function *storepb.FunctionMetadata) {
	_, _ = fmt.Fprintf(out, "%s\n\nGO\n\n", function.Definition)
}

func writeProcedure(out *strings.Builder, _ string, procedure *storepb.ProcedureMetadata) {
	_, _ = fmt.Fprintf(out, "%s\n\nGO\n\n", procedure.Definition)
}

func GetViewDefinition(schemaName string, view *storepb.ViewMetadata) (string, error) {
	var buf strings.Builder
	writeView(&buf, schemaName, view)
	if view.Comment != "" {
		_, _ = fmt.Fprintf(&buf, "%s;\nGO\n\n", generateViewCommentSQL("ADD", schemaName, view.Name, view.Comment))
	}
	return buf.String(), nil
}

func GetFunctionDefinition(schemaName string, function *storepb.FunctionMetadata) (string, error) {
	var buf strings.Builder
	writeFunction(&buf, schemaName, function)
	return buf.String(), nil
}

func GetProcedureDefinition(schemaName string, procedure *storepb.ProcedureMetadata) (string, error) {
	var buf strings.Builder
	writeProcedure(&buf, schemaName, procedure)
	return buf.String(), nil
}

func generateSpatialIndexDefinition(index *storepb.IndexMetadata, schemaName, tableName string) string {
	var buf strings.Builder

	// Build the CREATE SPATIAL INDEX statement
	_, _ = buf.WriteString("CREATE SPATIAL INDEX [")
	_, _ = buf.WriteString(index.Name)
	_, _ = buf.WriteString("] ON [")
	_, _ = buf.WriteString(schemaName)
	_, _ = buf.WriteString("].[")
	_, _ = buf.WriteString(tableName)
	_, _ = buf.WriteString("] (")

	// Add column expressions
	for i, expr := range index.Expressions {
		if i > 0 {
			_, _ = buf.WriteString(", ")
		}
		_, _ = buf.WriteString("[")
		_, _ = buf.WriteString(expr)
		_, _ = buf.WriteString("]")
	}
	_, _ = buf.WriteString(")")

	// Check if spatial configuration exists
	if index.SpatialConfig == nil || index.SpatialConfig.Tessellation == nil {
		return buf.String()
	}

	tessellation := index.SpatialConfig.Tessellation

	// Add USING clause for tessellation scheme
	if tessellation.Scheme != "" {
		_, _ = buf.WriteString("\nUSING ")
		_, _ = buf.WriteString(tessellation.Scheme)
	}

	// Build WITH clause parameters
	withParams := []string{}

	// Add tessellation parameters
	withParams = append(withParams, buildTessellationParams(tessellation)...)

	// Add storage parameters
	if index.SpatialConfig.Storage != nil {
		withParams = append(withParams, buildStorageParams(index.SpatialConfig.Storage)...)
	}

	// Add WITH clause if we have parameters
	if len(withParams) > 0 {
		_, _ = buf.WriteString("\nWITH (\n    ")
		_, _ = buf.WriteString(strings.Join(withParams, ",\n    "))
		_, _ = buf.WriteString("\n)")
	}

	return buf.String()
}

func buildTessellationParams(tessellation *storepb.TessellationConfig) []string {
	params := []string{}

	// BOUNDING_BOX for GEOMETRY indexes
	if tessellation.BoundingBox != nil {
		bbox := tessellation.BoundingBox
		params = append(params, fmt.Sprintf("BOUNDING_BOX = (%g, %g, %g, %g)",
			bbox.Xmin, bbox.Ymin, bbox.Xmax, bbox.Ymax))
	}

	// GRIDS configuration
	if len(tessellation.GridLevels) > 0 {
		gridParts := []string{}
		for _, level := range tessellation.GridLevels {
			gridParts = append(gridParts, fmt.Sprintf("LEVEL_%d = %s", level.Level, level.Density))
		}
		if len(gridParts) > 0 {
			params = append(params, fmt.Sprintf("GRIDS = (%s)", strings.Join(gridParts, ", ")))
		}
	}

	// CELLS_PER_OBJECT
	if tessellation.CellsPerObject > 0 {
		params = append(params, fmt.Sprintf("CELLS_PER_OBJECT = %d", tessellation.CellsPerObject))
	}

	return params
}

func buildStorageParams(storage *storepb.StorageConfig) []string {
	params := []string{}

	// PAD_INDEX (defaults to OFF, so only output when ON)
	if storage.PadIndex {
		params = append(params, "PAD_INDEX = ON")
	}

	// FILLFACTOR
	if storage.Fillfactor > 0 {
		params = append(params, fmt.Sprintf("FILLFACTOR = %d", storage.Fillfactor))
	}

	// SORT_IN_TEMPDB
	if storage.SortInTempdb != "" {
		params = append(params, fmt.Sprintf("SORT_IN_TEMPDB = %s", storage.SortInTempdb))
	}

	// DROP_EXISTING
	if storage.DropExisting {
		params = append(params, "DROP_EXISTING = ON")
	}

	// ONLINE
	if storage.Online {
		params = append(params, "ONLINE = ON")
	}

	// ALLOW_ROW_LOCKS and ALLOW_PAGE_LOCKS (default to ON for spatial indexes)
	// Only output when they differ from the default (ON)
	if !storage.AllowRowLocks {
		params = append(params, "ALLOW_ROW_LOCKS = OFF")
	}

	if !storage.AllowPageLocks {
		params = append(params, "ALLOW_PAGE_LOCKS = OFF")
	}

	// MAXDOP
	if storage.Maxdop > 0 {
		params = append(params, fmt.Sprintf("MAXDOP = %d", storage.Maxdop))
	}

	// DATA_COMPRESSION
	if storage.DataCompression != "" && storage.DataCompression != "NONE" {
		params = append(params, fmt.Sprintf("DATA_COMPRESSION = %s", storage.DataCompression))
	}

	return params
}
