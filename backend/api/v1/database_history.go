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
	entries, nextPageToken, err := s.listMetadataHistoryPage(ctx, guid, req.Msg.GetMetaType(), offset)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&v1pb.ListMetadataHistoryResponse{
		Entries:       entries,
		NextPageToken: nextPageToken,
	}), nil
}

// metadataHistoryProbeSize is how many history rows a page request reads: the
// rows the page can draw on, plus one row of context for the oldest of them.
// Each row yields at least one event, so this many rows always cover the page.
func metadataHistoryProbeSize(offset *pageOffset) int {
	return offset.offset + offset.limit + 2
}

// listMetadataHistoryPage reads only the history a page needs. The rows arrive
// newest-first, and one event can be described by two adjacent rows, so the
// oldest row of a full probe is context only: it tells the row after it whether
// it replaced it, and contributes no entry of its own.
func (s *DatabaseService) listMetadataHistoryPage(ctx context.Context, guid string, metaType v1pb.MetaType, offset *pageOffset) ([]*v1pb.MetadataHistoryTimelineEntry, string, error) {
	objectType := storepb.MetaType(metaType)
	probe := metadataHistoryProbeSize(offset)
	history, err := s.store.ListMetaRegistryHistory(ctx, &store.FindMetaRegistryHistoryMessage{
		GUID:       &guid,
		ObjectType: &objectType,
		Limit:      &probe,
		OrderDesc:  true,
	})
	if err != nil {
		return nil, "", connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list metadata history"))
	}

	events, nextPageToken, err := metadataHistoryPage(history, offset)
	if err != nil {
		return nil, "", err
	}

	entries := make([]*v1pb.MetadataHistoryTimelineEntry, 0, len(events))
	for _, event := range events {
		entries = append(entries, buildMetadataHistoryEventResult(guid, metaType, event).Entry)
	}
	return entries, nextPageToken, nil
}

// metadataHistoryPage turns the newest-first probe into the requested page of
// events. The oldest row of a full probe is context only, so the events of a
// page can be derived from a bounded number of rows instead of the whole
// history.
func metadataHistoryPage(probeRows []*store.MetaRegistryHistory, offset *pageOffset) ([]metadataHistoryEventContext, string, error) {
	rows := slices.Clone(probeRows)
	slices.Reverse(rows)
	contextOnly := len(rows) == metadataHistoryProbeSize(offset)

	events := buildMetadataHistoryEventContexts(rows, contextOnly)
	slices.Reverse(events)

	if offset.offset < len(events) {
		events = events[offset.offset:]
	} else {
		events = nil
	}
	page, nextPageToken, err := paginate(events, offset)
	if err != nil {
		return nil, "", err
	}
	return page, nextPageToken, nil
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
	eventTime := req.Msg.GetEventTime().AsTime()
	// One event is described by the rows that start or end at its time: the row
	// that opened it, the row it replaced, and the row that followed a deletion.
	// Reading the whole history to find them is what this used to do.
	history, err := s.store.ListMetaRegistryHistory(ctx, &store.FindMetaRegistryHistoryMessage{
		GUID:           &guid,
		ObjectType:     &metaType,
		TransitionTime: &eventTime,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list metadata history"))
	}

	events := buildMetadataHistoryEventContexts(history, false)
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

// buildMetadataHistoryEventContexts pairs each history row with the event it
// records. skipOldest drops the events of the first row, which is fetched only
// to tell the row after it whether it replaced it.
func buildMetadataHistoryEventContexts(history []*store.MetaRegistryHistory, skipOldest bool) []metadataHistoryEventContext {
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
		if !skipOldest || i > 0 {
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
		}

		if row.ValidTo == nil {
			continue
		}
		hasSuccessor := i+1 < len(rows) && rows[i+1].ValidFrom.Equal(*row.ValidTo)
		if hasSuccessor || (skipOldest && i == 0) {
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
