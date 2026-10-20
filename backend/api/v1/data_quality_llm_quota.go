package v1

import (
	"context"
	"errors"
	"sync"
	"time"

	"connectrpc.com/connect"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
)

const (
	// qualityLLMDailyAnalysisLimit is how many L1/L2 LLM analyses (rule drafts and
	// failure explanations) one workspace may run per UTC day. The v1 settings have
	// no field for it yet, so it is a fixed safety valve rather than a knob.
	qualityLLMDailyAnalysisLimit = 200
	// qualityLLMConcurrentLimit is how many data quality LLM analyses may run at
	// once in this process, across both kinds. The bound protects the providers'
	// rate limits and the memory of a single Go process, so it is process-wide.
	qualityLLMConcurrentLimit = 2
)

// qualityLLMKind selects the quota bucket one analysis consumes. L1 (rule drafts)
// and L2 (failure explanations) share the analysis bucket; L3 (column labels) has
// its own, smaller bucket but the same concurrency limit.
type qualityLLMKind int

const (
	qualityLLMAnalysis qualityLLMKind = iota
	qualityLLMColumnAnalysis
)

// qualityLLMQuota counts a workspace's daily LLM analyses and caps how many run at
// once. It is process-local: the deployment assumes one replica, and a restart
// resets the counters the same way the runner's day buckets do.
type qualityLLMQuota struct {
	mu sync.Mutex
	// day is the UTC day the counters below belong to. A different day resets
	// them, which is what makes the limit "per calendar day" without a timer.
	day            time.Time
	analyses       int
	columnAnalyses int
	// sem is a counting semaphore with qualityLLMConcurrentLimit slots.
	sem chan struct{}
}

func newQualityLLMQuota() *qualityLLMQuota {
	return &qualityLLMQuota{
		day: qualityLLMQuotaDay(time.Now()),
		sem: make(chan struct{}, qualityLLMConcurrentLimit),
	}
}

// qualityLLMQuotaDay returns the UTC day a moment belongs to. The quota resets at
// 00:00 UTC so the rollover does not depend on the server's local timezone and
// every replica would agree on it.
func qualityLLMQuotaDay(now time.Time) time.Time {
	utc := now.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

// begin reserves one analysis against the workspace's daily quota and one
// concurrency slot. The returned release must be called when the analysis
// finishes; repeated calls are no-ops.
//
// columnAnalysisLimit is the L3 daily ceiling, which the caller reads from the
// workspace settings; the L1/L2 kind ignores it. Exceeding either quota returns
// ResourceExhausted before touching the semaphore, so an over-quota request fails
// fast instead of queuing for a slot it could never use. Waiting for a slot honors
// the context: a cancelled request gives its quota back and reports the context's
// error.
func (q *qualityLLMQuota) begin(ctx context.Context, kind qualityLLMKind, columnAnalysisLimit int) (func(), error) {
	if q == nil {
		return func() {}, nil
	}

	q.mu.Lock()
	day := qualityLLMQuotaDay(time.Now())
	if !q.day.Equal(day) {
		q.day = day
		q.analyses = 0
		q.columnAnalyses = 0
	}
	switch kind {
	case qualityLLMAnalysis:
		if q.analyses >= qualityLLMDailyAnalysisLimit {
			q.mu.Unlock()
			return nil, connect.NewError(connect.CodeResourceExhausted, errors.New(
				"the workspace has used its daily data quality LLM analysis limit; it resets at 00:00 UTC"))
		}
		q.analyses++
	case qualityLLMColumnAnalysis:
		limit := columnAnalysisLimit
		if limit <= 0 {
			limit = dataquality.DefaultDailyColumnAnalysisLimit
		}
		if q.columnAnalyses >= limit {
			q.mu.Unlock()
			return nil, connect.NewError(connect.CodeResourceExhausted, errors.New(
				"the workspace has used its daily column analysis limit; it resets at 00:00 UTC"))
		}
		q.columnAnalyses++
	default:
		q.mu.Unlock()
		return nil, connect.NewError(connect.CodeInternal, errors.New("unknown data quality LLM analysis kind"))
	}
	q.mu.Unlock()

	select {
	case q.sem <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-q.sem }) }, nil
	case <-ctx.Done():
		// The analysis never started, so waiting must not consume the day's quota.
		q.mu.Lock()
		switch kind {
		case qualityLLMAnalysis:
			q.analyses--
		case qualityLLMColumnAnalysis:
			q.columnAnalyses--
		default:
			// The kind was validated above.
		}
		q.mu.Unlock()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, connect.NewError(connect.CodeDeadlineExceeded, ctx.Err())
		}
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	}
}
