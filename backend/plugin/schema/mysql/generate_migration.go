package mysql

import (
	"fmt"
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/schema"
)

func init() {
	schema.RegisterGenerateMigration(storepb.Engine_MYSQL, generateMigration)
	schema.RegisterGenerateMigration(storepb.Engine_OCEANBASE, generateMigration)
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
	for _, tc := range diff.TableChanges {
		if tc.Action == schema.MetadataDiffActionDrop && tc.OldTable != nil {
			for _, trigger := range tc.OldTable.Triggers {
				writeDropTrigger(buf, trigger.Name)
			}
		} else if tc.Action == schema.MetadataDiffActionAlter {
			for _, td := range tc.TriggerChanges {
				if td.Action == schema.MetadataDiffActionDrop {
					writeDropTrigger(buf, td.OldTrigger.Name)
				}
			}
		}
	}

	for _, tc := range diff.TableChanges {
		if tc.Action == schema.MetadataDiffActionAlter {
			for _, fkDiff := range tc.ForeignKeyChanges {
				if fkDiff.Action == schema.MetadataDiffActionDrop {
					writeDropForeignKey(buf, tc.TableName, fkDiff.OldForeignKey.Name)
				}
			}
		}
	}

	for _, tc := range diff.TableChanges {
		if tc.Action == schema.MetadataDiffActionDrop && tc.OldTable != nil {
			for _, fk := range tc.OldTable.ForeignKeys {
				writeDropForeignKey(buf, tc.TableName, fk.Name)
			}
		}
	}

	for _, eventDiff := range diff.EventChanges {
		if eventDiff.Action == schema.MetadataDiffActionDrop {
			writeDropEvent(buf, eventDiff.EventName)
		}
	}

	for _, viewDiff := range diff.ViewChanges {
		if viewDiff.Action == schema.MetadataDiffActionDrop {
			writeDropView(buf, viewDiff.ViewName)
		}
	}

	for _, procDiff := range diff.ProcedureChanges {
		if procDiff.Action == schema.MetadataDiffActionDrop {
			writeDropProcedure(buf, procDiff.ProcedureName)
		}
	}

	for _, funcDiff := range diff.FunctionChanges {
		if funcDiff.Action == schema.MetadataDiffActionDrop {
			writeDropFunction(buf, funcDiff.FunctionName)
		}
	}

	for _, tc := range diff.TableChanges {
		if tc.Action == schema.MetadataDiffActionDrop {
			writeDropTable(buf, tc.TableName)
		}
	}

	for _, tc := range diff.TableChanges {
		if tc.Action != schema.MetadataDiffActionAlter {
			continue
		}
		tableName := tc.TableName

		for _, checkDiff := range tc.CheckConstraintChanges {
			if checkDiff.Action == schema.MetadataDiffActionDrop {
				writeDropCheckConstraint(buf, tableName, checkDiff.OldCheckConstraint.Name)
			}
		}

		for _, indexDiff := range tc.IndexChanges {
			if indexDiff.Action == schema.MetadataDiffActionDrop {
				if indexDiff.OldIndex.Primary {
					writeDropPrimaryKey(buf, tableName)
				} else {
					writeDropIndex(buf, tableName, indexDiff.OldIndex.Name)
				}
			}
		}

		for _, colDiff := range tc.ColumnChanges {
			if colDiff.Action == schema.MetadataDiffActionDrop {
				writeDropColumn(buf, tableName, colDiff.OldColumn.Name)
			}
		}
	}
}

func createObjectsInOrder(diff *schema.MetadataDiff, buf *strings.Builder) error {
	for _, tc := range diff.TableChanges {
		if tc.Action == schema.MetadataDiffActionCreate && tc.NewTable != nil {
			if err := writeCreateTable(buf, tc.TableName, tc.NewTable); err != nil {
				return err
			}
		}
	}

	for _, tc := range diff.TableChanges {
		if tc.Action == schema.MetadataDiffActionCreate && tc.NewTable != nil {
			for _, fk := range tc.NewTable.ForeignKeys {
				writeAddForeignKey(buf, tc.TableName, fk)
			}
		}
	}

	for _, tc := range diff.TableChanges {
		if tc.Action == schema.MetadataDiffActionAlter {
			if err := generateAlterTable(tc, buf); err != nil {
				return err
			}
		}
	}

	for _, viewDiff := range diff.ViewChanges {
		switch viewDiff.Action {
		case schema.MetadataDiffActionCreate, schema.MetadataDiffActionAlter:
			if err := writeCreateOrReplaceView(buf, viewDiff.ViewName, viewDiff.NewView); err != nil {
				return err
			}
		default:
		}
	}

	for _, funcDiff := range diff.FunctionChanges {
		if funcDiff.Action == schema.MetadataDiffActionCreate || funcDiff.Action == schema.MetadataDiffActionAlter {
			if funcDiff.NewFunction != nil {
				writeCreateFunction(buf, funcDiff.NewFunction)
			}
		}
	}

	for _, procDiff := range diff.ProcedureChanges {
		if procDiff.Action == schema.MetadataDiffActionCreate || procDiff.Action == schema.MetadataDiffActionAlter {
			if procDiff.NewProcedure != nil {
				writeCreateProcedure(buf, procDiff.NewProcedure)
			}
		}
	}

	for _, eventDiff := range diff.EventChanges {
		if eventDiff.Action == schema.MetadataDiffActionCreate || eventDiff.Action == schema.MetadataDiffActionAlter {
			if eventDiff.NewEvent != nil {
				writeCreateEvent(buf, eventDiff.NewEvent)
			}
		}
	}

	for _, cc := range diff.CommentChanges {
		writeCommentChange(buf, cc)
	}

	return nil
}

func generateAlterTable(tc *schema.TableDiff, buf *strings.Builder) error {
	tableName := tc.TableName

	for _, colDiff := range tc.ColumnChanges {
		if colDiff.Action == schema.MetadataDiffActionAlter {
			writeModifyColumn(buf, tableName, colDiff.NewColumn)
		}
	}

	for _, colDiff := range tc.ColumnChanges {
		if colDiff.Action == schema.MetadataDiffActionCreate {
			writeAddColumn(buf, tableName, colDiff.NewColumn)
		}
	}

	for _, indexDiff := range tc.IndexChanges {
		if indexDiff.Action == schema.MetadataDiffActionCreate {
			if indexDiff.NewIndex.Primary {
				writeAddPrimaryKey(buf, tableName, indexDiff.NewIndex)
			} else if indexDiff.NewIndex.Unique {
				writeAddUniqueKey(buf, tableName, indexDiff.NewIndex)
			} else {
				writeCreateIndex(buf, tableName, indexDiff.NewIndex)
			}
		}
	}

	for _, checkDiff := range tc.CheckConstraintChanges {
		if checkDiff.Action == schema.MetadataDiffActionCreate {
			writeAddCheckConstraint(buf, tableName, checkDiff.NewCheckConstraint)
		}
	}

	for _, fkDiff := range tc.ForeignKeyChanges {
		if fkDiff.Action == schema.MetadataDiffActionCreate {
			writeAddForeignKey(buf, tableName, fkDiff.NewForeignKey)
		}
	}

	for _, triggerDiff := range tc.TriggerChanges {
		if triggerDiff.Action == schema.MetadataDiffActionCreate {
			writeCreateTrigger(buf, tableName, triggerDiff.NewTrigger)
		}
	}

	if tc.OldTable != nil && tc.NewTable != nil {
		if tc.OldTable.Comment != tc.NewTable.Comment {
			writeAlterTableComment(buf, tableName, tc.NewTable.Comment)
		}
		if tc.OldTable.Engine != tc.NewTable.Engine && tc.NewTable.Engine != "" {
			writeAlterTableEngine(buf, tableName, tc.NewTable.Engine)
		}
	}

	return nil
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
	for _, ec := range diff.EventChanges {
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

// ---- DDL write helpers (all use _, _ = to satisfy unhandled-error linter) ----

func writeDropTrigger(buf *strings.Builder, name string) {
	_, _ = fmt.Fprintf(buf, "DROP TRIGGER IF EXISTS `%s`;\n\n", name)
}

func writeDropForeignKey(buf *strings.Builder, table, constraint string) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE `%s` DROP FOREIGN KEY `%s`;\n\n", table, constraint)
}

func writeDropView(buf *strings.Builder, view string) {
	_, _ = fmt.Fprintf(buf, "DROP VIEW IF EXISTS `%s`;\n\n", view)
}

func writeDropProcedure(buf *strings.Builder, procedure string) {
	_, _ = fmt.Fprintf(buf, "DROP PROCEDURE IF EXISTS `%s`;\n\n", procedure)
}

func writeDropFunction(buf *strings.Builder, function string) {
	_, _ = fmt.Fprintf(buf, "DROP FUNCTION IF EXISTS `%s`;\n\n", function)
}

func writeDropEvent(buf *strings.Builder, event string) {
	_, _ = fmt.Fprintf(buf, "DROP EVENT IF EXISTS `%s`;\n\n", event)
}

func writeDropTable(buf *strings.Builder, table string) {
	_, _ = fmt.Fprintf(buf, "DROP TABLE IF EXISTS `%s`;\n\n", table)
}

func writeDropCheckConstraint(buf *strings.Builder, table, constraint string) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE `%s` DROP CHECK `%s`;\n\n", table, constraint)
}

func writeDropIndex(buf *strings.Builder, table, index string) {
	_, _ = fmt.Fprintf(buf, "DROP INDEX `%s` ON `%s`;\n\n", index, table)
}

func writeDropPrimaryKey(buf *strings.Builder, table string) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE `%s` DROP PRIMARY KEY;\n\n", table)
}

func writeDropColumn(buf *strings.Builder, table, column string) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE `%s` DROP COLUMN `%s`;\n\n", table, column)
}

func writeCreateTable(buf *strings.Builder, tableName string, table *storepb.TableMetadata) error {
	_, _ = fmt.Fprintf(buf, "CREATE TABLE IF NOT EXISTS `%s` (\n", tableName)

	for i, col := range table.Columns {
		if i > 0 {
			_, _ = buf.WriteString(",\n")
		}
		writeColumnDef(buf, col, table)
	}

	for _, index := range table.Indexes {
		if index.Primary {
			_, _ = buf.WriteString(",\n  PRIMARY KEY (")
			for i, expr := range index.Expressions {
				if i > 0 {
					_, _ = buf.WriteString(", ")
				}
				_, _ = fmt.Fprintf(buf, "`%s`", expr)
			}
			_, _ = buf.WriteString(")")
			break
		}
	}

	for _, index := range table.Indexes {
		if index.Unique && !index.Primary {
			_, _ = fmt.Fprintf(buf, ",\n  UNIQUE KEY `%s` (", index.Name)
			for i, expr := range index.Expressions {
				if i > 0 {
					_, _ = buf.WriteString(", ")
				}
				_, _ = fmt.Fprintf(buf, "`%s`", expr)
			}
			_, _ = buf.WriteString(")")
		}
	}

	for _, check := range table.CheckConstraints {
		_, _ = fmt.Fprintf(buf, ",\n  CONSTRAINT `%s` CHECK (%s)", check.Name, check.Expression)
	}

	_, _ = buf.WriteString("\n)")

	if table.Engine != "" {
		_, _ = fmt.Fprintf(buf, " ENGINE=%s", table.Engine)
	}
	if table.Charset != "" {
		_, _ = fmt.Fprintf(buf, " DEFAULT CHARSET=%s", table.Charset)
	}
	if table.Collation != "" {
		_, _ = fmt.Fprintf(buf, " COLLATE=%s", table.Collation)
	}
	if table.Comment != "" {
		_, _ = fmt.Fprintf(buf, " COMMENT='%s'", escapeSingleQuote(table.Comment))
	}

	_, _ = buf.WriteString(";\n")

	for _, index := range table.Indexes {
		if !index.Primary && !index.Unique {
			writeCreateIndex(buf, tableName, index)
		}
	}

	_, _ = buf.WriteString("\n")
	return nil
}

func writeColumnDef(buf *strings.Builder, col *storepb.ColumnMetadata, table *storepb.TableMetadata) {
	_, _ = fmt.Fprintf(buf, "  `%s` %s", col.Name, col.Type)

	if col.CharacterSet != "" && col.CharacterSet != table.Charset {
		_, _ = fmt.Fprintf(buf, " CHARACTER SET %s", col.CharacterSet)
	}
	if col.Collation != "" && col.Collation != table.Collation {
		_, _ = fmt.Fprintf(buf, " COLLATE %s", col.Collation)
	}

	if col.Generation != nil && col.Generation.Expression != "" {
		_, _ = fmt.Fprintf(buf, " GENERATED ALWAYS AS (%s) ", col.Generation.Expression)
		switch col.Generation.Type {
		case storepb.GenerationMetadata_TYPE_STORED:
			_, _ = buf.WriteString("STORED")
		case storepb.GenerationMetadata_TYPE_VIRTUAL:
			_, _ = buf.WriteString("VIRTUAL")
		default:
		}
	}

	if !col.Nullable {
		_, _ = buf.WriteString(" NOT NULL")
	}

	if hasDefaultValue(col) && !hasAutoIncrement(col) && (col.Generation == nil || col.Generation.Expression == "") {
		_, _ = fmt.Fprintf(buf, " DEFAULT %s", col.Default)
	}

	if hasAutoIncrement(col) {
		_, _ = buf.WriteString(" AUTO_INCREMENT")
	}

	if col.OnUpdate != "" {
		_, _ = fmt.Fprintf(buf, " ON UPDATE %s", col.OnUpdate)
	}
	if col.Comment != "" {
		_, _ = fmt.Fprintf(buf, " COMMENT '%s'", escapeSingleQuote(col.Comment))
	}
}

func writeAddColumn(buf *strings.Builder, table string, col *storepb.ColumnMetadata) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE `%s` ADD COLUMN `%s` %s", table, col.Name, col.Type)

	if !col.Nullable {
		_, _ = buf.WriteString(" NOT NULL")
	}
	if hasDefaultValue(col) && !hasAutoIncrement(col) {
		_, _ = fmt.Fprintf(buf, " DEFAULT %s", col.Default)
	}
	if hasAutoIncrement(col) {
		_, _ = buf.WriteString(" AUTO_INCREMENT")
	}
	if col.OnUpdate != "" {
		_, _ = fmt.Fprintf(buf, " ON UPDATE %s", col.OnUpdate)
	}
	if col.Comment != "" {
		_, _ = fmt.Fprintf(buf, " COMMENT '%s'", escapeSingleQuote(col.Comment))
	}

	_, _ = buf.WriteString(";\n\n")
}

func writeModifyColumn(buf *strings.Builder, table string, col *storepb.ColumnMetadata) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE `%s` MODIFY COLUMN `%s` %s", table, col.Name, col.Type)

	if !col.Nullable {
		_, _ = buf.WriteString(" NOT NULL")
	}
	if hasDefaultValue(col) && !hasAutoIncrement(col) {
		_, _ = fmt.Fprintf(buf, " DEFAULT %s", col.Default)
	}
	if hasAutoIncrement(col) {
		_, _ = buf.WriteString(" AUTO_INCREMENT")
	}
	if col.OnUpdate != "" {
		_, _ = fmt.Fprintf(buf, " ON UPDATE %s", col.OnUpdate)
	}
	if col.Comment != "" {
		_, _ = fmt.Fprintf(buf, " COMMENT '%s'", escapeSingleQuote(col.Comment))
	}

	_, _ = buf.WriteString(";\n\n")
}

func writeCreateIndex(buf *strings.Builder, table string, index *storepb.IndexMetadata) {
	_, _ = buf.WriteString("CREATE ")
	if strings.EqualFold(index.Type, "FULLTEXT") {
		_, _ = buf.WriteString("FULLTEXT ")
	} else if strings.EqualFold(index.Type, "SPATIAL") {
		_, _ = buf.WriteString("SPATIAL ")
	}

	_, _ = fmt.Fprintf(buf, "INDEX `%s` ON `%s` (", index.Name, table)

	for i, expr := range index.Expressions {
		if i > 0 {
			_, _ = buf.WriteString(", ")
		}
		if strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") {
			_, _ = buf.WriteString(strings.ReplaceAll(expr, "\\'", "'"))
		} else {
			_, _ = fmt.Fprintf(buf, "`%s`", expr)
		}
		if i < len(index.KeyLength) && index.KeyLength[i] > 0 &&
			!strings.EqualFold(index.Type, "SPATIAL") {
			_, _ = fmt.Fprintf(buf, "(%d)", index.KeyLength[i])
		}
	}

	_, _ = buf.WriteString(")")

	if index.Comment != "" {
		_, _ = fmt.Fprintf(buf, " COMMENT '%s'", escapeSingleQuote(index.Comment))
	}

	_, _ = buf.WriteString(";\n\n")
}

func writeAddPrimaryKey(buf *strings.Builder, table string, index *storepb.IndexMetadata) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE `%s` ADD PRIMARY KEY (", table)
	for i, expr := range index.Expressions {
		if i > 0 {
			_, _ = buf.WriteString(", ")
		}
		_, _ = fmt.Fprintf(buf, "`%s`", expr)
	}
	_, _ = buf.WriteString(");\n\n")
}

func writeAddUniqueKey(buf *strings.Builder, table string, index *storepb.IndexMetadata) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE `%s` ADD UNIQUE KEY `%s` (", table, index.Name)
	for i, expr := range index.Expressions {
		if i > 0 {
			_, _ = buf.WriteString(", ")
		}
		_, _ = fmt.Fprintf(buf, "`%s`", expr)
	}
	_, _ = buf.WriteString(");\n\n")
}

func writeAddForeignKey(buf *strings.Builder, table string, fk *storepb.ForeignKeyMetadata) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE `%s` ADD CONSTRAINT `%s` FOREIGN KEY (", table, fk.Name)
	for i, col := range fk.Columns {
		if i > 0 {
			_, _ = buf.WriteString(", ")
		}
		_, _ = fmt.Fprintf(buf, "`%s`", col)
	}
	_, _ = fmt.Fprintf(buf, ") REFERENCES `%s` (", fk.ReferencedTable)
	for i, col := range fk.ReferencedColumns {
		if i > 0 {
			_, _ = buf.WriteString(", ")
		}
		_, _ = fmt.Fprintf(buf, "`%s`", col)
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

func writeAddCheckConstraint(buf *strings.Builder, table string, check *storepb.CheckConstraintMetadata) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE `%s` ADD CONSTRAINT `%s` CHECK (%s);\n\n", table, check.Name, check.Expression)
}

func writeCreateTrigger(buf *strings.Builder, tableName string, trigger *storepb.TriggerMetadata) {
	_, _ = fmt.Fprintf(buf, "CREATE TRIGGER `%s` %s %s ON `%s` FOR EACH ROW %s",
		trigger.Name, trigger.Timing, trigger.Event, tableName, trigger.Body)
	if !strings.HasSuffix(strings.TrimSpace(trigger.Body), ";") {
		_, _ = buf.WriteString(";")
	}
	_, _ = buf.WriteString("\n\n")
}

func writeCreateOrReplaceView(buf *strings.Builder, viewName string, view *storepb.ViewMetadata) error {
	_, _ = fmt.Fprintf(buf, "CREATE OR REPLACE VIEW `%s` AS %s;\n\n", viewName, view.Definition)
	return nil
}

func writeCreateFunction(buf *strings.Builder, fn *storepb.FunctionMetadata) {
	_, _ = fmt.Fprintf(buf, "DELIMITER ;;\n%s ;;\nDELIMITER ;\n\n", fn.Definition)
}

func writeCreateProcedure(buf *strings.Builder, proc *storepb.ProcedureMetadata) {
	_, _ = fmt.Fprintf(buf, "DELIMITER ;;\n%s ;;\nDELIMITER ;\n\n", proc.Definition)
}

func writeCreateEvent(buf *strings.Builder, event *storepb.EventMetadata) {
	_, _ = fmt.Fprintf(buf, "%s;\n\n", event.Definition)
}

func writeAlterTableComment(buf *strings.Builder, table, comment string) {
	if comment == "" {
		_, _ = fmt.Fprintf(buf, "ALTER TABLE `%s` COMMENT '';\n\n", table)
	} else {
		_, _ = fmt.Fprintf(buf, "ALTER TABLE `%s` COMMENT '%s';\n\n", table, escapeSingleQuote(comment))
	}
}

func writeAlterTableEngine(buf *strings.Builder, table, engine string) {
	_, _ = fmt.Fprintf(buf, "ALTER TABLE `%s` ENGINE=%s;\n\n", table, engine)
}

func writeCommentChange(buf *strings.Builder, cc *schema.CommentDiff) {
	switch cc.ObjectType {
	case schema.CommentObjectTypeTable:
		if cc.NewComment == "" {
			_, _ = fmt.Fprintf(buf, "ALTER TABLE `%s` COMMENT '';\n\n", cc.ObjectName)
		} else {
			_, _ = fmt.Fprintf(buf, "ALTER TABLE `%s` COMMENT '%s';\n\n", cc.ObjectName, escapeSingleQuote(cc.NewComment))
		}
	case schema.CommentObjectTypeColumn:
		parts := strings.SplitN(cc.ObjectName, ".", 2)
		if len(parts) == 2 {
			tableName, colName := parts[0], parts[1]
			if cc.NewComment == "" {
				_, _ = fmt.Fprintf(buf, "ALTER TABLE `%s` MODIFY COLUMN `%s` INT COMMENT '';\n\n", tableName, colName)
			} else {
				_, _ = fmt.Fprintf(buf, "COMMENT ON COLUMN `%s`.`%s` IS '%s';\n\n", tableName, colName, escapeSingleQuote(cc.NewComment))
			}
		}
	default:
	}
}

func hasDefaultValue(col *storepb.ColumnMetadata) bool {
	return col.Default != "" && !strings.EqualFold(col.Default, "NULL")
}

//nolint:unused
func hasAutoIncrement(col *storepb.ColumnMetadata) bool {
	return strings.EqualFold(col.Default, "AUTO_INCREMENT")
}

func escapeSingleQuote(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
