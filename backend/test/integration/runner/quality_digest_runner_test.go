//go:build integration

package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common/crypto"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/notification"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/runner/notificationdispatcher"
	"github.com/Ranxy/metaxisdata/backend/runner/qualitydigestrunner"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// webhookDelivery is the subset of a generic webhook's body the digest test
// reads. The shape is the delivery contract, not a test-only invention.
type webhookDelivery struct {
	EventID   string `json:"eventId"`
	EventType string `json:"eventType"`
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	Text      string `json:"text"`
	Link      string `json:"link"`
}

// The weekly digest's end-to-end path against a real store and a real dispatcher:
// the workspace enables the schedule, one channel explicitly subscribes to the
// digest type, the digest runner's startup pass aggregates the most recent
// scheduled period and queues one delivery for it, and the dispatcher posts it to
// the channel's endpoint.
func TestQualityDigestRunnerPostsTheWeeklyDigestToASubscribedChannel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	stores := newIsolatedMetadataStore(ctx, t)

	// A production channel's endpoint is sealed with the deployment data key. The
	// isolated database has no key yet, so install one for the test.
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	cipher, err := crypto.NewCipher(key)
	require.NoError(t, err)
	stores.SetCipher(cipher)

	// The schedule fired ninety minutes ago in UTC, which is inside the runner's
	// 24h catch-up window.
	scheduled := time.Now().UTC().Add(-90 * time.Minute).Truncate(time.Minute)
	profile := &storepb.WorkspaceProfileSetting{
		DataQuality: &storepb.DataQualitySetting{
			WeeklyDigest: &storepb.QualityDigestSetting{
				Enabled:   true,
				DayOfWeek: dataquality.DigestDayOfWeek(scheduled.Weekday()),
				Time:      scheduled.Format("15:04"),
				Timezone:  "UTC",
			},
		},
	}
	raw, err := protojson.Marshal(profile)
	require.NoError(t, err)
	_, err = stores.UpsertSetting(ctx, &store.SetSettingMessage{
		Name:  storepb.SettingName_WORKSPACE_PROFILE,
		Value: string(raw),
	})
	require.NoError(t, err)

	posted := make(chan webhookDelivery, 4)
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body webhookDelivery
		if decodeErr := json.NewDecoder(r.Body).Decode(&body); decodeErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case posted <- body:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer webhook.Close()

	channel, err := stores.CreateNotificationChannel(ctx, &store.CreateNotificationChannelMessage{
		Payload: &storepb.NotificationChannel{
			DisplayName: "weekly digest",
			Kind:        storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WEBHOOK,
			Enabled:     true,
			EventTypes:  []storepb.NotificationType{storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY_DIGEST},
			Language:    "en-US",
			Config: &storepb.NotificationChannelConfig{
				Config: &storepb.NotificationChannelConfig_Webhook{Webhook: &storepb.WebhookChannelConfig{}},
			},
		},
		Secret: &storepb.NotificationChannelSecret{Endpoint: webhook.URL},
	})
	require.NoError(t, err)

	// A second channel subscribed to the other quality type must not receive the
	// digest: subscription to it is opt-in.
	_, err = stores.CreateNotificationChannel(ctx, &store.CreateNotificationChannelMessage{
		Payload: &storepb.NotificationChannel{
			DisplayName: "issue alerts",
			Kind:        storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WEBHOOK,
			Enabled:     true,
			EventTypes:  []storepb.NotificationType{storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY},
			Config: &storepb.NotificationChannelConfig{
				Config: &storepb.NotificationChannelConfig_Webhook{Webhook: &storepb.WebhookChannelConfig{}},
			},
		},
	})
	require.NoError(t, err)

	runCtx, stopRunners := context.WithCancel(ctx)
	var runners sync.WaitGroup
	runners.Add(2)
	go func() {
		defer runners.Done()
		qualitydigestrunner.NewRunner(stores, notification.New(stores)).Run(runCtx)
	}()
	go func() {
		defer runners.Done()
		notificationdispatcher.NewRunner(stores, nil).Run(runCtx)
	}()
	t.Cleanup(func() {
		stopRunners()
		stopped := make(chan struct{})
		go func() {
			runners.Wait()
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-time.After(10 * time.Second):
			t.Error("the runners did not stop")
		}
	})

	year, week := scheduled.ISOWeek()
	periodKey := fmt.Sprintf("%d-W%02d", year, week)

	var delivery webhookDelivery
	select {
	case delivery = <-posted:
	case <-time.After(45 * time.Second):
		t.Fatal("the weekly digest was never posted to the subscribed channel")
	}
	require.Equal(t, "dqdigest:"+periodKey, delivery.EventID, "the event identity is the period")
	require.Equal(t, "DATA_QUALITY_DIGEST", delivery.EventType)
	require.Equal(t, "INFO", delivery.Severity, "a workspace with no quality data had a clean week")
	require.Contains(t, delivery.Title, "Data quality weekly digest")
	require.Contains(t, delivery.Text, "Executions: 0 completed")
	require.Contains(t, delivery.Text, "Coverage: 0 targets · 0 checks")
	require.Empty(t, delivery.Link, "no external URL means no link")

	// The period is recorded once, and exactly one delivery was queued, for the
	// subscribed channel only, carrying the aggregated detail.
	recorded, err := stores.QualityDigestSendRecorded(ctx, periodKey)
	require.NoError(t, err)
	require.True(t, recorded)

	// The dispatcher posts the row and then records the outcome. The row has to end
	// SENT on its first attempt: a delivery that only succeeds through retries or a
	// reclaimed lease would still be a bug in the delivery path.
	var sent *store.NotificationDeliveryMessage
	require.Eventually(t, func() bool {
		deliveries, listErr := stores.ListNotificationDeliveries(ctx, store.NotificationDeliveryFilter{Limit: 10})
		if listErr != nil || len(deliveries) != 1 {
			return false
		}
		if deliveries[0].State != storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_SENT {
			return false
		}
		sent = deliveries[0]
		return true
	}, 15*time.Second, 100*time.Millisecond, "the digest delivery never reached SENT")
	require.Equal(t, int32(1), sent.Attempts, "the first attempt delivered it; no lease reclaim or retry")
	require.Equal(t, channel.ID, sent.ChannelID)
	require.Equal(t, "dqdigest:"+periodKey, sent.EventID)
	require.Equal(t, storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY_DIGEST, sent.Payload.GetType())
	detail := sent.Payload.GetDataQualityDigest()
	require.NotNil(t, detail)
	require.True(t, detail.GetPeriodEnd().AsTime().Equal(scheduled))
	require.True(t, detail.GetPeriodStart().AsTime().Equal(scheduled.Add(-7*24*time.Hour)))
	require.Equal(t, int32(0), detail.GetExecutionCount())
	require.Equal(t, int32(0), detail.GetOpenIssueCount())
	require.Equal(t, "UTC", detail.GetTimezone(), "the schedule's zone travels with the period label")

	// A restart inside the same week re-runs the runner against the recorded
	// period: it aggregates nothing, queues nothing, and the delivery row stays
	// single. (The store's unique indexes are the durable half of this; the test
	// proves the runner does not even attempt a second enqueue.)
	secondCtx, stopSecond := context.WithCancel(ctx)
	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		qualitydigestrunner.NewRunner(stores, notification.New(stores)).Run(secondCtx)
	}()
	time.Sleep(500 * time.Millisecond)
	stopSecond()
	<-secondDone

	var deliveriesAfterRestart int
	require.NoError(t, stores.GetDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notification_delivery WHERE event_id = $1`,
		"dqdigest:"+periodKey).Scan(&deliveriesAfterRestart))
	require.Equal(t, 1, deliveriesAfterRestart, "a second process must not queue the same week again")
	var recordedPeriods int
	require.NoError(t, stores.GetDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM quality_digest_send WHERE period_key = $1`, periodKey).Scan(&recordedPeriods))
	require.Equal(t, 1, recordedPeriods)

	// A digest is delivery-only: it writes no inbox row for anybody.
	var inboxRows int
	require.NoError(t, stores.GetDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM notification`).Scan(&inboxRows))
	require.Zero(t, inboxRows)
}
