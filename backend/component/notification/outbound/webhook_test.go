package outbound

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func testMessage() Message {
	return Message{
		EventID:    "evt-1",
		DeliveryID: 42,
		EventType:  storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY,
		Severity:   storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR,
		Title:      "Data quality: no nulls failed",
		Text:       "Status: PASS → FAIL · Observed: 12 rows",
		Link:       "https://app.example.com/data-quality/executions/9",
		OccurredAt: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC),
	}
}

func TestWebhookPostsStableBodyAndSignature(t *testing.T) {
	t.Parallel()

	var (
		gotBody      []byte
		gotSignature string
		gotEvent     string
		gotDelivery  string
		gotContent   string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContent = r.Header.Get("Content-Type")
		gotSignature = r.Header.Get("X-Metaxisdata-Signature")
		gotEvent = r.Header.Get("X-Metaxisdata-Event")
		gotDelivery = r.Header.Get("X-Metaxisdata-Delivery")
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	receiver := NewWebhook(server.URL, "topsecret", true)
	answer, err := receiver.Post(context.Background(), testMessage())
	require.NoError(t, err)
	require.Equal(t, `{"ok":true}`, answer)
	require.Equal(t, "application/json", gotContent)
	require.Equal(t, "DATA_QUALITY", gotEvent)
	require.Equal(t, "42", gotDelivery)

	mac := hmac.New(sha256.New, []byte("topsecret"))
	_, _ = mac.Write(gotBody)
	require.Equal(t, "sha256="+hex.EncodeToString(mac.Sum(nil)), gotSignature)

	var payload webhookPayload
	require.NoError(t, json.Unmarshal(gotBody, &payload))
	require.Equal(t, "evt-1", payload.EventID)
	require.Equal(t, int64(42), payload.DeliveryID)
	require.Equal(t, "DATA_QUALITY", payload.EventType)
	require.Equal(t, "ERROR", payload.Severity)
	require.Equal(t, "2026-10-31T12:00:00Z", payload.OccurredAt)
	require.Equal(t, testMessage().Link, payload.Link)
}

func TestWebhookOmitsSignatureWhenNotConfigured(t *testing.T) {
	t.Parallel()

	var signature string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signature = r.Header.Get("X-Metaxisdata-Signature")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	_, err := NewWebhook(server.URL, "", false).Post(context.Background(), testMessage())
	require.NoError(t, err)
	require.Empty(t, signature)
}

func TestWebhookRejectsNon2xx(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad channel"))
	}))
	defer server.Close()

	_, err := NewWebhook(server.URL, "", false).Post(context.Background(), testMessage())
	require.Error(t, err)
	require.Contains(t, err.Error(), "400")
	require.Contains(t, err.Error(), "bad channel")
}

// A transport error quotes the URL, and for a chat robot the URL is the
// credential. The error that reaches the delivery row must not carry it.
func TestWebhookTransportErrorDoesNotLeakEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL + "/robot/send?access_token=verysecret"
	server.Close()

	_, err := NewWebhook(endpoint, "", false).Post(context.Background(), testMessage())
	require.Error(t, err)
	require.NotContains(t, err.Error(), "verysecret")
	require.NotContains(t, err.Error(), endpoint)
}

func TestValidateChannel(t *testing.T) {
	t.Parallel()

	valid := &storepb.NotificationChannel{
		DisplayName: "on-call",
		Kind:        storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WEBHOOK,
		Config: &storepb.NotificationChannelConfig{
			Config: &storepb.NotificationChannelConfig_Webhook{Webhook: &storepb.WebhookChannelConfig{}},
		},
	}
	require.NoError(t, ValidateChannel(valid, &storepb.NotificationChannelSecret{WebhookUrl: "https://example.com/hook"}))

	require.Error(t, ValidateChannel(valid, nil), "a webhook needs an endpoint")
	require.Error(t, ValidateChannel(valid, &storepb.NotificationChannelSecret{WebhookUrl: "ftp://example.com"}), "scheme")
	require.Error(t, ValidateChannel(valid, &storepb.NotificationChannelSecret{WebhookUrl: "https://"}), "host")
	require.Error(t, ValidateChannel(&storepb.NotificationChannel{Kind: storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WEBHOOK}, &storepb.NotificationChannelSecret{WebhookUrl: "https://example.com"}))
	require.Error(t, ValidateChannel(&storepb.NotificationChannel{}, &storepb.NotificationChannelSecret{WebhookUrl: "https://example.com"}))

	signed := &storepb.NotificationChannel{
		DisplayName: "on-call",
		Kind:        storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WEBHOOK,
		Config: &storepb.NotificationChannelConfig{
			Config: &storepb.NotificationChannelConfig_Webhook{Webhook: &storepb.WebhookChannelConfig{Sign: true}},
		},
	}
	require.Error(t, ValidateChannel(signed, &storepb.NotificationChannelSecret{WebhookUrl: "https://example.com/hook"}))
	require.NoError(t, ValidateChannel(signed, &storepb.NotificationChannelSecret{WebhookUrl: "https://example.com/hook", WebhookSigningSecret: "s3cret"}))
}

// Redirects are not followed: a 307 would re-POST the signed body to the
// endpoint's chosen host, and a 301/302 would silently downgrade the POST to a
// GET with the body dropped.
func TestWebhookDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	var targetHits int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	for _, status := range []int{http.StatusMovedPermanently, http.StatusTemporaryRedirect} {
		redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL+"/stolen", status)
		}))
		_, err := NewWebhook(redirect.URL, "secret", true).Post(context.Background(), testMessage())
		redirect.Close()
		require.Error(t, err)
	}
	require.Zero(t, targetHits, "no redirect target may be contacted")
}

// The transport quotes the parsed URL, so an uppercase scheme must still be
// sanitized even though it does not match the stored string byte for byte.
func TestWebhookSanitizesNormalizedEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := strings.ToUpper("http://") + strings.TrimPrefix(server.URL, "http://") + "/robot?access_token=verysecret"
	server.Close()

	_, err := NewWebhook(endpoint, "", false).Post(context.Background(), testMessage())
	require.Error(t, err)
	require.NotContains(t, err.Error(), "verysecret")
	require.NotContains(t, err.Error(), "access_token")
}
