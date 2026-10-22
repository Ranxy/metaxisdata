package dataquality

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
)

func llmQuotaCounts(q *LLMQuota) (analyses, columnAnalyses, inFlight int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.analyses, q.columnAnalyses, q.inFlight
}

func setLLMQuotaCounts(q *LLMQuota, analyses, columnAnalyses int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.analyses = analyses
	q.columnAnalyses = columnAnalyses
}

// testLimits is the quota policy most tests run under: generous daily ceilings and
// a small concurrency bound so waiting is observable.
func testLimits(daily, columnDaily, concurrency int) LLMLimits {
	return LLMLimits{DailyAnalyses: daily, DailyColumnAnalyses: columnDaily, Concurrency: concurrency}
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

// NextReset is when the day's counters roll over: the next 00:00 UTC.
func TestLLMQuotaNextReset(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	require.Equal(t, time.Date(2026, time.October, 21, 0, 0, 0, 0, time.UTC),
		quota.NextReset(time.Date(2026, time.October, 20, 23, 59, 0, 0, time.UTC)))
	// A moment just after midnight resets at the next midnight, not today's.
	require.Equal(t, time.Date(2026, time.October, 21, 0, 0, 0, 0, time.UTC),
		quota.NextReset(time.Date(2026, time.October, 20, 0, 0, 1, 0, time.UTC)))
}

func TestNewLLMQuotaInitialState(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	require.Equal(t, time.UTC, quota.day.Location(), "the counter day is a UTC day")
	require.Zero(t, quota.day.Hour())
	require.Zero(t, quota.day.Minute())
	require.Zero(t, quota.day.Second())
	require.NotNil(t, quota.cond)
	analyses, columnAnalyses, inFlight := llmQuotaCounts(quota)
	require.Zero(t, analyses)
	require.Zero(t, columnAnalyses)
	require.Zero(t, inFlight)
}

func TestLLMQuotaCountsKindsSeparately(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	limits := testLimits(10, 10, 2)
	analysisRelease, err := quota.Begin(context.Background(), LLMAnalysisQuota, limits)
	require.NoError(t, err)
	analysisRelease()
	columnRelease, err := quota.Begin(context.Background(), LLMColumnAnalysisQuota, limits)
	require.NoError(t, err)
	columnRelease()

	analyses, columnAnalyses, inFlight := llmQuotaCounts(quota)
	require.Equal(t, 1, analyses)
	require.Equal(t, 1, columnAnalyses)
	require.Zero(t, inFlight, "a released analysis no longer holds a slot")
}

func TestLLMQuotaResetsOnNewUTCDay(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	quota.mu.Lock()
	quota.day = llmQuotaDay(time.Now()).AddDate(0, 0, -1)
	quota.analyses = 5
	quota.columnAnalyses = 7
	quota.mu.Unlock()

	release, err := quota.Begin(context.Background(), LLMAnalysisQuota, testLimits(10, 10, 2))
	require.NoError(t, err, "a new UTC day starts with an empty quota")
	release()

	analyses, columnAnalyses, _ := llmQuotaCounts(quota)
	require.Equal(t, 1, analyses)
	require.Zero(t, columnAnalyses)
	require.Equal(t, llmQuotaDay(time.Now()), quota.day)
}

// The daily ceiling comes from the caller's limits, so a settings change applies
// to the next analysis without a restart.
func TestLLMQuotaEnforcesTheConfiguredDailyLimit(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	limits := testLimits(2, 100, 8)
	for range 2 {
		release, err := quota.Begin(context.Background(), LLMAnalysisQuota, limits)
		require.NoError(t, err)
		release()
	}
	_, err := quota.Begin(context.Background(), LLMAnalysisQuota, limits)
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
	require.Contains(t, err.Error(), "daily")

	// A larger configured limit admits the next analysis immediately.
	release, err := quota.Begin(context.Background(), LLMAnalysisQuota, testLimits(3, 100, 8))
	require.NoError(t, err)
	release()

	// The buckets are independent: the column analyses are still allowed.
	release, err = quota.Begin(context.Background(), LLMColumnAnalysisQuota, limits)
	require.NoError(t, err)
	release()
}

func TestLLMQuotaEnforcesTheColumnLimit(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	setLLMQuotaCounts(quota, 0, 2)
	_, err := quota.Begin(context.Background(), LLMColumnAnalysisQuota, testLimits(10, 2, 8))
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))

	// The L1/L2 bucket is unaffected by the column bucket's limit.
	release, err := quota.Begin(context.Background(), LLMAnalysisQuota, testLimits(10, 2, 8))
	require.NoError(t, err)
	release()
}

// A zero-valued limits struct falls back to the server defaults rather than
// admitting an unbounded analysis or serializing everything.
func TestLLMQuotaZeroLimitsUseTheDefaults(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	release, err := quota.Begin(context.Background(), LLMAnalysisQuota, LLMLimits{})
	require.NoError(t, err)
	release()
	require.Equal(t, DefaultDailyLLMAnalysisLimit-1, quota.RemainingAnalyses(LLMLimits{}))
}

// An over-quota request fails fast even while every concurrency slot is held: it
// must not queue for a slot it could never use.
func TestLLMQuotaRejectsAnOverLimitRequestWithoutWaitingForASlot(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	limits := testLimits(1, 10, 1)
	first, err := quota.Begin(context.Background(), LLMAnalysisQuota, limits)
	require.NoError(t, err)
	t.Cleanup(first)

	done := make(chan error, 1)
	go func() {
		_, err := quota.Begin(context.Background(), LLMAnalysisQuota, limits)
		done <- err
	}()
	select {
	case err := <-done:
		require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
	case <-time.After(2 * time.Second):
		t.Fatal("an over-quota request waited for a concurrency slot")
	}
}

// The concurrency bound is dynamic: with one slot held, a second analysis waits
// until the release broadcasts, and the limit from the later call applies.
func TestLLMQuotaSerializesConcurrencyAndBroadcastsOnRelease(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	limits := testLimits(10, 10, 1)
	first, err := quota.Begin(context.Background(), LLMAnalysisQuota, limits)
	require.NoError(t, err)

	acquired := make(chan func(), 1)
	waiterErr := make(chan error, 1)
	go func() {
		release, err := quota.Begin(context.Background(), LLMColumnAnalysisQuota, limits)
		if err != nil {
			waiterErr <- err
			return
		}
		acquired <- release
	}()
	select {
	case <-acquired:
		t.Fatal("the second analysis acquired a slot while the only one was held")
	case err := <-waiterErr:
		t.Fatalf("the waiter failed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	first()
	select {
	case release := <-acquired:
		release()
	case err := <-waiterErr:
		t.Fatalf("the waiter failed after the release: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("the release did not wake the waiter")
	}
	_, _, inFlight := llmQuotaCounts(quota)
	require.Zero(t, inFlight)
}

// A waiter whose context ends while it waits returns the context error, holds no
// slot, and consumes none of the day's quota.
func TestLLMQuotaCancelledWaiterConsumesNothing(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	limits := testLimits(10, 10, 1)
	first, err := quota.Begin(context.Background(), LLMAnalysisQuota, limits)
	require.NoError(t, err)
	t.Cleanup(first)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := quota.Begin(ctx, LLMAnalysisQuota, limits)
		done <- err
	}()
	require.Eventually(t, func() bool {
		_, _, inFlight := llmQuotaCounts(quota)
		return inFlight == 1 // the waiter is queued behind the held slot
	}, 2*time.Second, time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.Equal(t, connect.CodeCanceled, connect.CodeOf(err))
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled waiter was not woken")
	}
	analyses, _, inFlight := llmQuotaCounts(quota)
	require.Equal(t, 1, analyses, "a cancelled waiter consumes no quota")
	require.Equal(t, 1, inFlight, "only the held analysis owns a slot")

	// The held slot is still usable and the quota still admits exactly the
	// remaining budget.
	first()
	release, err := quota.Begin(context.Background(), LLMAnalysisQuota, limits)
	require.NoError(t, err)
	release()
}

// A deadline that expires while waiting is reported as DeadlineExceeded and leaves
// the quota unchanged.
func TestLLMQuotaDeadlineWhileWaiting(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	limits := testLimits(10, 10, 1)
	first, err := quota.Begin(context.Background(), LLMAnalysisQuota, limits)
	require.NoError(t, err)
	t.Cleanup(first)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = quota.Begin(ctx, LLMAnalysisQuota, limits)
	require.Equal(t, connect.CodeDeadlineExceeded, connect.CodeOf(err))
	analyses, _, _ := llmQuotaCounts(quota)
	require.Equal(t, 1, analyses)
}

func TestLLMQuotaReleaseIsIdempotent(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	release, err := quota.Begin(context.Background(), LLMAnalysisQuota, testLimits(10, 10, 2))
	require.NoError(t, err)

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
	_, _, inFlight := llmQuotaCounts(quota)
	require.Zero(t, inFlight, "the analysis held exactly one slot")

	// Both slots are still usable, which they would not be after an extra release.
	first, err := quota.Begin(context.Background(), LLMAnalysisQuota, testLimits(10, 10, 2))
	require.NoError(t, err)
	second, err := quota.Begin(context.Background(), LLMAnalysisQuota, testLimits(10, 10, 2))
	require.NoError(t, err)
	first()
	second()
}

func TestLLMQuotaRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	_, err := quota.Begin(context.Background(), LLMQuotaKind(99), testLimits(10, 10, 2))
	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	_, _, inFlight := llmQuotaCounts(quota)
	require.Zero(t, inFlight)
}

// RemainingAnalyses and Usage report the caller's budget and the live counters.
func TestLLMQuotaRemainingAndUsage(t *testing.T) {
	t.Parallel()

	quota := NewLLMQuota(nil)
	limits := testLimits(5, 4, 3)
	require.Equal(t, 5, quota.RemainingAnalyses(limits))

	first, err := quota.Begin(context.Background(), LLMAnalysisQuota, limits)
	require.NoError(t, err)
	require.Equal(t, 4, quota.RemainingAnalyses(limits))
	column, err := quota.Begin(context.Background(), LLMColumnAnalysisQuota, limits)
	require.NoError(t, err)

	analyses, columnAnalyses, inFlight := quota.Usage()
	require.Equal(t, 1, analyses)
	require.Equal(t, 1, columnAnalyses)
	require.Equal(t, 2, inFlight)

	first()
	column()
	analyses, columnAnalyses, inFlight = quota.Usage()
	require.Equal(t, 1, analyses)
	require.Equal(t, 1, columnAnalyses)
	require.Zero(t, inFlight)

	// The remaining budget is clamped at zero, and the counters roll over with the
	// new day before the answer.
	setLLMQuotaCounts(quota, 5, 0)
	require.Zero(t, quota.RemainingAnalyses(limits))
	quota.mu.Lock()
	quota.day = llmQuotaDay(time.Now()).AddDate(0, 0, -1)
	quota.mu.Unlock()
	require.Equal(t, 5, quota.RemainingAnalyses(limits))

	// A nil quota (a test-only service) reports no headroom rather than panicking.
	var missing *LLMQuota
	require.Zero(t, missing.RemainingAnalyses(limits))
	analyses, columnAnalyses, inFlight = missing.Usage()
	require.Zero(t, analyses)
	require.Zero(t, columnAnalyses)
	require.Zero(t, inFlight)
}

// LimitsFromSettings fills defaults and clamps out-of-band settings to the server
// bounds.
func TestLimitsFromSettings(t *testing.T) {
	t.Parallel()

	limits := LimitsFromSettings(DefaultSettings())
	require.Equal(t, DefaultDailyLLMAnalysisLimit, limits.DailyAnalyses)
	require.Equal(t, DefaultDailyColumnAnalysisLimit, limits.DailyColumnAnalyses)
	require.Equal(t, DefaultLLMConcurrencyLimit, limits.Concurrency)

	limits = LimitsFromSettings(Settings{DailyLLMAnalysisLimit: 42, DailyColumnAnalysisLimit: 7, LLMConcurrencyLimit: 3})
	require.Equal(t, 42, limits.DailyAnalyses)
	require.Equal(t, 7, limits.DailyColumnAnalyses)
	require.Equal(t, 3, limits.Concurrency)

	limits = LimitsFromSettings(Settings{
		DailyLLMAnalysisLimit: MaxDailyLLMAnalysisLimit + 1,
		LLMConcurrencyLimit:   MaxLLMConcurrencyLimit + 1,
	})
	require.Equal(t, MaxDailyLLMAnalysisLimit, limits.DailyAnalyses)
	require.Equal(t, MaxLLMConcurrencyLimit, limits.Concurrency)
}
