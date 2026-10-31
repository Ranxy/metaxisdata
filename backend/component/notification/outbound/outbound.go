// Package outbound turns one notification event into the text an external
// destination receives and posts it. It is deliberately independent of the
// inbox: nothing here reads or writes per-recipient state, and a channel is
// workspace-level, so an event reaches a channel once no matter how many inbox
// rows it produced.
package outbound

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// DefaultLanguage is the language outbound text is written in when a channel
// does not name one.
const DefaultLanguage = "en-US"

// timeout bounds one delivery attempt. A destination that takes longer than
// this is treated as failed and retried by the dispatcher instead of holding the
// worker open; an IM robot that cannot answer in ten seconds is down or broken.
const timeout = 10 * time.Second

// Message is one rendered notification, ready for a Receiver. Title and Text are
// already in the channel's language; Link is empty when the deployment has no
// external URL to point at.
type Message struct {
	EventID    string
	DeliveryID int64
	EventType  storepb.NotificationType
	Severity   storepb.NotificationSeverity
	Title      string
	Text       string
	Link       string
	OccurredAt time.Time
}

// Receiver posts one rendered message to one destination and returns the
// destination's response body, trimmed, for the test-send surface. An error
// means the attempt failed and the dispatcher should retry.
type Receiver interface {
	Post(ctx context.Context, message Message) (string, error)
}

// NewReceiver builds the receiver a channel's kind and decrypted secret imply.
func NewReceiver(channel *storepb.NotificationChannel, secret *storepb.NotificationChannelSecret) (Receiver, error) {
	if channel == nil {
		return nil, errors.New("a notification channel is required")
	}
	switch channel.GetKind() {
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WEBHOOK:
		if secret.GetWebhookUrl() == "" {
			return nil, errors.New("the webhook channel has no endpoint")
		}
		return NewWebhook(secret.GetWebhookUrl(), secret.GetWebhookSigningSecret(), channel.GetConfig().GetWebhook().GetSign()), nil
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_UNSPECIFIED:
		return nil, errors.New("the notification channel has no kind")
	default:
		return nil, fmt.Errorf("unsupported notification channel kind %q", channel.GetKind().String())
	}
}

// NormalizeLanguage reduces a BCP-47 tag to the catalog the renderer has: the
// Chinese catalog for anything whose primary tag is zh, English otherwise.
func NormalizeLanguage(language string) string {
	if len(language) >= 2 && strings.EqualFold(language[:2], "zh") {
		return "zh-CN"
	}
	return "en-US"
}
