package notification

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func testChannel(id int64, payload *storepb.NotificationChannel) *store.NotificationChannelMessage {
	return &store.NotificationChannelMessage{
		ID:         id,
		ResourceID: "chan" + string(rune('a'+id)),
		Payload:    payload,
	}
}

func enabledWebhookChannel() *storepb.NotificationChannel {
	return &storepb.NotificationChannel{
		DisplayName: "on-call",
		Kind:        storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WEBHOOK,
		Enabled:     true,
		Config: &storepb.NotificationChannelConfig{
			Config: &storepb.NotificationChannelConfig_Webhook{Webhook: &storepb.WebhookChannelConfig{}},
		},
	}
}

func adminFakeStore(t *testing.T, channels ...*store.NotificationChannelMessage) *fakeStore {
	t.Helper()
	return &fakeStore{
		workspaceID: "ws-1",
		channels:    channels,
		policy: &storepb.IamPolicy{
			Bindings: []*storepb.Binding{
				{Role: common.FormatRole(common.WorkspaceMember), Members: []string{common.AllUsers}},
				// Two administrators: one event, two inbox rows, one delivery per channel.
				{Role: common.FormatRole(common.WorkspaceAdmin), Members: []string{"users/2", "users/3"}},
			},
		},
		users: map[int]*store.UserMessage{
			2: {ID: 2, Name: "alice"},
			3: {ID: 3, Name: "bob"},
		},
	}
}

// One event addressed to several administrators is one outbound event: every
// inbox row shares the event id, and a channel is queued once.
func TestSendToWorkspaceAdminsQueuesOneDeliveryPerChannel(t *testing.T) {
	t.Parallel()

	fake := adminFakeStore(t,
		testChannel(1, enabledWebhookChannel()),
		testChannel(2, enabledWebhookChannel()),
	)
	service := newServiceWithStore(fake)

	message := newOpenLineageMessage()
	require.NoError(t, service.SendToWorkspaceAdmins(context.Background(), message))

	require.Len(t, fake.created, 2)
	require.Len(t, fake.deliveries, 2)
	require.Equal(t, fake.deliveries[0].EventID, fake.deliveries[1].EventID)
	require.NotEmpty(t, fake.deliveries[0].EventID)
	for _, delivery := range fake.deliveries {
		require.Equal(t, storepb.NotificationType_NOTIFICATION_TYPE_OPENLINEAGE, delivery.Payload.GetType())
		require.NotNil(t, delivery.Payload.GetOpenlineage())
		// The audience is every administrator the event concerned, even though the
		// delivery itself is one row per channel.
		require.ElementsMatch(t, []int32{2, 3}, delivery.Payload.GetAudienceUserIds())
	}
}

// A single-recipient event carries that recipient as the audience the email
// channel resolves at send time.
func TestSendCarriesTheRecipientAsTheAudience(t *testing.T) {
	t.Parallel()

	fake := adminFakeStore(t, testChannel(1, enabledWebhookChannel()))
	service := newServiceWithStore(fake)

	require.NoError(t, service.Send(context.Background(), &storepb.Notification{
		RecipientId: 2,
		Type:        storepb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC,
		Severity:    storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO,
		Detail:      &storepb.Notification_SchemaSync{SchemaSync: &storepb.SchemaSyncDetail{InstanceTitle: "prod"}},
	}))
	require.Len(t, fake.deliveries, 1)
	require.Equal(t, []int32{2}, fake.deliveries[0].Payload.GetAudienceUserIds())
}

// A repeated event inside its dedupe window writes no inbox row and therefore
// queues no delivery.
func TestDedupedSendQueuesNothing(t *testing.T) {
	t.Parallel()

	fake := adminFakeStore(t, testChannel(1, enabledWebhookChannel()))
	fake.suppressNext = true
	service := newServiceWithStore(fake)

	require.NoError(t, service.Send(context.Background(), &storepb.Notification{
		RecipientId: 2,
		Type:        storepb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC,
		Severity:    storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO,
		DedupeKey:   "schema-sync:instances/i1:1",
		Detail:      &storepb.Notification_SchemaSync{SchemaSync: &storepb.SchemaSyncDetail{InstanceTitle: "prod"}},
	}))
	require.Empty(t, fake.deliveries)
}

// The channel's own filters decide what it receives: type set, severity floor and
// the enabled switch.
func TestEnqueueRespectsChannelSubscription(t *testing.T) {
	t.Parallel()

	disabled := enabledWebhookChannel()
	disabled.Enabled = false
	onlyOpenLineage := enabledWebhookChannel()
	onlyOpenLineage.EventTypes = []storepb.NotificationType{storepb.NotificationType_NOTIFICATION_TYPE_OPENLINEAGE}
	onlyErrors := enabledWebhookChannel()
	onlyErrors.MinSeverity = storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR
	everySeverity := enabledWebhookChannel()

	fake := adminFakeStore(t,
		testChannel(1, disabled),
		testChannel(2, onlyOpenLineage),
		testChannel(3, onlyErrors),
		testChannel(4, everySeverity),
	)
	service := newServiceWithStore(fake)

	// A warning data-quality event: the disabled channel and the wrong-type
	// channel skip it, the ERROR floor skips it, the open channel takes it.
	require.NoError(t, service.Send(context.Background(), &storepb.Notification{
		RecipientId: 2,
		Type:        storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY,
		Severity:    storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING,
		Detail: &storepb.Notification_DataQuality{DataQuality: &storepb.DataQualityDetail{
			Kind:       storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED,
			RuleTitle:  "no nulls",
			TargetPath: "prod/db/public/orders",
		}},
	}))
	require.Len(t, fake.deliveries, 1)
	require.Equal(t, int64(4), fake.deliveries[0].ChannelID)

	// An ERROR event also clears the floor.
	fake.deliveries = nil
	require.NoError(t, service.Send(context.Background(), &storepb.Notification{
		RecipientId: 2,
		Type:        storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY,
		Severity:    storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR,
		Detail: &storepb.Notification_DataQuality{DataQuality: &storepb.DataQualityDetail{
			Kind:       storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED,
			RuleTitle:  "no nulls",
			TargetPath: "prod/db/public/orders",
		}},
	}))
	require.Len(t, fake.deliveries, 2)
	require.ElementsMatch(t, []int64{3, 4}, []int64{fake.deliveries[0].ChannelID, fake.deliveries[1].ChannelID})
}

func TestSeverityFloorOrdering(t *testing.T) {
	t.Parallel()

	require.Less(t, severityRank(storepb.NotificationSeverity_NOTIFICATION_SEVERITY_UNSPECIFIED), severityRank(storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO))
	require.Less(t, severityRank(storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO), severityRank(storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING))
	require.Less(t, severityRank(storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING), severityRank(storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR))

	// A channel with no floor accepts everything, including an INFO event.
	channel := enabledWebhookChannel()
	channel.MinSeverity = storepb.NotificationSeverity_NOTIFICATION_SEVERITY_UNSPECIFIED
	event := &storepb.NotificationDeliveryEvent{
		Type:     storepb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC,
		Severity: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO,
		Detail:   &storepb.NotificationDeliveryEvent_SchemaSync{SchemaSync: &storepb.SchemaSyncDetail{}},
	}
	require.True(t, subscribes(channel, event))
}

// A message with no detail has nothing to post and must not queue a delivery.
func TestEnqueueSkipsMessageWithoutDetail(t *testing.T) {
	t.Parallel()

	fake := adminFakeStore(t, testChannel(1, enabledWebhookChannel()))
	service := newServiceWithStore(fake)

	require.NoError(t, service.Send(context.Background(), &storepb.Notification{
		RecipientId: 2,
		Type:        storepb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC,
		Severity:    storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO,
	}))
	require.Len(t, fake.created, 1)
	require.Empty(t, fake.deliveries)
}

// The delivery payload carries the event, not the recipient: an outbox row is
// workspace-level and must not hold one person's address. The audience is the
// event-level list of user ids, which the email channel resolves at send time.
func TestDeliveryEventStripsRecipient(t *testing.T) {
	t.Parallel()

	event := deliveryEvent(&storepb.Notification{
		RecipientId: 7,
		Type:        storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY,
		Severity:    storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR,
		Detail: &storepb.Notification_DataQuality{DataQuality: &storepb.DataQualityDetail{
			Kind:       storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED,
			RuleTitle:  "no nulls",
			TargetPath: "prod/db/public/orders",
		}},
	}, []int32{7})
	require.NotNil(t, event)
	require.Equal(t, storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY, event.GetType())
	require.Equal(t, "no nulls", event.GetDataQuality().GetRuleTitle())
	// The per-recipient columns are gone; the audience is the event-level
	// replacement the email channel reads.
	require.NotContains(t, event.String(), "recipientId")
	require.Equal(t, []int32{7}, event.GetAudienceUserIds())
}

func TestDeliveryEventWithoutDetailIsNil(t *testing.T) {
	t.Parallel()

	require.Nil(t, deliveryEvent(&storepb.Notification{Type: storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY}, nil))
}

// The queue write is best effort: a store failure must not fail the producer
// that reported an issue, and the inbox row is already written.
func TestEnqueueFailureDoesNotFailTheSend(t *testing.T) {
	t.Parallel()

	fake := adminFakeStore(t, testChannel(1, enabledWebhookChannel()))
	fake.deliveryErr = context.Canceled
	service := newServiceWithStore(fake)

	require.NoError(t, service.Send(context.Background(), &storepb.Notification{
		RecipientId: 2,
		Type:        storepb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC,
		Severity:    storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO,
		Detail:      &storepb.Notification_SchemaSync{SchemaSync: &storepb.SchemaSyncDetail{InstanceTitle: "prod"}},
	}))
	require.Len(t, fake.created, 1)
}

// The event id is fresh per call: two distinct events must not share one.
func TestEventIDsArePerCall(t *testing.T) {
	t.Parallel()

	fake := adminFakeStore(t, testChannel(1, enabledWebhookChannel()))
	service := newServiceWithStore(fake)

	for i := 0; i < 2; i++ {
		require.NoError(t, service.Send(context.Background(), &storepb.Notification{
			RecipientId: 2,
			Type:        storepb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC,
			Severity:    storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO,
			Detail:      &storepb.Notification_SchemaSync{SchemaSync: &storepb.SchemaSyncDetail{InstanceTitle: "prod"}},
		}))
	}
	require.Len(t, fake.deliveries, 2)
	require.NotEqual(t, fake.deliveries[0].EventID, fake.deliveries[1].EventID)
	require.WithinDuration(t, time.Now(), fake.deliveries[0].NextAttempt, time.Minute)
}
