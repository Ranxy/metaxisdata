package v1

import (
	"context"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/notification"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestNotificationCallerRequiresAuthentication(t *testing.T) {
	t.Parallel()

	// An inbox has no workspace-level permission to check, so authentication is
	// the only thing standing between a caller and other people's messages.
	_, err := notificationCaller(context.Background())
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	require.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}

func TestConvertToV1NotificationNamesAndMapsTheEnvelope(t *testing.T) {
	t.Parallel()

	notification := &storepb.Notification{
		Id:          7,
		Parent:      "workspaces/ws",
		RecipientId: 42,
		Severity:    storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR,
		Type:        storepb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC,
		Detail: &storepb.Notification_SchemaSync{SchemaSync: &storepb.SchemaSyncDetail{
			Instance:       "instances/inst1",
			InstanceTitle:  "prod",
			Trigger:        storepb.SyncTrigger_SYNC_TRIGGER_MANUAL,
			InstanceError:  "boom",
			SucceededCount: 3,
			FailedCount:    1,
			Databases: []*storepb.SyncDatabaseResult{
				{
					Database: "databases/inst1/app",
					State:    storepb.SyncDatabaseState_SYNC_DATABASE_STATE_FAILED,
					Error:    "permission denied",
				},
				{
					Database: "databases/inst1/audit",
					State:    storepb.SyncDatabaseState_SYNC_DATABASE_STATE_UNFINISHED,
				},
			},
		}},
	}

	converted := convertToV1Notification(notification, "ws")
	require.Equal(t, "workspaces/ws/notifications/7", converted.GetName())
	require.Equal(t, v1pb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC, converted.GetType())
	require.Equal(t, v1pb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR, converted.GetSeverity())
	require.Nil(t, converted.GetReadTime())
	require.Equal(t, "instances/inst1", converted.GetSchemaSync().GetInstance())
	require.Equal(t, "prod", converted.GetSchemaSync().GetInstanceTitle())
	require.Equal(t, v1pb.SyncTrigger_SYNC_TRIGGER_MANUAL, converted.GetSchemaSync().GetTrigger())
	require.Equal(t, "boom", converted.GetSchemaSync().GetInstanceError())
	require.Equal(t, int32(3), converted.GetSchemaSync().GetSucceededCount())
	require.Len(t, converted.GetSchemaSync().GetDatabases(), 2)
	require.Equal(t, v1pb.SyncDatabaseState_SYNC_DATABASE_STATE_FAILED, converted.GetSchemaSync().GetDatabases()[0].GetState())
	require.Equal(t, "permission denied", converted.GetSchemaSync().GetDatabases()[0].GetError())
	require.Equal(t, v1pb.SyncDatabaseState_SYNC_DATABASE_STATE_UNFINISHED, converted.GetSchemaSync().GetDatabases()[1].GetState())
}

func TestConvertToV1NotificationMapsAnOpenLineageDetail(t *testing.T) {
	t.Parallel()

	converted := convertToV1Notification(&storepb.Notification{
		Id:     8,
		Parent: "workspaces/ws",
		Type:   storepb.NotificationType_NOTIFICATION_TYPE_OPENLINEAGE,
		Detail: &storepb.Notification_Openlineage{Openlineage: &storepb.OpenLineageDetail{
			Kind:          storepb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_NAMESPACE_UNMAPPED,
			Namespace:     "mysql://db:3306",
			Dataset:       "shop.orders",
			ApiKey:        "mxd_ol_...ab12",
			ReceivedCount: 10,
			FailedCount:   2,
		}},
	}, "ws")

	require.Equal(t, "workspaces/ws/notifications/8", converted.GetName())
	require.Equal(t, v1pb.NotificationType_NOTIFICATION_TYPE_OPENLINEAGE, converted.GetType())
	require.Equal(t, "mysql://db:3306", converted.GetOpenlineage().GetNamespace())
	require.Equal(t, "shop.orders", converted.GetOpenlineage().GetDataset())
	require.Equal(t, "mxd_ol_...ab12", converted.GetOpenlineage().GetApiKey())
	require.Equal(t, v1pb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_NAMESPACE_UNMAPPED, converted.GetOpenlineage().GetKind())
	require.Equal(t, int32(10), converted.GetOpenlineage().GetReceivedCount())
	require.Equal(t, int32(2), converted.GetOpenlineage().GetFailedCount())
}

// A notification written by a newer server may carry a detail this build does
// not know; the envelope still has to render.
func TestConvertToV1NotificationWithoutAKnownDetail(t *testing.T) {
	t.Parallel()

	converted := convertToV1Notification(&storepb.Notification{Id: 9, Parent: "workspaces/ws"}, "ws")
	require.Equal(t, "workspaces/ws/notifications/9", converted.GetName())
	require.Nil(t, converted.GetDetail())
	require.Equal(t, v1pb.NotificationType_NOTIFICATION_TYPE_UNSPECIFIED, converted.GetType())
	require.Equal(t, v1pb.NotificationSeverity_NOTIFICATION_SEVERITY_UNSPECIFIED, converted.GetSeverity())
}

func TestConvertToV1NotificationHandlesNil(t *testing.T) {
	t.Parallel()

	require.Nil(t, convertToV1Notification(nil, "ws"))
	require.Nil(t, convertSchemaSyncDetail(nil))
	require.Nil(t, convertOpenLineageDetail(nil))
}

// A mark-read batch is refused on its own shape before anything is read: the request
// body limit allows tens of thousands of names, and each one would be an id in the
// update's array. The nil store proves no query runs for a malformed batch.
func TestBatchMarkNotificationsReadValidatesTheBatch(t *testing.T) {
	t.Parallel()

	service := NewNotificationService(nil, nil)
	ctx := context.WithValue(context.Background(), common.UserContextKey, &store.UserMessage{ID: 7})

	_, err := service.BatchMarkNotificationsRead(ctx, connect.NewRequest(&v1pb.BatchMarkNotificationsReadRequest{
		Parent: "workspaces/-",
	}))
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	require.Equal(t, connect.CodeInvalidArgument, connectErr.Code(), "an empty batch is refused")

	tooMany := make([]string, maxNotificationsPerBatch+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("workspaces/ws/notifications/%d", i+1)
	}
	_, err = service.BatchMarkNotificationsRead(ctx, connect.NewRequest(&v1pb.BatchMarkNotificationsReadRequest{
		Parent: "workspaces/-",
		Names:  tooMany,
	}))
	require.ErrorAs(t, err, &connectErr)
	require.Equal(t, connect.CodeInvalidArgument, connectErr.Code(), "an over-long batch is refused")
}

func TestFormatNotificationRoundTrips(t *testing.T) {
	t.Parallel()

	name := common.FormatNotification("ws", 12)
	require.Equal(t, "workspaces/ws/notifications/12", name)
	id, err := common.GetNotificationID(name)
	require.NoError(t, err)
	require.Equal(t, int64(12), id)

	_, err = common.GetNotificationID("workspaces/ws/notifications/abc")
	require.Error(t, err)
	_, err = common.GetNotificationID("notifications/12")
	require.Error(t, err)
}

// A streamed message is the same envelope the list serves, so a client renders both
// with one code path.
func TestConvertToV1NotificationEventNamesAndCounts(t *testing.T) {
	t.Parallel()

	converted := convertToV1NotificationEvent(notification.Event{
		Notification: &storepb.Notification{
			Id:       9,
			Parent:   "workspaces/ws",
			Type:     storepb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC,
			Severity: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO,
			Detail: &storepb.Notification_SchemaSync{
				SchemaSync: &storepb.SchemaSyncDetail{Instance: "instances/inst1"},
			},
		},
		UnreadCount:      4,
		UnreadCountKnown: true,
	}, "ws")

	event, ok := converted.GetEvent().(*v1pb.SubscribeNotificationsResponse_Notification)
	require.True(t, ok)
	require.Equal(t, "workspaces/ws/notifications/9", event.Notification.GetNotification().GetName())
	require.Equal(t, "instances/inst1", event.Notification.GetNotification().GetSchemaSync().GetInstance())
	require.Equal(t, int32(4), event.Notification.GetUnreadCount())
}

// A count the store could not answer must stay absent rather than arrive as a zero
// the client would read as "everything is read".
func TestConvertToV1NotificationEventWithoutACount(t *testing.T) {
	t.Parallel()

	converted := convertToV1NotificationEvent(notification.Event{
		Notification: &storepb.Notification{Id: 3, Parent: "workspaces/ws"},
	}, "ws")

	event, ok := converted.GetEvent().(*v1pb.SubscribeNotificationsResponse_Notification)
	require.True(t, ok)
	require.Nil(t, event.Notification.UnreadCount)
	require.Equal(t, int32(0), event.Notification.GetUnreadCount())
}

// A detail written by a newer server still streams: the envelope renders and the
// client falls back to a generic line.
func TestConvertToV1NotificationEventWithoutAKnownDetail(t *testing.T) {
	t.Parallel()

	converted := convertToV1NotificationEvent(notification.Event{
		Notification: &storepb.Notification{Id: 5, Parent: "workspaces/ws"},
	}, "ws")

	event, ok := converted.GetEvent().(*v1pb.SubscribeNotificationsResponse_Notification)
	require.True(t, ok)
	require.Nil(t, event.Notification.GetNotification().GetDetail())
}

// A subscription is not reachable without credentials: the handler refuses before it
// registers anything or reads the store.
func TestSubscribeNotificationsRequiresAuthentication(t *testing.T) {
	t.Parallel()

	service := NewNotificationService(nil, fakeSubscriptions{})
	err := service.SubscribeNotifications(context.Background(), connect.NewRequest(&v1pb.SubscribeNotificationsRequest{
		Parent: "workspaces/-",
	}), nil)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	require.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}

// A service built without a push channel refuses the stream instead of accepting one that
// could never deliver anything. The nil store proves the refusal comes before the
// workspace read: a request that reached that read would panic instead of answering.
func TestSubscribeNotificationsWithoutAPushChannel(t *testing.T) {
	t.Parallel()

	service := NewNotificationService(nil, nil)
	ctx := context.WithValue(context.Background(), common.UserContextKey, &store.UserMessage{ID: 7})
	err := service.SubscribeNotifications(ctx, connect.NewRequest(&v1pb.SubscribeNotificationsRequest{
		Parent: "workspaces/-",
	}), nil)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	require.Equal(t, connect.CodeUnimplemented, connectErr.Code())
}

// fakeSubscriptions satisfies the push interface. Nothing is asserted through it: the
// scope it would record is covered end to end, by the integration test that connects two
// principals at once, because reaching the registration needs a workspace read.
type fakeSubscriptions struct{}

func (fakeSubscriptions) Subscribe(int) (<-chan notification.Event, func(), error) {
	events := make(chan notification.Event)
	return events, func() {}, nil
}
