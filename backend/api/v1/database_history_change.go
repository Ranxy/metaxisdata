package v1

import (
	"fmt"
	"slices"
	"strings"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func buildMetadataHistoryChangeGroups(metaType v1pb.MetaType, before, after *store.MetaRegistryHistory, operation v1pb.MetadataHistoryOperation) []*v1pb.MetadataHistoryChangeGroup {
	switch metaType {
	case v1pb.MetaType_TABLE:
		return buildTableHistoryChangeGroups(before, after, operation)
	case v1pb.MetaType_COLUMN:
		return buildColumnHistoryChangeGroups(before, after, operation)
	case v1pb.MetaType_VIEW:
		return buildViewHistoryChangeGroups(before, after, operation)
	case v1pb.MetaType_MATERIALIZED_VIEW:
		return buildMaterializedViewHistoryChangeGroups(before, after, operation)
	case v1pb.MetaType_MANUAL_SQL:
		return buildManualSQLHistoryChangeGroups(before, after, operation)
	default:
		return nil
	}
}

func buildTableHistoryChangeGroups(before, after *store.MetaRegistryHistory, operation v1pb.MetadataHistoryOperation) []*v1pb.MetadataHistoryChangeGroup {
	beforeMeta := convertStoredMetadataMessage(historyMetadata(before))
	afterMeta := convertStoredMetadataMessage(historyMetadata(after))
	beforeTable := (*v1pb.TableMetadata)(nil)
	afterTable := (*v1pb.TableMetadata)(nil)
	if beforeMeta != nil {
		beforeTable = beforeMeta.GetTableMetadata()
	}
	if afterMeta != nil {
		afterTable = afterMeta.GetTableMetadata()
	}

	var groups []*v1pb.MetadataHistoryChangeGroup
	if operation == v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED && beforeTable != nil && afterTable != nil {
		if fields := compareTableSelfFields(beforeTable, afterTable); len(fields) > 0 {
			groups = append(groups, &v1pb.MetadataHistoryChangeGroup{
				Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_SELF,
				Changes: []*v1pb.MetadataHistoryChangeItem{newSelfChangeItem(fields)},
			})
		}
	}
	if group := diffColumnGroup(beforeTable, afterTable); group != nil {
		groups = append(groups, group)
	}
	if group := diffIndexGroup(beforeTable, afterTable); group != nil {
		groups = append(groups, group)
	}
	if group := diffForeignKeyGroup(beforeTable, afterTable); group != nil {
		groups = append(groups, group)
	}
	if group := diffCheckConstraintGroup(beforeTable, afterTable); group != nil {
		groups = append(groups, group)
	}
	if group := diffPartitionGroup(beforeTable, afterTable); group != nil {
		groups = append(groups, group)
	}
	return groups
}

func buildColumnHistoryChangeGroups(before, after *store.MetaRegistryHistory, operation v1pb.MetadataHistoryOperation) []*v1pb.MetadataHistoryChangeGroup {
	beforeMeta := convertStoredMetadataMessage(historyMetadata(before))
	afterMeta := convertStoredMetadataMessage(historyMetadata(after))
	beforeColumn := (*v1pb.ColumnMetadata)(nil)
	afterColumn := (*v1pb.ColumnMetadata)(nil)
	if beforeMeta != nil {
		beforeColumn = beforeMeta.GetColumnMetadata()
	}
	if afterMeta != nil {
		afterColumn = afterMeta.GetColumnMetadata()
	}
	if operation != v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED || beforeColumn == nil || afterColumn == nil {
		return nil
	}
	fieldChanges := compareColumnFields(beforeColumn, afterColumn)
	if len(fieldChanges) == 0 {
		return nil
	}
	return []*v1pb.MetadataHistoryChangeGroup{{
		Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_SELF,
		Changes: []*v1pb.MetadataHistoryChangeItem{{
			Section:      v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_SELF,
			Operation:    v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED,
			Key:          metadataName(beforeMeta, afterMeta),
			DisplayName:  metadataName(beforeMeta, afterMeta),
			Summary:      summarizeFieldChanges(fieldChanges),
			FieldChanges: fieldChanges,
		}},
	}}
}

func buildViewHistoryChangeGroups(before, after *store.MetaRegistryHistory, operation v1pb.MetadataHistoryOperation) []*v1pb.MetadataHistoryChangeGroup {
	beforeMeta := convertStoredMetadataMessage(historyMetadata(before))
	afterMeta := convertStoredMetadataMessage(historyMetadata(after))
	beforeView := (*v1pb.ViewMetadata)(nil)
	afterView := (*v1pb.ViewMetadata)(nil)
	if beforeMeta != nil {
		beforeView = beforeMeta.GetViewMetadata()
	}
	if afterMeta != nil {
		afterView = afterMeta.GetViewMetadata()
	}

	var groups []*v1pb.MetadataHistoryChangeGroup
	if operation == v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED && beforeView != nil && afterView != nil {
		if fields := compareViewSelfFields(beforeView, afterView); len(fields) > 0 {
			groups = append(groups, &v1pb.MetadataHistoryChangeGroup{
				Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_SELF,
				Changes: []*v1pb.MetadataHistoryChangeItem{newSelfChangeItem(fields)},
			})
		}
	}
	if group := diffViewColumnGroup(beforeView, afterView); group != nil {
		groups = append(groups, group)
	}
	if group := diffTriggerGroup(viewTriggers(beforeView), viewTriggers(afterView)); group != nil {
		groups = append(groups, group)
	}
	if group := diffRuleGroup(viewRules(beforeView), viewRules(afterView)); group != nil {
		groups = append(groups, group)
	}
	return groups
}

func buildMaterializedViewHistoryChangeGroups(before, after *store.MetaRegistryHistory, operation v1pb.MetadataHistoryOperation) []*v1pb.MetadataHistoryChangeGroup {
	beforeMeta := convertStoredMetadataMessage(historyMetadata(before))
	afterMeta := convertStoredMetadataMessage(historyMetadata(after))
	beforeView := (*v1pb.MaterializedViewMetadata)(nil)
	afterView := (*v1pb.MaterializedViewMetadata)(nil)
	if beforeMeta != nil {
		beforeView = beforeMeta.GetMaterializedViewMetadata()
	}
	if afterMeta != nil {
		afterView = afterMeta.GetMaterializedViewMetadata()
	}

	var groups []*v1pb.MetadataHistoryChangeGroup
	if operation == v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED && beforeView != nil && afterView != nil {
		if fields := compareMaterializedViewSelfFields(beforeView, afterView); len(fields) > 0 {
			groups = append(groups, &v1pb.MetadataHistoryChangeGroup{
				Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_SELF,
				Changes: []*v1pb.MetadataHistoryChangeItem{newSelfChangeItem(fields)},
			})
		}
	}
	if group := diffIndexGroupFromList(indexMetadataList(beforeView), indexMetadataList(afterView)); group != nil {
		groups = append(groups, group)
	}
	if group := diffTriggerGroup(materializedViewTriggers(beforeView), materializedViewTriggers(afterView)); group != nil {
		groups = append(groups, group)
	}
	return groups
}

func buildManualSQLHistoryChangeGroups(before, after *store.MetaRegistryHistory, operation v1pb.MetadataHistoryOperation) []*v1pb.MetadataHistoryChangeGroup {
	beforeMeta := convertStoredMetadataMessage(historyMetadata(before))
	afterMeta := convertStoredMetadataMessage(historyMetadata(after))
	beforeManual := (*v1pb.ManualSQLMetadata)(nil)
	afterManual := (*v1pb.ManualSQLMetadata)(nil)
	if beforeMeta != nil {
		beforeManual = beforeMeta.GetManualSqlMetadata()
	}
	if afterMeta != nil {
		afterManual = afterMeta.GetManualSqlMetadata()
	}

	var groups []*v1pb.MetadataHistoryChangeGroup
	if operation == v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED && beforeManual != nil && afterManual != nil {
		fieldChanges := compareManualSQLFields(beforeManual, afterManual)
		if len(fieldChanges) > 0 {
			groups = append(groups, &v1pb.MetadataHistoryChangeGroup{
				Section: v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_SELF,
				Changes: []*v1pb.MetadataHistoryChangeItem{{
					Section:      v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_SELF,
					Operation:    v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED,
					Key:          metadataName(beforeMeta, afterMeta),
					DisplayName:  metadataName(beforeMeta, afterMeta),
					Summary:      summarizeFieldChanges(fieldChanges),
					FieldChanges: fieldChanges,
				}},
			})
		}
	}
	if group := diffTagGroup(tags(beforeManual), tags(afterManual)); group != nil {
		groups = append(groups, group)
	}
	if group := diffAttributeGroup(attributes(beforeManual), attributes(afterManual)); group != nil {
		groups = append(groups, group)
	}
	return groups
}

func newSelfChangeItem(fields []*v1pb.MetadataFieldChange) *v1pb.MetadataHistoryChangeItem {
	return &v1pb.MetadataHistoryChangeItem{
		Section:      v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_SELF,
		Operation:    v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED,
		Summary:      summarizeFieldChanges(fields),
		FieldChanges: fields,
	}
}

func buildMetadataHistorySummary(operation v1pb.MetadataHistoryOperation, groups []*v1pb.MetadataHistoryChangeGroup) string {
	sectionChanges := buildMetadataHistorySectionCounts(groups)
	parts := make([]string, 0, len(sectionChanges)+1)
	switch operation {
	case v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_CREATED:
		parts = append(parts, "created")
	case v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_DELETED:
		parts = append(parts, "deleted")
	default:
	}
	for _, count := range sectionChanges {
		parts = append(parts, summarizeSectionCount(count))
	}
	if len(parts) == 0 {
		return "updated"
	}
	return strings.Join(parts, ", ")
}

func buildMetadataHistorySectionCounts(groups []*v1pb.MetadataHistoryChangeGroup) []*v1pb.MetadataHistorySectionChangeCount {
	bySection := make(map[v1pb.MetadataHistorySection]*v1pb.MetadataHistorySectionChangeCount, len(groups))
	for _, group := range groups {
		count, ok := bySection[group.Section]
		if !ok {
			count = &v1pb.MetadataHistorySectionChangeCount{Section: group.Section}
			bySection[group.Section] = count
		}
		for _, item := range group.Changes {
			switch item.Operation {
			case v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_CREATED:
				count.Added++
			case v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED:
				count.Updated++
			case v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_DELETED:
				count.Removed++
			default:
			}
		}
	}
	counts := make([]*v1pb.MetadataHistorySectionChangeCount, 0, len(bySection))
	for _, section := range metadataHistorySectionOrder() {
		count, ok := bySection[section]
		if !ok {
			continue
		}
		if count.Added == 0 && count.Updated == 0 && count.Removed == 0 {
			continue
		}
		counts = append(counts, count)
	}
	return counts
}

func metadataHistorySectionOrder() []v1pb.MetadataHistorySection {
	return []v1pb.MetadataHistorySection{
		v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_COLUMN,
		v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_INDEX,
		v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_FOREIGN_KEY,
		v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_CHECK_CONSTRAINT,
		v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_PARTITION,
		v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_TRIGGER,
		v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_RULE,
		v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_TAG,
		v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_ATTRIBUTE,
		v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_SELF,
	}
}

func summarizeSectionCount(count *v1pb.MetadataHistorySectionChangeCount) string {
	name := sectionLabel(count.Section)
	parts := []string{}
	if count.Added > 0 {
		parts = append(parts, fmt.Sprintf("+%d %s", count.Added, pluralize(name, count.Added)))
	}
	if count.Updated > 0 {
		parts = append(parts, fmt.Sprintf("~%d %s", count.Updated, pluralize(name, count.Updated)))
	}
	if count.Removed > 0 {
		parts = append(parts, fmt.Sprintf("-%d %s", count.Removed, pluralize(name, count.Removed)))
	}
	return strings.Join(parts, " ")
}

func sectionLabel(section v1pb.MetadataHistorySection) string {
	switch section {
	case v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_SELF:
		return "property"
	case v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_COLUMN:
		return "column"
	case v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_INDEX:
		return "index"
	case v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_FOREIGN_KEY:
		return "foreign key"
	case v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_CHECK_CONSTRAINT:
		return "check constraint"
	case v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_PARTITION:
		return "partition"
	case v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_TRIGGER:
		return "trigger"
	case v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_RULE:
		return "rule"
	case v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_TAG:
		return "tag"
	case v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_ATTRIBUTE:
		return "attribute"
	default:
		return "change"
	}
}

func pluralize(label string, count int32) string {
	if count == 1 {
		return label
	}
	if strings.HasSuffix(label, "y") {
		return strings.TrimSuffix(label, "y") + "ies"
	}
	return label + "s"
}

func summarizeFieldChanges(fields []*v1pb.MetadataFieldChange) string {
	if len(fields) == 0 {
		return "updated"
	}
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.DisplayName)
	}
	return strings.Join(names, ", ") + " changed"
}

func compareTableSelfFields(before, after *v1pb.TableMetadata) []*v1pb.MetadataFieldChange {
	changes := []*v1pb.MetadataFieldChange{}
	appendStringFieldChange(&changes, "comment", "comment", before.GetComment(), after.GetComment())
	appendStringFieldChange(&changes, "user_comment", "user comment", before.GetUserComment(), after.GetUserComment())
	appendStringFieldChange(&changes, "engine", "engine", before.GetEngine(), after.GetEngine())
	appendStringFieldChange(&changes, "charset", "character set", before.GetCharset(), after.GetCharset())
	appendStringFieldChange(&changes, "collation", "collation", before.GetCollation(), after.GetCollation())
	appendStringFieldChange(&changes, "owner", "owner", before.GetOwner(), after.GetOwner())
	appendStringFieldChange(&changes, "create_options", "create options", before.GetCreateOptions(), after.GetCreateOptions())
	appendStringFieldChange(&changes, "primary_key_type", "primary key type", before.GetPrimaryKeyType(), after.GetPrimaryKeyType())
	appendStringFieldChange(&changes, "sharding_info", "sharding info", before.GetShardingInfo(), after.GetShardingInfo())
	return changes
}

func compareViewSelfFields(before, after *v1pb.ViewMetadata) []*v1pb.MetadataFieldChange {
	changes := []*v1pb.MetadataFieldChange{}
	appendStringFieldChange(&changes, "definition", "definition", before.GetDefinition(), after.GetDefinition())
	appendStringFieldChange(&changes, "comment", "comment", before.GetComment(), after.GetComment())
	return changes
}

func compareMaterializedViewSelfFields(before, after *v1pb.MaterializedViewMetadata) []*v1pb.MetadataFieldChange {
	changes := []*v1pb.MetadataFieldChange{}
	appendStringFieldChange(&changes, "definition", "definition", before.GetDefinition(), after.GetDefinition())
	appendStringFieldChange(&changes, "comment", "comment", before.GetComment(), after.GetComment())
	return changes
}

func compareManualSQLFields(before, after *v1pb.ManualSQLMetadata) []*v1pb.MetadataFieldChange {
	changes := []*v1pb.MetadataFieldChange{}
	appendStringFieldChange(&changes, "title", "title", before.GetTitle(), after.GetTitle())
	appendStringFieldChange(&changes, "schema_name", "schema", before.GetSchemaName(), after.GetSchemaName())
	appendStringFieldChange(&changes, "comment", "comment", before.GetComment(), after.GetComment())
	appendStringFieldChange(&changes, "sql_text", "SQL text", before.GetSqlText(), after.GetSqlText())
	return changes
}

func compareColumnFields(before, after *v1pb.ColumnMetadata) []*v1pb.MetadataFieldChange {
	changes := []*v1pb.MetadataFieldChange{}
	appendInt32FieldChange(&changes, "position", "position", before.GetPosition(), after.GetPosition())
	appendStringFieldChange(&changes, "type", "type", before.GetType(), after.GetType())
	appendBoolFieldChange(&changes, "nullable", "nullable", before.GetNullable(), after.GetNullable())
	appendStringFieldChange(&changes, "default", "default", before.GetDefault(), after.GetDefault())
	appendStringFieldChange(&changes, "comment", "comment", before.GetComment(), after.GetComment())
	appendStringFieldChange(&changes, "user_comment", "user comment", before.GetUserComment(), after.GetUserComment())
	appendStringFieldChange(&changes, "character_set", "character set", before.GetCharacterSet(), after.GetCharacterSet())
	appendStringFieldChange(&changes, "collation", "collation", before.GetCollation(), after.GetCollation())
	appendStringFieldChange(&changes, "on_update", "on update", before.GetOnUpdate(), after.GetOnUpdate())
	appendBoolFieldChange(&changes, "default_on_null", "default on null", before.GetDefaultOnNull(), after.GetDefaultOnNull())
	appendBoolFieldChange(&changes, "is_identity", "identity", before.GetIsIdentity(), after.GetIsIdentity())
	appendStringFieldChange(&changes, "identity_generation", "identity generation", before.GetIdentityGeneration().String(), after.GetIdentityGeneration().String())
	return changes
}

func appendStringFieldChange(changes *[]*v1pb.MetadataFieldChange, field, displayName, before, after string) {
	if before == after {
		return
	}
	*changes = append(*changes, &v1pb.MetadataFieldChange{Field: field, DisplayName: displayName, Before: before, After: after})
}

func appendBoolFieldChange(changes *[]*v1pb.MetadataFieldChange, field, displayName string, before, after bool) {
	if before == after {
		return
	}
	*changes = append(*changes, &v1pb.MetadataFieldChange{Field: field, DisplayName: displayName, Before: fmt.Sprintf("%t", before), After: fmt.Sprintf("%t", after)})
}

func appendInt32FieldChange(changes *[]*v1pb.MetadataFieldChange, field, displayName string, before, after int32) {
	if before == after {
		return
	}
	*changes = append(*changes, &v1pb.MetadataFieldChange{Field: field, DisplayName: displayName, Before: fmt.Sprintf("%d", before), After: fmt.Sprintf("%d", after)})
}

func newChildLifecycleItem(section v1pb.MetadataHistorySection, operation v1pb.MetadataHistoryOperation, key string, snapshot *v1pb.MetadataHistoryChildSnapshot) *v1pb.MetadataHistoryChangeItem {
	item := &v1pb.MetadataHistoryChangeItem{Section: section, Operation: operation, Key: key, DisplayName: key}
	switch operation {
	case v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_CREATED:
		item.Summary = "created"
		item.After = snapshot
	case v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_DELETED:
		item.Summary = "deleted"
		item.Before = snapshot
	default:
	}
	return item
}

func collectOrderedKeys(keys map[string]struct{}) []string {
	orderedKeys := make([]string, 0, len(keys))
	for key := range keys {
		orderedKeys = append(orderedKeys, key)
	}
	slices.Sort(orderedKeys)
	return orderedKeys
}
