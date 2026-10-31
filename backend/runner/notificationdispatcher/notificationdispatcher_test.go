package notificationdispatcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type finishedDelivery struct {
	id          int64
	state       storepb.NotificationDeliveryState
	lastError   string
	nextAttempt time.Time
}

type fakeDispatcherStore struct {
	claimed    []*store.NotificationDeliveryMessage
	channels   []*store.NotificationChannelMessage
	channelErr error
	secret     *storepb.NotificationChannelSecret
	setting    *storepb.WorkspaceProfileSetting

	finished []finishedDelivery
	released []int64
}

func (f *fakeDispatcherStore) ClaimNotificationDeliveries(context.Context, int, time.Duration) ([]*store.NotificationDeliveryMessage, error) {
	return f.claimed, nil
}

func (f *fakeDispatcherStore) FinishNotificationDelivery(_ context.Context, id int64, state storepb.NotificationDeliveryState, lastError string, nextAttempt time.Time) error {
	f.finished = append(f.finished, finishedDelivery{id: id, state: state, lastError: lastError, nextAttempt: nextAttempt})
	return nil
}

func (f *fakeDispatcherStore) ListNotificationChannels(context.Context) ([]*store.NotificationChannelMessage, error) {
	return f.channels, f.channelErr
}

func (f *fakeDispatcherStore) ReleaseNotificationDelivery(_ context.Context, id int64) error {
	f.released = append(f.released, id)
	return nil
}

func (f *fakeDispatcherStore) GetNotificationChannelSecret(context.Context, int64) (*storepb.NotificationChannelSecret, error) {
	return f.secret, nil
}

func (f *fakeDispatcherStore) GetWorkspaceGeneralSetting(context.Context) (*storepb.WorkspaceProfileSetting, error) {
	return f.setting, nil
}

func webhookChannelMessage(url string) (*store.NotificationChannelMessage, *storepb.NotificationChannelSecret) {
	channel := &store.NotificationChannelMessage{
		ID:         7,
		ResourceID: "channel7",
		Payload: &storepb.NotificationChannel{
			DisplayName: "on-call",
			Kind:        storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WEBHOOK,
			Enabled:     true,
			Config: &storepb.NotificationChannelConfig{
				Config: &storepb.NotificationChannelConfig_Webhook{Webhook: &storepb.WebhookChannelConfig{}},
			},
		},
	}
	return channel, &storepb.NotificationChannelSecret{WebhookUrl: url}
}

func delivery(id int64, attempts int32, channelID int64) *store.NotificationDeliveryMessage {
	return &store.NotificationDeliveryMessage{
		ID:        id,
		EventID:   "evt-1",
		ChannelID: channelID,
		State:     storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_SENDING,
		Attempts:  attempts,
		CreatedAt: time.Now(),
		Payload: &storepb.NotificationDeliveryEvent{
			Type:     storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY,
			Severity: storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR,
			Detail: &storepb.NotificationDeliveryEvent_DataQuality{DataQuality: &storepb.DataQualityDetail{
				Kind:       storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED,
				RuleTitle:  "no nulls",
				TargetPath: "prod/db/public/orders",
			}},
		},
	}
}

func TestDispatchDeliversWebhook(t *testing.T) {
	t.Parallel()

	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = r.Body.Read(gotBody)
		}
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer server.Close()

	channel, secret := webhookChannelMessage(server.URL)
	fake := &fakeDispatcherStore{
		claimed:  []*store.NotificationDeliveryMessage{delivery(11, 1, channel.ID)},
		channels: []*store.NotificationChannelMessage{channel},
		secret:   secret,
		setting:  &storepb.WorkspaceProfileSetting{ExternalUrl: "https://app.example.com"},
	}
	runner := NewRunner(nil, nil)
	runner.store = fake
	runner.dispatch(context.Background())

	require.Len(t, fake.finished, 1)
	require.Equal(t, storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_SENT, fake.finished[0].state)
	require.Empty(t, fake.finished[0].lastError)
	require.Contains(t, string(gotBody), `"eventId":"evt-1"`)
	require.Contains(t, string(gotBody), `"link":"https://app.example.com/data-quality/executions"`)
}

func TestDispatchRetriesWithBackoff(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer server.Close()

	channel, secret := webhookChannelMessage(server.URL)
	before := time.Now()
	fake := &fakeDispatcherStore{
		claimed:  []*store.NotificationDeliveryMessage{delivery(12, 1, channel.ID)},
		channels: []*store.NotificationChannelMessage{channel},
		secret:   secret,
	}
	runner := NewRunner(nil, nil)
	runner.store = fake
	runner.dispatch(context.Background())

	require.Len(t, fake.finished, 1)
	require.Equal(t, storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_PENDING, fake.finished[0].state)
	require.Contains(t, fake.finished[0].lastError, "500")
	require.WithinDuration(t, before.Add(time.Minute), fake.finished[0].nextAttempt, 10*time.Second)
}

func TestDispatchDeadAfterBudget(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	channel, secret := webhookChannelMessage(server.URL)
	fake := &fakeDispatcherStore{
		claimed:  []*store.NotificationDeliveryMessage{delivery(13, maxAttempts, channel.ID)},
		channels: []*store.NotificationChannelMessage{channel},
		secret:   secret,
	}
	runner := NewRunner(nil, nil)
	runner.store = fake
	runner.dispatch(context.Background())

	require.Len(t, fake.finished, 1)
	require.Equal(t, storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_DEAD, fake.finished[0].state)
}

func TestDispatchDeadWhenChannelGone(t *testing.T) {
	t.Parallel()

	fake := &fakeDispatcherStore{
		claimed: []*store.NotificationDeliveryMessage{delivery(14, 1, 99)},
	}
	runner := NewRunner(nil, nil)
	runner.store = fake
	runner.dispatch(context.Background())

	require.Len(t, fake.finished, 1)
	require.Equal(t, storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_DEAD, fake.finished[0].state)
	require.Contains(t, fake.finished[0].lastError, "no longer exists")
}

func TestDispatchDeadWhenCredentialMissing(t *testing.T) {
	t.Parallel()

	channel, _ := webhookChannelMessage("https://example.com/hook")
	fake := &fakeDispatcherStore{
		claimed:  []*store.NotificationDeliveryMessage{delivery(15, 1, channel.ID)},
		channels: []*store.NotificationChannelMessage{channel},
		secret:   nil,
	}
	runner := NewRunner(nil, nil)
	runner.store = fake
	runner.dispatch(context.Background())

	require.Len(t, fake.finished, 1)
	require.Equal(t, storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_DEAD, fake.finished[0].state)
	require.Contains(t, fake.finished[0].lastError, "no endpoint")
}

// A channel-list read failure must not be mistaken for "every channel is gone":
// the claimed deliveries are released without an attempt and retried next tick.
func TestDispatchReleasesBatchWhenChannelReadFails(t *testing.T) {
	t.Parallel()

	channel, secret := webhookChannelMessage("https://example.com/hook")
	fake := &fakeDispatcherStore{
		claimed: []*store.NotificationDeliveryMessage{
			delivery(21, 1, channel.ID),
			delivery(22, 1, channel.ID),
		},
		channels:   []*store.NotificationChannelMessage{channel},
		channelErr: context.DeadlineExceeded,
		secret:     secret,
	}
	runner := NewRunner(nil, nil)
	runner.store = fake
	runner.dispatch(context.Background())

	require.Empty(t, fake.finished, "no delivery may be finished when the channel list is unreadable")
	require.ElementsMatch(t, []int64{21, 22}, fake.released)
}

// A 3xx answer is a failed attempt: the body was not delivered, so recording SENT
// would lose the notification silently.
func TestDispatchTreatsRedirectAsFailure(t *testing.T) {
	t.Parallel()

	var targetHits int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/stolen", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	channel, secret := webhookChannelMessage(redirect.URL)
	fake := &fakeDispatcherStore{
		claimed:  []*store.NotificationDeliveryMessage{delivery(23, 1, channel.ID)},
		channels: []*store.NotificationChannelMessage{channel},
		secret:   secret,
	}
	runner := NewRunner(nil, nil)
	runner.store = fake
	runner.dispatch(context.Background())

	require.Len(t, fake.finished, 1)
	require.Equal(t, storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_PENDING, fake.finished[0].state)
	require.Contains(t, fake.finished[0].lastError, "307")
	require.Zero(t, targetHits, "the redirect target must never be contacted")
}
