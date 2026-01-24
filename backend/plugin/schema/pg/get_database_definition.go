package pg

import (
	"fmt"
	"io"
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/schema"
)

func init() {
	schema.RegisterGetTableDefinition(storepb.Engine_POSTGRES, GetTableDefinition)
	schema.RegisterGetViewDefinition(storepb.Engine_POSTGRES, GetViewDefinition)
	schema.RegisterGetMaterializedViewDefinition(storepb.Engine_POSTGRES, GetMaterializedViewDefinition)
	schema.RegisterGetFunctionDefinition(storepb.Engine_POSTGRES, GetFunctionDefinition)
	schema.RegisterGetSequenceDefinition(storepb.Engine_POSTGRES, GetSequenceDefinition)
}

func GetTableDefinition(schema string, table *storepb.TableMetadata, sequences []*storepb.SequenceMetadata) (string, error) {
	var buf strings.Builder
	if err := writeTable(&buf, schema, table, sequences); err != nil {
		return "", err
	}
	// Construct triggers.
	for _, trigger := range table.Triggers {
		if trigger.SkipDump {
			continue
		}
		if err := writeTrigger(&buf, schema, table.Name, trigger); err != nil {
			return "", err
		}
	}
	// Construct rules.
	if len(table.Rules) > 0 {
		if err := writeRules(&buf, schema, table.Name, table.Rules); err != nil {
			return "", err
		}
	}
	// Construct foreign keys.
	for _, fk := range table.ForeignKeys {
		if err := writeForeignKey(&buf, schema, table.Name, fk); err != nil {
			return "", err
		}
	}
	return buf.String(), nil
}

func GetViewDefinition(schema string, view *storepb.ViewMetadata) (string, error) {
	var buf strings.Builder
	if err := writeView(&buf, schema, view); err != nil {
		return "", err
	}
	// Construct triggers.
	for _, trigger := range view.Triggers {
		if trigger.SkipDump {
			continue
		}
		if err := writeTrigger(&buf, schema, view.Name, trigger); err != nil {
			return "", err
		}
	}
	// Construct rules (non-SELECT rules only, as SELECT rules are part of the view definition).
	var nonSelectRules []*storepb.RuleMetadata
	for _, rule := range view.Rules {
		if rule.Event != "SELECT" {
			nonSelectRules = append(nonSelectRules, rule)
		}
	}
	if len(nonSelectRules) > 0 {
		if err := writeRules(&buf, schema, view.Name, nonSelectRules); err != nil {
			return "", err
		}
	}
	return buf.String(), nil
}

func GetMaterializedViewDefinition(schema string, view *storepb.MaterializedViewMetadata) (string, error) {
	var buf strings.Builder
	if err := writeMaterializedView(&buf, schema, view); err != nil {
		return "", err
	}
	// Construct triggers.
	for _, trigger := range view.Triggers {
		if trigger.SkipDump {
			continue
		}
		if err := writeTrigger(&buf, schema, view.Name, trigger); err != nil {
			return "", err
		}
	}
	return buf.String(), nil
}

func GetFunctionDefinition(schema string, function *storepb.FunctionMetadata) (string, error) {
	var buf strings.Builder
	if err := writeFunction(&buf, schema, function); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func GetSequenceDefinition(schema string, sequence *storepb.SequenceMetadata) (string, error) {
	var buf strings.Builder
	if err := writeCreateSequence(&buf, schema, sequence); err != nil {
		return "", err
	}
	if sequence.OwnerColumn != "" && sequence.OwnerTable != "" {
		if err := writeAlterSequenceOwnedBy(&buf, schema, sequence); err != nil {
			return "", err
		}
	}
	return buf.String(), nil
}

func writeTrigger(out io.Writer, schema string, table string, trigger *storepb.TriggerMetadata) error {
	if _, err := io.WriteString(out, trigger.Body); err != nil {
		return err
	}
	if _, err := io.WriteString(out, ";\n\n"); err != nil {
		return err
	}

	if len(trigger.Comment) > 0 {
		if err := writeTriggerComment(out, schema, table, trigger); err != nil {
			return err
		}
	}

	return nil
}

func writeTriggerComment(out io.Writer, schema string, table string, trigger *storepb.TriggerMetadata) error {
	if _, err := io.WriteString(out, `COMMENT ON TRIGGER "`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, trigger.Name); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `" ON "`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, table); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `" IS '`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, escapeSingleQuote(trigger.Comment)); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `';`); err != nil {
		return err
	}

	_, err := io.WriteString(out, "\n\n")
	return err
}

func escapeSingleQuote(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func writeMaterializedView(out io.Writer, schema string, view *storepb.MaterializedViewMetadata) error {
	if _, err := io.WriteString(out, `CREATE MATERIALIZED VIEW "`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, view.Name); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\" AS \n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, strings.TrimRight(view.Definition, ";")); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\n  WITH NO DATA;\n\n"); err != nil {
		return err
	}

	for _, index := range view.Indexes {
		if err := writeIndex(out, schema, view.Name, index); err != nil {
			return err
		}
	}

	if len(view.Comment) > 0 {
		if err := writeMaterializedViewComment(out, schema, view); err != nil {
			return err
		}
	}

	return nil
}

func writeMaterializedViewComment(out io.Writer, schema string, view *storepb.MaterializedViewMetadata) error {
	if _, err := io.WriteString(out, `COMMENT ON MATERIALIZED VIEW "`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, view.Name); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `" IS '`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, escapeSingleQuote(view.Comment)); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `';`); err != nil {
		return err
	}

	_, err := io.WriteString(out, "\n\n")
	return err
}

func writeView(out io.Writer, schema string, view *storepb.ViewMetadata) error {
	if _, err := io.WriteString(out, `CREATE VIEW "`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, view.Name); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\" AS \n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, view.Definition); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\n\n"); err != nil {
		return err
	}

	if len(view.Comment) > 0 {
		if err := writeViewComment(out, schema, view); err != nil {
			return err
		}
	}

	return nil
}

func writeViewComment(out io.Writer, schema string, view *storepb.ViewMetadata) error {
	if _, err := io.WriteString(out, `COMMENT ON VIEW "`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, view.Name); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `" IS '`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, escapeSingleQuote(view.Comment)); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `';`); err != nil {
		return err
	}

	_, err := io.WriteString(out, "\n\n")
	return err
}

func writeRules(out io.Writer, _ string, _ string, rules []*storepb.RuleMetadata) error {
	for _, rule := range rules {
		// Write the full rule definition
		if _, err := io.WriteString(out, rule.Definition); err != nil {
			return err
		}
		if _, err := io.WriteString(out, "\n\n"); err != nil {
			return err
		}
	}
	return nil
}

func writeColumnIdentityGeneration(out io.Writer, schema string, generationTypes storepb.ColumnMetadata_IdentityGeneration, sequence *storepb.SequenceMetadata) error {
	if _, err := io.WriteString(out, `ALTER TABLE "`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, sequence.OwnerTable); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `" ALTER COLUMN "`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, sequence.OwnerColumn); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `" ADD GENERATED `); err != nil {
		return err
	}
	if generationTypes == storepb.ColumnMetadata_ALWAYS {
		if _, err := io.WriteString(out, "ALWAYS "); err != nil {
			return err
		}
	} else {
		if _, err := io.WriteString(out, "BY DEFAULT "); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(out, "AS IDENTITY (\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "    SEQUENCE NAME \""); err != nil {
		return err
	}
	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, sequence.Name); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\"\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "    START WITH "); err != nil {
		return err
	}
	if _, err := io.WriteString(out, sequence.Start); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\n    INCREMENT BY "); err != nil {
		return err
	}
	if _, err := io.WriteString(out, sequence.Increment); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\n    MINVALUE "); err != nil {
		return err
	}
	if _, err := io.WriteString(out, sequence.MinValue); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\n    MAXVALUE "); err != nil {
		return err
	}
	if _, err := io.WriteString(out, sequence.MaxValue); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\n    CACHE "); err != nil {
		return err
	}
	if _, err := io.WriteString(out, sequence.CacheSize); err != nil {
		return err
	}
	_, err := io.WriteString(out, "\n);\n\n")
	return err
}

func writeCreateSequence(out io.Writer, schema string, sequence *storepb.SequenceMetadata) error {
	if _, err := io.WriteString(out, `CREATE SEQUENCE "`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, sequence.Name); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\"\n    "); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "AS "); err != nil {
		return err
	}
	if _, err := io.WriteString(out, sequence.DataType); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\n	START WITH "); err != nil {
		return err
	}
	if _, err := io.WriteString(out, sequence.Start); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\n	INCREMENT BY "); err != nil {
		return err
	}
	if _, err := io.WriteString(out, sequence.Increment); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\n	MINVALUE "); err != nil {
		return err
	}
	if _, err := io.WriteString(out, sequence.MinValue); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\n	MAXVALUE "); err != nil {
		return err
	}
	if _, err := io.WriteString(out, sequence.MaxValue); err != nil {
		return err
	}
	if sequence.Cycle {
		if _, err := io.WriteString(out, "\n	CYCLE"); err != nil {
			return err
		}
	} else {
		if _, err := io.WriteString(out, "\n	NO CYCLE"); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(out, ";\n\n"); err != nil {
		return err
	}

	if len(sequence.Comment) > 0 {
		if err := writeSequenceComment(out, schema, sequence); err != nil {
			return err
		}
	}

	return nil
}

func writeSequenceComment(out io.Writer, schema string, sequence *storepb.SequenceMetadata) error {
	if _, err := io.WriteString(out, `COMMENT ON SEQUENCE "`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, sequence.Name); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `" IS '`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, escapeSingleQuote(sequence.Comment)); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `';`); err != nil {
		return err
	}

	_, err := io.WriteString(out, "\n\n")
	return err
}

func writeAlterSequenceOwnedBy(out io.Writer, schema string, sequence *storepb.SequenceMetadata) error {
	if _, err := io.WriteString(out, `ALTER SEQUENCE "`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, sequence.Name); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\" OWNED BY \""); err != nil {
		return err
	}
	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, sequence.OwnerTable); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, sequence.OwnerColumn); err != nil {
		return err
	}
	_, err := io.WriteString(out, "\";\n\n")
	return err
}

func writeCreateTable(out io.Writer, schema string, tableName string, columns []*storepb.ColumnMetadata, checks []*storepb.CheckConstraintMetadata, excludes []*storepb.ExcludeConstraintMetadata) error {
	if _, err := io.WriteString(out, `CREATE TABLE "`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, tableName); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `" (`); err != nil {
		return err
	}

	for i, column := range columns {
		if i > 0 {
			if _, err := io.WriteString(out, ","); err != nil {
				return err
			}
		}

		if _, err := io.WriteString(out, "\n    "); err != nil {
			return err
		}

		if _, err := io.WriteString(out, `"`); err != nil {
			return err
		}

		if _, err := io.WriteString(out, column.Name); err != nil {
			return err
		}

		if _, err := io.WriteString(out, `" `); err != nil {
			return err
		}

		if _, err := io.WriteString(out, column.Type); err != nil {
			return err
		}

		// Handle default values
		if column.Default != "" {
			if _, err := io.WriteString(out, ` DEFAULT `); err != nil {
				return err
			}
			if _, err := io.WriteString(out, column.Default); err != nil {
				return err
			}
		}

		if !column.Nullable {
			if _, err := io.WriteString(out, ` NOT NULL`); err != nil {
				return err
			}
		}
	}

	for _, check := range checks {
		_, _ = io.WriteString(out, ",\n    ")
		_, _ = io.WriteString(out, `CONSTRAINT "`)
		_, _ = io.WriteString(out, check.Name)
		_, _ = io.WriteString(out, `" CHECK `)
		_, _ = io.WriteString(out, check.Expression)
	}

	for _, exclude := range excludes {
		_, _ = io.WriteString(out, ",\n    ")
		_, _ = io.WriteString(out, `CONSTRAINT "`)
		_, _ = io.WriteString(out, exclude.Name)
		_, _ = io.WriteString(out, `" `)
		_, _ = io.WriteString(out, exclude.Expression) // Already includes "EXCLUDE USING ..."
	}

	_, err := io.WriteString(out, "\n)")
	return err
}

// isIdentityColumn checks if a column is an identity column.
func isIdentityColumn(column *storepb.ColumnMetadata) bool {
	return column.IdentityGeneration == storepb.ColumnMetadata_ALWAYS ||
		column.IdentityGeneration == storepb.ColumnMetadata_BY_DEFAULT
}

func splitSequencesByIdentityOrNot(table *storepb.TableMetadata, sequences []*storepb.SequenceMetadata) ([]storepb.ColumnMetadata_IdentityGeneration, []*storepb.SequenceMetadata, []*storepb.SequenceMetadata) {
	columnMap := make(map[string]*storepb.ColumnMetadata)
	for _, column := range table.Columns {
		columnMap[column.Name] = column
	}
	var generationType []storepb.ColumnMetadata_IdentityGeneration
	var identitySequences []*storepb.SequenceMetadata
	var nonIdentitySequences []*storepb.SequenceMetadata
	for _, sequence := range sequences {
		if column, ok := columnMap[sequence.OwnerColumn]; ok {
			if isIdentityColumn(column) {
				generationType = append(generationType, column.IdentityGeneration)
				identitySequences = append(identitySequences, sequence)
				continue
			}
		}
		nonIdentitySequences = append(nonIdentitySequences, sequence)
	}
	return generationType, identitySequences, nonIdentitySequences
}

func writeTable(out io.Writer, schema string, table *storepb.TableMetadata, sequences []*storepb.SequenceMetadata) error {
	generationTypes, identitySequences, nonIdentitySequences := splitSequencesByIdentityOrNot(table, sequences)

	if err := writeCreateTable(out, schema, table.Name, table.Columns, table.CheckConstraints, table.ExcludeConstraints); err != nil {
		return err
	}

	if len(table.Partitions) > 0 {
		if err := writePartitionClause(out, table.Partitions[0]); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(out, ";\n\n"); err != nil {
		return err
	}

	for _, sequence := range nonIdentitySequences {
		if err := writeAlterSequenceOwnedBy(out, schema, sequence); err != nil {
			return err
		}
	}

	// Construct comments.
	if len(table.Comment) > 0 {
		if err := writeTableComment(out, schema, table); err != nil {
			return err
		}
	}

	for _, column := range table.Columns {
		if len(column.Comment) > 0 {
			if err := writeColumnComment(out, schema, table.Name, column); err != nil {
				return err
			}
		}
	}

	for i, sequence := range identitySequences {
		if err := writeColumnIdentityGeneration(out, schema, generationTypes[i], sequence); err != nil {
			return err
		}
	}

	// Construct partition tables.
	for _, partition := range table.Partitions {
		if err := writePartitionTable(out, schema, table.Columns, partition); err != nil {
			return err
		}
	}

	for _, partition := range table.Partitions {
		if err := writeAttachPartition(out, schema, table.Name, partition); err != nil {
			return err
		}
	}

	// Construct Primary Key.
	for _, index := range table.Indexes {
		if index.Primary {
			if err := writePrimaryKey(out, schema, table.Name, index); err != nil {
				return err
			}
		}
	}

	// Construct Partition table primary key.
	for _, partition := range table.Partitions {
		if err := writePartitionPrimaryKey(out, schema, partition); err != nil {
			return err
		}
	}

	// Construct Unique Key.
	for _, index := range table.Indexes {
		if index.Unique && !index.Primary && index.IsConstraint {
			if err := writeUniqueKey(out, schema, table.Name, index); err != nil {
				return err
			}
		}
	}

	// Construct Partition table unique key.
	for _, partition := range table.Partitions {
		if err := writePartitionUniqueKey(out, schema, partition); err != nil {
			return err
		}
	}

	// Construct Index.
	for _, index := range table.Indexes {
		if !index.Primary && !index.IsConstraint {
			if err := writeIndex(out, schema, table.Name, index); err != nil {
				return err
			}
		}
	}

	// Construct Partition table index.
	for _, partition := range table.Partitions {
		if err := writePartitionIndex(out, schema, partition); err != nil {
			return err
		}
	}

	// Construct index attach partition.
	for _, partition := range table.Partitions {
		if err := writeAttachPartitionIndex(out, schema, partition); err != nil {
			return err
		}
	}

	return nil
}

func writeForeignKey(out io.Writer, schema string, table string, fk *storepb.ForeignKeyMetadata) error {
	if _, err := io.WriteString(out, `ALTER TABLE "`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, table); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\"\n    ADD CONSTRAINT \""); err != nil {
		return err
	}
	if _, err := io.WriteString(out, fk.Name); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\" FOREIGN KEY ("); err != nil {
		return err
	}
	for i, column := range fk.Columns {
		if i > 0 {
			if _, err := io.WriteString(out, ", "); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(out, `"`); err != nil {
			return err
		}
		if _, err := io.WriteString(out, column); err != nil {
			return err
		}
		if _, err := io.WriteString(out, `"`); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(out, ")\n    REFERENCES \""); err != nil {
		return err
	}
	if _, err := io.WriteString(out, fk.ReferencedSchema); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, fk.ReferencedTable); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\" ("); err != nil {
		return err
	}
	for i, column := range fk.ReferencedColumns {
		if i > 0 {
			if _, err := io.WriteString(out, ", "); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(out, `"`); err != nil {
			return err
		}
		if _, err := io.WriteString(out, column); err != nil {
			return err
		}
		if _, err := io.WriteString(out, `"`); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(out, ")"); err != nil {
		return err
	}

	// Add ON DELETE clause if specified
	if fk.OnDelete != "" && fk.OnDelete != "NO ACTION" {
		if _, err := io.WriteString(out, "\n    ON DELETE "); err != nil {
			return err
		}
		if _, err := io.WriteString(out, fk.OnDelete); err != nil {
			return err
		}
	}

	// Add ON UPDATE clause if specified
	if fk.OnUpdate != "" && fk.OnUpdate != "NO ACTION" {
		if _, err := io.WriteString(out, "\n    ON UPDATE "); err != nil {
			return err
		}
		if _, err := io.WriteString(out, fk.OnUpdate); err != nil {
			return err
		}
	}

	_, err := io.WriteString(out, ";\n\n")
	return err
}

func writeAttachPartitionIndex(out io.Writer, schema string, partition *storepb.TablePartitionMetadata) error {
	for _, index := range partition.Indexes {
		if err := writeAttachIndex(out, schema, index); err != nil {
			return err
		}
	}

	for _, subpartition := range partition.Subpartitions {
		if err := writeAttachPartitionIndex(out, schema, subpartition); err != nil {
			return err
		}
	}
	return nil
}

func writeAttachIndex(out io.Writer, schema string, index *storepb.IndexMetadata) error {
	if len(index.ParentIndexName) == 0 || len(index.ParentIndexSchema) == 0 {
		return nil
	}

	if _, err := io.WriteString(out, `ALTER INDEX "`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, index.ParentIndexSchema); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, index.ParentIndexName); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `" ATTACH PARTITION "`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, index.Name); err != nil {
		return err
	}
	_, err := io.WriteString(out, "\";\n\n")
	return err
}

func writePartitionIndex(out io.Writer, schema string, partition *storepb.TablePartitionMetadata) error {
	for _, index := range partition.Indexes {
		if !index.IsConstraint && !index.Primary {
			if err := writeIndex(out, schema, partition.Name, index); err != nil {
				return err
			}
		}
	}

	for _, subpartition := range partition.Subpartitions {
		if err := writePartitionIndex(out, schema, subpartition); err != nil {
			return err
		}
	}
	return nil
}

func writeIndex(out io.Writer, schema string, table string, index *storepb.IndexMetadata) error {
	return writeIndexInternal(out, schema, table, index, true, true)
}

func writeIndexKeyList(out io.Writer, index *storepb.IndexMetadata) error {
	if _, err := io.WriteString(out, `(`); err != nil {
		return err
	}

	for i, expression := range index.Expressions {
		if i > 0 {
			if _, err := io.WriteString(out, ", "); err != nil {
				return err
			}
		}

		if _, err := io.WriteString(out, expression); err != nil {
			return err
		}

		// Add opclass if it's not the default
		if i < len(index.OpclassNames) && i < len(index.OpclassDefaults) {
			if !index.OpclassDefaults[i] && index.OpclassNames[i] != "" {
				if _, err := io.WriteString(out, " "); err != nil {
					return err
				}
				if _, err := io.WriteString(out, index.OpclassNames[i]); err != nil {
					return err
				}
			}
		}

		// Add DESC if this column is marked as descending
		if i < len(index.Descending) && index.Descending[i] {
			if _, err := io.WriteString(out, " DESC"); err != nil {
				return err
			}
		}
		// Note: NULLS ordering information is not available in IndexMetadata
		// so we omit it in the generated DDL
	}

	_, err := io.WriteString(out, ")")
	return err
}

func writeIndexComment(out io.Writer, schema string, index *storepb.IndexMetadata) error {
	if _, err := io.WriteString(out, `COMMENT ON INDEX "`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, index.Name); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `" IS '`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, escapeSingleQuote(index.Comment)); err != nil {
		return err
	}

	_, err := io.WriteString(out, `';\n\n`)
	return err
}

func writePartitionUniqueKey(out io.Writer, schema string, partition *storepb.TablePartitionMetadata) error {
	for _, index := range partition.Indexes {
		if index.Unique && !index.Primary && index.IsConstraint {
			if err := writeUniqueKey(out, schema, partition.Name, index); err != nil {
				return err
			}
		}
	}

	for _, subpartition := range partition.Subpartitions {
		if err := writePartitionUniqueKey(out, schema, subpartition); err != nil {
			return err
		}
	}
	return nil
}

func writeUniqueKey(out io.Writer, schema string, table string, index *storepb.IndexMetadata) error {
	if _, err := io.WriteString(out, `ALTER TABLE ONLY "`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, table); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `" ADD CONSTRAINT "`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, index.Name); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `" UNIQUE (`); err != nil {
		return err
	}
	for i, expression := range index.Expressions {
		if i > 0 {
			if _, err := io.WriteString(out, ", "); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(out, expression); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(out, ");\n\n"); err != nil {
		return err
	}

	if len(index.Comment) > 0 {
		if err := writeConstraintComment(out, schema, table, index); err != nil {
			return err
		}
	}

	return nil
}

func writePartitionPrimaryKey(out io.Writer, schema string, partition *storepb.TablePartitionMetadata) error {
	for _, index := range partition.Indexes {
		if index.Primary {
			if err := writePrimaryKey(out, schema, partition.Name, index); err != nil {
				return err
			}
		}
	}

	for _, subpartition := range partition.Subpartitions {
		if err := writePartitionPrimaryKey(out, schema, subpartition); err != nil {
			return err
		}
	}
	return nil
}

func writePrimaryKey(out io.Writer, schema string, table string, index *storepb.IndexMetadata) error {
	if _, err := io.WriteString(out, `ALTER TABLE ONLY "`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, table); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `" ADD CONSTRAINT "`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, index.Name); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `" PRIMARY KEY (`); err != nil {
		return err
	}
	for i, expression := range index.Expressions {
		if i > 0 {
			if _, err := io.WriteString(out, ", "); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(out, expression); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(out, ");\n\n"); err != nil {
		return err
	}

	if len(index.Comment) > 0 {
		if err := writeConstraintComment(out, schema, table, index); err != nil {
			return err
		}
	}

	return nil
}

func writeConstraintComment(out io.Writer, schema string, table string, index *storepb.IndexMetadata) error {
	if _, err := io.WriteString(out, `COMMENT ON CONSTRAINT "`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, index.Name); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `" ON "`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, table); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `" IS '`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, escapeSingleQuote(index.Comment)); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `';`); err != nil {
		return err
	}

	_, err := io.WriteString(out, "\n\n")
	return err
}

func writeColumnComment(out io.Writer, schema string, table string, column *storepb.ColumnMetadata) error {
	if _, err := io.WriteString(out, `COMMENT ON COLUMN "`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, table); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, column.Name); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `" IS '`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, escapeSingleQuote(column.Comment)); err != nil {
		return err
	}
	_, err := io.WriteString(out, "';\n\n")
	return err
}

func writeTableComment(out io.Writer, schema string, table *storepb.TableMetadata) error {
	if _, err := io.WriteString(out, `COMMENT ON TABLE "`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, table.Name); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `" IS '`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, escapeSingleQuote(table.Comment)); err != nil {
		return err
	}
	_, err := io.WriteString(out, "';\n\n")
	return err
}

func writePartitionClause(out io.Writer, partition *storepb.TablePartitionMetadata) error {
	if _, err := io.WriteString(out, " PARTITION BY "); err != nil {
		return err
	}
	_, err := io.WriteString(out, partition.Expression)
	return err
}

func writeAttachPartition(out io.Writer, schema string, tableName string, partition *storepb.TablePartitionMetadata) error {
	if _, err := io.WriteString(out, `ALTER TABLE ONLY "`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, tableName); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `" ATTACH PARTITION "`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, partition.Name); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\" "); err != nil {
		return err
	}
	if _, err := io.WriteString(out, partition.Value); err != nil {
		return err
	}
	_, err := io.WriteString(out, ";\n\n")
	return err
}

func writePartitionTable(out io.Writer, schema string, columns []*storepb.ColumnMetadata, partition *storepb.TablePartitionMetadata) error {
	if err := writeCreateTable(out, schema, partition.Name, columns, partition.CheckConstraints, partition.ExcludeConstraints); err != nil {
		return err
	}

	if len(partition.Subpartitions) > 0 {
		if err := writePartitionClause(out, partition.Subpartitions[0]); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(out, ";\n\n"); err != nil {
		return err
	}

	// Construct subpartition tables.
	for _, subpartition := range partition.Subpartitions {
		if err := writePartitionTable(out, schema, columns, subpartition); err != nil {
			return err
		}
	}

	for _, subpartition := range partition.Subpartitions {
		if err := writeAttachPartition(out, schema, partition.Name, subpartition); err != nil {
			return err
		}
	}

	return nil
}

func writeFunction(out io.Writer, schema string, function *storepb.FunctionMetadata) error {
	if _, err := io.WriteString(out, function.Definition); err != nil {
		return err
	}

	if _, err := io.WriteString(out, ";\n\n"); err != nil {
		return err
	}

	if len(function.Comment) > 0 {
		if err := writeFunctionComment(out, schema, function); err != nil {
			return err
		}
	}

	return nil
}

// isDefinitionProcedure checks if the definition string represents a PROCEDURE (not a FUNCTION)
// Returns true if it's a PROCEDURE, false if it's a FUNCTION
// This function currently only uses string comparison.
func isDefinitionProcedure(definition string) bool {
	if definition == "" {
		return false
	}
	upperDef := strings.ToUpper(definition)
	return strings.Contains(upperDef, " PROCEDURE ") ||
		strings.HasPrefix(upperDef, "CREATE PROCEDURE") ||
		strings.HasPrefix(upperDef, "CREATE OR REPLACE PROCEDURE")
}

func writeFunctionComment(out io.Writer, schema string, function *storepb.FunctionMetadata) error {
	// Determine if this is a PROCEDURE or FUNCTION by checking the definition
	objectType := "FUNCTION"
	if isDefinitionProcedure(function.Definition) {
		objectType = "PROCEDURE"
	}

	if _, err := io.WriteString(out, "COMMENT ON "+objectType+" \""); err != nil {
		return err
	}

	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}

	if _, err := io.WriteString(out, `".`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, function.Signature); err != nil {
		return err
	}

	if _, err := io.WriteString(out, ` IS '`); err != nil {
		return err
	}

	if _, err := io.WriteString(out, escapeSingleQuote(function.Comment)); err != nil {
		return err
	}

	_, err := io.WriteString(out, "';\n\n")
	return err
}

// writeIndexInternal is the core index writing function with options for different modes
func writeIndexInternal(out io.Writer, schema string, table string, index *storepb.IndexMetadata, useOnlyClause bool, includeTerminatorAndComment bool) error {
	if index.Unique {
		if _, err := io.WriteString(out, `CREATE UNIQUE INDEX "`); err != nil {
			return err
		}
	} else {
		if _, err := io.WriteString(out, `CREATE INDEX "`); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(out, index.Name); err != nil {
		return err
	}

	if useOnlyClause {
		if _, err := io.WriteString(out, `" ON ONLY "`); err != nil {
			return err
		}
	} else {
		if _, err := io.WriteString(out, `" ON "`); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(out, schema); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"."`); err != nil {
		return err
	}
	if _, err := io.WriteString(out, table); err != nil {
		return err
	}
	if _, err := io.WriteString(out, `"`); err != nil {
		return err
	}

	// Add USING clause for non-btree indexes
	if index.Type != "" && index.Type != "btree" {
		if _, err := fmt.Fprintf(out, " USING %s", strings.ToUpper(index.Type)); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(out, ` `); err != nil {
		return err
	}
	if err := writeIndexKeyList(out, index); err != nil {
		return err
	}

	if includeTerminatorAndComment {
		if _, err := io.WriteString(out, ";\n\n"); err != nil {
			return err
		}

		if len(index.Comment) > 0 {
			if err := writeIndexComment(out, schema, index); err != nil {
				return err
			}
		}
	}

	return nil
}
