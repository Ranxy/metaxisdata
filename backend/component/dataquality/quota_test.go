package dataquality

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
)

func llmQuotaCounts(q *LLMQuota) (analyses, columnAnalyses int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.analyses, q.columnAnalyses
}

func setLLMQuotaCounts(q *LLMQuota, analyses, columnAnalyses int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.analyses = analyses
	q.columnAnalyses = columnAnalyses
}

func TestLLMQuotaDay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "utc afternoon",
			now:  time.Date(2026, time.October, 20, 15, 4, 5, 6, time.UTC),
			want: time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "local time before utc midnight belongs to the previous utc day",
			now:  time.Date(2026, time.October, 20, 1, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
			want: time.Date(2026, time.October, 19, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "local time after utc midnight belongs to the next utc day",
			now:  time.Date(2026, time.October, 20, 23, 0, 0, 0, time.FixedZone("EST", -5*60*60)),
			want: time.Date(2026, time.October, 21, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, llmQuotaDay(test.now))
		})
	}
}

func TestNewLLMQuotaInitialState(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	require.Equal(t, time.UTC, quota.day.Location(), "the counter day is a UTC day")
	require.Zero(t, quota.day.Hour())
	require.Zero(t, quota.day.Minute())
	require.Zero(t, quota.day.Second())
	analyses, columnAnalyses := llmQuotaCounts(quota)
	require.Zero(t, analyses)
	require.Zero(t, columnAnalyses)
	require.Equal(t, llmConcurrentLimit, cap(quota.sem))
}

func TestLLMQuotaCountsKindsSeparately(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	analysisRelease, err := quota.Begin(context.Background(), LLMAnalysisQuota, 0)
	require.NoError(t, err)
	analysisRelease()
	columnRelease, err := quota.Begin(context.Background(), LLMColumnAnalysisQuota, 2)
	require.NoError(t, err)
	columnRelease()

	analyses, columnAnalyses := llmQuotaCounts(quota)
	require.Equal(t, 1, analyses)
	require.Equal(t, 1, columnAnalyses)
}

func TestLLMQuotaResetsOnNewUTCDay(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	quota.mu.Lock()
	quota.day = llmQuotaDay(time.Now()).AddDate(0, 0, -1)
	quota.analyses = llmDailyAnalysisLimit
	quota.columnAnalyses = 7
	quota.mu.Unlock()

	release, err := quota.Begin(context.Background(), LLMAnalysisQuota, 0)
	require.NoError(t, err, "a new UTC day starts with an empty quota")
	release()

	analyses, columnAnalyses := llmQuotaCounts(quota)
	require.Equal(t, 1, analyses)
	require.Zero(t, columnAnalyses)
	require.Equal(t, llmQuotaDay(time.Now()), quota.day)
}

// RemainingAnalyses is the best-effort admission check a batch submission uses: it
// reports today's L1/L2 headroom and rolls the counters over with the new day.
func TestLLMQuotaRemainingAnalyses(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	require.Equal(t, llmDailyAnalysisLimit, quota.RemainingAnalyses())

	setLLMQuotaCounts(quota, 3, 0)
	require.Equal(t, llmDailyAnalysisLimit-3, quota.RemainingAnalyses())

	setLLMQuotaCounts(quota, llmDailyAnalysisLimit, 0)
	require.Zero(t, quota.RemainingAnalyses())

	// A stale counter day is rolled over before the answer, so yesterdays's usage
	// does not shrink today's budget.
	quota.mu.Lock()
	quota.day = llmQuotaDay(time.Now()).AddDate(0, 0, -1)
	quota.mu.Unlock()
	require.Equal(t, llmDailyAnalysisLimit, quota.RemainingAnalyses())

	// A nil quota (a test-only service) reports no headroom rather than panicking.
	var missing *LLMQuota
	require.Zero(t, missing.RemainingAnalyses())
}

func TestLLMQuotaRejectsOverDailyLimit(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	setLLMQuotaCounts(quota, llmDailyAnalysisLimit, 0)

	_, err := quota.Begin(context.Background(), LLMAnalysisQuota, 0)
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
	require.Contains(t, err.Error(), "daily")

	// The buckets are independent: the column analyses are still allowed.
	release, err := quota.Begin(context.Background(), LLMColumnAnalysisQuota, 10)
	require.NoError(t, err)
	release()
}

func TestLLMQuotaRejectsOverColumnLimit(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	setLLMQuotaCounts(quota, 0, 2)

	_, err := quota.Begin(context.Background(), LLMColumnAnalysisQuota, 2)
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))

	// The L1/L2 bucket is unaffected by the column bucket's limit.
	release, err := quota.Begin(context.Background(), LLMAnalysisQuota, 2)
	require.NoError(t, err)
	release()
}

func TestLLMQuotaColumnLimitFallsBackToTheSettingsDefault(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	setLLMQuotaCounts(quota, 0, DefaultDailyColumnAnalysisLimit)
	_, err := quota.Begin(context.Background(), LLMColumnAnalysisQuota, 0)
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err), "0 means the settings default, not unlimited")

	setLLMQuotaCounts(quota, 0, DefaultDailyColumnAnalysisLimit-1)
	release, err := quota.Begin(context.Background(), LLMColumnAnalysisQuota, 0)
	require.NoError(t, err)
	release()
}

func TestLLMQuotaRejectsAnOverLimitRequestWithoutWaitingForASlot(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	first, err := quota.Begin(context.Background(), LLMAnalysisQuota, 0)
	require.NoError(t, err)
	t.Cleanup(first)
	second, err := quota.Begin(context.Background(), LLMAnalysisQuota, 0)
	require.NoError(t, err)
	t.Cleanup(second)
	setLLMQuotaCounts(quota, llmDailyAnalysisLimit, 0)

	// The semaphore is full, yet an over-quota request must fail fast rather than
	// block on it.
	done := make(chan error, 1)
	go func() {
		_, err := quota.Begin(context.Background(), LLMAnalysisQuota, 0)
		done <- err
	}()
	select {
	case err := <-done:
		require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
	case <-time.After(2 * time.Second):
		t.Fatal("an over-quota request waited for a concurrency slot")
	}
}

func TestLLMQuotaSerializesConcurrentAnalyses(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	first, err := quota.Begin(context.Background(), LLMAnalysisQuota, 0)
	require.NoError(t, err)
	second, err := quota.Begin(context.Background(), LLMColumnAnalysisQuota, 10)
	require.NoError(t, err)
	require.Len(t, quota.sem, llmConcurrentLimit, "both kinds share the concurrency limit")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = quota.Begin(ctx, LLMAnalysisQuota, 0)
	require.Equal(t, connect.CodeDeadlineExceeded, connect.CodeOf(err))
	require.Less(t, time.Since(started), time.Second, "the waiter must stop when its context ends")

	// A request that never got a slot must not consume the day's quota.
	analyses, columnAnalyses := llmQuotaCounts(quota)
	require.Equal(t, 1, analyses)
	require.Equal(t, 1, columnAnalyses)

	first()
	second()
	release, err := quota.Begin(context.Background(), LLMAnalysisQuota, 0)
	require.NoError(t, err, "the released slots are reusable")
	release()
}

func TestLLMQuotaReportsCancelledWaiters(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	first, err := quota.Begin(context.Background(), LLMAnalysisQuota, 0)
	require.NoError(t, err)
	t.Cleanup(first)
	second, err := quota.Begin(context.Background(), LLMAnalysisQuota, 0)
	require.NoError(t, err)
	t.Cleanup(second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = quota.Begin(ctx, LLMAnalysisQuota, 0)
	require.Equal(t, connect.CodeCanceled, connect.CodeOf(err))

	analyses, _ := llmQuotaCounts(quota)
	require.Equal(t, 2, analyses, "a cancelled waiter gives its quota back")
}

func TestLLMQuotaReleaseIsIdempotent(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	release, err := quota.Begin(context.Background(), LLMAnalysisQuota, 0)
	require.NoError(t, err)
	require.Len(t, quota.sem, 1)

	done := make(chan struct{})
	go func() {
		release()
		release()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a second release tried to take another slot")
	}
	require.Empty(t, quota.sem, "the analysis held exactly one slot")

	// Both slots are still usable, which they would not be after an extra release.
	first, err := quota.Begin(context.Background(), LLMAnalysisQuota, 0)
	require.NoError(t, err)
	second, err := quota.Begin(context.Background(), LLMAnalysisQuota, 0)
	require.NoError(t, err)
	first()
	second()
}

func TestLLMQuotaRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	_, err := quota.Begin(context.Background(), LLMQuotaKind(99), 0)
	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	require.Empty(t, quota.sem)
}
