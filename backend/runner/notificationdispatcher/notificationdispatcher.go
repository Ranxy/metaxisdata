// Package notificationdispatcher posts queued outbound notification deliveries.
//
// It is the consumer half of the outbox: the notification service writes an
// inbox row and, in the same call, queues one delivery per subscribed channel;
// this runner leases those rows, renders the event in the channel's language and
// posts it. A delivery that cannot be posted is retried with backoff and becomes
// DEAD after a bounded number of attempts, so a broken endpoint never piles up
// work and never blocks the producer that reported an issue.
package notificationdispatcher

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/runner"
	"github.com/Ranxy/metaxisdata/backend/store"

	"github.com/Ranxy/metaxisdata/backend/component/notification/outbound"
)

const (
	// tick is how often the dispatcher looks for due deliveries.
	tick = 2 * time.Second
	// claimLimit bounds how many deliveries one tick leases.
	claimLimit = 20
	// lease bounds how long a claimed delivery stays SENDING. A dispatcher that
	// crashes mid-post leaves the row SENDING; past the lease another tick
	// reclaims it, so the attempt counter still advances and the budget ends.
	lease = 5 * time.Minute
	// maxAttempts is the automatic attempt budget. It is small on purpose: the
	// backoff already spans hours, and a channel that has been failing all day
	// needs a human, not a seventh try.
	maxAttempts = 6
	// maxConcurrent bounds how many deliveries are posted at once. A chat endpoint
	// that is slow must not let one tick stall every channel.
	maxConcurrent = 4
	// attemptTimeout bounds one post. The receiver's HTTP client has its own,
	// smaller timeout; this also covers the store reads around it.
	attemptTimeout = 15 * time.Second
	// maxErrorBytes bounds the error recorded on a delivery. An endpoint that
	// answers with a page must not be copied into the row.
	maxErrorBytes = 1024
)

// retryBackoff is how long to wait after the n-th failed attempt. The length is
// maxAttempts-1 because the last failure makes the delivery DEAD instead.
var retryBackoff = []time.Duration{
	time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	time.Hour,
	6 * time.Hour,
}

// Store is the slice of the store the dispatcher needs.
type Store interface {
	ClaimNotificationDeliveries(ctx context.Context, limit int, lease time.Duration) ([]*store.NotificationDeliveryMessage, error)
	FinishNotificationDelivery(ctx context.Context, id int64, state storepb.NotificationDeliveryState, lastError string, nextAttempt time.Time) error
	ReleaseNotificationDelivery(ctx context.Context, id int64) error
	ListNotificationChannels(ctx context.Context) ([]*store.NotificationChannelMessage, error)
	GetNotificationChannelSecret(ctx context.Context, channelID int64) (*storepb.NotificationChannelSecret, error)
	GetWorkspaceGeneralSetting(ctx context.Context) (*storepb.WorkspaceProfileSetting, error)
	// GetEmailSettingForDelivery returns the decrypted SMTP configuration, or nil
	// when email delivery is not configured.
	GetEmailSettingForDelivery(ctx context.Context) (*storepb.EmailSetting, error)
	// GetUserByID resolves an audience user id to the address email goes to.
	GetUserByID(ctx context.Context, id int) (*store.UserMessage, error)
	// GetQuietHoursSetting returns the workspace quiet-hours schedule, or nil.
	GetQuietHoursSetting(ctx context.Context) (*storepb.QuietHoursSetting, error)
	// DeferNotificationDelivery reschedules a claimed delivery without charging
	// it an attempt.
	DeferNotificationDelivery(ctx context.Context, id int64, nextAttempt time.Time) error
}

// Runner drives the dispatcher loop.
type Runner struct {
	store   Store
	metrics *metrics.Metrics
}

// NewRunner builds the dispatcher over the server's store.
func NewRunner(stores *store.Store, m *metrics.Metrics) *Runner {
	return &Runner{store: stores, metrics: m}
}

// Name identifies the runner in logs and metrics.
func (*Runner) Name() string {
	return "notificationdispatcher"
}

// Run leases and posts deliveries until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
	ticker := time.NewTicker(runner.Jitter(tick))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.dispatch(ctx)
		}
	}
}

// dispatch posts one batch of due deliveries. The channels and the deployment's
// external URL are read once per batch rather than per delivery: both are small
// and shared by every delivery in it.
func (r *Runner) dispatch(ctx context.Context) {
	deliveries, err := r.store.ClaimNotificationDeliveries(ctx, claimLimit, lease)
	if err != nil {
		slog.Warn("Failed to claim outbound notification deliveries", log.WithError(err))
		return
	}
	if len(deliveries) == 0 {
		return
	}

	channels, err := r.store.ListNotificationChannels(ctx)
	if err != nil {
		// The batch cannot be resolved: release every claimed delivery without
		// charging it an attempt. Treating an unreadable channel list as "every
		// channel is gone" would kill up to a whole tick's deliveries as DEAD.
		slog.Warn("Failed to read the notification channels for a delivery, releasing the batch", log.WithError(err))
		for _, delivery := range deliveries {
			if releaseErr := r.store.ReleaseNotificationDelivery(ctx, delivery.ID); releaseErr != nil {
				slog.Warn("Failed to release an outbound notification delivery",
					slog.Int64("delivery", delivery.ID), log.WithError(releaseErr))
			}
		}
		return
	}
	byID := make(map[int64]*store.NotificationChannelMessage, len(channels))
	for _, channel := range channels {
		byID[channel.ID] = channel
	}

	externalURL := ""
	if setting, err := r.store.GetWorkspaceGeneralSetting(ctx); err != nil {
		slog.Warn("Failed to read the external URL for outbound notifications", log.WithError(err))
	} else {
		externalURL = setting.GetExternalUrl()
	}

	// Quiet hours are evaluated once per batch. A delivery whose channel does not
	// bypass the schedule is put back without charging it an attempt; the first
	// tick after the window ends claims it again.
	quietUntil, quiet := r.quietHours(ctx)

	semaphore := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for _, delivery := range deliveries {
		wg.Go(func() {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			channel := byID[delivery.ChannelID]
			if quiet && channel != nil && !channel.Payload.GetBypassQuietHours() {
				r.deferDelivery(ctx, delivery, quietUntil)
				return
			}
			r.attempt(ctx, delivery, channel, externalURL)
		})
	}
	wg.Wait()
}

// quietHours reads the workspace schedule and reports whether the dispatcher is
// inside a window and when it ends. A read or parse failure fails open: a broken
// schedule must not hold every alert.
func (r *Runner) quietHours(ctx context.Context) (time.Time, bool) {
	setting, err := r.store.GetQuietHoursSetting(ctx)
	if err != nil {
		slog.Warn("Failed to read the quiet hours setting for outbound notifications", log.WithError(err))
		return time.Time{}, false
	}
	until, quiet := outbound.QuietUntil(time.Now(), setting)
	if quiet && !until.After(time.Now()) {
		// A window that ends now is not worth deferring to; attempt normally.
		return time.Time{}, false
	}
	return until, quiet
}

// deferDelivery puts a claimed delivery back to PENDING with its next attempt at
// the end of the quiet window. The claim incremented attempts, so the store
// decrements it back: deferral is scheduling, not a failed post.
func (r *Runner) deferDelivery(ctx context.Context, delivery *store.NotificationDeliveryMessage, until time.Time) {
	if err := r.store.DeferNotificationDelivery(ctx, delivery.ID, until); err != nil {
		slog.Warn("Failed to defer an outbound notification delivery for quiet hours",
			slog.Int64("delivery", delivery.ID), log.WithError(err))
		return
	}
	r.metrics.CountNotificationDelivery(metrics.NotificationDeliveryDeferred)
}

// attempt renders and posts one delivery, then records its terminal or retry
// state. Every failure below the attempt budget goes back to PENDING; a
// configuration error that no retry can fix becomes DEAD at once.
func (r *Runner) attempt(ctx context.Context, delivery *store.NotificationDeliveryMessage, channel *store.NotificationChannelMessage, externalURL string) {
	if channel == nil {
		r.metrics.CountNotificationDelivery(metrics.NotificationDeliveryDead)
		r.finish(ctx, delivery, storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_DEAD, "the notification channel no longer exists", time.Now())
		return
	}
	secret, err := r.store.GetNotificationChannelSecret(ctx, delivery.ChannelID)
	if err != nil {
		r.finishFailure(ctx, delivery, "failed to read the channel credential: "+err.Error())
		return
	}

	// An email channel delivers through the workspace SMTP setting and resolves
	// its recipients at send time: the audience ids are the people the event's
	// in-app message addressed, so a changed or deleted account is handled when
	// the delivery runs rather than when it was queued.
	var emailSetting *storepb.EmailSetting
	var recipients []string
	if channel.Payload.GetKind() == storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_EMAIL {
		emailSetting, err = r.store.GetEmailSettingForDelivery(ctx)
		if err != nil {
			r.finishFailure(ctx, delivery, "failed to read the email setting: "+err.Error())
			return
		}
		if emailSetting == nil {
			// A configuration gap an administrator can close: retry with the normal
			// backoff instead of burning the delivery, and record why it waits.
			r.finishFailure(ctx, delivery, "email delivery is not configured")
			return
		}
		recipients, err = outbound.EmailRecipients(ctx, channel.Payload.GetConfig().GetEmail(), delivery.Payload.GetAudienceUserIds(), r.userEmail)
		if err != nil {
			r.finishFailure(ctx, delivery, err.Error())
			return
		}
		if len(recipients) == 0 {
			// No address could be resolved (for example the only audience member was
			// deleted and the channel lists no fixed address): retrying cannot
			// invent one, so the delivery is dead and the reason is visible.
			r.metrics.CountNotificationDelivery(metrics.NotificationDeliveryDead)
			r.finish(ctx, delivery, storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_DEAD, "no email recipient could be resolved", time.Now())
			return
		}
	}

	receiver, err := outbound.NewReceiver(channel.Payload, secret, emailSetting)
	if err != nil {
		r.metrics.CountNotificationDelivery(metrics.NotificationDeliveryDead)
		r.finish(ctx, delivery, storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_DEAD, truncateError(err.Error()), time.Now())
		return
	}

	message := outbound.Render(channel.Payload.GetLanguage(), delivery.Payload, externalURL, delivery.CreatedAt)
	message.EventID = delivery.EventID
	message.DeliveryID = delivery.ID
	message.Recipients = recipients

	attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()
	started := time.Now()
	_, postErr := receiver.Post(attemptCtx, message)
	r.metrics.ObserveNotificationDeliveryDuration(time.Since(started))

	if postErr != nil {
		r.finishFailure(ctx, delivery, postErr.Error())
		return
	}
	r.metrics.CountNotificationDelivery(metrics.NotificationDeliverySent)
	r.finish(ctx, delivery, storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_SENT, "", time.Now())
}

// userEmail resolves one audience user id to its address. A missing or deleted
// account yields an empty address, which the resolver skips: the event's other
// recipients still receive the message, and a deleted account's old address is
// not mailed.
func (r *Runner) userEmail(ctx context.Context, userID int32) (string, error) {
	user, err := r.store.GetUserByID(ctx, int(userID))
	if err != nil {
		return "", err
	}
	if user == nil || user.MemberDeleted {
		return "", nil
	}
	return user.Email, nil
}

// finishFailure records a failed attempt: DEAD once the budget is spent, PENDING
// with the next backoff step otherwise.
func (r *Runner) finishFailure(ctx context.Context, delivery *store.NotificationDeliveryMessage, lastError string) {
	now := time.Now()
	if delivery.Attempts >= maxAttempts {
		r.metrics.CountNotificationDelivery(metrics.NotificationDeliveryDead)
		slog.Warn("Outbound notification delivery exhausted its attempts",
			slog.Int64("delivery", delivery.ID),
			slog.Int64("channel", delivery.ChannelID),
			slog.String("error", truncateError(lastError)))
		r.finish(ctx, delivery, storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_DEAD, truncateError(lastError), now)
		return
	}
	r.metrics.CountNotificationDelivery(metrics.NotificationDeliveryFailed)
	next := now.Add(retryBackoff[min(max(int(delivery.Attempts), 1)-1, len(retryBackoff)-1)])
	r.finish(ctx, delivery, storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_PENDING, truncateError(lastError), next)
}

// finish writes the delivery's new state. A write failure is logged: the lease
// expires and the row is reclaimed, which is the same recovery as a crash.
func (r *Runner) finish(ctx context.Context, delivery *store.NotificationDeliveryMessage, state storepb.NotificationDeliveryState, lastError string, nextAttempt time.Time) {
	if err := r.store.FinishNotificationDelivery(ctx, delivery.ID, state, lastError, nextAttempt); err != nil {
		slog.Warn("Failed to record the outbound notification delivery state",
			slog.Int64("delivery", delivery.ID),
			slog.String("state", state.String()),
			log.WithError(err))
	}
}

// truncateError bounds the stored error without splitting a rune: PostgreSQL
// rejects invalid UTF-8, and a rejected finish would leave the delivery SENDING
// forever, re-posting every lease period.
func truncateError(value string) string {
	if len(value) <= maxErrorBytes {
		return value
	}
	return common.TruncateUTF8Bytes(value, maxErrorBytes) + "…"
}
