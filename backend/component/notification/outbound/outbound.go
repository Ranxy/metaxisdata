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
// external URL to point at. Recipients is only set for an email delivery: it is
// the resolved address list, and an email receiver refuses an empty one.
type Message struct {
	EventID    string
	DeliveryID int64
	EventType  storepb.NotificationType
	Severity   storepb.NotificationSeverity
	Title      string
	Text       string
	Link       string
	Recipients []string
	OccurredAt time.Time
}

// Receiver posts one rendered message to one destination and returns the
// destination's response body, trimmed, for the test-send surface. An error
// means the attempt failed and the dispatcher should retry.
type Receiver interface {
	Post(ctx context.Context, message Message) (string, error)
}

// NewReceiver builds the receiver a channel's kind, decrypted secret and the
// workspace email setting imply. The email setting is only read for an EMAIL
// channel; every other kind delivers over HTTP.
func NewReceiver(channel *storepb.NotificationChannel, secret *storepb.NotificationChannelSecret, email *storepb.EmailSetting) (Receiver, error) {
	if channel == nil {
		return nil, errors.New("a notification channel is required")
	}
	switch channel.GetKind() {
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WEBHOOK:
		webhook := channel.GetConfig().GetWebhook()
		if webhook == nil {
			return nil, errors.New("the webhook channel has no configuration")
		}
		if secret.GetEndpoint() == "" {
			return nil, errors.New("the webhook channel has no endpoint")
		}
		return NewWebhook(secret.GetEndpoint(), secret.GetSigningSecret(), webhook.GetSign()), nil
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_DINGTALK:
		dingtalk := channel.GetConfig().GetDingtalk()
		if dingtalk == nil {
			return nil, errors.New("the DingTalk channel has no configuration")
		}
		if secret.GetEndpoint() == "" {
			return nil, errors.New("the DingTalk channel has no endpoint")
		}
		return NewDingTalk(secret.GetEndpoint(), secret.GetSigningSecret(), dingtalk.GetSign()), nil
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WECOM:
		if channel.GetConfig().GetWecom() == nil {
			return nil, errors.New("the WeCom channel has no configuration")
		}
		if secret.GetEndpoint() == "" {
			return nil, errors.New("the WeCom channel has no endpoint")
		}
		return NewWeCom(secret.GetEndpoint()), nil
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_FEISHU:
		feishu := channel.GetConfig().GetFeishu()
		if feishu == nil {
			return nil, errors.New("the Feishu channel has no configuration")
		}
		if secret.GetEndpoint() == "" {
			return nil, errors.New("the Feishu channel has no endpoint")
		}
		return NewFeishu(secret.GetEndpoint(), secret.GetSigningSecret(), feishu.GetSign()), nil
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_EMAIL:
		if channel.GetConfig().GetEmail() == nil {
			return nil, errors.New("the email channel has no configuration")
		}
		return NewEmail(email)
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
