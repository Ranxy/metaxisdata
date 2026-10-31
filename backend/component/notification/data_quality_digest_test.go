package notification

import (
	"context"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func digestChannel(id int64, enabled, routedOnly bool, minSeverity storepb.NotificationSeverity, eventTypes ...storepb.NotificationType) *store.NotificationChannelMessage {
	return testChannel(id, &storepb.NotificationChannel{
		DisplayName: "digest",
		Kind:        storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WEBHOOK,
		Enabled:     enabled,
		EventTypes:  eventTypes,
		MinSeverity: minSeverity,
		RoutedOnly:  routedOnly,
		Config: &storepb.NotificationChannelConfig{
			Config: &storepb.NotificationChannelConfig_Webhook{Webhook: &storepb.WebhookChannelConfig{}},
		},
	})
}

func digestEmailChannel(id int64, addresses ...string) *store.NotificationChannelMessage {
	return testChannel(id, &storepb.NotificationChannel{
		DisplayName: "digest email",
		Kind:        storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_EMAIL,
		Enabled:     true,
		EventTypes:  []storepb.NotificationType{storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY_DIGEST},
		Config: &storepb.NotificationChannelConfig{
			Config: &storepb.NotificationChannelConfig_Email{Email: &storepb.EmailChannelConfig{
				Addresses:       addresses,
				IncludeAudience: true,
			}},
		},
	})
}

func digestDetailFixture() *storepb.DataQualityDigestDetail {
	return &storepb.DataQualityDigestDetail{
		ExecutionCount: 10,
		FailedCount:    2,
	}
}

// The digest is opt-in: a channel configured before the type existed (empty
// event_types) or subscribed to another type only must not receive it, and
// neither may a disabled channel, a routed_only channel (routes match on an
// audience the digest does not have), an email channel with no fixed address
// (the digest has no audience to resolve) or one whose severity floor is above
// it.
func TestEnqueueDataQualityDigestIsOptIn(t *testing.T) {
	t.Parallel()

	fake := adminFakeStore(t,
		// A channel from before the digest type existed: empty event_types.
		digestChannel(1, true, false, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_UNSPECIFIED),
		// Subscribed to a different type only.
		digestChannel(2, true, false, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_UNSPECIFIED,
			storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY),
		// The one webhook channel that explicitly wants the digest.
		digestChannel(3, true, false, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING,
			storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY_DIGEST),
		// Disabled.
		digestChannel(4, false, false, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_UNSPECIFIED,
			storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY_DIGEST),
		// Routed-only: routes never apply to the digest.
		digestChannel(5, true, true, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_UNSPECIFIED,
			storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY_DIGEST),
		// Severity floor above the digest's.
		digestChannel(6, true, false, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR,
			storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY_DIGEST),
		// Email with include_audience but no usable fixed address (the list is empty
		// or blank): a workspace-level digest has no audience, so this could never
		// resolve a recipient.
		digestEmailChannel(7, "", "   "),
		// Email with a fixed address still receives it.
		digestEmailChannel(8, "oncall@example.com"),
	)
	// A wildcard route selects every event that has an audience, and it names the
	// pre-digest channel and the routed-only one. The digest fan-out never consults
	// routing, so neither may be queued and an unreadable routing table must not
	// affect the digest at all.
	fake.routes = []*store.NotificationRouteMessage{{ID: 1, ResourceID: "route-a", Payload: &storepb.NotificationRoute{
		Enabled:  true,
		Channels: []string{"chanb", "chanf"},
	}}}
	fake.routesErr = errors.New("routing table unavailable")
	service := newServiceWithStore(fake)

	queued, err := service.EnqueueDataQualityDigest(context.Background(), "2026-W44",
		storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING, digestDetailFixture())
	require.NoError(t, err)
	require.Equal(t, 2, queued)
	require.Len(t, fake.deliveries, 2)

	channels := map[int64]bool{}
	for _, delivery := range fake.deliveries {
		channels[delivery.ChannelID] = true
		require.Equal(t, "dqdigest:2026-W44", delivery.EventID)
		require.Equal(t, storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY_DIGEST, delivery.Payload.GetType())
		require.Equal(t, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING, delivery.Payload.GetSeverity())
		require.Equal(t, int32(10), delivery.Payload.GetDataQualityDigest().GetExecutionCount())
		require.Empty(t, delivery.Payload.GetAudienceUserIds(), "a digest concerns nobody in particular")
	}
	require.Equal(t, map[int64]bool{3: true, 8: true}, channels)
}

// A repeated enqueue of the same period is the same event, so the delivery
// table's unique (event_id, channel_id) index turns it into a no-op; the id must
// come from the period, not from the call.
func TestEnqueueDataQualityDigestUsesThePeriodAsEventIdentity(t *testing.T) {
	t.Parallel()

	fake := adminFakeStore(t, digestChannel(1, true, false, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_UNSPECIFIED,
		storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY_DIGEST))
	service := newServiceWithStore(fake)

	first, err := service.EnqueueDataQualityDigest(context.Background(), "2026-W44",
		storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO, digestDetailFixture())
	require.NoError(t, err)
	second, err := service.EnqueueDataQualityDigest(context.Background(), "2026-W44",
		storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO, digestDetailFixture())
	require.NoError(t, err)
	require.Equal(t, 1, first)
	require.Equal(t, 1, second)
	require.Len(t, fake.deliveries, 2)
	require.Equal(t, fake.deliveries[0].EventID, fake.deliveries[1].EventID)
	require.NotEqual(t, "dqdigest:2026-W45", fake.deliveries[0].EventID)
}

// No subscribed channel is not an error: the period is still finished, and the
// runner records it as sent for zero channels.
func TestEnqueueDataQualityDigestWithNoSubscribedChannel(t *testing.T) {
	t.Parallel()

	fake := adminFakeStore(t)
	service := newServiceWithStore(fake)

	queued, err := service.EnqueueDataQualityDigest(context.Background(), "2026-W44",
		storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO, digestDetailFixture())
	require.NoError(t, err)
	require.Zero(t, queued)
	require.Empty(t, fake.deliveries)
}

// A store failure must reach the caller: the runner records the period only
// after a successful enqueue, so returning nil here would silently skip a week.
func TestEnqueueDataQualityDigestReportsStoreFailures(t *testing.T) {
	t.Parallel()

	fake := adminFakeStore(t, digestChannel(1, true, false, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_UNSPECIFIED,
		storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY_DIGEST))
	fake.channelsErr = errors.New("channels unavailable")
	service := newServiceWithStore(fake)

	queued, err := service.EnqueueDataQualityDigest(context.Background(), "2026-W44",
		storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO, digestDetailFixture())
	require.Error(t, err)
	require.Zero(t, queued)

	fake.channelsErr = nil
	fake.deliveryErr = errors.New("outbox unavailable")
	queued, err = service.EnqueueDataQualityDigest(context.Background(), "2026-W44",
		storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO, digestDetailFixture())
	require.Error(t, err)
	require.Zero(t, queued)
}

func TestEnqueueDataQualityDigestRequiresADetail(t *testing.T) {
	t.Parallel()

	service := newServiceWithStore(adminFakeStore(t))
	queued, err := service.EnqueueDataQualityDigest(context.Background(), "2026-W44",
		storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO, nil)
	require.Error(t, err)
	require.Zero(t, queued)
}

// A blank period would produce the collision-prone event id "dqdigest:", so the
// enqueue refuses it like the store does.
func TestEnqueueDataQualityDigestRequiresAPeriodKey(t *testing.T) {
	t.Parallel()

	fake := adminFakeStore(t, digestChannel(1, true, false, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_UNSPECIFIED,
		storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY_DIGEST))
	service := newServiceWithStore(fake)

	queued, err := service.EnqueueDataQualityDigest(context.Background(), "   ",
		storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO, digestDetailFixture())
	require.Error(t, err)
	require.Zero(t, queued)
	require.Empty(t, fake.deliveries)
}
