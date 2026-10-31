package notification

import (
	"context"
	"log/slog"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	clog "github.com/Ranxy/metaxisdata/backend/common/log"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// outboundEventIDLength is the length of the id one Send call's event carries.
const outboundEventIDLength = 12

// enqueueOutbound queues one written notification event for every outbound
// channel that subscribes to it. It is best effort by contract, like the inbox
// write itself: an outbox row that cannot be written is logged and the inbox
// still holds the message, so a broken channel configuration never fails the
// producer that reported an issue.
//
// One call is one event. The same event addressed to five administrators
// produces five inbox rows but one event id, and the unique (event_id,
// channel_id) index makes a repeated enqueue a no-op. audience is the user ids
// the event's in-app message addressed, carried for the email channel: it is
// resolved to addresses at send time, so a changed or deleted account resolves
// then.
func (s *Service) enqueueOutbound(ctx context.Context, n *storepb.Notification, audience []int32) {
	event := deliveryEvent(n, audience)
	if event == nil {
		return
	}
	channels, err := s.store.ListNotificationChannels(ctx)
	if err != nil {
		slog.Warn("Failed to read the notification channels for an outbound delivery",
			clog.WithError(err))
		return
	}

	now := time.Now()
	var creates []*store.CreateNotificationDeliveryMessage
	for _, channel := range channels {
		if !subscribes(channel.Payload, event) {
			continue
		}
		creates = append(creates, &store.CreateNotificationDeliveryMessage{
			ChannelID:   channel.ID,
			Payload:     event,
			NextAttempt: now,
		})
	}
	if len(creates) == 0 {
		return
	}
	eventID, err := common.RandomResourceID(outboundEventIDLength)
	if err != nil {
		slog.Warn("Failed to generate a notification event id", clog.WithError(err))
		return
	}
	for _, create := range creates {
		create.EventID = eventID
	}
	if err := s.store.CreateNotificationDeliveries(ctx, creates); err != nil {
		slog.Warn("Failed to queue the outbound notification deliveries",
			slog.Int("channels", len(creates)), clog.WithError(err))
	}
}

// subscribes reports whether one channel wants one event: it must be enabled,
// its type filter must accept the type (empty accepts everything) and its
// severity floor must be at or below the event's severity.
func subscribes(channel *storepb.NotificationChannel, event *storepb.NotificationDeliveryEvent) bool {
	if channel == nil || !channel.GetEnabled() {
		return false
	}
	if types := channel.GetEventTypes(); len(types) > 0 {
		found := false
		for _, eventType := range types {
			if eventType == event.GetType() {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return severityRank(event.GetSeverity()) >= severityRank(channel.GetMinSeverity())
}

// severityRank orders the notification severities, with UNSPECIFIED at the
// bottom. An UNSPECIFIED floor therefore accepts every severity, which is the
// meaning the proto gives it.
func severityRank(severity storepb.NotificationSeverity) int {
	switch severity {
	case storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO:
		return 1
	case storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING:
		return 2
	case storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR:
		return 3
	case storepb.NotificationSeverity_NOTIFICATION_SEVERITY_UNSPECIFIED:
		return 0
	default:
		return 0
	}
}

// deliveryEvent strips the per-recipient half of a notification: the outbox is
// workspace-level, so recipient, read state and dedupe key do not belong in it.
// The audience ids are the event-level counterpart: who the message concerned,
// kept for the email channel because it resolves them to addresses at send time.
// It returns nil for a message with no detail, which has nothing to post.
func deliveryEvent(n *storepb.Notification, audience []int32) *storepb.NotificationDeliveryEvent {
	event := &storepb.NotificationDeliveryEvent{
		Type:            n.GetType(),
		Severity:        n.GetSeverity(),
		AudienceUserIds: audience,
	}
	switch detail := n.GetDetail().(type) {
	case *storepb.Notification_SchemaSync:
		event.Detail = &storepb.NotificationDeliveryEvent_SchemaSync{SchemaSync: detail.SchemaSync}
	case *storepb.Notification_Openlineage:
		event.Detail = &storepb.NotificationDeliveryEvent_Openlineage{Openlineage: detail.Openlineage}
	case *storepb.Notification_DataQuality:
		event.Detail = &storepb.NotificationDeliveryEvent_DataQuality{DataQuality: detail.DataQuality}
	default:
		return nil
	}
	return event
}
