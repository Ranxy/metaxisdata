package v1

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// maxNotificationsPerBatch bounds one mark-read request. The request body limit alone
// allows tens of thousands of names, and every one of them becomes an id in the
// update's array; a page of an inbox is at most a thousand, like the other batches
// this API accepts.
const maxNotificationsPerBatch = 1000

// NotificationService serves the caller's own in-app messages: the outcome of a
// sync operation they asked for, and the ingestion failures a workspace
// administrator needs to see.
//
// No method carries a permission annotation, and none is reachable without
// credentials. An inbox belongs to one principal, which the workspace-scoped
// permission catalog cannot express, so every query here is scoped by the
// authenticated caller's id — the same shape UserService's self-service paths
// use. Gating these methods on a permission instead would lock a custom role out
// of its own messages.
type NotificationService struct {
	v1connect.UnimplementedNotificationServiceHandler
	store *store.Store
}

// NewNotificationService returns a notification service.
func NewNotificationService(store *store.Store) *NotificationService {
	return &NotificationService{store: store}
}

// ListNotifications lists the caller's notifications, newest first.
func (s *NotificationService) ListNotifications(ctx context.Context, req *connect.Request[v1pb.ListNotificationsRequest]) (*connect.Response[v1pb.ListNotificationsResponse], error) {
	user, err := notificationCaller(ctx)
	if err != nil {
		return nil, err
	}
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}

	offset, err := parseLimitAndOffset(&pageSize{
		token: req.Msg.GetPageToken(),
		limit: int(req.Msg.GetPageSize()),
		// One page of an inbox is small; the cap keeps a single read from
		// materializing a year of messages.
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	notifications, err := s.store.ListNotifications(ctx, &store.FindNotificationMessage{
		RecipientID: &user.ID,
		UnreadOnly:  req.Msg.GetUnreadOnly(),
		Limit:       &limitPlusOne,
		Offset:      &offset.offset,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list notifications"))
	}

	notifications, nextPageToken, err := paginate(notifications, offset)
	if err != nil {
		return nil, err
	}

	response := &v1pb.ListNotificationsResponse{NextPageToken: nextPageToken}
	for _, notification := range notifications {
		response.Notifications = append(response.Notifications, convertToV1Notification(notification, workspaceID))
	}
	return connect.NewResponse(response), nil
}

// GetUnreadNotificationCount counts the caller's unread notifications. The
// notification bell polls this, so it answers from a partial index rather than
// reading the messages themselves.
func (s *NotificationService) GetUnreadNotificationCount(ctx context.Context, req *connect.Request[v1pb.GetUnreadNotificationCountRequest]) (*connect.Response[v1pb.GetUnreadNotificationCountResponse], error) {
	user, err := notificationCaller(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}

	count, err := s.store.CountUnreadNotifications(ctx, user.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to count unread notifications"))
	}
	return connect.NewResponse(&v1pb.GetUnreadNotificationCountResponse{UnreadCount: int32(count)}), nil
}

// BatchMarkNotificationsRead marks the named notifications read. A name that
// belongs to somebody else changes nothing: the update is scoped by recipient.
func (s *NotificationService) BatchMarkNotificationsRead(ctx context.Context, req *connect.Request[v1pb.BatchMarkNotificationsReadRequest]) (*connect.Response[emptypb.Empty], error) {
	user, err := notificationCaller(ctx)
	if err != nil {
		return nil, err
	}
	// The request's own shape is checked before the workspace is resolved: a malformed
	// batch is refused without a query, and the refusal reads the same whether or not
	// the workspace happens to exist.
	if len(req.Msg.GetNames()) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("names must not be empty"))
	}
	if len(req.Msg.GetNames()) > maxNotificationsPerBatch {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("a batch can contain at most %d notifications", maxNotificationsPerBatch))
	}
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(req.Msg.GetNames()))
	for _, name := range req.Msg.GetNames() {
		id, err := common.GetNotificationID(name)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid notification name %q", name))
		}
		ids = append(ids, id)
	}
	if _, err := s.store.MarkNotificationsRead(ctx, user.ID, ids); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to mark notifications read"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// MarkAllNotificationsRead marks every unread notification of the caller read.
func (s *NotificationService) MarkAllNotificationsRead(ctx context.Context, req *connect.Request[v1pb.MarkAllNotificationsReadRequest]) (*connect.Response[emptypb.Empty], error) {
	user, err := notificationCaller(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}

	if _, err := s.store.MarkAllNotificationsRead(ctx, user.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to mark all notifications read"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// DeleteNotification deletes one of the caller's notifications.
func (s *NotificationService) DeleteNotification(ctx context.Context, req *connect.Request[v1pb.DeleteNotificationRequest]) (*connect.Response[emptypb.Empty], error) {
	user, err := notificationCaller(ctx)
	if err != nil {
		return nil, err
	}

	id, err := common.GetNotificationID(req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid notification name %q", req.Msg.GetName()))
	}
	deleted, err := s.store.DeleteNotification(ctx, user.ID, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to delete notification"))
	}
	if !deleted {
		// The row is either gone or somebody else's, and the caller is not told
		// which: an inbox must not become a probe for other users' message ids.
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("notification %q not found", req.Msg.GetName()))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// notificationCaller returns the authenticated caller. Every RPC of this
// service starts here, because the caller's id is the scope.
func notificationCaller(ctx context.Context) (*store.UserMessage, error) {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	return user, nil
}

// workspaceID resolves the workspace a request names. The workspace is single
// tenant, so the only values that resolve are the current one and the
// "workspaces/-" shorthand the SPA sends; anything else is a stale or invented
// name and is refused rather than silently answered with the caller's own
// messages under a name that is not theirs.
func (s *NotificationService) workspaceID(ctx context.Context, parent string) (string, error) {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("parent is required"))
	}

	workspaceID, err := s.store.GetWorkspaceID(ctx)
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve the workspace"))
	}
	current := common.FormatWorkspace(workspaceID)
	if parent != "workspaces/-" && parent != current {
		return "", connect.NewError(connect.CodeNotFound, errors.Errorf("workspace %q not found", parent))
	}
	return workspaceID, nil
}

func convertToV1Notification(notification *storepb.Notification, workspaceID string) *v1pb.Notification {
	if notification == nil {
		return nil
	}
	converted := &v1pb.Notification{
		Name:       common.FormatNotification(workspaceID, notification.GetId()),
		CreateTime: notification.GetCreateTime(),
		Type:       convertNotificationType(notification.GetType()),
		Severity:   convertNotificationSeverity(notification.GetSeverity()),
		ReadTime:   notification.GetReadTime(),
	}
	switch {
	case notification.GetSchemaSync() != nil:
		converted.Detail = &v1pb.Notification_SchemaSync{
			SchemaSync: convertSchemaSyncDetail(notification.GetSchemaSync()),
		}
	case notification.GetOpenlineage() != nil:
		converted.Detail = &v1pb.Notification_Openlineage{
			Openlineage: convertOpenLineageDetail(notification.GetOpenlineage()),
		}
	default:
		// A detail written by a newer server is not in this build's catalog; the
		// envelope still renders, and the client falls back to a generic line.
	}
	return converted
}

func convertNotificationType(notificationType storepb.NotificationType) v1pb.NotificationType {
	switch notificationType {
	case storepb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC:
		return v1pb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC
	case storepb.NotificationType_NOTIFICATION_TYPE_OPENLINEAGE:
		return v1pb.NotificationType_NOTIFICATION_TYPE_OPENLINEAGE
	case storepb.NotificationType_NOTIFICATION_TYPE_UNSPECIFIED:
		return v1pb.NotificationType_NOTIFICATION_TYPE_UNSPECIFIED
	default:
		return v1pb.NotificationType_NOTIFICATION_TYPE_UNSPECIFIED
	}
}

func convertNotificationSeverity(severity storepb.NotificationSeverity) v1pb.NotificationSeverity {
	switch severity {
	case storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO:
		return v1pb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO
	case storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING:
		return v1pb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING
	case storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR:
		return v1pb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR
	case storepb.NotificationSeverity_NOTIFICATION_SEVERITY_UNSPECIFIED:
		return v1pb.NotificationSeverity_NOTIFICATION_SEVERITY_UNSPECIFIED
	default:
		return v1pb.NotificationSeverity_NOTIFICATION_SEVERITY_UNSPECIFIED
	}
}

func convertSchemaSyncDetail(detail *storepb.SchemaSyncDetail) *v1pb.SchemaSyncDetail {
	if detail == nil {
		return nil
	}
	converted := &v1pb.SchemaSyncDetail{
		Instance:        detail.GetInstance(),
		InstanceTitle:   detail.GetInstanceTitle(),
		Trigger:         convertSyncTrigger(detail.GetTrigger()),
		InstanceError:   detail.GetInstanceError(),
		SucceededCount:  detail.GetSucceededCount(),
		FailedCount:     detail.GetFailedCount(),
		UnfinishedCount: detail.GetUnfinishedCount(),
	}
	for _, database := range detail.GetDatabases() {
		converted.Databases = append(converted.Databases, &v1pb.SyncDatabaseResult{
			Database: database.GetDatabase(),
			State:    convertSyncDatabaseState(database.GetState()),
			Error:    database.GetError(),
		})
	}
	return converted
}

func convertSyncTrigger(trigger storepb.SyncTrigger) v1pb.SyncTrigger {
	switch trigger {
	case storepb.SyncTrigger_SYNC_TRIGGER_MANUAL:
		return v1pb.SyncTrigger_SYNC_TRIGGER_MANUAL
	case storepb.SyncTrigger_SYNC_TRIGGER_BACKGROUND:
		return v1pb.SyncTrigger_SYNC_TRIGGER_BACKGROUND
	case storepb.SyncTrigger_SYNC_TRIGGER_UNSPECIFIED:
		return v1pb.SyncTrigger_SYNC_TRIGGER_UNSPECIFIED
	default:
		return v1pb.SyncTrigger_SYNC_TRIGGER_UNSPECIFIED
	}
}

func convertSyncDatabaseState(state storepb.SyncDatabaseState) v1pb.SyncDatabaseState {
	switch state {
	case storepb.SyncDatabaseState_SYNC_DATABASE_STATE_SUCCEEDED:
		return v1pb.SyncDatabaseState_SYNC_DATABASE_STATE_SUCCEEDED
	case storepb.SyncDatabaseState_SYNC_DATABASE_STATE_FAILED:
		return v1pb.SyncDatabaseState_SYNC_DATABASE_STATE_FAILED
	case storepb.SyncDatabaseState_SYNC_DATABASE_STATE_UNFINISHED:
		return v1pb.SyncDatabaseState_SYNC_DATABASE_STATE_UNFINISHED
	case storepb.SyncDatabaseState_SYNC_DATABASE_STATE_UNSPECIFIED:
		return v1pb.SyncDatabaseState_SYNC_DATABASE_STATE_UNSPECIFIED
	default:
		return v1pb.SyncDatabaseState_SYNC_DATABASE_STATE_UNSPECIFIED
	}
}

func convertOpenLineageDetail(detail *storepb.OpenLineageDetail) *v1pb.OpenLineageDetail {
	if detail == nil {
		return nil
	}
	return &v1pb.OpenLineageDetail{
		Kind:          convertOpenLineageFailureKind(detail.GetKind()),
		Namespace:     detail.GetNamespace(),
		Job:           detail.GetJob(),
		RunId:         detail.GetRunId(),
		Dataset:       detail.GetDataset(),
		ApiKey:        detail.GetApiKey(),
		ReceivedCount: detail.GetReceivedCount(),
		FailedCount:   detail.GetFailedCount(),
		Error:         detail.GetError(),
	}
}

func convertOpenLineageFailureKind(kind storepb.OpenLineageFailureKind) v1pb.OpenLineageFailureKind {
	switch kind {
	case storepb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_INVALID_EVENT:
		return v1pb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_INVALID_EVENT
	case storepb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_LIMIT_EXCEEDED:
		return v1pb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_LIMIT_EXCEEDED
	case storepb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_SCOPE_MISMATCH:
		return v1pb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_SCOPE_MISMATCH
	case storepb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_PERSIST_FAILED:
		return v1pb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_PERSIST_FAILED
	case storepb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_PROCESS_FAILED:
		return v1pb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_PROCESS_FAILED
	case storepb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_NAMESPACE_UNMAPPED:
		return v1pb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_NAMESPACE_UNMAPPED
	case storepb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_UNSPECIFIED:
		return v1pb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_UNSPECIFIED
	default:
		return v1pb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_UNSPECIFIED
	}
}
