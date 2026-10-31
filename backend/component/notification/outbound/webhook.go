package outbound

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// maxWebhookURLLength bounds a configured endpoint. A URL longer than this is a
// copy-paste accident, and storing it would only fail at delivery time.
const maxWebhookURLLength = 2048

// maxResponseBytes bounds how much of an endpoint's answer is kept in the error
// or the test-send response. A chat endpoint answers with a short JSON document;
// a broken one can answer with a page.
const maxResponseBytes = 4 << 10

// webhookPayload is the stable delivery body of a generic webhook. It is a fixed
// JSON shape on purpose: destinations script against it, so it changes only with
// a versioned field addition.
type webhookPayload struct {
	EventID    string `json:"eventId"`
	DeliveryID int64  `json:"deliveryId"`
	EventType  string `json:"eventType"`
	Severity   string `json:"severity"`
	Title      string `json:"title"`
	Text       string `json:"text"`
	Link       string `json:"link,omitempty"`
	OccurredAt string `json:"occurredAt"`
}

type webhookReceiver struct {
	url           string
	signingSecret string
	sign          bool
	client        *http.Client
}

// NewWebhook builds a generic webhook receiver. A signing secret may be empty
// when sign is false.
func NewWebhook(endpoint, signingSecret string, sign bool) Receiver {
	return &webhookReceiver{
		url:           endpoint,
		signingSecret: signingSecret,
		sign:          sign,
		client: &http.Client{
			Timeout: timeout,
			// A redirect is not followed on purpose. Go re-issues a 301/302/303 as a
			// GET without the body (the endpoint answers 200 and the body is silently
			// lost), and a 307/308 re-POSTs the body and the signature to a host the
			// endpoint chose — outside the destination the administrator configured.
			// A 3xx is therefore a failed attempt.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (w *webhookReceiver) Post(ctx context.Context, message Message) (string, error) {
	body, err := json.Marshal(webhookPayload{
		EventID:    message.EventID,
		DeliveryID: message.DeliveryID,
		EventType:  strings.TrimPrefix(message.EventType.String(), "NOTIFICATION_TYPE_"),
		Severity:   strings.TrimPrefix(message.Severity.String(), "NOTIFICATION_SEVERITY_"),
		Title:      message.Title,
		Text:       message.Text,
		Link:       message.Link,
		OccurredAt: message.OccurredAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return "", fmt.Errorf("failed to encode the webhook body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to build the webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "metaxisdata")
	req.Header.Set("X-Metaxisdata-Event", strings.TrimPrefix(message.EventType.String(), "NOTIFICATION_TYPE_"))
	req.Header.Set("X-Metaxisdata-Delivery", strconv.FormatInt(message.DeliveryID, 10))
	if w.sign && w.signingSecret != "" {
		mac := hmac.New(sha256.New, []byte(w.signingSecret))
		_, _ = mac.Write(body)
		req.Header.Set("X-Metaxisdata-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := w.client.Do(req)
	if err != nil {
		// The transport error quotes the request URL verbatim, and for a chat robot
		// that URL is the credential. Replace it before the message reaches an API
		// response, an audit row or the delivery's last_error.
		return "", fmt.Errorf("the webhook endpoint could not be reached: %s", w.sanitize(err.Error()))
	}
	defer resp.Body.Close()
	answer, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if readErr != nil {
		return "", fmt.Errorf("failed to read the webhook answer: %w", readErr)
	}
	trimmed := truncate(strings.TrimSpace(string(answer)), 1000)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("the webhook endpoint answered %s: %s", resp.Status, trimmed)
	}
	return trimmed, nil
}

// sanitize removes the endpoint from an error message.
func (w *webhookReceiver) sanitize(message string) string {
	message = strings.ReplaceAll(message, w.url, "<endpoint>")
	// The transport quotes the parsed, normalized URL ("HTTPS://Host/path" becomes
	// "https://host/path"), which the raw replacement would miss.
	if parsed, err := url.Parse(w.url); err == nil {
		message = strings.ReplaceAll(message, parsed.String(), "<endpoint>")
	}
	return message
}

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
		if err := validateWebhookURL(secret.GetWebhookUrl()); err != nil {
			return err
		}
		if channel.GetConfig().GetWebhook().GetSign() && secret.GetWebhookSigningSecret() == "" {
			return errors.New("a signing secret is required when the webhook body is signed")
		}
		return nil
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_UNSPECIFIED:
		return errors.New("a notification channel kind is required")
	default:
		return fmt.Errorf("unsupported notification channel kind %q", channel.GetKind().String())
	}
}

func validateWebhookURL(endpoint string) error {
	if strings.TrimSpace(endpoint) == "" {
		return errors.New("a webhook endpoint is required")
	}
	if len(endpoint) > maxWebhookURLLength {
		return fmt.Errorf("the webhook endpoint is longer than %d characters", maxWebhookURLLength)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		// Do not echo the parser error: it may carry the URL, which is a credential.
		return errors.New("the webhook endpoint is not a valid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("the webhook endpoint must use http or https")
	}
	if parsed.Host == "" {
		return errors.New("the webhook endpoint has no host")
	}
	return nil
}
