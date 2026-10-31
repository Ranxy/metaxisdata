// Package qualitydigestrunner sends the workspace's weekly data quality digest.
//
// The digest is a scheduled summary of one week of quality work: how the checks
// ran, how the problem ledger moved, and which owners and targets carry the open
// state. It has no in-app copy; it is queued as an outbound notification event
// for the channels that subscribed to it, and the dispatcher posts it like every
// other outbound message.
//
// Exactly one digest is queued per scheduled period. The period key (the ISO week
// of the scheduled instant in the schedule's time zone) is recorded in
// quality_digest_send after a successful enqueue, so a second tick, a restart in
// the middle of the schedule hour or a catch-up pass after a short outage cannot
// send the same week twice. An enqueue that fails leaves the period unrecorded
// so the next tick inside the catch-up window retries it.
package qualitydigestrunner

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	clog "github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// tickInterval is how often the runner asks whether a digest is due. The
	// question costs one indexed probe once a week and nothing the rest of the
	// time, so it is a cheap tick rather than a jittered full pass.
	tickInterval = time.Minute
	// catchUpWindow is how late a digest may be sent. A tick that misses the
	// scheduled minute still sends it, while a period missed by more than a day is
	// skipped: a weekly summary that is days stale is noise, not a report.
	catchUpWindow = 24 * time.Hour
	// periodLength is the window a digest covers, ending at the scheduled instant.
	periodLength = 7 * 24 * time.Hour
)

// Store is the slice of the store the digest runner needs.
type Store interface {
	// QualityDigestSendRecorded reports whether one period was already queued.
	QualityDigestSendRecorded(ctx context.Context, periodKey string) (bool, error)
	// QualityDigestSnapshot aggregates the period's executions, issues, owners and
	// failing targets.
	QualityDigestSnapshot(ctx context.Context, since, until time.Time) (*store.QualityDigestSnapshot, error)
	// TryRecordQualityDigestSend records one period after its digest was queued.
	TryRecordQualityDigestSend(ctx context.Context, periodKey string, channelCount int) (bool, error)
}

// Notifier queues one digest event for the outbound channels that subscribe to
// it and reports how many were queued.
type Notifier interface {
	EnqueueDataQualityDigest(ctx context.Context, periodKey string, severity storepb.NotificationSeverity, detail *storepb.DataQualityDigestDetail) (int, error)
}

// settingsLoader reads the workspace data quality settings. It is a function so
// the schedule logic can be tested without a database.
type settingsLoader func(ctx context.Context) (dataquality.Settings, error)

// Runner sends the weekly digest on the workspace's schedule.
type Runner struct {
	store        Store
	notifier     Notifier
	loadSettings settingsLoader
	now          func() time.Time
	// handledPeriodKey is the last period this process reached a decision about:
	// queued, already recorded, or skipped as stale. A tick that sees the same
	// period does no store read, and the stale warning is logged once per period
	// rather than every minute for the rest of the week. It is process state only
	// (like every other runner's), so a restart re-reads one period from the store
	// and converges.
	handledPeriodKey string
	// warnedPeriodKey and warnedFailures remember which failure kinds were already
	// warned about for the current period, so a persistent store or channel failure
	// keeps retrying every minute but writes one warning instead of a day of them.
	warnedPeriodKey string
	warnedFailures  map[string]struct{}
}

// NewRunner builds the digest runner over the server's store and notification
// service.
func NewRunner(stores *store.Store, notifier Notifier) *Runner {
	return &Runner{
		store:    stores,
		notifier: notifier,
		loadSettings: func(ctx context.Context) (dataquality.Settings, error) {
			return dataquality.LoadSettings(ctx, stores)
		},
		now: time.Now,
	}
}

// Name identifies the runner in logs and metrics.
func (*Runner) Name() string {
	return "qualitydigest"
}

// Run passes once at startup and then once per tick until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
	// A catch-up digest is due as soon as the process starts: a restart during the
	// schedule hour must not wait a whole tick, and the period guard makes a
	// second pass for the same week a no-op.
	r.guardPass(ctx)
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.guardPass(ctx)
		}
	}
}

// guardPass isolates one pass. runner.Go's panic boundary covers the whole loop,
// so an uncaught panic here would stop the digest forever; a pass that panics
// leaves its period unrecorded, so the next tick retries it.
func (r *Runner) guardPass(ctx context.Context) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("weekly data quality digest pass panicked",
				slog.Any("panic", recovered), clog.Stack("panic-stack"))
		}
	}()
	r.pass(ctx)
}

// warnFailure logs one failure kind for one period at most once. The retry stays
// on every tick, because a transient failure should recover immediately; only the
// warning is rate-limited, so a week-long outage writes a handful of lines
// instead of one a minute. The map is reset when the period changes, so its size
// is bounded by the number of failure kinds.
func (r *Runner) warnFailure(periodKey, kind, message string, err error) {
	if r.warnedPeriodKey != periodKey {
		r.warnedPeriodKey = periodKey
		r.warnedFailures = make(map[string]struct{}, 4)
	}
	if _, warned := r.warnedFailures[kind]; warned {
		return
	}
	r.warnedFailures[kind] = struct{}{}
	slog.Warn(message, slog.String("period_key", periodKey), clog.WithError(err))
}

// pass sends the digest of the most recent scheduled period when that digest has
// not been queued yet.
//
// The snapshot's execution window is keyed on started_at, the same axis the
// overview uses. An execution that starts inside the period but is still running
// when the digest aggregates is not counted here (no verdict yet) and is not
// counted by the next digest either (it started before that window), so it falls
// between the two. Delaying aggregation by a settle window would trade that for a
// different skew and a late digest; the started-at attribution is the accepted
// behaviour.
func (r *Runner) pass(ctx context.Context) {
	settings, err := r.loadSettings(ctx)
	if err != nil {
		slog.Warn("Failed to load the data quality settings for the weekly digest", clog.WithError(err))
		return
	}
	if !settings.Digest.Enabled {
		return
	}
	now := r.now()
	period := mostRecentDigest(settings.Digest, now)
	if period.periodKey == "" || period.periodKey == r.handledPeriodKey {
		return
	}

	recorded, err := r.store.QualityDigestSendRecorded(ctx, period.periodKey)
	if err != nil {
		r.warnFailure(period.periodKey, "read", "Failed to read the weekly digest period", err)
		return
	}
	if recorded {
		r.handledPeriodKey = period.periodKey
		return
	}
	if !period.dueAt(now) {
		// A period older than the catch-up window is deliberately not backfilled: a
		// weekly summary days late is noise. The warning is the only trace of an
		// outage that outlived the window, so it fires once per period.
		slog.Warn("Skipping a stale weekly data quality digest period",
			slog.String("period_key", period.periodKey),
			slog.Time("scheduled", period.until),
			slog.Duration("catch_up_window", catchUpWindow))
		r.handledPeriodKey = period.periodKey
		return
	}

	snapshot, err := r.store.QualityDigestSnapshot(ctx, period.since, period.until)
	if err != nil {
		r.warnFailure(period.periodKey, "snapshot", "Failed to aggregate the weekly digest period", err)
		return
	}
	severity := digestSeverity(snapshot)
	channels, err := r.notifier.EnqueueDataQualityDigest(ctx, period.periodKey, severity, digestDetail(period, snapshot, settings.Digest.Timezone))
	if err != nil {
		// The period is left unrecorded on purpose: the enqueue failed, so the next
		// tick inside the catch-up window retries the pass. The delivery event id is
		// derived from the period, so a retry after a partial success cannot post
		// the same digest twice.
		r.warnFailure(period.periodKey, "enqueue", "Failed to queue the weekly data quality digest", err)
		return
	}
	if _, err := r.store.TryRecordQualityDigestSend(ctx, period.periodKey, channels); err != nil {
		r.warnFailure(period.periodKey, "record", "Failed to record the weekly digest period", err)
		return
	}
	r.handledPeriodKey = period.periodKey
	slog.Info("Queued the weekly data quality digest",
		slog.String("period_key", period.periodKey),
		slog.Int("channels", channels),
		slog.String("severity", severity.String()))
}

// digestPeriod is one scheduled digest: the window it covers and the ISO week
// key that identifies it.
type digestPeriod struct {
	since time.Time
	until time.Time
	// periodKey is the ISO week of the scheduled instant in the schedule's time
	// zone, for example 2026-W44. It names the send, not the covered window: a
	// digest window is a rolling seven days and does not align with ISO weeks.
	periodKey string
}

// mostRecentDigest returns the most recent scheduled slot at or before now. The
// zero value means no slot fell in the last week, which only a pathological zone
// offset could produce.
func mostRecentDigest(schedule dataquality.DigestSchedule, now time.Time) digestPeriod {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		// The settings resolver only keeps a valid zone; a value written out of
		// band falls back to UTC rather than making the digest disappear.
		location = time.UTC
	}
	local := now.In(location)
	// Eight days cover the last scheduled slot in every case, including a week in
	// which the configured weekday also carried a daylight-saving transition.
	for offset := 0; offset < 8; offset++ {
		day := local.AddDate(0, 0, -offset)
		candidate := time.Date(day.Year(), day.Month(), day.Day(), schedule.Hour, schedule.Minute, 0, 0, location)
		if candidate.Weekday() != schedule.Day || candidate.After(now) {
			continue
		}
		return digestPeriod{
			since:     candidate.Add(-periodLength),
			until:     candidate,
			periodKey: isoWeekKey(candidate),
		}
	}
	return digestPeriod{}
}

// dueAt reports whether the period is still inside the catch-up window. A slot
// missed by more than catchUpWindow is skipped rather than backfilled.
func (p digestPeriod) dueAt(now time.Time) bool {
	return !p.until.IsZero() && now.Sub(p.until) <= catchUpWindow
}

// isoWeekKey formats the ISO 8601 week of one instant in its own location.
func isoWeekKey(at time.Time) string {
	year, week := at.ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}

// digestSeverity ranks one period for the delivery envelope: an open error or
// critical problem, or a check that could not run, is ERROR; a failed check or
// any open problem is WARNING; a clean week is INFO.
func digestSeverity(snapshot *store.QualityDigestSnapshot) storepb.NotificationSeverity {
	switch {
	case snapshot.OpenErrorCount > 0 || snapshot.OpenCriticalCount > 0 || snapshot.ErroredCount > 0:
		return storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR
	case snapshot.FailedCount > 0 || snapshot.OpenIssueCount > 0:
		return storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING
	default:
		return storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO
	}
}

// digestDetail maps one aggregated period onto the message the channels render.
// ExecutionCount is the snapshot's sum of the four verdicts by definition: a
// SKIPPED or NOT_EVALUATED execution observed no data, so it is not part of the
// headline number nor of any tally the reader can add up. Timezone is the
// schedule's zone: the period label is only meaningful in the zone the slot was
// evaluated in, and the renderer must not fall back to UTC for a UTC+10..+14
// schedule.
func digestDetail(period digestPeriod, snapshot *store.QualityDigestSnapshot, timezone string) *storepb.DataQualityDigestDetail {
	detail := &storepb.DataQualityDigestDetail{
		PeriodStart:         timestamppb.New(period.since),
		PeriodEnd:           timestamppb.New(period.until),
		Timezone:            timezone,
		TargetCount:         snapshot.TargetCount,
		RuleCount:           snapshot.RuleCount,
		ExecutionCount:      snapshot.ExecutionCount,
		PassedCount:         snapshot.PassedCount,
		WarnedCount:         snapshot.WarnedCount,
		FailedCount:         snapshot.FailedCount,
		ErroredCount:        snapshot.ErroredCount,
		NewIssueCount:       snapshot.NewIssueCount,
		RecoveredIssueCount: snapshot.RecoveredIssueCount,
		OpenIssueCount:      snapshot.OpenIssueCount,
		OpenWarningCount:    snapshot.OpenWarningCount,
		OpenErrorCount:      snapshot.OpenErrorCount,
		OpenCriticalCount:   snapshot.OpenCriticalCount,
	}
	for _, owner := range snapshot.Owners {
		digestOwner := &storepb.DataQualityDigestOwner{
			OwnerName:        owner.Name,
			OpenIssueCount:   owner.OpenIssueCount,
			FailingRuleCount: owner.FailingRuleCount,
		}
		// Zero is the unassigned bucket, which the proto spells as an empty owner.
		if owner.UserID > 0 {
			digestOwner.Owner = common.FormatUserUID(int(owner.UserID))
		}
		detail.Owners = append(detail.Owners, digestOwner)
	}
	for _, target := range snapshot.Targets {
		detail.Targets = append(detail.Targets, &storepb.DataQualityDigestTarget{
			TargetPath:            target.DisplayPath,
			FailedExecutionCount:  target.FailedExecutionCount,
			ErroredExecutionCount: target.ErroredExecutionCount,
			OpenIssueCount:        target.OpenIssueCount,
		})
	}
	return detail
}
