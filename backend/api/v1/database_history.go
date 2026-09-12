package v1

import (
	"context"
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
	guid := req.Msg.GetGuid()
	metaType := storepb.MetaType(req.Msg.GetMetaType())
	history, err := s.store.ListMetaRegistryHistory(ctx, &store.FindMetaRegistryHistoryMessage{
		GUID:       &guid,
		ObjectType: &metaType,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list metadata history"))
	}

	events := s.buildMetadataHistoryEvents(ctx, req.Msg.GetGuid(), req.Msg.GetMetaType(), history)

	if offset.offset < len(events) {
		events = events[offset.offset:]
	} else {
		events = nil
	}
	entries, nextPageToken, err := paginate(events, offset)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&v1pb.ListMetadataHistoryResponse{
		Entries:       entries,
		NextPageToken: nextPageToken,
	}), nil
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
		result := buildMetadataHistoryEventResult(req.Msg.GetGuid(), req.Msg.GetMetaType(), event)
		return connect.NewResponse(result), nil
	}

	return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("metadata history event for %q not found", req.Msg.GetGuid()))
}

func (*DatabaseService) buildMetadataHistoryEvents(_ context.Context, guid string, metaType v1pb.MetaType, history []*store.MetaRegistryHistory) []*v1pb.MetadataHistoryTimelineEntry {
	contexts := buildMetadataHistoryEventContexts(history)
	entries := make([]*v1pb.MetadataHistoryTimelineEntry, 0, len(contexts))
	for _, event := range contexts {
		result := buildMetadataHistoryEventResult(guid, metaType, event)
		entries = append(entries, result.Entry)
	}
	slices.Reverse(entries)
	return entries
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

func buildMetadataHistoryEventResult(guid string, metaType v1pb.MetaType, event metadataHistoryEventContext) *v1pb.MetadataHistoryEvent {
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
	}
}

func historyMetadata(history *store.MetaRegistryHistory) *storepb.StoredMetadata {
	if history == nil {
		return nil
	}
	return history.Metadata
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
