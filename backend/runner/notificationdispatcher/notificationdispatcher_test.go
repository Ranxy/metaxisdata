package notificationdispatcher

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

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
	claimed       []*store.NotificationDeliveryMessage
	channels      []*store.NotificationChannelMessage
	channelErr    error
	secret        *storepb.NotificationChannelSecret
	setting       *storepb.WorkspaceProfileSetting
	email         *storepb.EmailSetting
	quietHours    *storepb.QuietHoursSetting
	quietHoursErr error
	users         map[int]*store.UserMessage

	finished []finishedDelivery
	deferred []deferredDelivery
	released []int64
}

type deferredDelivery struct {
	id          int64
	nextAttempt time.Time
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

func (f *fakeDispatcherStore) GetEmailSettingForDelivery(context.Context) (*storepb.EmailSetting, error) {
	return f.email, nil
}

func (f *fakeDispatcherStore) GetQuietHoursSetting(context.Context) (*storepb.QuietHoursSetting, error) {
	return f.quietHours, f.quietHoursErr
}

func (f *fakeDispatcherStore) DeferNotificationDelivery(_ context.Context, id int64, nextAttempt time.Time) error {
	f.deferred = append(f.deferred, deferredDelivery{id: id, nextAttempt: nextAttempt})
	return nil
}

func (f *fakeDispatcherStore) GetUserByID(_ context.Context, id int) (*store.UserMessage, error) {
	return f.users[id], nil
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
	return channel, &storepb.NotificationChannelSecret{Endpoint: url}
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

// emailChannelMessage is an email channel listing the given fixed addresses.
func emailChannelMessage(addresses []string, includeAudience bool) *store.NotificationChannelMessage {
	return &store.NotificationChannelMessage{
		ID:         8,
		ResourceID: "channel8",
		Payload: &storepb.NotificationChannel{
			DisplayName: "email",
			Kind:        storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_EMAIL,
			Enabled:     true,
			Config: &storepb.NotificationChannelConfig{
				Config: &storepb.NotificationChannelConfig_Email{Email: &storepb.EmailChannelConfig{
					Addresses:       addresses,
					IncludeAudience: includeAudience,
				}},
			},
		},
	}
}

// emailSetting is an SMTP configuration pointing at the given server.
func emailSetting(host string, port int32) *storepb.EmailSetting {
	return &storepb.EmailSetting{
		From:     "alerts@example.com",
		FromName: "Metaxisdata",
		Smtp: &storepb.EmailSMTPConfig{
			Host:           host,
			Port:           port,
			Encryption:     storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_NONE,
			Authentication: storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_NONE,
		},
	}
}

// fakeSMTPServer speaks enough SMTP for one net/smtp transaction and reports the
// DATA section it received. An address in rejected is answered 550, which is how
// a relay refuses one stale mailbox while accepting the rest.
func fakeSMTPServer(t *testing.T, rejected ...string) (string, int32, <-chan string) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)
	rejectedAddresses := make(map[string]struct{}, len(rejected))
	for _, address := range rejected {
		rejectedAddresses[address] = struct{}{}
	}

	messages := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		reader := bufio.NewReader(conn)
		writer := bufio.NewWriter(conn)
		write := func(line string) {
			_, _ = writer.WriteString(line + "\r\n")
			_ = writer.Flush()
		}
		write("220 localhost ESMTP")
		var body strings.Builder
		inData := false
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if inData {
				if line == "." {
					inData = false
					messages <- body.String()
					write("250 OK")
					continue
				}
				_, _ = body.WriteString(line)
				_, _ = body.WriteString("\r\n")
				continue
			}
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
				write("250-localhost")
				write("250 OK")
			case strings.HasPrefix(line, "MAIL FROM"):
				write("250 OK")
			case strings.HasPrefix(line, "RCPT TO"):
				refused := false
				for address := range rejectedAddresses {
					if strings.Contains(line, address) {
						refused = true
						break
					}
				}
				if refused {
					write("550 no such user")
					continue
				}
				write("250 OK")
			case line == "DATA":
				inData = true
				write("354 End data with <CR><LF>.<CR><LF>")
			case line == "QUIT":
				write("221 Bye")
				return
			default:
				write("250 OK")
			}
		}
	}()
	return host, int32(port), messages
}

// An email delivery waits while the workspace SMTP setting is missing: the gap is
// configuration an administrator can close, not a permanent failure.
func TestDispatchEmailNotConfiguredRetries(t *testing.T) {
	t.Parallel()

	channel := emailChannelMessage([]string{"oncall@example.com"}, false)
	fake := &fakeDispatcherStore{
		claimed:  []*store.NotificationDeliveryMessage{delivery(31, 1, channel.ID)},
		channels: []*store.NotificationChannelMessage{channel},
	}
	runner := NewRunner(nil, nil)
	runner.store = fake
	runner.dispatch(context.Background())

	require.Len(t, fake.finished, 1)
	require.Equal(t, storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_PENDING, fake.finished[0].state)
	require.Contains(t, fake.finished[0].lastError, "not configured")
}

// An email channel whose only recipient source is an audience that resolves to
// nobody cannot recover by retrying, so the delivery is dead and says why.
func TestDispatchEmailWithoutResolvableRecipientIsDead(t *testing.T) {
	t.Parallel()

	host, port, _ := fakeSMTPServer(t)
	channel := emailChannelMessage(nil, true)
	message := delivery(32, 1, channel.ID)
	message.Payload.AudienceUserIds = []int32{7}
	fake := &fakeDispatcherStore{
		claimed:  []*store.NotificationDeliveryMessage{message},
		channels: []*store.NotificationChannelMessage{channel},
		email:    emailSetting(host, port),
		users:    map[int]*store.UserMessage{},
	}
	runner := NewRunner(nil, nil)
	runner.store = fake
	runner.dispatch(context.Background())

	require.Len(t, fake.finished, 1)
	require.Equal(t, storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_DEAD, fake.finished[0].state)
	require.Contains(t, fake.finished[0].lastError, "no email recipient")
}

// One email delivery carries the fixed addresses and the event's audience, and a
// completed SMTP transaction is the receipt.
func TestDispatchSendsEmail(t *testing.T) {
	t.Parallel()

	host, port, messages := fakeSMTPServer(t)
	channel := emailChannelMessage([]string{"oncall@example.com"}, true)
	message := delivery(33, 1, channel.ID)
	message.Payload.AudienceUserIds = []int32{7}
	fake := &fakeDispatcherStore{
		claimed:  []*store.NotificationDeliveryMessage{message},
		channels: []*store.NotificationChannelMessage{channel},
		email:    emailSetting(host, port),
		users:    map[int]*store.UserMessage{7: {ID: 7, Email: "owner@example.com"}},
	}
	runner := NewRunner(nil, nil)
	runner.store = fake
	runner.dispatch(context.Background())

	require.Len(t, fake.finished, 1)
	require.Equal(t, storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_SENT, fake.finished[0].state)
	body := <-messages
	// Recipients are envelope-only, so the shared mailbox does not learn the
	// internal owner's address (and vice versa).
	require.Contains(t, body, "To: undisclosed-recipients:;")
	require.NotContains(t, body, "oncall@example.com")
	require.NotContains(t, body, "owner@example.com")
	require.Contains(t, body, "From: Metaxisdata <alerts@example.com>")
	require.Contains(t, body, "Subject:")
}

// One stale fixed address must not cancel the alert for everyone else: the
// rejected recipient is skipped and the accepted ones still receive the message.
func TestDispatchSendsEmailPastARejectedRecipient(t *testing.T) {
	t.Parallel()

	host, port, messages := fakeSMTPServer(t, "stale@example.com")
	channel := emailChannelMessage([]string{"stale@example.com", "oncall@example.com"}, false)
	fake := &fakeDispatcherStore{
		claimed:  []*store.NotificationDeliveryMessage{delivery(34, 1, channel.ID)},
		channels: []*store.NotificationChannelMessage{channel},
		email:    emailSetting(host, port),
	}
	runner := NewRunner(nil, nil)
	runner.store = fake
	runner.dispatch(context.Background())

	require.Len(t, fake.finished, 1)
	require.Equal(t, storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_SENT, fake.finished[0].state)
	body := <-messages
	require.Contains(t, body, "Subject:")
	require.NotContains(t, body, "stale@example.com")
}

// A delivery fails only when the server accepts no recipient; the error names no
// address, so a mistyped mailbox is not copied into last_error.
func TestDispatchEmailFailsWhenEveryRecipientIsRejected(t *testing.T) {
	t.Parallel()

	host, port, _ := fakeSMTPServer(t, "oncall@example.com")
	channel := emailChannelMessage([]string{"oncall@example.com"}, false)
	fake := &fakeDispatcherStore{
		claimed:  []*store.NotificationDeliveryMessage{delivery(35, 1, channel.ID)},
		channels: []*store.NotificationChannelMessage{channel},
		email:    emailSetting(host, port),
	}
	runner := NewRunner(nil, nil)
	runner.store = fake
	runner.dispatch(context.Background())

	require.Len(t, fake.finished, 1)
	require.Equal(t, storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_PENDING, fake.finished[0].state)
	require.Contains(t, fake.finished[0].lastError, "rejected every recipient")
	require.NotContains(t, fake.finished[0].lastError, "oncall@example.com")
}

// quietNow is a schedule that is active right now, expressed as a maintenance
// window so the test does not depend on the current weekday.
func quietNow() *storepb.QuietHoursSetting {
	now := time.Now()
	return &storepb.QuietHoursSetting{
		Enabled:  true,
		Timezone: "UTC",
		MaintenanceWindows: []*storepb.MaintenanceWindow{{
			Start: timestamppb.New(now.Add(-time.Hour)),
			End:   timestamppb.New(now.Add(time.Hour)),
		}},
	}
}

// A quiet-window delivery is rescheduled to the end of the window without an
// attempt: the endpoint is never contacted and the delivery is not charged.
func TestDispatchDefersQuietHours(t *testing.T) {
	t.Parallel()

	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer server.Close()

	channel, secret := webhookChannelMessage(server.URL)
	fake := &fakeDispatcherStore{
		claimed:    []*store.NotificationDeliveryMessage{delivery(41, 1, channel.ID)},
		channels:   []*store.NotificationChannelMessage{channel},
		secret:     secret,
		quietHours: quietNow(),
	}
	runner := NewRunner(nil, nil)
	runner.store = fake
	before := time.Now()
	runner.dispatch(context.Background())

	require.Zero(t, hits, "the endpoint must not be contacted during the window")
	require.Empty(t, fake.finished)
	require.Len(t, fake.deferred, 1)
	require.Equal(t, int64(41), fake.deferred[0].id)
	require.WithinDuration(t, before.Add(time.Hour), fake.deferred[0].nextAttempt, time.Minute)
}

// A channel that bypasses quiet hours keeps delivering during the window.
func TestDispatchQuietHoursBypassChannel(t *testing.T) {
	t.Parallel()

	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer server.Close()

	channel, secret := webhookChannelMessage(server.URL)
	channel.Payload.BypassQuietHours = true
	fake := &fakeDispatcherStore{
		claimed:    []*store.NotificationDeliveryMessage{delivery(42, 1, channel.ID)},
		channels:   []*store.NotificationChannelMessage{channel},
		secret:     secret,
		quietHours: quietNow(),
	}
	runner := NewRunner(nil, nil)
	runner.store = fake
	runner.dispatch(context.Background())

	require.Equal(t, 1, hits)
	require.Empty(t, fake.deferred)
	require.Len(t, fake.finished, 1)
	require.Equal(t, storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_SENT, fake.finished[0].state)
}
