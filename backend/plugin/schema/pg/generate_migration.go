package pg

import (
	"fmt"
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/schema"
)

func init() {
	schema.RegisterGenerateMigration(storepb.Engine_POSTGRES, generateMigration)
}

func generateMigration(diff *schema.MetadataDiff) (string, error) {
	var buf strings.Builder

	dropObjectsInOrder(diff, &buf)

	dropPhaseHasContent := buf.Len() > 0
	createPhaseWillHaveContent := hasCreateOrAlterObjects(diff)

	if dropPhaseHasContent && createPhaseWillHaveContent {
		_, _ = buf.WriteString("\n")
	}

	if err := createObjectsInOrder(diff, &buf); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func dropObjectsInOrder(diff *schema.MetadataDiff, buf *strings.Builder) {
	// Drop event triggers first
	for _, etDiff := range diff.EventTriggerChanges {
		if etDiff.Action == schema.MetadataDiffActionDrop {
			writeDropEventTrigger(buf, etDiff.EventTriggerName)
		}
	}

	// Drop triggers on tables and views
	for _, tc := range diff.TableChanges {
		if tc.Action == schema.MetadataDiffActionDrop && tc.OldTable != nil {
			for _, trigger := range tc.OldTable.Triggers {
				writeDropTrigger(buf, tc.SchemaName, tc.TableName, trigger.Name)
			}
		} else if tc.Action == schema.MetadataDiffActionAlter {
			for _, td := range tc.TriggerChanges {
				if td.Action == schema.MetadataDiffActionDrop {
					writeDropTrigger(buf, tc.SchemaName, tc.TableName, td.OldTrigger.Name)
				}
			}
		}
	}

	// Drop foreign keys from altering tables
	for _, tc := range diff.TableChanges {
		if tc.Action == schema.MetadataDiffActionAlter {
			for _, fkDiff := range tc.ForeignKeyChanges {
				if fkDiff.Action == schema.MetadataDiffActionDrop {
					writeDropForeignKey(buf, tc.SchemaName, tc.TableName, fkDiff.OldForeignKey.Name)
				}
			}
		}
	}

	// Drop foreign keys from tables being dropped
	for _, tc := range diff.TableChanges {
		if tc.Action == schema.MetadataDiffActionDrop && tc.OldTable != nil {
			for _, fk := range tc.OldTable.ForeignKeys {
				writeDropForeignKey(buf, tc.SchemaName, tc.TableName, fk.Name)
			}
		}
	}

	// Drop views
	for _, viewDiff := range diff.ViewChanges {
		if viewDiff.Action == schema.MetadataDiffActionDrop {
			writeDropView(buf, viewDiff.SchemaName, viewDiff.ViewName)
		}
	}

	// Drop materialized views
	for _, mvDiff := range diff.MaterializedViewChanges {
		if mvDiff.Action == schema.MetadataDiffActionDrop {
			writeDropMaterializedView(buf, mvDiff.SchemaName, mvDiff.MaterializedViewName)
		}
	}

	// Drop functions
	for _, funcDiff := range diff.FunctionChanges {
		if funcDiff.Action == schema.MetadataDiffActionDrop {
			writeDropFunction(buf, funcDiff.SchemaName, funcDiff.FunctionName)
		}
	}

	// Drop procedures
	for _, procDiff := range diff.ProcedureChanges {
		if procDiff.Action == schema.MetadataDiffActionDrop {
			writeDropProcedure(buf, procDiff.SchemaName, procDiff.ProcedureName)
		}
	}

	// Drop events
	for _, eventDiff := range diff.EventChanges {
		if eventDiff.Action == schema.MetadataDiffActionDrop {
			writeDropEvent(buf, eventDiff.EventName)
		}
	}

	// Drop tables
	for _, tc := range diff.TableChanges {
		if tc.Action == schema.MetadataDiffActionDrop {
			writeDropTable(buf, tc.SchemaName, tc.TableName)
		}
	}

	// Drop sequences
	for _, seqDiff := range diff.SequenceChanges {
		if seqDiff.Action == schema.MetadataDiffActionDrop {
			writeDropSequence(buf, seqDiff.SchemaName, seqDiff.SequenceName)
		}
	}

	// Drop enum types
	for _, enumDiff := range diff.EnumTypeChanges {
		if enumDiff.Action == schema.MetadataDiffActionDrop {
			writeDropEnumType(buf, enumDiff.SchemaName, enumDiff.EnumTypeName)
		}
	}

	// Drop schemas
	for _, schemaDiff := range diff.SchemaChanges {
		if schemaDiff.Action == schema.MetadataDiffActionDrop {
			writeDropSchema(buf, schemaDiff.SchemaName)
		}
	}

	// Drop extensions
	for _, extDiff := range diff.ExtensionChanges {
		if extDiff.Action == schema.MetadataDiffActionDrop {
			writeDropExtension(buf, extDiff.ExtensionName)
		}
	}

	// Handle ALTER table drops
	for _, tc := range diff.TableChanges {
		if tc.Action != schema.MetadataDiffActionAlter {
			continue
		}

		// Drop check constraints
		for _, checkDiff := range tc.CheckConstraintChanges {
			if checkDiff.Action == schema.MetadataDiffActionDrop {
				writeDropCheckConstraint(buf, tc.SchemaName, tc.TableName, checkDiff.OldCheckConstraint.Name)
			}
		}

		// Drop indexes
		for _, indexDiff := range tc.IndexChanges {
			if indexDiff.Action == schema.MetadataDiffActionDrop {
				writeDropIndex(buf, tc.SchemaName, indexDiff.OldIndex.Name)
			}
		}

		// Drop columns
		for _, colDiff := range tc.ColumnChanges {
			if colDiff.Action == schema.MetadataDiffActionDrop {
				writeDropColumn(buf, tc.SchemaName, tc.TableName, colDiff.OldColumn.Name)
			}
		}
	}
}

func createObjectsInOrder(diff *schema.MetadataDiff, buf *strings.Builder) error {
	// Create extensions first
	for _, extDiff := range diff.ExtensionChanges {
		if extDiff.Action == schema.MetadataDiffActionCreate {
			writeCreateExtension(buf, extDiff.ExtensionName)
		}
	}

	// Create schemas
	for _, schemaDiff := range diff.SchemaChanges {
		if schemaDiff.Action == schema.MetadataDiffActionCreate {
			writeCreateSchema(buf, schemaDiff.SchemaName)
		}
	}

	// Create enum types
	for _, enumDiff := range diff.EnumTypeChanges {
		if enumDiff.Action == schema.MetadataDiffActionCreate {
			writeCreateEnumType(buf, enumDiff.SchemaName, enumDiff.NewEnumType)
		}
	}
	// Alter enum types (add values)
	for _, enumDiff := range diff.EnumTypeChanges {
		if enumDiff.Action == schema.MetadataDiffActionAlter {
			writeAlterEnumType(buf, enumDiff.SchemaName, enumDiff.EnumTypeName, enumDiff.NewEnumType)
		}
	}

	// Create sequences
	for _, seqDiff := range diff.SequenceChanges {
		if seqDiff.Action == schema.MetadataDiffActionCreate {
			writeCreateSequenceDiff(buf, seqDiff.SchemaName, seqDiff.NewSequence)
		}
	}

	// Create tables (without FKs)
	for _, tc := range diff.TableChanges {
		if tc.Action == schema.MetadataDiffActionCreate && tc.NewTable != nil {
			if err := writeCreateTableDiff(buf, tc.SchemaName, tc.TableName, tc.NewTable); err != nil {
				return err
			}
		}
	}

	// Add foreign keys after all tables are created
	for _, tc := range diff.TableChanges {
		if tc.Action == schema.MetadataDiffActionCreate && tc.NewTable != nil {
			for _, fk := range tc.NewTable.ForeignKeys {
				writeAddForeignKey(buf, tc.SchemaName, tc.TableName, fk)
			}
		}
	}

	// Handle ALTER table operations
	for _, tc := range diff.TableChanges {
		if tc.Action == schema.MetadataDiffActionAlter {
			writeAlterTable(tc, buf)
		}
	}

	// Create views
	for _, viewDiff := range diff.ViewChanges {
		switch viewDiff.Action {
		case schema.MetadataDiffActionCreate:
			if viewDiff.NewView != nil {
				writeCreateView(buf, viewDiff.SchemaName, viewDiff.ViewName, viewDiff.NewView)
			}
		case schema.MetadataDiffActionAlter:
			if viewDiff.NewView != nil {
				writeCreateOrReplaceView(buf, viewDiff.SchemaName, viewDiff.ViewName, viewDiff.NewView)
			}
		default:
		}
	}

	// Create materialized views
	for _, mvDiff := range diff.MaterializedViewChanges {
		if mvDiff.Action == schema.MetadataDiffActionCreate && mvDiff.NewMaterializedView != nil {
			writeCreateMaterializedView(buf, mvDiff.SchemaName, mvDiff.MaterializedViewName, mvDiff.NewMaterializedView)
		}
	}

	// Create functions
	for _, funcDiff := range diff.FunctionChanges {
		if funcDiff.Action == schema.MetadataDiffActionCreate || funcDiff.Action == schema.MetadataDiffActionAlter {
			if funcDiff.NewFunction != nil {
				writeCreateOrReplaceFunction(buf, funcDiff.SchemaName, funcDiff.NewFunction)
			}
		}
	}

	// Create procedures
	for _, procDiff := range diff.ProcedureChanges {
		if procDiff.Action == schema.MetadataDiffActionCreate || procDiff.Action == schema.MetadataDiffActionAlter {
			if procDiff.NewProcedure != nil {
				writeCreateOrReplaceProcedure(buf, procDiff.SchemaName, procDiff.NewProcedure)
			}
		}
	}

	// Create triggers on tables
	for _, tc := range diff.TableChanges {
		for _, td := range tc.TriggerChanges {
			if td.Action == schema.MetadataDiffActionCreate {
				writeCreateTrigger(buf, tc.SchemaName, tc.TableName, td.NewTrigger)
			}
		}
	}

	// Create event triggers
	for _, etDiff := range diff.EventTriggerChanges {
		if etDiff.Action == schema.MetadataDiffActionCreate {
			writeCreateEventTrigger(buf, etDiff.EventTriggerName, etDiff.NewEventTrigger)
		}
	}

	// Handle comment changes
	for _, cc := range diff.CommentChanges {
		writeCommentChange(buf, cc)
	}

	return nil
}

func writeAlterTable(tc *schema.TableDiff, buf *strings.Builder) {
	schemaName := tc.SchemaName
	tableName := tc.TableName

	// Add columns
	for _, colDiff := range tc.ColumnChanges {
		if colDiff.Action == schema.MetadataDiffActionCreate {
			writeAddColumn(buf, schemaName, tableName, colDiff.NewColumn)
		}
	}

	// Alter columns
	for _, colDiff := range tc.ColumnChanges {
		if colDiff.Action == schema.MetadataDiffActionAlter {
			writeAlterColumn(buf, schemaName, tableName, colDiff.NewColumn)
		}
	}

	// Add indexes
	for _, indexDiff := range tc.IndexChanges {
		if indexDiff.Action == schema.MetadataDiffActionCreate {
			if indexDiff.NewIndex.Primary {
				writeAddPrimaryKey(buf, schemaName, tableName, indexDiff.NewIndex)
			} else if indexDiff.NewIndex.Unique {
				writeAddUniqueConstraint(buf, schemaName, tableName, indexDiff.NewIndex)
			} else {
				writeCreateIndex(buf, schemaName, tableName, indexDiff.NewIndex)
			}
		}
	}

	// Add check constraints
	for _, checkDiff := range tc.CheckConstraintChanges {
		if checkDiff.Action == schema.MetadataDiffActionCreate {
			writeAddCheckConstraint(buf, schemaName, tableName, checkDiff.NewCheckConstraint)
		}
	}

	// Add foreign keys
	for _, fkDiff := range tc.ForeignKeyChanges {
		if fkDiff.Action == schema.MetadataDiffActionCreate {
			writeAddForeignKey(buf, schemaName, tableName, fkDiff.NewForeignKey)
		}
	}

	// Handle table comment changes
	if tc.OldTable != nil && tc.NewTable != nil {
		if tc.OldTable.Comment != tc.NewTable.Comment {
			writeCommentOnTable(buf, schemaName, tableName, tc.NewTable.Comment)
		}
	}
}

func hasCreateOrAlterObjects(diff *schema.MetadataDiff) bool {
	for _, tc := range diff.TableChanges {
		if tc.Action == schema.MetadataDiffActionCreate || tc.Action == schema.MetadataDiffActionAlter {
			return true
		}
	}
	for _, vc := range diff.ViewChanges {
		if vc.Action == schema.MetadataDiffActionCreate || vc.Action == schema.MetadataDiffActionAlter {
			return true
		}
	}
	for _, mc := range diff.MaterializedViewChanges {
		if mc.Action == schema.MetadataDiffActionCreate || mc.Action == schema.MetadataDiffActionAlter {
			return true
		}
	}
	for _, fc := range diff.FunctionChanges {
		if fc.Action == schema.MetadataDiffActionCreate || fc.Action == schema.MetadataDiffActionAlter {
			return true
		}
	}
	for _, pc := range diff.ProcedureChanges {
		if pc.Action == schema.MetadataDiffActionCreate || pc.Action == schema.MetadataDiffActionAlter {
			return true
		}
	}
	for _, sc := range diff.SchemaChanges {
		if sc.Action == schema.MetadataDiffActionCreate || sc.Action == schema.MetadataDiffActionAlter {
			return true
		}
	}
	for _, ec := range diff.EnumTypeChanges {
		if ec.Action == schema.MetadataDiffActionCreate || ec.Action == schema.MetadataDiffActionAlter {
			return true
		}
	}
	for _, sc := range diff.SequenceChanges {
		if sc.Action == schema.MetadataDiffActionCreate || sc.Action == schema.MetadataDiffActionAlter {
			return true
		}
	}
	return false
}

// ---- DDL write helpers ----

func writeDropEventTrigger(buf *strings.Builder, name string) {
	_, _ = fmt.Fprintf(buf, "DROP EVENT TRIGGER IF EXISTS \"%s\";\n\n", name)
}

func writeDropTrigger(buf *strings.Builder, schema, table, name string) {
	_, _ = fmt.Fprintf(buf, "DROP TRIGGER IF EXISTS \"%s\" ON \"%s\".\"%s\";\n\n", name, schema, table)
}

func writeDropForeignKey(buf *strings.Builder, schema, table, name string) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE \"%s\".\"%s\" DROP CONSTRAINT \"%s\";\n\n", schema, table, name)
}

func writeDropView(buf *strings.Builder, schema, view string) {
	_, _ = fmt.Fprintf(buf, "DROP VIEW IF EXISTS \"%s\".\"%s\";\n\n", schema, view)
}

func writeDropMaterializedView(buf *strings.Builder, schema, view string) {
	_, _ = fmt.Fprintf(buf, "DROP MATERIALIZED VIEW IF EXISTS \"%s\".\"%s\";\n\n", schema, view)
}

func writeDropFunction(buf *strings.Builder, schema, name string) {
	_, _ = fmt.Fprintf(buf, "DROP FUNCTION IF EXISTS \"%s\".%s;\n\n", schema, name)
}

func writeDropProcedure(buf *strings.Builder, schema, name string) {
	_, _ = fmt.Fprintf(buf, "DROP PROCEDURE IF EXISTS \"%s\".%s;\n\n", schema, name)
}

func writeDropEvent(buf *strings.Builder, name string) {
	_, _ = fmt.Fprintf(buf, "DROP EVENT IF EXISTS \"%s\";\n\n", name)
}

func writeDropTable(buf *strings.Builder, schema, table string) {
	_, _ = fmt.Fprintf(buf, "DROP TABLE IF EXISTS \"%s\".\"%s\";\n\n", schema, table)
}

func writeDropSequence(buf *strings.Builder, schema, name string) {
	_, _ = fmt.Fprintf(buf, "DROP SEQUENCE IF EXISTS \"%s\".\"%s\";\n\n", schema, name)
}

func writeDropEnumType(buf *strings.Builder, schema, name string) {
	_, _ = fmt.Fprintf(buf, "DROP TYPE IF EXISTS \"%s\".\"%s\";\n\n", schema, name)
}

func writeDropSchema(buf *strings.Builder, name string) {
	_, _ = fmt.Fprintf(buf, "DROP SCHEMA IF EXISTS \"%s\" CASCADE;\n\n", name)
}

func writeDropExtension(buf *strings.Builder, name string) {
	_, _ = fmt.Fprintf(buf, "DROP EXTENSION IF EXISTS \"%s\";\n\n", name)
}

func writeDropCheckConstraint(buf *strings.Builder, schema, table, name string) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE \"%s\".\"%s\" DROP CONSTRAINT \"%s\";\n\n", schema, table, name)
}

func writeDropIndex(buf *strings.Builder, schema, name string) {
	_, _ = fmt.Fprintf(buf, "DROP INDEX IF EXISTS \"%s\".\"%s\";\n\n", schema, name)
}

func writeDropColumn(buf *strings.Builder, schema, table, column string) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE \"%s\".\"%s\" DROP COLUMN \"%s\";\n\n", schema, table, column)
}

func writeCreateExtension(buf *strings.Builder, name string) {
	_, _ = fmt.Fprintf(buf, "CREATE EXTENSION IF NOT EXISTS \"%s\";\n\n", name)
}

func writeCreateSchema(buf *strings.Builder, name string) {
	_, _ = fmt.Fprintf(buf, "CREATE SCHEMA IF NOT EXISTS \"%s\";\n\n", name)
}

func writeCreateEnumType(buf *strings.Builder, schema string, enum *storepb.EnumTypeMetadata) {
	_, _ = fmt.Fprintf(buf, "CREATE TYPE \"%s\".\"%s\" AS ENUM (", schema, enum.Name)
	for i, val := range enum.Values {
		if i > 0 {
			_, _ = buf.WriteString(", ")
		}
		_, _ = fmt.Fprintf(buf, "'%s'", escapePgQuote(val))
	}
	_, _ = buf.WriteString(");\n\n")
}

func writeAlterEnumType(buf *strings.Builder, schema, name string, enum *storepb.EnumTypeMetadata) {
	for _, val := range enum.Values {
		_, _ = fmt.Fprintf(buf, "ALTER TYPE \"%s\".\"%s\" ADD VALUE IF NOT EXISTS '%s';\n", schema, name, escapePgQuote(val))
	}
	_, _ = buf.WriteString("\n")
}

func writeCreateSequenceDiff(buf *strings.Builder, schema string, seq *storepb.SequenceMetadata) {
	_, _ = fmt.Fprintf(buf, "CREATE SEQUENCE IF NOT EXISTS \"%s\".\"%s\"", schema, seq.Name)
	if seq.DataType != "" {
		_, _ = fmt.Fprintf(buf, " AS %s", seq.DataType)
	}
	if seq.Start != "" {
		_, _ = fmt.Fprintf(buf, " START WITH %s", seq.Start)
	}
	if seq.Increment != "" {
		_, _ = fmt.Fprintf(buf, " INCREMENT BY %s", seq.Increment)
	}
	if seq.MinValue != "" {
		_, _ = fmt.Fprintf(buf, " MINVALUE %s", seq.MinValue)
	}
	if seq.MaxValue != "" {
		_, _ = fmt.Fprintf(buf, " MAXVALUE %s", seq.MaxValue)
	}
	if seq.CacheSize != "" {
		_, _ = fmt.Fprintf(buf, " CACHE %s", seq.CacheSize)
	}
	if seq.Cycle {
		_, _ = buf.WriteString(" CYCLE")
	}
	_, _ = buf.WriteString(";\n\n")
}

func writeCreateTableDiff(buf *strings.Builder, schema, tableName string, table *storepb.TableMetadata) error {
	_, _ = fmt.Fprintf(buf, "CREATE TABLE IF NOT EXISTS \"%s\".\"%s\" (\n", schema, tableName)

	for i, col := range table.Columns {
		if i > 0 {
			_, _ = buf.WriteString(",\n")
		}
		_, _ = fmt.Fprintf(buf, "  \"%s\" %s", col.Name, col.Type)

		if col.Generation != nil && col.Generation.Expression != "" {
			_, _ = fmt.Fprintf(buf, " GENERATED ALWAYS AS (%s) STORED", col.Generation.Expression)
		}

		if !col.Nullable {
			_, _ = buf.WriteString(" NOT NULL")
		}

		if col.Default != "" && !strings.EqualFold(col.Default, "NULL") &&
			(col.Generation == nil || col.Generation.Expression == "") {
			_, _ = fmt.Fprintf(buf, " DEFAULT %s", col.Default)
		}
	}

	// Primary key
	for _, index := range table.Indexes {
		if index.Primary {
			_, _ = buf.WriteString(",\n  PRIMARY KEY (")
			for i, expr := range index.Expressions {
				if i > 0 {
					_, _ = buf.WriteString(", ")
				}
				_, _ = fmt.Fprintf(buf, "\"%s\"", expr)
			}
			_, _ = buf.WriteString(")")
			break
		}
	}

	// Unique constraints
	for _, index := range table.Indexes {
		if index.Unique && !index.Primary && index.IsConstraint {
			_, _ = fmt.Fprintf(buf, ",\n  CONSTRAINT \"%s\" UNIQUE (", index.Name)
			for i, expr := range index.Expressions {
				if i > 0 {
					_, _ = buf.WriteString(", ")
				}
				_, _ = fmt.Fprintf(buf, "\"%s\"", expr)
			}
			_, _ = buf.WriteString(")")
		}
	}

	// Check constraints
	for _, check := range table.CheckConstraints {
		_, _ = fmt.Fprintf(buf, ",\n  CONSTRAINT \"%s\" CHECK (%s)", check.Name, check.Expression)
	}

	_, _ = buf.WriteString("\n);\n")

	// Non-constraint indexes
	for _, index := range table.Indexes {
		if !index.Primary && !index.IsConstraint {
			writeCreateIndex(buf, schema, tableName, index)
		}
	}

	// Table comment
	if table.Comment != "" {
		writeCommentOnTable(buf, schema, tableName, table.Comment)
	}

	_, _ = buf.WriteString("\n")
	return nil
}

func writeCreateIndex(buf *strings.Builder, schema, table string, index *storepb.IndexMetadata) {
	_, _ = fmt.Fprintf(buf, "CREATE INDEX \"%s\" ON \"%s\".\"%s\"", index.Name, schema, table)

	if index.Type != "" && !strings.EqualFold(index.Type, "btree") {
		_, _ = fmt.Fprintf(buf, " USING %s", strings.ToUpper(index.Type))
	}

	_, _ = buf.WriteString(" (")
	for i, expr := range index.Expressions {
		if i > 0 {
			_, _ = buf.WriteString(", ")
		}
		_, _ = buf.WriteString(expr)
	}
	_, _ = buf.WriteString(");\n\n")
}

func writeAddColumn(buf *strings.Builder, schema, table string, col *storepb.ColumnMetadata) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE \"%s\".\"%s\" ADD COLUMN \"%s\" %s", schema, table, col.Name, col.Type)

	if !col.Nullable {
		_, _ = buf.WriteString(" NOT NULL")
	}
	if col.Default != "" && !strings.EqualFold(col.Default, "NULL") {
		_, _ = fmt.Fprintf(buf, " DEFAULT %s", col.Default)
	}

	_, _ = buf.WriteString(";\n\n")
}

func writeAlterColumn(buf *strings.Builder, schema, table string, col *storepb.ColumnMetadata) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE \"%s\".\"%s\" ALTER COLUMN \"%s\" TYPE %s", schema, table, col.Name, col.Type)

	if !col.Nullable {
		_, _ = fmt.Fprintf(buf, ";\nALTER TABLE \"%s\".\"%s\" ALTER COLUMN \"%s\" SET NOT NULL", schema, table, col.Name)
	}

	_, _ = buf.WriteString(";\n\n")
}

func writeAddPrimaryKey(buf *strings.Builder, schema, table string, index *storepb.IndexMetadata) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE \"%s\".\"%s\" ADD PRIMARY KEY (", schema, table)
	for i, expr := range index.Expressions {
		if i > 0 {
			_, _ = buf.WriteString(", ")
		}
		_, _ = fmt.Fprintf(buf, "\"%s\"", expr)
	}
	_, _ = buf.WriteString(");\n\n")
}

func writeAddUniqueConstraint(buf *strings.Builder, schema, table string, index *storepb.IndexMetadata) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE \"%s\".\"%s\" ADD CONSTRAINT \"%s\" UNIQUE (", schema, table, index.Name)
	for i, expr := range index.Expressions {
		if i > 0 {
			_, _ = buf.WriteString(", ")
		}
		_, _ = fmt.Fprintf(buf, "\"%s\"", expr)
	}
	_, _ = buf.WriteString(");\n\n")
}

func writeAddForeignKey(buf *strings.Builder, schema, table string, fk *storepb.ForeignKeyMetadata) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE \"%s\".\"%s\" ADD CONSTRAINT \"%s\" FOREIGN KEY (", schema, table, fk.Name)
	for i, col := range fk.Columns {
		if i > 0 {
			_, _ = buf.WriteString(", ")
		}
		_, _ = fmt.Fprintf(buf, "\"%s\"", col)
	}

	refSchema := fk.ReferencedSchema
	if refSchema == "" {
		refSchema = schema
	}
	_, _ = fmt.Fprintf(buf, ") REFERENCES \"%s\".\"%s\" (", refSchema, fk.ReferencedTable)
	for i, col := range fk.ReferencedColumns {
		if i > 0 {
			_, _ = buf.WriteString(", ")
		}
		_, _ = fmt.Fprintf(buf, "\"%s\"", col)
	}
	_, _ = buf.WriteString(")")

	if fk.OnDelete != "" && !strings.EqualFold(fk.OnDelete, "NO ACTION") {
		_, _ = fmt.Fprintf(buf, " ON DELETE %s", fk.OnDelete)
	}
	if fk.OnUpdate != "" && !strings.EqualFold(fk.OnUpdate, "NO ACTION") {
		_, _ = fmt.Fprintf(buf, " ON UPDATE %s", fk.OnUpdate)
	}

	_, _ = buf.WriteString(";\n\n")
}

func writeAddCheckConstraint(buf *strings.Builder, schema, table string, check *storepb.CheckConstraintMetadata) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE \"%s\".\"%s\" ADD CONSTRAINT \"%s\" CHECK (%s);\n\n", schema, table, check.Name, check.Expression)
}

func writeCreateTrigger(buf *strings.Builder, _, _ string, trigger *storepb.TriggerMetadata) {
	_, _ = fmt.Fprintf(buf, "%s;\n\n", strings.TrimRight(trigger.Body, ";"))
}

func writeCreateView(buf *strings.Builder, schema, viewName string, view *storepb.ViewMetadata) {
	_, _ = fmt.Fprintf(buf, "CREATE VIEW \"%s\".\"%s\" AS\n%s;\n\n", schema, viewName, view.Definition)
}

func writeCreateOrReplaceView(buf *strings.Builder, schema, viewName string, view *storepb.ViewMetadata) {
	_, _ = fmt.Fprintf(buf, "CREATE OR REPLACE VIEW \"%s\".\"%s\" AS\n%s;\n\n", schema, viewName, view.Definition)
}

func writeCreateMaterializedView(buf *strings.Builder, schema, viewName string, view *storepb.MaterializedViewMetadata) {
	_, _ = fmt.Fprintf(buf, "CREATE MATERIALIZED VIEW \"%s\".\"%s\" AS\n%s\nWITH NO DATA;\n\n", schema, viewName, view.Definition)
}

func writeCreateOrReplaceFunction(buf *strings.Builder, _ string, fn *storepb.FunctionMetadata) {
	_, _ = fmt.Fprintf(buf, "%s;\n\n", strings.TrimRight(fn.Definition, ";"))
}

func writeCreateOrReplaceProcedure(buf *strings.Builder, _ string, proc *storepb.ProcedureMetadata) {
	_, _ = fmt.Fprintf(buf, "%s;\n\n", strings.TrimRight(proc.Definition, ";"))
}

func writeCreateEventTrigger(buf *strings.Builder, _ string, trigger *storepb.EventTriggerMetadata) {
	_, _ = fmt.Fprintf(buf, "%s;\n\n", strings.TrimRight(trigger.Definition, ";"))
}

func writeCommentOnTable(buf *strings.Builder, schema, table, comment string) {
	if comment == "" {
		_, _ = fmt.Fprintf(buf, "COMMENT ON TABLE \"%s\".\"%s\" IS NULL;\n", schema, table)
	} else {
		_, _ = fmt.Fprintf(buf, "COMMENT ON TABLE \"%s\".\"%s\" IS '%s';\n", schema, table, escapePgQuote(comment))
	}
}

func writeCommentChange(buf *strings.Builder, cc *schema.CommentDiff) {
	switch cc.ObjectType {
	case schema.CommentObjectTypeTable:
		if cc.NewComment == "" {
			_, _ = fmt.Fprintf(buf, "COMMENT ON TABLE \"%s\".\"%s\" IS NULL;\n\n", cc.SchemaName, cc.ObjectName)
		} else {
			_, _ = fmt.Fprintf(buf, "COMMENT ON TABLE \"%s\".\"%s\" IS '%s';\n\n", cc.SchemaName, cc.ObjectName, escapePgQuote(cc.NewComment))
		}
	case schema.CommentObjectTypeColumn:
		parts := strings.SplitN(cc.ObjectName, ".", 2)
		if len(parts) == 2 {
			tableName, colName := parts[0], parts[1]
			if cc.NewComment == "" {
				_, _ = fmt.Fprintf(buf, "COMMENT ON COLUMN \"%s\".\"%s\".\"%s\" IS NULL;\n\n", cc.SchemaName, tableName, colName)
			} else {
				_, _ = fmt.Fprintf(buf, "COMMENT ON COLUMN \"%s\".\"%s\".\"%s\" IS '%s';\n\n", cc.SchemaName, tableName, colName, escapePgQuote(cc.NewComment))
			}
		}
	default:
	}
}

func escapePgQuote(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
