package v1

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/notification"
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

const (
	// notificationHeartbeat is how often an idle subscription is poked. It has to
	// stay under a proxy's read timeout — nginx defaults to sixty seconds — and it is
	// also how a peer that is gone is noticed: the write fails and the handler
	// returns instead of holding a goroutine for a connection nobody is reading.
	notificationHeartbeat = 25 * time.Second
	// notificationStreamLifetime bounds one subscription. A stream is authenticated
	// once, when it is opened, so it ends itself well before a session decision could
	// go stale: the client reconnects, which is what re-authenticates it and what
	// makes a logout or a revocation take effect. Ending the stream is not an error —
	// the client opens the next one.
	notificationStreamLifetime = 30 * time.Minute
)

// NotificationSubscriptions is the push side of the notification component: what
// SubscribeNotifications listens to while a caller's browser is connected.
// *notification.Service is the production implementation; the interface keeps this
// handler from depending on how a message is fanned out, and lets a test drive it
// without a database.
type NotificationSubscriptions interface {
	Subscribe(recipientID int) (<-chan notification.Event, func(), error)
}

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
	store         *store.Store
	subscriptions NotificationSubscriptions
}

// NewNotificationService returns a notification service. subscriptions is what
// carries a written message to a connected caller; a service built without one
// refuses SubscribeNotifications rather than accepting a stream that would never
// deliver anything.
func NewNotificationService(store *store.Store, subscriptions NotificationSubscriptions) *NotificationService {
	return &NotificationService{store: store, subscriptions: subscriptions}
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

// GetUnreadNotificationCount counts the caller's unread notifications and the subset
// of them written since the caller last opened the inbox. The stream carries both with
// every message it delivers, so this is their fallback: it answers from a partial index
// rather than reading the messages themselves.
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
	unseen, err := s.store.CountUnseenNotifications(ctx, user.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to count unseen notifications"))
	}
	return connect.NewResponse(&v1pb.GetUnreadNotificationCountResponse{
		UnreadCount: int32(count),
		UnseenCount: int32(unseen),
	}), nil
}

// MarkNotificationsSeen records that the caller has opened the inbox, which clears the
// badge every connected tab of that inbox shows. It is not a mark-read call: the messages
// stay unread, and each one is read when it is clicked. Calling it twice is harmless —
// the second call only moves the watermark further forward.
func (s *NotificationService) MarkNotificationsSeen(ctx context.Context, req *connect.Request[v1pb.MarkNotificationsSeenRequest]) (*connect.Response[emptypb.Empty], error) {
	user, err := notificationCaller(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}

	if err := s.store.MarkNotificationsSeen(ctx, user.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to mark notifications seen"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
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

// SubscribeNotifications streams the caller's notifications as they are written.
//
// The stream is the replacement for polling: a client holds one connection and is
// told about a message when it exists, so a background sync's outcome reaches the
// user without a page reload. It is scoped like every other method here — the
// subscription is registered for the authenticated caller's own inbox.
//
// Nothing about it is audited: a connection that lasts half an hour is not an
// administrative action, and a ledger row per connection would be noise in a table
// that is kept forever.
func (s *NotificationService) SubscribeNotifications(ctx context.Context, req *connect.Request[v1pb.SubscribeNotificationsRequest], stream *connect.ServerStream[v1pb.SubscribeNotificationsResponse]) error {
	user, err := notificationCaller(ctx)
	if err != nil {
		return err
	}
	// A server built without a push channel refuses the stream before it reads anything:
	// accepting one that can never deliver would leave the client waiting on a silent
	// connection, and checking it here keeps the refusal independent of the store.
	if s.subscriptions == nil {
		return connect.NewError(connect.CodeUnimplemented, errors.New("live notifications are not configured"))
	}
	// Resolved once per connection: every message this stream carries is named under
	// the same workspace, and the name is the one field the write path does not have.
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return err
	}
	events, unsubscribe, err := s.subscriptions.Subscribe(user.ID)
	if err != nil {
		return connect.NewError(connect.CodeResourceExhausted, err)
	}
	defer unsubscribe()

	// nginx buffers a proxied response body until a buffer fills, which would hold
	// every event in the proxy. This is the per-response switch it honours, and it
	// has to be set before the first Send: connect-go emits the headers with it.
	stream.ResponseHeader().Set("X-Accel-Buffering", "no")

	heartbeat := time.NewTicker(notificationHeartbeat)
	defer heartbeat.Stop()
	lifetime := time.After(notificationStreamLifetime)

	for {
		select {
		case <-ctx.Done():
			// The caller closed the connection or it dropped.
			return nil
		case <-lifetime:
			// See notificationStreamLifetime: ending the stream is how the client
			// re-authenticates.
			return nil
		case event, ok := <-events:
			if !ok {
				// The subscription was dropped because it fell behind, or the server
				// is draining. The client reconnects and refreshes either way.
				return nil
			}
			if err := stream.Send(convertToV1NotificationEvent(event, workspaceID)); err != nil {
				return err
			}
		case <-heartbeat.C:
			if err := stream.Send(&v1pb.SubscribeNotificationsResponse{
				Event: &v1pb.SubscribeNotificationsResponse_KeepAlive{KeepAlive: &v1pb.KeepAlive{}},
			}); err != nil {
				return err
			}
		}
	}
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

// convertToV1NotificationEvent builds one streamed event. An unknown detail is left
// to convertToV1Notification, which degrades it to the envelope: a client older than
// the server still renders the message instead of choking on it.
func convertToV1NotificationEvent(event notification.Event, workspaceID string) *v1pb.SubscribeNotificationsResponse {
	converted := &v1pb.NotificationEvent{
		Notification: convertToV1Notification(event.Notification, workspaceID),
	}
	if event.UnreadCountKnown {
		// Absent when the count could not be read, so the client asks for it rather
		// than showing the zero this would otherwise be indistinguishable from.
		converted.UnreadCount = proto.Int32(event.UnreadCount)
	}
	if event.UnseenCountKnown {
		// The number the badge shows: unread messages the recipient has not seen
		// since opening the inbox. Absent under the same condition as unread_count.
		converted.UnseenCount = proto.Int32(event.UnseenCount)
	}
	return &v1pb.SubscribeNotificationsResponse{
		Event: &v1pb.SubscribeNotificationsResponse_Notification{Notification: converted},
	}
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
