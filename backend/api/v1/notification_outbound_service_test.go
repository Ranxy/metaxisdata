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

func storedWebhookChannel() *storepb.NotificationChannel {
	return &storepb.NotificationChannel{
		DisplayName: "on-call",
		Kind:        storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WEBHOOK,
		Enabled:     true,
		MinSeverity: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING,
		Language:    "zh-CN",
		EventTypes:  []storepb.NotificationType{storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY},
		Config: &storepb.NotificationChannelConfig{
			Config: &storepb.NotificationChannelConfig_Webhook{Webhook: &storepb.WebhookChannelConfig{Sign: true}},
		},
	}
}

// A masked update only has to carry the fields it names.
func TestMergeNotificationChannelUpdateAppliesMask(t *testing.T) {
	t.Parallel()

	update := &v1pb.NotificationChannel{Enabled: false}
	merged, err := mergeNotificationChannelUpdate(update, []string{"enabled"}, storedWebhookChannel())
	require.NoError(t, err)
	require.Equal(t, "on-call", merged.GetDisplayName())
	require.False(t, merged.GetEnabled())
	require.Equal(t, "zh-CN", merged.GetLanguage())
	require.True(t, merged.GetConfig().GetWebhook().GetSign())

	update = &v1pb.NotificationChannel{
		DisplayName: "renamed",
		MinSeverity: v1pb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR,
	}
	merged, err = mergeNotificationChannelUpdate(update, []string{"display_name", "min_severity"}, storedWebhookChannel())
	require.NoError(t, err)
	require.Equal(t, "renamed", merged.GetDisplayName())
	require.Equal(t, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR, merged.GetMinSeverity())

	// A masked update that clears the display name is invalid.
	_, err = mergeNotificationChannelUpdate(&v1pb.NotificationChannel{}, []string{"display_name"}, storedWebhookChannel())
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestMergeNotificationChannelUpdateRejectsUnsupportedPaths(t *testing.T) {
	t.Parallel()

	_, err := mergeNotificationChannelUpdate(&v1pb.NotificationChannel{}, []string{"kind"}, storedWebhookChannel())
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	_, err = mergeNotificationChannelUpdate(&v1pb.NotificationChannel{}, []string{"displayName", "surprise"}, storedWebhookChannel())
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

// An empty mask is a full update; it must still validate the fields it writes.
func TestMergeNotificationChannelUpdateFullReplace(t *testing.T) {
	t.Parallel()

	_, err := mergeNotificationChannelUpdate(&v1pb.NotificationChannel{}, nil, storedWebhookChannel())
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "a full replace needs a display name")

	merged, err := mergeNotificationChannelUpdate(&v1pb.NotificationChannel{DisplayName: "renamed"}, nil, storedWebhookChannel())
	require.NoError(t, err)
	require.Equal(t, "renamed", merged.GetDisplayName())
}

func TestStoreNotificationChannelPayload(t *testing.T) {
	t.Parallel()

	payload, err := storeNotificationChannelPayload(&v1pb.NotificationChannel{
		DisplayName: "  on-call  ",
		Kind:        v1pb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WEBHOOK,
		Enabled:     true,
		EventTypes: []v1pb.NotificationType{
			v1pb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY,
			v1pb.NotificationType_NOTIFICATION_TYPE_UNSPECIFIED,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "on-call", payload.GetDisplayName())
	require.Equal(t, []storepb.NotificationType{storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY}, payload.GetEventTypes())
	require.NotNil(t, payload.GetConfig().GetWebhook())

	_, err = storeNotificationChannelPayload(&v1pb.NotificationChannel{DisplayName: " "})
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	longName := make([]rune, 101)
	for i := range longName {
		longName[i] = 'a'
	}
	_, err = storeNotificationChannelPayload(&v1pb.NotificationChannel{DisplayName: string(longName)})
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestStoreNotificationChannelSecret(t *testing.T) {
	t.Parallel()

	require.Nil(t, storeNotificationChannelSecret(nil))
	require.Nil(t, storeNotificationChannelSecret(&v1pb.NotificationChannelSecret{}))
	require.Nil(t, storeNotificationChannelSecret(&v1pb.NotificationChannelSecret{WebhookUrl: "  "}))

	secret := storeNotificationChannelSecret(&v1pb.NotificationChannelSecret{
		WebhookUrl:           " https://example.com/hook ",
		WebhookSigningSecret: "s3cret",
	})
	require.Equal(t, "https://example.com/hook", secret.GetWebhookUrl())
	require.Equal(t, "s3cret", secret.GetWebhookSigningSecret())
}

func TestConvertToV1NotificationChannel(t *testing.T) {
	t.Parallel()

	channel := convertToV1NotificationChannel(&store.NotificationChannelMessage{
		ID:         1,
		ResourceID: "abc123",
		Payload:    storedWebhookChannel(),
		HasSecret:  true,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}, "ws-1")
	require.Equal(t, "workspaces/ws-1/notificationChannels/abc123", channel.GetName())
	require.True(t, channel.GetSecretSet())
	require.Equal(t, v1pb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WEBHOOK, channel.GetKind())
	require.Equal(t, []v1pb.NotificationType{v1pb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY}, channel.GetEventTypes())
}

func TestConvertToV1NotificationDeliveryNextAttempt(t *testing.T) {
	t.Parallel()

	delivery := &store.NotificationDeliveryMessage{
		ID:          9,
		EventID:     "evt",
		ChannelID:   1,
		State:       storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_PENDING,
		Attempts:    1,
		NextAttempt: time.Now(),
		CreatedAt:   time.Now(),
		Payload: &storepb.NotificationDeliveryEvent{
			Type:     storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY,
			Severity: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING,
		},
	}
	converted := convertToV1NotificationDelivery(delivery, nil, "ws-1")
	require.NotNil(t, converted.GetNextAttemptTime())
	require.Equal(t, "workspaces/ws-1/notificationDeliveries/9", converted.GetName())

	delivery.State = storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_SENT
	sentAt := time.Now()
	delivery.SentAt = &sentAt
	converted = convertToV1NotificationDelivery(delivery, nil, "ws-1")
	require.Nil(t, converted.GetNextAttemptTime())
	require.NotNil(t, converted.GetSentTime())
}
