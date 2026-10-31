package notification

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// digestEventIDPrefix makes a digest's delivery event identity a function of its
// period, not of the tick that queued it: a retried or resumed pass produces the
// same (event_id, channel_id) pairs, which the delivery table's unique index
// refuses as a repeat.
const digestEventIDPrefix = "dqdigest:"

// EnqueueDataQualityDigest queues one weekly digest for every outbound channel
// that subscribed to it and returns how many channels were queued. It writes no
// inbox row: a digest is workspace-level and has no per-recipient copy, so the
// delivery row is the only record of the send.
//
// Subscription is opt-in. A channel receives the digest only when its
// event_types explicitly contains NOTIFICATION_TYPE_DATA_QUALITY_DIGEST: empty
// means "every type that existed when the channel was configured", and the
// digest is newer than every channel a deployment already has. routed_only
// channels are skipped too: a routing rule matches on an event's audience, and a
// workspace-level digest has none. An EMAIL channel is skipped when it names no
// fixed address: include_audience resolves the event's audience, and the digest
// has none, so the delivery could only ever be dead. The enabled switch and the
// severity floor still apply.
func (s *Service) EnqueueDataQualityDigest(ctx context.Context, periodKey string, severity storepb.NotificationSeverity, detail *storepb.DataQualityDigestDetail) (int, error) {
	if strings.TrimSpace(periodKey) == "" {
		return 0, errors.New("a data quality digest period key is required")
	}
	if detail == nil {
		return 0, errors.New("a data quality digest detail is required")
	}
	event := &storepb.NotificationDeliveryEvent{
		Type:     storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY_DIGEST,
		Severity: severity,
		Detail:   &storepb.NotificationDeliveryEvent_DataQualityDigest{DataQualityDigest: detail},
	}
	channels, err := s.store.ListNotificationChannels(ctx)
	if err != nil {
		return 0, errors.Wrap(err, "failed to read the notification channels for a digest")
	}
	eventID := digestEventIDPrefix + periodKey
	now := time.Now()
	var creates []*store.CreateNotificationDeliveryMessage
	for _, channel := range channels {
		if !subscribesDigest(channel.Payload, severity) {
			continue
		}
		if digestEmailWithoutAddresses(channel.Payload) {
			slog.Debug("Skipping an email channel with no fixed addresses for the data quality digest",
				slog.String("channel", channel.ResourceID))
			continue
		}
		creates = append(creates, &store.CreateNotificationDeliveryMessage{
			EventID:     eventID,
			ChannelID:   channel.ID,
			Payload:     event,
			NextAttempt: now,
		})
	}
	if len(creates) == 0 {
		return 0, nil
	}
	if err := s.store.CreateNotificationDeliveries(ctx, creates); err != nil {
		return 0, errors.Wrap(err, "failed to queue the data quality digest deliveries")
	}
	return len(creates), nil
}

// subscribesDigest reports whether one channel receives the digest. Unlike
// subscribes, an empty event_types list does not select it: the digest is
// opt-in, so a channel configured before the type existed keeps its meaning and
// an email channel with no fixed addresses is not queued an event it could never
// resolve recipients for.
func subscribesDigest(channel *storepb.NotificationChannel, severity storepb.NotificationSeverity) bool {
	if channel == nil || !channel.GetEnabled() {
		return false
	}
	if channel.GetRoutedOnly() {
		return false
	}
	subscribed := false
	for _, eventType := range channel.GetEventTypes() {
		if eventType == storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY_DIGEST {
			subscribed = true
			break
		}
	}
	if !subscribed {
		return false
	}
	return severityRank(severity) >= severityRank(channel.GetMinSeverity())
}

// digestEmailWithoutAddresses reports whether one channel is an EMAIL channel
// that names no fixed address. The digest has no audience, so its
// include_audience resolution has nobody to add; queuing the delivery would only
// record a DEAD row (and a log line) every week.
func digestEmailWithoutAddresses(channel *storepb.NotificationChannel) bool {
	if channel.GetKind() != storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_EMAIL {
		return false
	}
	// A blank address is not a destination: count only what would actually
	// receive the mail, so a misconfigured channel is skipped rather than queued a
	// delivery that can only die.
	for _, address := range channel.GetConfig().GetEmail().GetAddresses() {
		if strings.TrimSpace(address) != "" {
			return false
		}
	}
	return true
}
