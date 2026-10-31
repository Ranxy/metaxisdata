package v1

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func storedNotificationRoute() *storepb.NotificationRoute {
	return &storepb.NotificationRoute{
		DisplayName: "Data platform",
		Enabled:     true,
		Match: &storepb.NotificationRouteMatch{
			EventTypes:  []storepb.NotificationType{storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY},
			MinSeverity: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING,
			Owners:      []string{"users/7"},
			Groups:      []string{"data-platform@example.com"},
		},
		Channels: []string{"abc123"},
	}
}

func TestStoreNotificationRoutePayload(t *testing.T) {
	t.Parallel()

	payload, err := storeNotificationRoutePayload(&v1pb.NotificationRoute{
		DisplayName: "  Data platform  ",
		Enabled:     true,
		Match: &v1pb.NotificationRouteMatch{
			EventTypes: []v1pb.NotificationType{
				v1pb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY,
				v1pb.NotificationType_NOTIFICATION_TYPE_UNSPECIFIED,
			},
			MinSeverity: v1pb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING,
			Owners:      []string{"users/7", "users/7"},
			Groups:      []string{" Data-Platform@example.com ", "data-platform@example.com"},
		},
		Channels: []string{
			"workspaces/ws-1/notificationChannels/abc123",
			"workspaces/ws-1/notificationChannels/abc123",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "Data platform", payload.GetDisplayName())
	require.Equal(t, []storepb.NotificationType{storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY}, payload.GetMatch().GetEventTypes())
	require.Equal(t, []string{"users/7"}, payload.GetMatch().GetOwners())
	require.Equal(t, []string{"data-platform@example.com"}, payload.GetMatch().GetGroups())
	require.Equal(t, []string{"abc123"}, payload.GetChannels())

	_, err = storeNotificationRoutePayload(&v1pb.NotificationRoute{
		DisplayName: "route",
		Match:       &v1pb.NotificationRouteMatch{Owners: []string{"not-a-user"}},
	})
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "invalid owner")

	_, err = storeNotificationRoutePayload(&v1pb.NotificationRoute{
		DisplayName: "route",
		Channels:    []string{"workspaces/ws-1/notificationChannels/not a valid id"},
	})
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "invalid channel name")

	_, err = storeNotificationRoutePayload(&v1pb.NotificationRoute{DisplayName: " "})
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "display name required")
}

func TestMergeNotificationRouteUpdateAppliesMask(t *testing.T) {
	t.Parallel()

	merged, err := mergeNotificationRouteUpdate(&v1pb.NotificationRoute{
		DisplayName: "renamed",
		Enabled:     false,
	}, []string{"display_name", "enabled"}, storedNotificationRoute())
	require.NoError(t, err)
	require.Equal(t, "renamed", merged.GetDisplayName())
	require.False(t, merged.GetEnabled())
	require.Equal(t, []storepb.NotificationType{storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY}, merged.GetMatch().GetEventTypes())
	require.Equal(t, []string{"abc123"}, merged.GetChannels())

	merged, err = mergeNotificationRouteUpdate(&v1pb.NotificationRoute{
		Channels: []string{"workspaces/ws-1/notificationChannels/def456"},
	}, []string{"channels"}, storedNotificationRoute())
	require.NoError(t, err)
	require.Equal(t, []string{"def456"}, merged.GetChannels())
	require.Equal(t, "Data platform", merged.GetDisplayName())

	_, err = mergeNotificationRouteUpdate(&v1pb.NotificationRoute{}, []string{"surprise"}, storedNotificationRoute())
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// A full update replaces the mutable fields and still needs a display name.
	_, err = mergeNotificationRouteUpdate(&v1pb.NotificationRoute{}, nil, storedNotificationRoute())
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	merged, err = mergeNotificationRouteUpdate(&v1pb.NotificationRoute{DisplayName: "full"}, nil, storedNotificationRoute())
	require.NoError(t, err)
	require.Equal(t, "full", merged.GetDisplayName())
	require.Empty(t, merged.GetChannels())
}

func TestConvertToV1NotificationRoute(t *testing.T) {
	t.Parallel()

	converted := convertToV1NotificationRoute(&store.NotificationRouteMessage{
		ID:         1,
		ResourceID: "route123",
		Payload:    storedNotificationRoute(),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}, "ws-1")
	require.Equal(t, "workspaces/ws-1/notificationRoutes/route123", converted.GetName())
	require.Equal(t, "Data platform", converted.GetDisplayName())
	require.Equal(t, []string{"users/7"}, converted.GetMatch().GetOwners())
	require.Equal(t, []string{"data-platform@example.com"}, converted.GetMatch().GetGroups())
	require.Equal(t, []string{"workspaces/ws-1/notificationChannels/abc123"}, converted.GetChannels())
	require.NotNil(t, converted.GetCreateTime())
}
