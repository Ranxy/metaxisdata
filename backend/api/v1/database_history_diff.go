package v1

import (
	"strings"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

func diffColumnGroup(beforeTable, afterTable *v1pb.TableMetadata) *v1pb.MetadataHistoryChangeGroup {
	return diffColumnGroupFromList(tableColumns(beforeTable), tableColumns(afterTable))
}

func diffViewColumnGroup(beforeView, afterView *v1pb.ViewMetadata) *v1pb.MetadataHistoryChangeGroup {
	return diffColumnGroupFromList(viewColumns(beforeView), viewColumns(afterView))
}

func diffColumnGroupFromList(beforeCols, afterCols []*v1pb.ColumnMetadata) *v1pb.MetadataHistoryChangeGroup {
	beforeMap := map[string]*v1pb.ColumnMetadata{}
	afterMap := map[string]*v1pb.ColumnMetadata{}
	keys := map[string]struct{}{}
	for _, col := range beforeCols {
		beforeMap[col.GetName()] = col
		keys[col.GetName()] = struct{}{}
	}
	for _, col := range afterCols {
		afterMap[col.GetName()] = col
		keys[col.GetName()] = struct{}{}
	}

	orderedKeys := collectOrderedKeys(keys)
	items := []*v1pb.MetadataHistoryChangeItem{}
	for _, key := range orderedKeys {
		before := beforeMap[key]
		after := afterMap[key]
		switch {
		case before == nil && after != nil:
			items = append(items, &v1pb.MetadataHistoryChangeItem{
				Section:     v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_COLUMN,
				Operation:   v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_CREATED,
				Key:         key,
				DisplayName: key,
				Summary:     "created",
				After:       columnSnapshot(after),
			})
		case before != nil && after == nil:
			items = append(items, &v1pb.MetadataHistoryChangeItem{
				Section:     v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_COLUMN,
				Operation:   v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_DELETED,
				Key:         key,
				DisplayName: key,
				Summary:     "deleted",
				Before:      columnSnapshot(before),
			})
		case before != nil && after != nil:
			fieldChanges := compareColumnFields(before, after)
			if len(fieldChanges) == 0 {
				continue
			}
			items = append(items, &v1pb.MetadataHistoryChangeItem{
				Section:      v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_COLUMN,
				Operation:    v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED,
				Key:          key,
				DisplayName:  key,
				Summary:      summarizeFieldChanges(fieldChanges),
				FieldChanges: fieldChanges,
				Before:       columnSnapshot(before),
				After:        columnSnapshot(after),
			})

		default:
		}
	}
	if len(items) == 0 {
		return nil
	}
	return &v1pb.MetadataHistoryChangeGroup{Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_COLUMN, Changes: items}
}

func diffIndexGroup(beforeTable, afterTable *v1pb.TableMetadata) *v1pb.MetadataHistoryChangeGroup {
	return diffIndexGroupFromList(tableIndexes(beforeTable), tableIndexes(afterTable))
}

func diffIndexGroupFromList(beforeIndexes, afterIndexes []*v1pb.IndexMetadata) *v1pb.MetadataHistoryChangeGroup {
	return diffNamedIndexLikeGroup(v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_INDEX, beforeIndexes, afterIndexes)
}

func diffForeignKeyGroup(beforeTable, afterTable *v1pb.TableMetadata) *v1pb.MetadataHistoryChangeGroup {
	beforeMap := map[string]*v1pb.ForeignKeyMetadata{}
	afterMap := map[string]*v1pb.ForeignKeyMetadata{}
	keys := map[string]struct{}{}
	for _, item := range foreignKeys(beforeTable) {
		beforeMap[item.GetName()] = item
		keys[item.GetName()] = struct{}{}
	}
	for _, item := range foreignKeys(afterTable) {
		afterMap[item.GetName()] = item
		keys[item.GetName()] = struct{}{}
	}
	items := []*v1pb.MetadataHistoryChangeItem{}
	for _, key := range collectOrderedKeys(keys) {
		before := beforeMap[key]
		after := afterMap[key]
		switch {
		case before == nil && after != nil:
			items = append(items, newChildLifecycleItem(v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_FOREIGN_KEY, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_CREATED, key, foreignKeySnapshot(nil, after)))
		case before != nil && after == nil:
			items = append(items, newChildLifecycleItem(v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_FOREIGN_KEY, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_DELETED, key, foreignKeySnapshot(before, nil)))
		case before != nil && after != nil:
			fieldChanges := []*v1pb.MetadataFieldChange{}
			appendStringFieldChange(&fieldChanges, "columns", "columns", strings.Join(before.GetColumns(), ", "), strings.Join(after.GetColumns(), ", "))
			appendStringFieldChange(&fieldChanges, "referenced_table", "referenced table", before.GetReferencedTable(), after.GetReferencedTable())
			appendStringFieldChange(&fieldChanges, "referenced_columns", "referenced columns", strings.Join(before.GetReferencedColumns(), ", "), strings.Join(after.GetReferencedColumns(), ", "))
			appendStringFieldChange(&fieldChanges, "on_delete", "on delete", before.GetOnDelete(), after.GetOnDelete())
			appendStringFieldChange(&fieldChanges, "on_update", "on update", before.GetOnUpdate(), after.GetOnUpdate())
			if len(fieldChanges) == 0 {
				continue
			}
			items = append(items, &v1pb.MetadataHistoryChangeItem{Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_FOREIGN_KEY, Operation: v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED, Key: key, DisplayName: key, Summary: summarizeFieldChanges(fieldChanges), FieldChanges: fieldChanges, Before: foreignKeySnapshot(before, nil), After: foreignKeySnapshot(nil, after)})
		default:
		}
	}
	if len(items) == 0 {
		return nil
	}
	return &v1pb.MetadataHistoryChangeGroup{Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_FOREIGN_KEY, Changes: items}
}

func diffCheckConstraintGroup(beforeTable, afterTable *v1pb.TableMetadata) *v1pb.MetadataHistoryChangeGroup {
	beforeMap := map[string]*v1pb.CheckConstraintMetadata{}
	afterMap := map[string]*v1pb.CheckConstraintMetadata{}
	keys := map[string]struct{}{}
	for _, item := range checkConstraints(beforeTable) {
		beforeMap[item.GetName()] = item
		keys[item.GetName()] = struct{}{}
	}
	for _, item := range checkConstraints(afterTable) {
		afterMap[item.GetName()] = item
		keys[item.GetName()] = struct{}{}
	}
	items := []*v1pb.MetadataHistoryChangeItem{}
	for _, key := range collectOrderedKeys(keys) {
		before := beforeMap[key]
		after := afterMap[key]
		switch {
		case before == nil && after != nil:
			items = append(items, newChildLifecycleItem(v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_CHECK_CONSTRAINT, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_CREATED, key, checkConstraintSnapshot(nil, after)))
		case before != nil && after == nil:
			items = append(items, newChildLifecycleItem(v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_CHECK_CONSTRAINT, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_DELETED, key, checkConstraintSnapshot(before, nil)))
		case before != nil && after != nil && before.GetExpression() != after.GetExpression():
			fieldChanges := []*v1pb.MetadataFieldChange{{Field: "expression", DisplayName: "expression", Before: before.GetExpression(), After: after.GetExpression()}}
			items = append(items, &v1pb.MetadataHistoryChangeItem{Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_CHECK_CONSTRAINT, Operation: v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED, Key: key, DisplayName: key, Summary: summarizeFieldChanges(fieldChanges), FieldChanges: fieldChanges, Before: checkConstraintSnapshot(before, nil), After: checkConstraintSnapshot(nil, after)})
		default:
		}
	}
	if len(items) == 0 {
		return nil
	}
	return &v1pb.MetadataHistoryChangeGroup{Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_CHECK_CONSTRAINT, Changes: items}
}

func diffPartitionGroup(beforeTable, afterTable *v1pb.TableMetadata) *v1pb.MetadataHistoryChangeGroup {
	beforeMap := map[string]*v1pb.TablePartitionMetadata{}
	afterMap := map[string]*v1pb.TablePartitionMetadata{}
	keys := map[string]struct{}{}
	for _, item := range partitions(beforeTable) {
		beforeMap[item.GetName()] = item
		keys[item.GetName()] = struct{}{}
	}
	for _, item := range partitions(afterTable) {
		afterMap[item.GetName()] = item
		keys[item.GetName()] = struct{}{}
	}
	items := []*v1pb.MetadataHistoryChangeItem{}
	for _, key := range collectOrderedKeys(keys) {
		before := beforeMap[key]
		after := afterMap[key]
		switch {
		case before == nil && after != nil:
			items = append(items, newChildLifecycleItem(v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_PARTITION, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_CREATED, key, partitionSnapshot(nil, after)))
		case before != nil && after == nil:
			items = append(items, newChildLifecycleItem(v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_PARTITION, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_DELETED, key, partitionSnapshot(before, nil)))
		case before != nil && after != nil:
			fieldChanges := []*v1pb.MetadataFieldChange{}
			appendStringFieldChange(&fieldChanges, "type", "type", before.GetType().String(), after.GetType().String())
			appendStringFieldChange(&fieldChanges, "expression", "expression", before.GetExpression(), after.GetExpression())
			appendStringFieldChange(&fieldChanges, "value", "value", before.GetValue(), after.GetValue())
			if len(fieldChanges) == 0 {
				continue
			}
			items = append(items, &v1pb.MetadataHistoryChangeItem{Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_PARTITION, Operation: v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED, Key: key, DisplayName: key, Summary: summarizeFieldChanges(fieldChanges), FieldChanges: fieldChanges, Before: partitionSnapshot(before, nil), After: partitionSnapshot(nil, after)})
		default:
		}
	}
	if len(items) == 0 {
		return nil
	}
	return &v1pb.MetadataHistoryChangeGroup{Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_PARTITION, Changes: items}
}

func diffTriggerGroup(beforeTriggers, afterTriggers []*v1pb.TriggerMetadata) *v1pb.MetadataHistoryChangeGroup {
	beforeMap := map[string]*v1pb.TriggerMetadata{}
	afterMap := map[string]*v1pb.TriggerMetadata{}
	keys := map[string]struct{}{}
	for _, item := range beforeTriggers {
		beforeMap[item.GetName()] = item
		keys[item.GetName()] = struct{}{}
	}
	for _, item := range afterTriggers {
		afterMap[item.GetName()] = item
		keys[item.GetName()] = struct{}{}
	}
	items := []*v1pb.MetadataHistoryChangeItem{}
	for _, key := range collectOrderedKeys(keys) {
		before := beforeMap[key]
		after := afterMap[key]
		switch {
		case before == nil && after != nil:
			items = append(items, newChildLifecycleItem(v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_TRIGGER, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_CREATED, key, triggerSnapshot(nil, after)))
		case before != nil && after == nil:
			items = append(items, newChildLifecycleItem(v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_TRIGGER, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_DELETED, key, triggerSnapshot(before, nil)))
		case before != nil && after != nil:
			fieldChanges := []*v1pb.MetadataFieldChange{}
			appendStringFieldChange(&fieldChanges, "event", "event", before.GetEvent(), after.GetEvent())
			appendStringFieldChange(&fieldChanges, "timing", "timing", before.GetTiming(), after.GetTiming())
			appendStringFieldChange(&fieldChanges, "comment", "comment", before.GetComment(), after.GetComment())
			if len(fieldChanges) == 0 {
				continue
			}
			items = append(items, &v1pb.MetadataHistoryChangeItem{Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_TRIGGER, Operation: v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED, Key: key, DisplayName: key, Summary: summarizeFieldChanges(fieldChanges), FieldChanges: fieldChanges, Before: triggerSnapshot(before, nil), After: triggerSnapshot(nil, after)})
		default:
		}
	}
	if len(items) == 0 {
		return nil
	}
	return &v1pb.MetadataHistoryChangeGroup{Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_TRIGGER, Changes: items}
}

func diffRuleGroup(beforeRules, afterRules []*v1pb.RuleMetadata) *v1pb.MetadataHistoryChangeGroup {
	beforeMap := map[string]*v1pb.RuleMetadata{}
	afterMap := map[string]*v1pb.RuleMetadata{}
	keys := map[string]struct{}{}
	for _, item := range beforeRules {
		beforeMap[item.GetName()] = item
		keys[item.GetName()] = struct{}{}
	}
	for _, item := range afterRules {
		afterMap[item.GetName()] = item
		keys[item.GetName()] = struct{}{}
	}
	items := []*v1pb.MetadataHistoryChangeItem{}
	for _, key := range collectOrderedKeys(keys) {
		before := beforeMap[key]
		after := afterMap[key]
		switch {
		case before == nil && after != nil:
			items = append(items, newChildLifecycleItem(v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_RULE, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_CREATED, key, ruleSnapshot(nil, after)))
		case before != nil && after == nil:
			items = append(items, newChildLifecycleItem(v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_RULE, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_DELETED, key, ruleSnapshot(before, nil)))
		case before != nil && after != nil:
			fieldChanges := []*v1pb.MetadataFieldChange{}
			appendStringFieldChange(&fieldChanges, "event", "event", before.GetEvent(), after.GetEvent())
			appendStringFieldChange(&fieldChanges, "condition", "condition", before.GetCondition(), after.GetCondition())
			appendStringFieldChange(&fieldChanges, "action", "action", before.GetAction(), after.GetAction())
			appendBoolFieldChange(&fieldChanges, "is_instead", "instead", before.GetIsInstead(), after.GetIsInstead())
			appendBoolFieldChange(&fieldChanges, "is_enabled", "enabled", before.GetIsEnabled(), after.GetIsEnabled())
			if len(fieldChanges) == 0 {
				continue
			}
			items = append(items, &v1pb.MetadataHistoryChangeItem{Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_RULE, Operation: v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED, Key: key, DisplayName: key, Summary: summarizeFieldChanges(fieldChanges), FieldChanges: fieldChanges, Before: ruleSnapshot(before, nil), After: ruleSnapshot(nil, after)})
		default:
		}
	}
	if len(items) == 0 {
		return nil
	}
	return &v1pb.MetadataHistoryChangeGroup{Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_RULE, Changes: items}
}

func diffTagGroup(beforeTags, afterTags []string) *v1pb.MetadataHistoryChangeGroup {
	beforeSet := map[string]struct{}{}
	afterSet := map[string]struct{}{}
	for _, tag := range beforeTags {
		beforeSet[tag] = struct{}{}
	}
	for _, tag := range afterTags {
		afterSet[tag] = struct{}{}
	}
	keys := map[string]struct{}{}
	for tag := range beforeSet {
		keys[tag] = struct{}{}
	}
	for tag := range afterSet {
		keys[tag] = struct{}{}
	}
	items := []*v1pb.MetadataHistoryChangeItem{}
	for _, key := range collectOrderedKeys(keys) {
		_, hadBefore := beforeSet[key]
		_, hasAfter := afterSet[key]
		switch {
		case !hadBefore && hasAfter:
			items = append(items, &v1pb.MetadataHistoryChangeItem{Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_TAG, Operation: v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_CREATED, Key: key, DisplayName: key, Summary: "created"})
		case hadBefore && !hasAfter:
			items = append(items, &v1pb.MetadataHistoryChangeItem{Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_TAG, Operation: v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_DELETED, Key: key, DisplayName: key, Summary: "deleted"})
		default:
		}
	}
	if len(items) == 0 {
		return nil
	}
	return &v1pb.MetadataHistoryChangeGroup{Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_TAG, Changes: items}
}

func diffAttributeGroup(beforeAttributes, afterAttributes map[string]string) *v1pb.MetadataHistoryChangeGroup {
	keys := map[string]struct{}{}
	for key := range beforeAttributes {
		keys[key] = struct{}{}
	}
	for key := range afterAttributes {
		keys[key] = struct{}{}
	}
	items := []*v1pb.MetadataHistoryChangeItem{}
	for _, key := range collectOrderedKeys(keys) {
		before, hadBefore := beforeAttributes[key]
		after, hasAfter := afterAttributes[key]
		switch {
		case !hadBefore && hasAfter:
			items = append(items, &v1pb.MetadataHistoryChangeItem{Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_ATTRIBUTE, Operation: v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_CREATED, Key: key, DisplayName: key, Summary: "created", FieldChanges: []*v1pb.MetadataFieldChange{{Field: "value", DisplayName: "value", After: after}}})
		case hadBefore && !hasAfter:
			items = append(items, &v1pb.MetadataHistoryChangeItem{Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_ATTRIBUTE, Operation: v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_DELETED, Key: key, DisplayName: key, Summary: "deleted", FieldChanges: []*v1pb.MetadataFieldChange{{Field: "value", DisplayName: "value", Before: before}}})
		case hadBefore && hasAfter && before != after:
			fieldChanges := []*v1pb.MetadataFieldChange{{Field: "value", DisplayName: "value", Before: before, After: after}}
			items = append(items, &v1pb.MetadataHistoryChangeItem{Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_ATTRIBUTE, Operation: v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED, Key: key, DisplayName: key, Summary: summarizeFieldChanges(fieldChanges), FieldChanges: fieldChanges})
		default:
		}
	}
	if len(items) == 0 {
		return nil
	}
	return &v1pb.MetadataHistoryChangeGroup{Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_ATTRIBUTE, Changes: items}
}

func diffNamedIndexLikeGroup(section v1pb.MetadataHistorySection, beforeIndexes, afterIndexes []*v1pb.IndexMetadata) *v1pb.MetadataHistoryChangeGroup {
	beforeMap := map[string]*v1pb.IndexMetadata{}
	afterMap := map[string]*v1pb.IndexMetadata{}
	keys := map[string]struct{}{}
	for _, item := range beforeIndexes {
		beforeMap[item.GetName()] = item
		keys[item.GetName()] = struct{}{}
	}
	for _, item := range afterIndexes {
		afterMap[item.GetName()] = item
		keys[item.GetName()] = struct{}{}
	}
	items := []*v1pb.MetadataHistoryChangeItem{}
	for _, key := range collectOrderedKeys(keys) {
		before := beforeMap[key]
		after := afterMap[key]
		switch {
		case before == nil && after != nil:
			items = append(items, newChildLifecycleItem(section, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_CREATED, key, indexSnapshot(nil, after)))
		case before != nil && after == nil:
			items = append(items, newChildLifecycleItem(section, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_DELETED, key, indexSnapshot(before, nil)))
		case before != nil && after != nil:
			fieldChanges := []*v1pb.MetadataFieldChange{}
			appendStringFieldChange(&fieldChanges, "expressions", "expressions", strings.Join(before.GetExpressions(), ", "), strings.Join(after.GetExpressions(), ", "))
			appendStringFieldChange(&fieldChanges, "type", "type", before.GetType(), after.GetType())
			appendBoolFieldChange(&fieldChanges, "unique", "unique", before.GetUnique(), after.GetUnique())
			appendBoolFieldChange(&fieldChanges, "primary", "primary", before.GetPrimary(), after.GetPrimary())
			appendBoolFieldChange(&fieldChanges, "visible", "visible", before.GetVisible(), after.GetVisible())
			appendStringFieldChange(&fieldChanges, "comment", "comment", before.GetComment(), after.GetComment())
			appendStringFieldChange(&fieldChanges, "definition", "definition", before.GetDefinition(), after.GetDefinition())
			if len(fieldChanges) == 0 {
				continue
			}
			items = append(items, &v1pb.MetadataHistoryChangeItem{Section: section, Operation: v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED, Key: key, DisplayName: key, Summary: summarizeFieldChanges(fieldChanges), FieldChanges: fieldChanges, Before: indexSnapshot(before, nil), After: indexSnapshot(nil, after)})
		default:
		}
	}
	if len(items) == 0 {
		return nil
	}
	return &v1pb.MetadataHistoryChangeGroup{Section: section, Changes: items}
}

func tableColumns(table *v1pb.TableMetadata) []*v1pb.ColumnMetadata {
	if table == nil {
		return nil
	}
	return table.GetColumns()
}

func viewColumns(view *v1pb.ViewMetadata) []*v1pb.ColumnMetadata {
	if view == nil {
		return nil
	}
	return view.GetColumns()
}

func tableIndexes(table *v1pb.TableMetadata) []*v1pb.IndexMetadata {
	if table == nil {
		return nil
	}
	return table.GetIndexes()
}

func foreignKeys(table *v1pb.TableMetadata) []*v1pb.ForeignKeyMetadata {
	if table == nil {
		return nil
	}
	return table.GetForeignKeys()
}

func checkConstraints(table *v1pb.TableMetadata) []*v1pb.CheckConstraintMetadata {
	if table == nil {
		return nil
	}
	return table.GetCheckConstraints()
}

func partitions(table *v1pb.TableMetadata) []*v1pb.TablePartitionMetadata {
	if table == nil {
		return nil
	}
	return table.GetPartitions()
}

func viewTriggers(view *v1pb.ViewMetadata) []*v1pb.TriggerMetadata {
	if view == nil {
		return nil
	}
	return view.GetTriggers()
}

func materializedViewTriggers(view *v1pb.MaterializedViewMetadata) []*v1pb.TriggerMetadata {
	if view == nil {
		return nil
	}
	return view.GetTriggers()
}

func viewRules(view *v1pb.ViewMetadata) []*v1pb.RuleMetadata {
	if view == nil {
		return nil
	}
	return view.GetRules()
}

func indexMetadataList(view *v1pb.MaterializedViewMetadata) []*v1pb.IndexMetadata {
	if view == nil {
		return nil
	}
	return view.GetIndexes()
}

func tags(manual *v1pb.ManualSQLMetadata) []string {
	if manual == nil {
		return nil
	}
	return manual.GetTags()
}

func attributes(manual *v1pb.ManualSQLMetadata) map[string]string {
	if manual == nil {
		return nil
	}
	return manual.GetAttributes()
}

func columnSnapshot(column *v1pb.ColumnMetadata) *v1pb.MetadataHistoryChildSnapshot {
	if column == nil {
		return nil
	}
	return &v1pb.MetadataHistoryChildSnapshot{Metadata: &v1pb.MetadataHistoryChildSnapshot_ColumnMetadata{ColumnMetadata: column}}
}

func indexSnapshot(before, after *v1pb.IndexMetadata) *v1pb.MetadataHistoryChildSnapshot {
	for _, item := range []*v1pb.IndexMetadata{after, before} {
		if item != nil {
			return &v1pb.MetadataHistoryChildSnapshot{Metadata: &v1pb.MetadataHistoryChildSnapshot_IndexMetadata{IndexMetadata: item}}
		}
	}
	return nil
}

func foreignKeySnapshot(before, after *v1pb.ForeignKeyMetadata) *v1pb.MetadataHistoryChildSnapshot {
	for _, item := range []*v1pb.ForeignKeyMetadata{after, before} {
		if item != nil {
			return &v1pb.MetadataHistoryChildSnapshot{Metadata: &v1pb.MetadataHistoryChildSnapshot_ForeignKeyMetadata{ForeignKeyMetadata: item}}
		}
	}
	return nil
}

func checkConstraintSnapshot(before, after *v1pb.CheckConstraintMetadata) *v1pb.MetadataHistoryChildSnapshot {
	for _, item := range []*v1pb.CheckConstraintMetadata{after, before} {
		if item != nil {
			return &v1pb.MetadataHistoryChildSnapshot{Metadata: &v1pb.MetadataHistoryChildSnapshot_CheckConstraintMetadata{CheckConstraintMetadata: item}}
		}
	}
	return nil
}

func partitionSnapshot(before, after *v1pb.TablePartitionMetadata) *v1pb.MetadataHistoryChildSnapshot {
	for _, item := range []*v1pb.TablePartitionMetadata{after, before} {
		if item != nil {
			return &v1pb.MetadataHistoryChildSnapshot{Metadata: &v1pb.MetadataHistoryChildSnapshot_PartitionMetadata{PartitionMetadata: item}}
		}
	}
	return nil
}

func triggerSnapshot(before, after *v1pb.TriggerMetadata) *v1pb.MetadataHistoryChildSnapshot {
	for _, item := range []*v1pb.TriggerMetadata{after, before} {
		if item != nil {
			return &v1pb.MetadataHistoryChildSnapshot{Metadata: &v1pb.MetadataHistoryChildSnapshot_TriggerMetadata{TriggerMetadata: item}}
		}
	}
	return nil
}

func ruleSnapshot(before, after *v1pb.RuleMetadata) *v1pb.MetadataHistoryChildSnapshot {
	for _, item := range []*v1pb.RuleMetadata{after, before} {
		if item != nil {
			return &v1pb.MetadataHistoryChildSnapshot{Metadata: &v1pb.MetadataHistoryChildSnapshot_RuleMetadata{RuleMetadata: item}}
		}
	}
	return nil
}
