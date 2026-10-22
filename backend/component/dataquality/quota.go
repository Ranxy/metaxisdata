package dataquality

import (
	"context"
	"errors"
	"sync"
	"time"

	"connectrpc.com/connect"

	"github.com/Ranxy/metaxisdata/backend/metrics"
)

// LLMQuotaKind selects the quota bucket one analysis consumes. L1 (rule drafts)
// and L2 (failure explanations) share the analysis bucket; L3 (column labels) has
// its own, smaller bucket but the same concurrency limit.
type LLMQuotaKind int

const (
	// LLMAnalysisQuota is the L1/L2 bucket: rule drafts and failure explanations.
	LLMAnalysisQuota LLMQuotaKind = iota
	// LLMColumnAnalysisQuota is the L3 bucket: column-label analyses, whose daily
	// ceiling comes from the workspace settings.
	LLMColumnAnalysisQuota
)

// LLMLimits is the effective quota policy one analysis runs under. It is resolved
// from the workspace settings when the caller starts, so a settings change applies
// to the next analysis without a restart.
type LLMLimits struct {
	// DailyAnalyses is the L1+L2 daily ceiling.
	DailyAnalyses int
	// DailyColumnAnalyses is the L3 daily ceiling.
	DailyColumnAnalyses int
	// Concurrency is how many analyses may run at once, across both buckets.
	Concurrency int
}

// LimitsFromSettings resolves the quota policy from the workspace settings,
// filling defaults and clamping out-of-band values to the documented bounds.
func LimitsFromSettings(settings Settings) LLMLimits {
	return LLMLimits{
		DailyAnalyses:       boundedLimit(settings.DailyLLMAnalysisLimit, DefaultDailyLLMAnalysisLimit, MaxDailyLLMAnalysisLimit),
		DailyColumnAnalyses: positiveOrDefault(int32(settings.DailyColumnAnalysisLimit), DefaultDailyColumnAnalysisLimit),
		Concurrency:         boundedLimit(settings.LLMConcurrencyLimit, DefaultLLMConcurrencyLimit, MaxLLMConcurrencyLimit),
	}
}

// dailyLimit resolves the ceiling for one kind from the caller's limits, applying
// the defaults so a zero-valued Limits can never admit an unbounded analysis.
func (l LLMLimits) dailyLimit(kind LLMQuotaKind) int {
	switch kind {
	case LLMAnalysisQuota:
		return boundedLimit(l.DailyAnalyses, DefaultDailyLLMAnalysisLimit, MaxDailyLLMAnalysisLimit)
	case LLMColumnAnalysisQuota:
		return positiveOrDefault(int32(l.DailyColumnAnalyses), DefaultDailyColumnAnalysisLimit)
	default:
		return 0
	}
}

// concurrencyLimit resolves the shared concurrency ceiling from the caller's
// limits.
func (l LLMLimits) concurrencyLimit() int {
	return boundedLimit(l.Concurrency, DefaultLLMConcurrencyLimit, MaxLLMConcurrencyLimit)
}

// LLMQuota counts a workspace's daily LLM analyses and caps how many run at once.
// It is process-local: the deployment assumes one replica, and a restart resets
// the counters the same way the runner's day buckets do. One quota is created with
// the server and shared by the API handlers and the suggestion runner, so a
// background drafting job spends the same budget a synchronous request does.
//
// The concurrency bound is dynamic: every Begin carries the limits its caller
// loaded, so a settings change takes effect on the next analysis instead of
// requiring a restart.
type LLMQuota struct {
	mu sync.Mutex
	// cond wakes waiters when a slot is released or a waiter's context ends.
	cond *sync.Cond
	// day is the UTC day the counters below belong to. A different day resets
	// them, which is what makes the limit "per calendar day" without a timer.
	day            time.Time
	analyses       int
	columnAnalyses int
	// inFlight is how many analyses hold a concurrency slot right now.
	inFlight int
	// metrics records daily-quota rejections by kind; a nil value disables
	// instrumentation.
	metrics *metrics.Metrics
}

// NewLLMQuota creates the process-wide data quality LLM quota. m may be nil.
func NewLLMQuota(m *metrics.Metrics) *LLMQuota {
	quota := &LLMQuota{
		day:     llmQuotaDay(time.Now()),
		metrics: m,
	}
	quota.cond = sync.NewCond(&quota.mu)
	return quota
}

// llmQuotaDay returns the UTC day a moment belongs to. The quota resets at 00:00
// UTC so the rollover does not depend on the server's local timezone and every
// replica would agree on it.
func llmQuotaDay(now time.Time) time.Time {
	utc := now.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

// NextReset returns the next 00:00 UTC instant: when today's counters roll over.
func (*LLMQuota) NextReset(now time.Time) time.Time {
	return llmQuotaDay(now).AddDate(0, 0, 1)
}

// rolloverLocked resets the counters when the UTC day changed. The caller holds
// mu. In-flight analyses are not reset: they belong to the wall clock, not the
// counter day.
func (q *LLMQuota) rolloverLocked() {
	day := llmQuotaDay(time.Now())
	if !q.day.Equal(day) {
		q.day = day
		q.analyses = 0
		q.columnAnalyses = 0
	}
}

// RemainingAnalyses reports how many L1/L2 analyses are left in today's budget
// under limits. It is a best-effort admission check for a batch: another caller
// may consume the same budget between the check and its own Begin calls.
func (q *LLMQuota) RemainingAnalyses(limits LLMLimits) int {
	if q == nil {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.rolloverLocked()
	remaining := limits.dailyLimit(LLMAnalysisQuota) - q.analyses
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Usage snapshots today's counters and the analyses currently holding a slot.
func (q *LLMQuota) Usage() (analyses, columnAnalyses, inFlight int) {
	if q == nil {
		return 0, 0, 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.rolloverLocked()
	return q.analyses, q.columnAnalyses, q.inFlight
}

// watchContext broadcasts the condition when ctx ends, so a waiter blocked on a
// full concurrency limit wakes up and observes the cancellation. The returned stop
// function ends the watcher when the slot is acquired; taking the lock before the
// broadcast avoids the lost-wakeup race between a cancellation and Wait.
func (q *LLMQuota) watchContext(ctx context.Context) func() {
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			q.mu.Lock()
			q.cond.Broadcast()
			q.mu.Unlock()
		case <-stop:
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stop) }) }
}

// Begin reserves one analysis against the caller's daily quota and one concurrency
// slot. The returned release must be called when the analysis finishes; repeated
// calls are no-ops.
//
// The daily check fails fast with ResourceExhausted before waiting for a slot, so
// an over-quota request never queues. Waiting for a slot honors the context: a
// cancelled waiter returns the context error and consumes neither the day's quota
// nor a slot. The concurrency ceiling comes from limits, so a settings change
// applies to the next call.
func (q *LLMQuota) Begin(ctx context.Context, kind LLMQuotaKind, limits LLMLimits) (func(), error) {
	if q == nil {
		return func() {}, nil
	}
	switch kind {
	case LLMAnalysisQuota, LLMColumnAnalysisQuota:
	default:
		return nil, connect.NewError(connect.CodeInternal, errors.New("unknown data quality LLM analysis kind"))
	}

	q.mu.Lock()
	q.rolloverLocked()
	if err := q.quotaExhaustedLocked(kind, limits); err != nil {
		q.mu.Unlock()
		return nil, err
	}
	concurrency := limits.concurrencyLimit()
	stopWatch := q.watchContext(ctx)
	for q.inFlight >= concurrency {
		if ctx.Err() != nil {
			stopWatch()
			q.mu.Unlock()
			return nil, llmWaitError(ctx)
		}
		q.cond.Wait()
	}
	stopWatch()
	if ctx.Err() != nil {
		q.mu.Unlock()
		return nil, llmWaitError(ctx)
	}
	// A waiter that passed the fast-path check may have consumed the last unit of
	// the day's budget while this waiter queued for a slot: re-check under the lock
	// that also guards the counters.
	if err := q.quotaExhaustedLocked(kind, limits); err != nil {
		q.mu.Unlock()
		return nil, err
	}
	switch kind {
	case LLMAnalysisQuota:
		q.analyses++
	case LLMColumnAnalysisQuota:
		q.columnAnalyses++
	default:
		// The kind was validated above.
	}
	q.inFlight++
	q.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			q.mu.Lock()
			q.inFlight--
			q.cond.Broadcast()
			q.mu.Unlock()
		})
	}, nil
}

// quotaExhaustedLocked reports the kind's daily limit as a ResourceExhausted error
// and records the rejection. The caller holds mu and unlocks before returning.
func (q *LLMQuota) quotaExhaustedLocked(kind LLMQuotaKind, limits LLMLimits) error {
	switch kind {
	case LLMAnalysisQuota:
		if q.analyses < limits.dailyLimit(kind) {
			return nil
		}
		q.metrics.CountLLMQuotaRejection(metrics.LLMQuotaAnalysis)
		return connect.NewError(connect.CodeResourceExhausted, errors.New(
			"the workspace has used its daily data quality LLM analysis limit; it resets at 00:00 UTC"))
	case LLMColumnAnalysisQuota:
		if q.columnAnalyses < limits.dailyLimit(kind) {
			return nil
		}
		q.metrics.CountLLMQuotaRejection(metrics.LLMQuotaColumnAnalysis)
		return connect.NewError(connect.CodeResourceExhausted, errors.New(
			"the workspace has used its daily column analysis limit; it resets at 00:00 UTC"))
	default:
		return connect.NewError(connect.CodeInternal, errors.New("unknown data quality LLM analysis kind"))
	}
}

// llmWaitError maps a cancelled slot wait onto the RPC's error space.
func llmWaitError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return connect.NewError(connect.CodeDeadlineExceeded, ctx.Err())
	}
	return connect.NewError(connect.CodeCanceled, ctx.Err())
}
