package v1

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type metadataHistoryEventContext struct {
	eventTime time.Time
	validFrom time.Time
	validTo   *time.Time
	operation v1pb.MetadataHistoryOperation
	before    *store.MetaRegistryHistory
	after     *store.MetaRegistryHistory
}

func (s *DatabaseService) ListMetadataHistory(ctx context.Context, req *connect.Request[v1pb.ListMetadataHistoryRequest]) (*connect.Response[v1pb.ListMetadataHistoryResponse], error) {
	if strings.TrimSpace(req.Msg.GetGuid()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("guid is required"))
	}
	if req.Msg.GetMetaType() == v1pb.MetaType_UNSPECIFIED {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("meta_type is required"))
	}

	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.GetPageToken(),
		limit:   int(req.Msg.GetPageSize()),
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	guid := req.Msg.GetGuid()
	metaType := storepb.MetaType(req.Msg.GetMetaType())
	history, err := s.store.ListMetaRegistryHistory(ctx, &store.FindMetaRegistryHistoryMessage{
		GUID:       &guid,
		ObjectType: &metaType,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list metadata history"))
	}

	events, err := s.buildMetadataHistoryEvents(ctx, req.Msg.GetGuid(), req.Msg.GetMetaType(), history)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to build metadata history timeline"))
	}

	response := &v1pb.ListMetadataHistoryResponse{}
	if offset.offset < len(events) {
		events = events[offset.offset:]
	} else {
		events = nil
	}
	if len(events) > limitPlusOne {
		response.Entries = events[:offset.limit]
		response.NextPageToken, err = offset.getNextPageToken()
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to marshal next page token"))
		}
	} else {
		response.Entries = events
	}

	return connect.NewResponse(response), nil
}

func (s *DatabaseService) GetMetadataHistoryEvent(ctx context.Context, req *connect.Request[v1pb.GetMetadataHistoryEventRequest]) (*connect.Response[v1pb.MetadataHistoryEvent], error) {
	if strings.TrimSpace(req.Msg.GetGuid()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("guid is required"))
	}
	if req.Msg.GetMetaType() == v1pb.MetaType_UNSPECIFIED {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("meta_type is required"))
	}
	if req.Msg.GetOperation() == v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UNSPECIFIED {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("operation is required"))
	}
	if !req.Msg.GetEventTime().IsValid() {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("event_time is required"))
	}

	guid := req.Msg.GetGuid()
	metaType := storepb.MetaType(req.Msg.GetMetaType())
	history, err := s.store.ListMetaRegistryHistory(ctx, &store.FindMetaRegistryHistoryMessage{
		GUID:       &guid,
		ObjectType: &metaType,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list metadata history"))
	}

	eventTime := req.Msg.GetEventTime().AsTime()
	events := buildMetadataHistoryEventContexts(history)
	for _, event := range events {
		if event.operation != req.Msg.GetOperation() {
			continue
		}
		if !event.eventTime.Equal(eventTime) {
			continue
		}
		result, err := buildMetadataHistoryEventResult(req.Msg.GetGuid(), req.Msg.GetMetaType(), event)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to build metadata history event"))
		}
		return connect.NewResponse(result), nil
	}

	return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("metadata history event for %q not found", req.Msg.GetGuid()))
}

func (s *DatabaseService) buildMetadataHistoryEvents(_ context.Context, guid string, metaType v1pb.MetaType, history []*store.MetaRegistryHistory) ([]*v1pb.MetadataHistoryTimelineEntry, error) {
	contexts := buildMetadataHistoryEventContexts(history)
	entries := make([]*v1pb.MetadataHistoryTimelineEntry, 0, len(contexts))
	for _, event := range contexts {
		result, err := buildMetadataHistoryEventResult(guid, metaType, event)
		if err != nil {
			return nil, err
		}
		entries = append(entries, result.Entry)
	}
	slices.Reverse(entries)
	return entries, nil
}

func buildMetadataHistoryEventContexts(history []*store.MetaRegistryHistory) []metadataHistoryEventContext {
	if len(history) == 0 {
		return nil
	}

	rows := slices.Clone(history)
	slices.SortFunc(rows, func(a, b *store.MetaRegistryHistory) int {
		if a.ValidFrom.Before(b.ValidFrom) {
			return -1
		}
		if a.ValidFrom.After(b.ValidFrom) {
			return 1
		}
		return 0
	})

	events := make([]metadataHistoryEventContext, 0, len(rows)*2)
	for i, row := range rows {
		var previous *store.MetaRegistryHistory
		if i > 0 {
			previous = rows[i-1]
		}
		operation := v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_CREATED
		if previous != nil && previous.ValidTo != nil && previous.ValidTo.Equal(row.ValidFrom) {
			operation = v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED
			if previous.ValidTo != nil && !previous.ValidTo.Equal(row.ValidFrom) {
				operation = v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_CREATED
			}
		}
		events = append(events, metadataHistoryEventContext{
			eventTime: row.ValidFrom,
			validFrom: row.ValidFrom,
			validTo:   row.ValidTo,
			operation: operation,
			before:    eventBeforeHistory(previous, row.ValidFrom),
			after:     row,
		})

		if row.ValidTo == nil {
			continue
		}
		hasSuccessor := i+1 < len(rows) && rows[i+1].ValidFrom.Equal(*row.ValidTo)
		if hasSuccessor {
			continue
		}
		events = append(events, metadataHistoryEventContext{
			eventTime: *row.ValidTo,
			validFrom: row.ValidFrom,
			validTo:   row.ValidTo,
			operation: v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_DELETED,
			before:    row,
			after:     nil,
		})
	}

	return events
}

func eventBeforeHistory(previous *store.MetaRegistryHistory, eventTime time.Time) *store.MetaRegistryHistory {
	if previous == nil || previous.ValidTo == nil {
		return nil
	}
	if previous.ValidTo.Equal(eventTime) {
		return previous
	}
	return nil
}

func buildMetadataHistoryEventResult(guid string, metaType v1pb.MetaType, event metadataHistoryEventContext) (*v1pb.MetadataHistoryEvent, error) {
	beforeMetadata := convertStoredMetadataMessage(historyMetadata(event.before))
	afterMetadata := convertStoredMetadataMessage(historyMetadata(event.after))
	groups := buildMetadataHistoryChangeGroups(metaType, event.before, event.after, event.operation)
	entry := &v1pb.MetadataHistoryTimelineEntry{
		Guid:           guid,
		MetaType:       metaType,
		EventTime:      timestamppb.New(event.eventTime),
		ValidFrom:      timestamppb.New(event.validFrom),
		Operation:      event.operation,
		Summary:        buildMetadataHistorySummary(event.operation, groups),
		SectionChanges: buildMetadataHistorySectionCounts(groups),
	}
	if event.validTo != nil {
		entry.ValidTo = timestamppb.New(*event.validTo)
	}
	return &v1pb.MetadataHistoryEvent{
		Entry:          entry,
		BeforeMetadata: beforeMetadata,
		AfterMetadata:  afterMetadata,
		ChangeGroups:   groups,
	}, nil
}

func historyMetadata(history *store.MetaRegistryHistory) *storepb.StoredMetadata {
	if history == nil {
		return nil
	}
	return history.Metadata
}

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

func metadataName(before, after *v1pb.StoredMetadata) string {
	for _, meta := range []*v1pb.StoredMetadata{after, before} {
		if meta == nil {
			continue
		}
		switch value := meta.Type.(type) {
		case *v1pb.StoredMetadata_TableMetadata:
			return value.TableMetadata.GetName()
		case *v1pb.StoredMetadata_ViewMetadata:
			return value.ViewMetadata.GetName()
		case *v1pb.StoredMetadata_MaterializedViewMetadata:
			return value.MaterializedViewMetadata.GetName()
		case *v1pb.StoredMetadata_ColumnMetadata:
			return value.ColumnMetadata.GetName()
		case *v1pb.StoredMetadata_ManualSqlMetadata:
			if value.ManualSqlMetadata.GetTitle() != "" {
				return value.ManualSqlMetadata.GetTitle()
			}
			return value.ManualSqlMetadata.GetName()
		}
	}
	return ""
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
		}
	}
	if len(items) == 0 {
		return nil
	}
	return &v1pb.MetadataHistoryChangeGroup{Section: section, Changes: items}
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
