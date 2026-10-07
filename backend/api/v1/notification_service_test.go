package v1

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
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
			Failures: []*storepb.SyncDatabaseResult{
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
	require.Len(t, converted.GetSchemaSync().GetFailures(), 2)
	require.Equal(t, v1pb.SyncDatabaseState_SYNC_DATABASE_STATE_FAILED, converted.GetSchemaSync().GetFailures()[0].GetState())
	require.Equal(t, "permission denied", converted.GetSchemaSync().GetFailures()[0].GetError())
	require.Equal(t, v1pb.SyncDatabaseState_SYNC_DATABASE_STATE_UNFINISHED, converted.GetSchemaSync().GetFailures()[1].GetState())
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
