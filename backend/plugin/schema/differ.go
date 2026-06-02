package schema

import (
	"slices"
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// MetadataDiffAction represents the type of change action.
type MetadataDiffAction string

const (
	MetadataDiffActionCreate MetadataDiffAction = "CREATE"
	MetadataDiffActionDrop   MetadataDiffAction = "DROP"
	MetadataDiffActionAlter  MetadataDiffAction = "ALTER"
)

// MetadataDiff represents the differences between two database schemas.
type MetadataDiff struct {
	DatabaseName string

	SchemaChanges           []*SchemaDiff
	TableChanges            []*TableDiff
	ViewChanges             []*ViewDiff
	MaterializedViewChanges []*MaterializedViewDiff
	FunctionChanges         []*FunctionDiff
	ProcedureChanges        []*ProcedureDiff
	SequenceChanges         []*SequenceDiff
	EnumTypeChanges         []*EnumTypeDiff
	ExtensionChanges        []*ExtensionDiff
	EventTriggerChanges     []*EventTriggerDiff
	EventChanges            []*EventDiff
	CommentChanges          []*CommentDiff
}

// SchemaDiff represents changes to a schema.
//
//nolint:revive
type SchemaDiff struct {
	Action     MetadataDiffAction
	SchemaName string
	OldSchema  *storepb.SchemaMetadata
	NewSchema  *storepb.SchemaMetadata
}

// TableDiff represents changes to a table.
type TableDiff struct {
	Action                 MetadataDiffAction
	SchemaName             string
	TableName              string
	OldTable               *storepb.TableMetadata
	NewTable               *storepb.TableMetadata
	ColumnChanges          []*ColumnDiff
	IndexChanges           []*IndexDiff
	ForeignKeyChanges      []*ForeignKeyDiff
	CheckConstraintChanges []*CheckConstraintDiff
	TriggerChanges         []*TriggerDiff
	PartitionChanges       []*PartitionDiff
}

// ColumnDiff represents changes to a column.
type ColumnDiff struct {
	Action    MetadataDiffAction
	OldColumn *storepb.ColumnMetadata
	NewColumn *storepb.ColumnMetadata
}

// IndexDiff represents changes to an index.
type IndexDiff struct {
	Action    MetadataDiffAction
	IndexName string
	OldIndex  *storepb.IndexMetadata
	NewIndex  *storepb.IndexMetadata
}

// ForeignKeyDiff represents changes to a foreign key.
type ForeignKeyDiff struct {
	Action         MetadataDiffAction
	ForeignKeyName string
	OldForeignKey  *storepb.ForeignKeyMetadata
	NewForeignKey  *storepb.ForeignKeyMetadata
}

// CheckConstraintDiff represents changes to a check constraint.
type CheckConstraintDiff struct {
	Action              MetadataDiffAction
	CheckConstraintName string
	OldCheckConstraint  *storepb.CheckConstraintMetadata
	NewCheckConstraint  *storepb.CheckConstraintMetadata
}

// TriggerDiff represents changes to a trigger.
type TriggerDiff struct {
	Action      MetadataDiffAction
	TriggerName string
	OldTrigger  *storepb.TriggerMetadata
	NewTrigger  *storepb.TriggerMetadata
}

// PartitionDiff represents changes to a partition.
type PartitionDiff struct {
	Action        MetadataDiffAction
	PartitionName string
	OldPartition  *storepb.TablePartitionMetadata
	NewPartition  *storepb.TablePartitionMetadata
}

// ViewDiff represents changes to a view.
type ViewDiff struct {
	Action     MetadataDiffAction
	SchemaName string
	ViewName   string
	OldView    *storepb.ViewMetadata
	NewView    *storepb.ViewMetadata
}

// MaterializedViewDiff represents changes to a materialized view.
type MaterializedViewDiff struct {
	Action               MetadataDiffAction
	SchemaName           string
	MaterializedViewName string
	OldMaterializedView  *storepb.MaterializedViewMetadata
	NewMaterializedView  *storepb.MaterializedViewMetadata
}

// FunctionDiff represents changes to a function.
type FunctionDiff struct {
	Action       MetadataDiffAction
	SchemaName   string
	FunctionName string
	OldFunction  *storepb.FunctionMetadata
	NewFunction  *storepb.FunctionMetadata
}

// ProcedureDiff represents changes to a procedure.
type ProcedureDiff struct {
	Action        MetadataDiffAction
	SchemaName    string
	ProcedureName string
	OldProcedure  *storepb.ProcedureMetadata
	NewProcedure  *storepb.ProcedureMetadata
}

// SequenceDiff represents changes to a sequence.
type SequenceDiff struct {
	Action       MetadataDiffAction
	SchemaName   string
	SequenceName string
	OldSequence  *storepb.SequenceMetadata
	NewSequence  *storepb.SequenceMetadata
}

// EnumTypeDiff represents changes to an enum type.
type EnumTypeDiff struct {
	Action       MetadataDiffAction
	SchemaName   string
	EnumTypeName string
	OldEnumType  *storepb.EnumTypeMetadata
	NewEnumType  *storepb.EnumTypeMetadata
}

// ExtensionDiff represents changes to an extension.
type ExtensionDiff struct {
	Action        MetadataDiffAction
	ExtensionName string
	OldExtension  *storepb.ExtensionMetadata
	NewExtension  *storepb.ExtensionMetadata
}

// EventTriggerDiff represents changes to an event trigger.
type EventTriggerDiff struct {
	Action           MetadataDiffAction
	EventTriggerName string
	OldEventTrigger  *storepb.EventTriggerMetadata
	NewEventTrigger  *storepb.EventTriggerMetadata
}

// EventDiff represents changes to an event.
type EventDiff struct {
	Action     MetadataDiffAction
	SchemaName string
	EventName  string
	OldEvent   *storepb.EventMetadata
	NewEvent   *storepb.EventMetadata
}

// CommentObjectType represents the type of object a comment belongs to.
type CommentObjectType string

const (
	CommentObjectTypeTable            CommentObjectType = "TABLE"
	CommentObjectTypeColumn           CommentObjectType = "COLUMN"
	CommentObjectTypeView             CommentObjectType = "VIEW"
	CommentObjectTypeMaterializedView CommentObjectType = "MATERIALIZED_VIEW"
	CommentObjectTypeFunction         CommentObjectType = "FUNCTION"
	CommentObjectTypeSequence         CommentObjectType = "SEQUENCE"
	CommentObjectTypeSchema           CommentObjectType = "SCHEMA"
	CommentObjectTypeIndex            CommentObjectType = "INDEX"
)

// CommentDiff represents changes to a comment.
type CommentDiff struct {
	Action     MetadataDiffAction
	SchemaName string
	ObjectType CommentObjectType
	ObjectName string
	OldComment string
	NewComment string
}

// GetDatabaseSchemaDiff compares two DatabaseSchemaMetadata instances and returns the differences.
func GetDatabaseSchemaDiff(engine storepb.Engine, oldSchema, newSchema *storepb.DatabaseSchemaMetadata) (*MetadataDiff, error) {
	if oldSchema == nil || newSchema == nil {
		return nil, nil
	}

	diff := &MetadataDiff{
		DatabaseName: newSchema.Name,
	}

	// Build schema name -> SchemaMetadata maps for efficient lookup
	oldSchemas := make(map[string]*storepb.SchemaMetadata)
	for _, s := range oldSchema.Schemas {
		oldSchemas[s.Name] = s
	}
	newSchemas := make(map[string]*storepb.SchemaMetadata)
	for _, s := range newSchema.Schemas {
		newSchemas[s.Name] = s
	}

	// Find dropped schemas
	for _, schemaName := range sortedKeys(oldSchemas) {
		if _, ok := newSchemas[schemaName]; !ok {
			oldSchemaMeta := oldSchemas[schemaName]
			if oldSchemaMeta != nil && !oldSchemaMeta.SkipDump {
				diff.SchemaChanges = append(diff.SchemaChanges, &SchemaDiff{
					Action:     MetadataDiffActionDrop,
					SchemaName: schemaName,
					OldSchema:  oldSchemaMeta,
				})
			}
		}
	}

	// Find new and modified schemas
	for _, schemaName := range sortedKeys(newSchemas) {
		newSchemaMeta := newSchemas[schemaName]
		if newSchemaMeta == nil || newSchemaMeta.SkipDump {
			continue
		}

		if oldSchemaMeta, ok := oldSchemas[schemaName]; !ok || oldSchemaMeta == nil || oldSchemaMeta.SkipDump {
			// New schema — add all objects as created
			diff.SchemaChanges = append(diff.SchemaChanges, &SchemaDiff{
				Action:     MetadataDiffActionCreate,
				SchemaName: schemaName,
				NewSchema:  newSchemaMeta,
			})
			addNewSchemaObjects(diff, schemaName, newSchemaMeta)
		} else {
			// Compare schema objects
			compareSchemaObjects(engine, diff, schemaName, oldSchemaMeta, newSchemaMeta)
		}
	}

	// Compare database-level objects (extensions, event triggers)
	compareExtensions(diff, oldSchema, newSchema)
	compareEventTriggers(diff, oldSchema, newSchema)

	// Sort all diff lists to ensure stable output order
	sortDiffLists(diff)

	return diff, nil
}

// addNewSchemaObjects adds all objects from a new schema as created.
func addNewSchemaObjects(diff *MetadataDiff, schemaName string, schema *storepb.SchemaMetadata) {
	// Add all tables
	for _, table := range schema.Tables {
		if !table.SkipDump {
			diff.TableChanges = append(diff.TableChanges, &TableDiff{
				Action:     MetadataDiffActionCreate,
				SchemaName: schemaName,
				TableName:  table.Name,
				NewTable:   table,
			})
		}
	}

	// Add all views
	for _, view := range schema.Views {
		if !view.SkipDump {
			diff.ViewChanges = append(diff.ViewChanges, &ViewDiff{
				Action:     MetadataDiffActionCreate,
				SchemaName: schemaName,
				ViewName:   view.Name,
				NewView:    view,
			})
		}
	}

	// Add all materialized views
	for _, mv := range schema.MaterializedViews {
		if !mv.SkipDump {
			diff.MaterializedViewChanges = append(diff.MaterializedViewChanges, &MaterializedViewDiff{
				Action:               MetadataDiffActionCreate,
				SchemaName:           schemaName,
				MaterializedViewName: mv.Name,
				NewMaterializedView:  mv,
			})
		}
	}

	// Add all functions
	for _, function := range schema.Functions {
		if !function.SkipDump {
			diff.FunctionChanges = append(diff.FunctionChanges, &FunctionDiff{
				Action:       MetadataDiffActionCreate,
				SchemaName:   schemaName,
				FunctionName: function.Name,
				NewFunction:  function,
			})
		}
	}

	// Add all procedures
	for _, proc := range schema.Procedures {
		if !proc.SkipDump {
			diff.ProcedureChanges = append(diff.ProcedureChanges, &ProcedureDiff{
				Action:        MetadataDiffActionCreate,
				SchemaName:    schemaName,
				ProcedureName: proc.Name,
				NewProcedure:  proc,
			})
		}
	}

	// Add all sequences
	for _, seq := range schema.Sequences {
		if !seq.SkipDump {
			diff.SequenceChanges = append(diff.SequenceChanges, &SequenceDiff{
				Action:       MetadataDiffActionCreate,
				SchemaName:   schemaName,
				SequenceName: seq.Name,
				NewSequence:  seq,
			})
		}
	}

	// Add all enum types
	for _, enumType := range schema.EnumTypes {
		if !enumType.SkipDump {
			diff.EnumTypeChanges = append(diff.EnumTypeChanges, &EnumTypeDiff{
				Action:       MetadataDiffActionCreate,
				SchemaName:   schemaName,
				EnumTypeName: enumType.Name,
				NewEnumType:  enumType,
			})
		}
	}

	// Add all events
	for _, event := range schema.Events {
		diff.EventChanges = append(diff.EventChanges, &EventDiff{
			Action:     MetadataDiffActionCreate,
			SchemaName: schemaName,
			EventName:  event.Name,
			NewEvent:   event,
		})
	}
}

// compareSchemaObjects compares objects between two schemas.
func compareSchemaObjects(engine storepb.Engine, diff *MetadataDiff, schemaName string, oldSchema, newSchema *storepb.SchemaMetadata) {
	// Build name→object maps for efficient lookup
	oldTables := buildTableMap(oldSchema.Tables)
	newTables := buildTableMap(newSchema.Tables)

	// Check for dropped tables
	for _, tableName := range sortedKeys(oldTables) {
		if _, ok := newTables[tableName]; !ok {
			oldTable := oldTables[tableName]
			if !oldTable.SkipDump {
				diff.TableChanges = append(diff.TableChanges, &TableDiff{
					Action:     MetadataDiffActionDrop,
					SchemaName: schemaName,
					TableName:  tableName,
					OldTable:   oldTable,
				})
			}
		}
	}

	// Check for new and modified tables
	for _, tableName := range sortedKeys(newTables) {
		newTable := newTables[tableName]
		if newTable.SkipDump {
			continue
		}

		if oldTable, ok := oldTables[tableName]; !ok || oldTable.SkipDump {
			diff.TableChanges = append(diff.TableChanges, &TableDiff{
				Action:     MetadataDiffActionCreate,
				SchemaName: schemaName,
				TableName:  tableName,
				NewTable:   newTable,
			})
		} else {
			// Check for table-level comment changes
			if oldTable.Comment != newTable.Comment {
				diff.CommentChanges = append(diff.CommentChanges, &CommentDiff{
					ObjectType: CommentObjectTypeTable,
					SchemaName: schemaName,
					ObjectName: tableName,
					OldComment: oldTable.Comment,
					NewComment: newTable.Comment,
				})
			}
			// Check for column-level comment changes
			compareColumnComments(diff, schemaName, tableName, oldTable, newTable)

			tableDiff := compareTableDetails(engine, schemaName, tableName, oldTable, newTable)
			if tableDiff != nil {
				diff.TableChanges = append(diff.TableChanges, tableDiff)
			}
		}
	}

	compareViews(engine, diff, schemaName, oldSchema, newSchema)
	compareMaterializedViews(engine, diff, schemaName, oldSchema, newSchema)
	compareFunctions(diff, schemaName, oldSchema, newSchema)
	compareProcedures(diff, schemaName, oldSchema, newSchema)
	compareSequences(diff, schemaName, oldSchema, newSchema)
	compareEnumTypes(diff, schemaName, oldSchema, newSchema)
	compareEvents(diff, schemaName, oldSchema, newSchema)
}

// compareTableDetails compares the details of two tables.
func compareTableDetails(_ storepb.Engine, schemaName, tableName string, oldTable, newTable *storepb.TableMetadata) *TableDiff {
	tableDiff := &TableDiff{
		Action:     MetadataDiffActionAlter,
		SchemaName: schemaName,
		TableName:  tableName,
		OldTable:   oldTable,
		NewTable:   newTable,
	}

	hasChanges := false

	// Compare columns
	columnChanges := compareColumns(oldTable, newTable)
	if len(columnChanges) > 0 {
		tableDiff.ColumnChanges = columnChanges
		hasChanges = true
	}

	// Compare indexes
	indexChanges := compareIndexes(oldTable, newTable)
	if len(indexChanges) > 0 {
		tableDiff.IndexChanges = indexChanges
		hasChanges = true
	}

	// Compare foreign keys
	fkChanges := compareForeignKeys(oldTable, newTable)
	if len(fkChanges) > 0 {
		tableDiff.ForeignKeyChanges = fkChanges
		hasChanges = true
	}

	// Compare check constraints
	checkChanges := compareCheckConstraints(oldTable, newTable)
	if len(checkChanges) > 0 {
		tableDiff.CheckConstraintChanges = checkChanges
		hasChanges = true
	}

	// Compare triggers (PostgreSQL-specific)
	triggerChanges := compareTriggers(oldTable, newTable)
	if len(triggerChanges) > 0 {
		tableDiff.TriggerChanges = triggerChanges
		hasChanges = true
	}

	// Compare partitions
	partitionChanges := comparePartitions(oldTable, newTable)
	if len(partitionChanges) > 0 {
		tableDiff.PartitionChanges = partitionChanges
		hasChanges = true
	}

	if !hasChanges {
		// Check if table-level attributes changed (comment, engine, charset, etc.)
		if oldTable.Comment != newTable.Comment ||
			oldTable.Engine != newTable.Engine ||
			oldTable.Charset != newTable.Charset ||
			oldTable.Collation != newTable.Collation ||
			oldTable.Owner != newTable.Owner {
			hasChanges = true
		}
	}

	if !hasChanges {
		return nil
	}

	return tableDiff
}

// compareColumnComments detects comment changes on columns and adds them to diff.
func compareColumnComments(diff *MetadataDiff, schemaName, tableName string, oldTable, newTable *storepb.TableMetadata) {
	oldCols := buildColumnMap(oldTable.Columns)
	newCols := buildColumnMap(newTable.Columns)
	for _, colName := range sortedKeys(oldCols) {
		oldCol := oldCols[colName]
		if newCol, ok := newCols[colName]; ok && oldCol.Comment != newCol.Comment {
			diff.CommentChanges = append(diff.CommentChanges, &CommentDiff{
				ObjectType: CommentObjectTypeColumn,
				SchemaName: schemaName,
				ObjectName: tableName + "." + colName,
				OldComment: oldCol.Comment,
				NewComment: newCol.Comment,
			})
		}
	}
}

// compareColumns compares columns between two tables.
func compareColumns(oldTable, newTable *storepb.TableMetadata) []*ColumnDiff {
	oldCols := buildColumnMap(oldTable.Columns)
	newCols := buildColumnMap(newTable.Columns)

	var changes []*ColumnDiff

	// Dropped columns
	for _, colName := range sortedKeys(oldCols) {
		if _, ok := newCols[colName]; !ok {
			changes = append(changes, &ColumnDiff{
				Action:    MetadataDiffActionDrop,
				OldColumn: oldCols[colName],
			})
		}
	}

	// New and modified columns
	for _, colName := range sortedKeys(newCols) {
		newCol := newCols[colName]
		if oldCol, ok := oldCols[colName]; !ok {
			changes = append(changes, &ColumnDiff{
				Action:    MetadataDiffActionCreate,
				NewColumn: newCol,
			})
		} else if !columnsEqual(oldCol, newCol) {
			changes = append(changes, &ColumnDiff{
				Action:    MetadataDiffActionAlter,
				OldColumn: oldCol,
				NewColumn: newCol,
			})
		}
	}

	return changes
}

// columnsEqual checks if two columns are equal.
func columnsEqual(a, b *storepb.ColumnMetadata) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Name == b.Name &&
		a.Type == b.Type &&
		a.Nullable == b.Nullable &&
		a.Default == b.Default &&
		a.Comment == b.Comment &&
		a.UserComment == b.UserComment &&
		a.CharacterSet == b.CharacterSet &&
		a.Collation == b.Collation &&
		a.OnUpdate == b.OnUpdate &&
		a.Position == b.Position &&
		a.DefaultOnNull == b.DefaultOnNull &&
		a.IsIdentity == b.IsIdentity &&
		a.IdentityGeneration == b.IdentityGeneration
}

// compareIndexes compares indexes between two tables.
func compareIndexes(oldTable, newTable *storepb.TableMetadata) []*IndexDiff {
	oldIndexes := buildIndexMap(oldTable.Indexes)
	newIndexes := buildIndexMap(newTable.Indexes)

	var changes []*IndexDiff

	// Dropped indexes
	for _, idxName := range sortedKeys(oldIndexes) {
		if _, ok := newIndexes[idxName]; !ok {
			changes = append(changes, &IndexDiff{
				Action:    MetadataDiffActionDrop,
				IndexName: idxName,
				OldIndex:  oldIndexes[idxName],
			})
		}
	}

	// New and modified indexes
	for _, idxName := range sortedKeys(newIndexes) {
		newIdx := newIndexes[idxName]
		if oldIdx, ok := oldIndexes[idxName]; !ok {
			changes = append(changes, &IndexDiff{
				Action:    MetadataDiffActionCreate,
				IndexName: idxName,
				NewIndex:  newIdx,
			})
		} else if !indexesEqual(oldIdx, newIdx) {
			changes = append(changes, &IndexDiff{
				Action:    MetadataDiffActionAlter,
				IndexName: idxName,
				OldIndex:  oldIdx,
				NewIndex:  newIdx,
			})
		}
	}

	return changes
}

// indexesEqual checks if two indexes are equal.
func indexesEqual(a, b *storepb.IndexMetadata) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Name == b.Name &&
		slices.Equal(a.Expressions, b.Expressions) &&
		a.Unique == b.Unique &&
		a.Primary == b.Primary &&
		a.Type == b.Type &&
		a.IsConstraint == b.IsConstraint
}

// compareForeignKeys compares foreign keys between two tables.
func compareForeignKeys(oldTable, newTable *storepb.TableMetadata) []*ForeignKeyDiff {
	oldFKs := buildFKMap(oldTable.ForeignKeys)
	newFKs := buildFKMap(newTable.ForeignKeys)

	var changes []*ForeignKeyDiff

	for _, fkName := range sortedKeys(oldFKs) {
		if _, ok := newFKs[fkName]; !ok {
			changes = append(changes, &ForeignKeyDiff{
				Action:         MetadataDiffActionDrop,
				ForeignKeyName: fkName,
				OldForeignKey:  oldFKs[fkName],
			})
		}
	}

	for _, fkName := range sortedKeys(newFKs) {
		newFK := newFKs[fkName]
		if oldFK, ok := oldFKs[fkName]; !ok {
			changes = append(changes, &ForeignKeyDiff{
				Action:         MetadataDiffActionCreate,
				ForeignKeyName: fkName,
				NewForeignKey:  newFK,
			})
		} else if !foreignKeysEqual(oldFK, newFK) {
			changes = append(changes, &ForeignKeyDiff{
				Action:         MetadataDiffActionAlter,
				ForeignKeyName: fkName,
				OldForeignKey:  oldFK,
				NewForeignKey:  newFK,
			})
		}
	}

	return changes
}

func foreignKeysEqual(a, b *storepb.ForeignKeyMetadata) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Name == b.Name &&
		slices.Equal(a.Columns, b.Columns) &&
		a.ReferencedTable == b.ReferencedTable &&
		slices.Equal(a.ReferencedColumns, b.ReferencedColumns) &&
		a.OnDelete == b.OnDelete &&
		a.OnUpdate == b.OnUpdate
}

// compareCheckConstraints compares check constraints between two tables.
func compareCheckConstraints(oldTable, newTable *storepb.TableMetadata) []*CheckConstraintDiff {
	oldChecks := buildCheckMap(oldTable.CheckConstraints)
	newChecks := buildCheckMap(newTable.CheckConstraints)

	var changes []*CheckConstraintDiff

	for _, name := range sortedKeys(oldChecks) {
		if _, ok := newChecks[name]; !ok {
			changes = append(changes, &CheckConstraintDiff{
				Action:              MetadataDiffActionDrop,
				CheckConstraintName: name,
				OldCheckConstraint:  oldChecks[name],
			})
		}
	}

	for _, name := range sortedKeys(newChecks) {
		newCheck := newChecks[name]
		if oldCheck, ok := oldChecks[name]; !ok {
			changes = append(changes, &CheckConstraintDiff{
				Action:              MetadataDiffActionCreate,
				CheckConstraintName: name,
				NewCheckConstraint:  newCheck,
			})
		} else if oldCheck.Expression != newCheck.Expression {
			changes = append(changes, &CheckConstraintDiff{
				Action:              MetadataDiffActionAlter,
				CheckConstraintName: name,
				OldCheckConstraint:  oldCheck,
				NewCheckConstraint:  newCheck,
			})
		}
	}

	return changes
}

// compareTriggers compares triggers between two tables.
func compareTriggers(oldTable, newTable *storepb.TableMetadata) []*TriggerDiff {
	oldTrigs := buildTriggerMap(oldTable.Triggers)
	newTrigs := buildTriggerMap(newTable.Triggers)

	var changes []*TriggerDiff

	for _, name := range sortedKeys(oldTrigs) {
		if _, ok := newTrigs[name]; !ok {
			changes = append(changes, &TriggerDiff{
				Action:      MetadataDiffActionDrop,
				TriggerName: name,
				OldTrigger:  oldTrigs[name],
			})
		}
	}

	for _, name := range sortedKeys(newTrigs) {
		newTrig := newTrigs[name]
		if oldTrig, ok := oldTrigs[name]; !ok {
			changes = append(changes, &TriggerDiff{
				Action:      MetadataDiffActionCreate,
				TriggerName: name,
				NewTrigger:  newTrig,
			})
		} else if !triggersEqual(oldTrig, newTrig) {
			changes = append(changes, &TriggerDiff{
				Action:      MetadataDiffActionAlter,
				TriggerName: name,
				OldTrigger:  oldTrig,
				NewTrigger:  newTrig,
			})
		}
	}

	return changes
}

func triggersEqual(a, b *storepb.TriggerMetadata) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Name == b.Name &&
		a.Event == b.Event &&
		a.Timing == b.Timing &&
		a.Body == b.Body
}

// comparePartitions compares partitions between two tables.
func comparePartitions(oldTable, newTable *storepb.TableMetadata) []*PartitionDiff {
	oldParts := buildPartitionMap(oldTable.Partitions)
	newParts := buildPartitionMap(newTable.Partitions)

	var changes []*PartitionDiff

	for _, name := range sortedKeys(oldParts) {
		if _, ok := newParts[name]; !ok {
			changes = append(changes, &PartitionDiff{
				Action:        MetadataDiffActionDrop,
				PartitionName: name,
				OldPartition:  oldParts[name],
			})
		}
	}

	for _, name := range sortedKeys(newParts) {
		newPart := newParts[name]
		if oldPart, ok := oldParts[name]; !ok {
			changes = append(changes, &PartitionDiff{
				Action:        MetadataDiffActionCreate,
				PartitionName: name,
				NewPartition:  newPart,
			})
		} else if oldPart.Type != newPart.Type || oldPart.Expression != newPart.Expression {
			changes = append(changes, &PartitionDiff{
				Action:        MetadataDiffActionAlter,
				PartitionName: name,
				OldPartition:  oldPart,
				NewPartition:  newPart,
			})
		}
	}

	return changes
}

// compareViews compares views between two schemas.
func compareViews(_ storepb.Engine, diff *MetadataDiff, schemaName string, oldSchema, newSchema *storepb.SchemaMetadata) {
	oldViews := buildViewMap(oldSchema.Views)
	newViews := buildViewMap(newSchema.Views)

	for _, viewName := range sortedKeys(oldViews) {
		if _, ok := newViews[viewName]; !ok {
			oldView := oldViews[viewName]
			if !oldView.SkipDump {
				diff.ViewChanges = append(diff.ViewChanges, &ViewDiff{
					Action:     MetadataDiffActionDrop,
					SchemaName: schemaName,
					ViewName:   viewName,
					OldView:    oldView,
				})
			}
		}
	}

	for _, viewName := range sortedKeys(newViews) {
		newView := newViews[viewName]
		if newView.SkipDump {
			continue
		}
		if oldView, ok := oldViews[viewName]; !ok || oldView.SkipDump {
			diff.ViewChanges = append(diff.ViewChanges, &ViewDiff{
				Action:     MetadataDiffActionCreate,
				SchemaName: schemaName,
				ViewName:   viewName,
				NewView:    newView,
			})
		} else if oldView.Definition != newView.Definition || oldView.Comment != newView.Comment {
			diff.ViewChanges = append(diff.ViewChanges, &ViewDiff{
				Action:     MetadataDiffActionAlter,
				SchemaName: schemaName,
				ViewName:   viewName,
				OldView:    oldView,
				NewView:    newView,
			})
		}
	}
}

// compareMaterializedViews compares materialized views between two schemas.
func compareMaterializedViews(_ storepb.Engine, diff *MetadataDiff, schemaName string, oldSchema, newSchema *storepb.SchemaMetadata) {
	oldMVs := buildMVMap(oldSchema.MaterializedViews)
	newMVs := buildMVMap(newSchema.MaterializedViews)

	for _, mvName := range sortedKeys(oldMVs) {
		if _, ok := newMVs[mvName]; !ok {
			oldMV := oldMVs[mvName]
			if !oldMV.SkipDump {
				diff.MaterializedViewChanges = append(diff.MaterializedViewChanges, &MaterializedViewDiff{
					Action:               MetadataDiffActionDrop,
					SchemaName:           schemaName,
					MaterializedViewName: mvName,
					OldMaterializedView:  oldMV,
				})
			}
		}
	}

	for _, mvName := range sortedKeys(newMVs) {
		newMV := newMVs[mvName]
		if newMV.SkipDump {
			continue
		}
		if oldMV, ok := oldMVs[mvName]; !ok || oldMV.SkipDump {
			diff.MaterializedViewChanges = append(diff.MaterializedViewChanges, &MaterializedViewDiff{
				Action:               MetadataDiffActionCreate,
				SchemaName:           schemaName,
				MaterializedViewName: mvName,
				NewMaterializedView:  newMV,
			})
		} else if oldMV.Definition != newMV.Definition || oldMV.Comment != newMV.Comment {
			diff.MaterializedViewChanges = append(diff.MaterializedViewChanges, &MaterializedViewDiff{
				Action:               MetadataDiffActionAlter,
				SchemaName:           schemaName,
				MaterializedViewName: mvName,
				OldMaterializedView:  oldMV,
				NewMaterializedView:  newMV,
			})
		}
	}
}

// compareFunctions compares functions between two schemas.
func compareFunctions(diff *MetadataDiff, schemaName string, oldSchema, newSchema *storepb.SchemaMetadata) {
	oldFuncs := buildFunctionMap(oldSchema.Functions)
	newFuncs := buildFunctionMap(newSchema.Functions)

	for _, funcName := range sortedKeys(oldFuncs) {
		if _, ok := newFuncs[funcName]; !ok {
			oldFn := oldFuncs[funcName]
			if !oldFn.SkipDump {
				diff.FunctionChanges = append(diff.FunctionChanges, &FunctionDiff{
					Action:       MetadataDiffActionDrop,
					SchemaName:   schemaName,
					FunctionName: funcName,
					OldFunction:  oldFn,
				})
			}
		}
	}

	for _, funcName := range sortedKeys(newFuncs) {
		newFn := newFuncs[funcName]
		if newFn.SkipDump {
			continue
		}
		if oldFn, ok := oldFuncs[funcName]; !ok || oldFn.SkipDump {
			diff.FunctionChanges = append(diff.FunctionChanges, &FunctionDiff{
				Action:       MetadataDiffActionCreate,
				SchemaName:   schemaName,
				FunctionName: funcName,
				NewFunction:  newFn,
			})
		} else if oldFn.Definition != newFn.Definition {
			diff.FunctionChanges = append(diff.FunctionChanges, &FunctionDiff{
				Action:       MetadataDiffActionAlter,
				SchemaName:   schemaName,
				FunctionName: funcName,
				OldFunction:  oldFn,
				NewFunction:  newFn,
			})
		}
	}
}

// compareProcedures compares procedures between two schemas.
func compareProcedures(diff *MetadataDiff, schemaName string, oldSchema, newSchema *storepb.SchemaMetadata) {
	oldProcs := buildProcedureMap(oldSchema.Procedures)
	newProcs := buildProcedureMap(newSchema.Procedures)

	for _, procName := range sortedKeys(oldProcs) {
		if _, ok := newProcs[procName]; !ok {
			oldProc := oldProcs[procName]
			if !oldProc.SkipDump {
				diff.ProcedureChanges = append(diff.ProcedureChanges, &ProcedureDiff{
					Action:        MetadataDiffActionDrop,
					SchemaName:    schemaName,
					ProcedureName: procName,
					OldProcedure:  oldProc,
				})
			}
		}
	}

	for _, procName := range sortedKeys(newProcs) {
		newProc := newProcs[procName]
		if newProc.SkipDump {
			continue
		}
		if oldProc, ok := oldProcs[procName]; !ok || oldProc.SkipDump {
			diff.ProcedureChanges = append(diff.ProcedureChanges, &ProcedureDiff{
				Action:        MetadataDiffActionCreate,
				SchemaName:    schemaName,
				ProcedureName: procName,
				NewProcedure:  newProc,
			})
		} else if oldProc.Definition != newProc.Definition {
			diff.ProcedureChanges = append(diff.ProcedureChanges, &ProcedureDiff{
				Action:        MetadataDiffActionAlter,
				SchemaName:    schemaName,
				ProcedureName: procName,
				OldProcedure:  oldProc,
				NewProcedure:  newProc,
			})
		}
	}
}

// compareSequences compares sequences between two schemas.
func compareSequences(diff *MetadataDiff, schemaName string, oldSchema, newSchema *storepb.SchemaMetadata) {
	oldSeqs := buildSequenceMap(oldSchema.Sequences)
	newSeqs := buildSequenceMap(newSchema.Sequences)

	for _, seqName := range sortedKeys(oldSeqs) {
		if _, ok := newSeqs[seqName]; !ok {
			oldSeq := oldSeqs[seqName]
			if !oldSeq.SkipDump {
				diff.SequenceChanges = append(diff.SequenceChanges, &SequenceDiff{
					Action:       MetadataDiffActionDrop,
					SchemaName:   schemaName,
					SequenceName: seqName,
					OldSequence:  oldSeq,
				})
			}
		}
	}

	for _, seqName := range sortedKeys(newSeqs) {
		newSeq := newSeqs[seqName]
		if newSeq.SkipDump {
			continue
		}
		if oldSeq, ok := oldSeqs[seqName]; !ok || oldSeq.SkipDump {
			diff.SequenceChanges = append(diff.SequenceChanges, &SequenceDiff{
				Action:       MetadataDiffActionCreate,
				SchemaName:   schemaName,
				SequenceName: seqName,
				NewSequence:  newSeq,
			})
		} else if !sequencesEqual(oldSeq, newSeq) {
			diff.SequenceChanges = append(diff.SequenceChanges, &SequenceDiff{
				Action:       MetadataDiffActionAlter,
				SchemaName:   schemaName,
				SequenceName: seqName,
				OldSequence:  oldSeq,
				NewSequence:  newSeq,
			})
		}
	}
}

func sequencesEqual(a, b *storepb.SequenceMetadata) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Name == b.Name &&
		a.DataType == b.DataType &&
		a.Start == b.Start &&
		a.MinValue == b.MinValue &&
		a.MaxValue == b.MaxValue &&
		a.Increment == b.Increment &&
		a.Cycle == b.Cycle &&
		a.CacheSize == b.CacheSize
}

// compareEnumTypes compares enum types between two schemas.
func compareEnumTypes(diff *MetadataDiff, schemaName string, oldSchema, newSchema *storepb.SchemaMetadata) {
	oldEnums := buildEnumTypeMap(oldSchema.EnumTypes)
	newEnums := buildEnumTypeMap(newSchema.EnumTypes)

	for _, enumName := range sortedKeys(oldEnums) {
		if _, ok := newEnums[enumName]; !ok {
			oldEnum := oldEnums[enumName]
			if !oldEnum.SkipDump {
				diff.EnumTypeChanges = append(diff.EnumTypeChanges, &EnumTypeDiff{
					Action:       MetadataDiffActionDrop,
					SchemaName:   schemaName,
					EnumTypeName: enumName,
					OldEnumType:  oldEnum,
				})
			}
		}
	}

	for _, enumName := range sortedKeys(newEnums) {
		newEnum := newEnums[enumName]
		if newEnum.SkipDump {
			continue
		}
		if oldEnum, ok := oldEnums[enumName]; !ok || oldEnum.SkipDump {
			diff.EnumTypeChanges = append(diff.EnumTypeChanges, &EnumTypeDiff{
				Action:       MetadataDiffActionCreate,
				SchemaName:   schemaName,
				EnumTypeName: enumName,
				NewEnumType:  newEnum,
			})
		} else if !slices.Equal(oldEnum.Values, newEnum.Values) {
			diff.EnumTypeChanges = append(diff.EnumTypeChanges, &EnumTypeDiff{
				Action:       MetadataDiffActionAlter,
				SchemaName:   schemaName,
				EnumTypeName: enumName,
				OldEnumType:  oldEnum,
				NewEnumType:  newEnum,
			})
		}
	}
}

// compareEvents compares events between two schemas.
func compareEvents(diff *MetadataDiff, schemaName string, oldSchema, newSchema *storepb.SchemaMetadata) {
	oldEvents := buildEventMap(oldSchema.Events)
	newEvents := buildEventMap(newSchema.Events)

	for _, eventName := range sortedKeys(oldEvents) {
		if _, ok := newEvents[eventName]; !ok {
			diff.EventChanges = append(diff.EventChanges, &EventDiff{
				Action:     MetadataDiffActionDrop,
				SchemaName: schemaName,
				EventName:  eventName,
				OldEvent:   oldEvents[eventName],
			})
		}
	}

	for _, eventName := range sortedKeys(newEvents) {
		newEvent := newEvents[eventName]
		if oldEvent, ok := oldEvents[eventName]; !ok {
			diff.EventChanges = append(diff.EventChanges, &EventDiff{
				Action:     MetadataDiffActionCreate,
				SchemaName: schemaName,
				EventName:  eventName,
				NewEvent:   newEvent,
			})
		} else if oldEvent.Definition != newEvent.Definition {
			diff.EventChanges = append(diff.EventChanges, &EventDiff{
				Action:     MetadataDiffActionAlter,
				SchemaName: schemaName,
				EventName:  eventName,
				OldEvent:   oldEvent,
				NewEvent:   newEvent,
			})
		}
	}
}

// compareExtensions compares extensions between two database schemas.
func compareExtensions(diff *MetadataDiff, oldSchema, newSchema *storepb.DatabaseSchemaMetadata) {
	oldExts := buildExtensionMap(oldSchema.Extensions)
	newExts := buildExtensionMap(newSchema.Extensions)

	for _, extName := range sortedKeys(oldExts) {
		if _, ok := newExts[extName]; !ok {
			diff.ExtensionChanges = append(diff.ExtensionChanges, &ExtensionDiff{
				Action:        MetadataDiffActionDrop,
				ExtensionName: extName,
				OldExtension:  oldExts[extName],
			})
		}
	}

	for _, extName := range sortedKeys(newExts) {
		newExt := newExts[extName]
		if oldExt, ok := oldExts[extName]; !ok {
			diff.ExtensionChanges = append(diff.ExtensionChanges, &ExtensionDiff{
				Action:        MetadataDiffActionCreate,
				ExtensionName: extName,
				NewExtension:  newExt,
			})
		} else if oldExt.Version != newExt.Version || oldExt.Schema != newExt.Schema {
			diff.ExtensionChanges = append(diff.ExtensionChanges, &ExtensionDiff{
				Action:        MetadataDiffActionAlter,
				ExtensionName: extName,
				OldExtension:  oldExt,
				NewExtension:  newExt,
			})
		}
	}
}

// compareEventTriggers compares event triggers between two database schemas.
func compareEventTriggers(diff *MetadataDiff, oldSchema, newSchema *storepb.DatabaseSchemaMetadata) {
	oldETs := buildEventTriggerMap(oldSchema.EventTriggers)
	newETs := buildEventTriggerMap(newSchema.EventTriggers)

	for _, etName := range sortedKeys(oldETs) {
		if _, ok := newETs[etName]; !ok {
			diff.EventTriggerChanges = append(diff.EventTriggerChanges, &EventTriggerDiff{
				Action:           MetadataDiffActionDrop,
				EventTriggerName: etName,
				OldEventTrigger:  oldETs[etName],
			})
		}
	}

	for _, etName := range sortedKeys(newETs) {
		newET := newETs[etName]
		if oldET, ok := oldETs[etName]; !ok {
			diff.EventTriggerChanges = append(diff.EventTriggerChanges, &EventTriggerDiff{
				Action:           MetadataDiffActionCreate,
				EventTriggerName: etName,
				NewEventTrigger:  newET,
			})
		} else if oldET.Definition != newET.Definition {
			diff.EventTriggerChanges = append(diff.EventTriggerChanges, &EventTriggerDiff{
				Action:           MetadataDiffActionAlter,
				EventTriggerName: etName,
				OldEventTrigger:  oldET,
				NewEventTrigger:  newET,
			})
		}
	}
}

// sortDiffLists sorts all diff lists for stable output order.
func sortDiffLists(diff *MetadataDiff) {
	// Sort schema changes
	slices.SortFunc(diff.SchemaChanges, func(a, b *SchemaDiff) int {
		return strings.Compare(a.SchemaName, b.SchemaName)
	})

	// Sort table changes
	slices.SortFunc(diff.TableChanges, func(a, b *TableDiff) int {
		if c := strings.Compare(a.SchemaName, b.SchemaName); c != 0 {
			return c
		}
		return strings.Compare(a.TableName, b.TableName)
	})

	// Sort view changes
	slices.SortFunc(diff.ViewChanges, func(a, b *ViewDiff) int {
		if c := strings.Compare(a.SchemaName, b.SchemaName); c != 0 {
			return c
		}
		return strings.Compare(a.ViewName, b.ViewName)
	})

	// Sort materialized view changes
	slices.SortFunc(diff.MaterializedViewChanges, func(a, b *MaterializedViewDiff) int {
		if c := strings.Compare(a.SchemaName, b.SchemaName); c != 0 {
			return c
		}
		return strings.Compare(a.MaterializedViewName, b.MaterializedViewName)
	})

	// Sort function changes
	slices.SortFunc(diff.FunctionChanges, func(a, b *FunctionDiff) int {
		if c := strings.Compare(a.SchemaName, b.SchemaName); c != 0 {
			return c
		}
		return strings.Compare(a.FunctionName, b.FunctionName)
	})

	// Sort procedure changes
	slices.SortFunc(diff.ProcedureChanges, func(a, b *ProcedureDiff) int {
		if c := strings.Compare(a.SchemaName, b.SchemaName); c != 0 {
			return c
		}
		return strings.Compare(a.ProcedureName, b.ProcedureName)
	})

	// Sort sequence changes
	slices.SortFunc(diff.SequenceChanges, func(a, b *SequenceDiff) int {
		if c := strings.Compare(a.SchemaName, b.SchemaName); c != 0 {
			return c
		}
		return strings.Compare(a.SequenceName, b.SequenceName)
	})

	// Sort enum type changes
	slices.SortFunc(diff.EnumTypeChanges, func(a, b *EnumTypeDiff) int {
		if c := strings.Compare(a.SchemaName, b.SchemaName); c != 0 {
			return c
		}
		return strings.Compare(a.EnumTypeName, b.EnumTypeName)
	})

	// Sort extension changes
	slices.SortFunc(diff.ExtensionChanges, func(a, b *ExtensionDiff) int {
		return strings.Compare(a.ExtensionName, b.ExtensionName)
	})

	// Sort event trigger changes
	slices.SortFunc(diff.EventTriggerChanges, func(a, b *EventTriggerDiff) int {
		return strings.Compare(a.EventTriggerName, b.EventTriggerName)
	})

	// Sort event changes
	slices.SortFunc(diff.EventChanges, func(a, b *EventDiff) int {
		if c := strings.Compare(a.SchemaName, b.SchemaName); c != 0 {
			return c
		}
		return strings.Compare(a.EventName, b.EventName)
	})

	// Sort comment changes
	slices.SortFunc(diff.CommentChanges, func(a, b *CommentDiff) int {
		if c := strings.Compare(a.SchemaName, b.SchemaName); c != 0 {
			return c
		}
		return strings.Compare(string(a.ObjectType), string(b.ObjectType))
	})

	// Sort sub-object changes within table diffs
	for _, td := range diff.TableChanges {
		sortTableSubObjectChanges(td)
	}
}

func sortTableSubObjectChanges(td *TableDiff) {
	slices.SortFunc(td.ColumnChanges, func(a, b *ColumnDiff) int {
		aName := columnDiffName(a)
		bName := columnDiffName(b)
		return strings.Compare(aName, bName)
	})
	slices.SortFunc(td.IndexChanges, func(a, b *IndexDiff) int {
		return strings.Compare(a.IndexName, b.IndexName)
	})
	slices.SortFunc(td.ForeignKeyChanges, func(a, b *ForeignKeyDiff) int {
		return strings.Compare(a.ForeignKeyName, b.ForeignKeyName)
	})
	slices.SortFunc(td.CheckConstraintChanges, func(a, b *CheckConstraintDiff) int {
		return strings.Compare(a.CheckConstraintName, b.CheckConstraintName)
	})
	slices.SortFunc(td.TriggerChanges, func(a, b *TriggerDiff) int {
		return strings.Compare(a.TriggerName, b.TriggerName)
	})
}

func columnDiffName(cd *ColumnDiff) string {
	if cd.NewColumn != nil {
		return cd.NewColumn.Name
	}
	if cd.OldColumn != nil {
		return cd.OldColumn.Name
	}
	return ""
}

// ---- Helper map/accessor functions ----

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func buildTableMap(tables []*storepb.TableMetadata) map[string]*storepb.TableMetadata {
	m := make(map[string]*storepb.TableMetadata)
	for _, t := range tables {
		m[t.Name] = t
	}
	return m
}

func buildColumnMap(columns []*storepb.ColumnMetadata) map[string]*storepb.ColumnMetadata {
	m := make(map[string]*storepb.ColumnMetadata)
	for _, c := range columns {
		m[c.Name] = c
	}
	return m
}

func buildIndexMap(indexes []*storepb.IndexMetadata) map[string]*storepb.IndexMetadata {
	m := make(map[string]*storepb.IndexMetadata)
	for _, idx := range indexes {
		m[idx.Name] = idx
	}
	return m
}

func buildFKMap(fks []*storepb.ForeignKeyMetadata) map[string]*storepb.ForeignKeyMetadata {
	m := make(map[string]*storepb.ForeignKeyMetadata)
	for _, fk := range fks {
		m[fk.Name] = fk
	}
	return m
}

func buildCheckMap(checks []*storepb.CheckConstraintMetadata) map[string]*storepb.CheckConstraintMetadata {
	m := make(map[string]*storepb.CheckConstraintMetadata)
	for _, c := range checks {
		m[c.Name] = c
	}
	return m
}

func buildTriggerMap(triggers []*storepb.TriggerMetadata) map[string]*storepb.TriggerMetadata {
	m := make(map[string]*storepb.TriggerMetadata)
	for _, t := range triggers {
		m[t.Name] = t
	}
	return m
}

func buildPartitionMap(parts []*storepb.TablePartitionMetadata) map[string]*storepb.TablePartitionMetadata {
	m := make(map[string]*storepb.TablePartitionMetadata)
	for _, p := range parts {
		m[p.Name] = p
	}
	return m
}

func buildViewMap(views []*storepb.ViewMetadata) map[string]*storepb.ViewMetadata {
	m := make(map[string]*storepb.ViewMetadata)
	for _, v := range views {
		m[v.Name] = v
	}
	return m
}

func buildMVMap(mvs []*storepb.MaterializedViewMetadata) map[string]*storepb.MaterializedViewMetadata {
	m := make(map[string]*storepb.MaterializedViewMetadata)
	for _, mv := range mvs {
		m[mv.Name] = mv
	}
	return m
}

func buildFunctionMap(funcs []*storepb.FunctionMetadata) map[string]*storepb.FunctionMetadata {
	m := make(map[string]*storepb.FunctionMetadata)
	for _, f := range funcs {
		m[f.Name] = f
	}
	return m
}

func buildProcedureMap(procs []*storepb.ProcedureMetadata) map[string]*storepb.ProcedureMetadata {
	m := make(map[string]*storepb.ProcedureMetadata)
	for _, p := range procs {
		m[p.Name] = p
	}
	return m
}

func buildSequenceMap(seqs []*storepb.SequenceMetadata) map[string]*storepb.SequenceMetadata {
	m := make(map[string]*storepb.SequenceMetadata)
	for _, s := range seqs {
		m[s.Name] = s
	}
	return m
}

func buildEnumTypeMap(enums []*storepb.EnumTypeMetadata) map[string]*storepb.EnumTypeMetadata {
	m := make(map[string]*storepb.EnumTypeMetadata)
	for _, e := range enums {
		m[e.Name] = e
	}
	return m
}

func buildEventMap(events []*storepb.EventMetadata) map[string]*storepb.EventMetadata {
	m := make(map[string]*storepb.EventMetadata)
	for _, e := range events {
		m[e.Name] = e
	}
	return m
}

func buildExtensionMap(exts []*storepb.ExtensionMetadata) map[string]*storepb.ExtensionMetadata {
	m := make(map[string]*storepb.ExtensionMetadata)
	for _, e := range exts {
		m[e.Name] = e
	}
	return m
}

func buildEventTriggerMap(ets []*storepb.EventTriggerMetadata) map[string]*storepb.EventTriggerMetadata {
	m := make(map[string]*storepb.EventTriggerMetadata)
	for _, et := range ets {
		m[et.Name] = et
	}
	return m
}
