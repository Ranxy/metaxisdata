package outbound

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// maxEndpointLength bounds a configured endpoint. A URL longer than this is a
// copy-paste accident, and storing it would only fail at delivery time.
const maxEndpointLength = 2048

// maxFixedEmailAddresses bounds the fixed addresses one email channel may list.
const maxFixedEmailAddresses = 20

// ValidateChannel checks the write-time invariants of one channel. It lives here
// rather than in the API handler because the receiver is the thing that has to
// be able to post with these values.
func ValidateChannel(channel *storepb.NotificationChannel, secret *storepb.NotificationChannelSecret) error {
	if channel == nil {
		return errors.New("a notification channel is required")
	}
	if strings.TrimSpace(channel.GetDisplayName()) == "" {
		return errors.New("a channel display name is required")
	}
	switch channel.GetKind() {
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WEBHOOK:
		webhook := channel.GetConfig().GetWebhook()
		if webhook == nil {
			return errors.New("the webhook channel has no configuration")
		}
		return validateEndpointChannel(secret, webhook.GetSign())
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_DINGTALK:
		dingtalk := channel.GetConfig().GetDingtalk()
		if dingtalk == nil {
			return errors.New("the DingTalk channel has no configuration")
		}
		if err := validateEndpointChannel(secret, dingtalk.GetSign()); err != nil {
			return err
		}
		return nil
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WECOM:
		if channel.GetConfig().GetWecom() == nil {
			return errors.New("the WeCom channel has no configuration")
		}
		return validateEndpointChannel(secret, false)
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_FEISHU:
		feishu := channel.GetConfig().GetFeishu()
		if feishu == nil {
			return errors.New("the Feishu channel has no configuration")
		}
		if err := validateEndpointChannel(secret, feishu.GetSign()); err != nil {
			return err
		}
		return nil
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_EMAIL:
		config := channel.GetConfig().GetEmail()
		if config == nil {
			return errors.New("the email channel has no configuration")
		}
		if secret.GetEndpoint() != "" || secret.GetSigningSecret() != "" {
			return errors.New("an email channel takes no credential")
		}
		addresses, err := NormalizeEmailAddresses(config.GetAddresses())
		if err != nil {
			return err
		}
		if len(addresses) == 0 && !config.GetIncludeAudience() {
			return errors.New("an email channel needs at least one address or the event audience")
		}
		return nil
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_UNSPECIFIED:
		return errors.New("a notification channel kind is required")
	default:
		return fmt.Errorf("unsupported notification channel kind %q", channel.GetKind().String())
	}
}

// validateEndpointChannel checks the endpoint and signing secret a webhook-like
// channel (webhook, DingTalk, WeCom, Feishu) needs.
func validateEndpointChannel(secret *storepb.NotificationChannelSecret, sign bool) error {
	if err := validateEndpoint(secret.GetEndpoint()); err != nil {
		return err
	}
	if sign && secret.GetSigningSecret() == "" {
		return errors.New("a signing secret is required when the channel signs its requests")
	}
	return nil
}

// validateEndpoint checks a configured endpoint. The endpoint is a credential
// for most kinds (a robot token sits in its query), so the parser error is never
// echoed.
func validateEndpoint(endpoint string) error {
	if strings.TrimSpace(endpoint) == "" {
		return errors.New("a channel endpoint is required")
	}
	if len(endpoint) > maxEndpointLength {
		return fmt.Errorf("the channel endpoint is longer than %d characters", maxEndpointLength)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return errors.New("the channel endpoint is not a valid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("the channel endpoint must use http or https")
	}
	if parsed.Host == "" {
		return errors.New("the channel endpoint has no host")
	}
	return nil
}

// NormalizeEmailAddresses trims, validates and de-duplicates the fixed addresses
// of an email channel. Addresses are compared case-insensitively, and the stored
// list keeps the casing the administrator typed.
func NormalizeEmailAddresses(addresses []string) ([]string, error) {
	result := make([]string, 0, len(addresses))
	seen := make(map[string]struct{}, len(addresses))
	for _, raw := range addresses {
		address := strings.TrimSpace(raw)
		if address == "" {
			continue
		}
		parsed, err := mail.ParseAddress(address)
		if err != nil || strings.ContainsAny(address, "\r\n") || parsed.Address != address {
			return nil, fmt.Errorf("invalid email address %q", address)
		}
		key := strings.ToLower(address)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, address)
		if len(result) > maxFixedEmailAddresses {
			return nil, fmt.Errorf("an email channel lists more than %d addresses", maxFixedEmailAddresses)
		}
	}
	return result, nil
}
