package v1

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
)

func qualityLLMQuotaCounts(q *qualityLLMQuota) (analyses, columnAnalyses int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.analyses, q.columnAnalyses
}

func setQualityLLMQuotaCounts(q *qualityLLMQuota, analyses, columnAnalyses int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.analyses = analyses
	q.columnAnalyses = columnAnalyses
}

func TestQualityLLMQuotaDay(t *testing.T) {
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
			require.Equal(t, test.want, qualityLLMQuotaDay(test.now))
		})
	}
}

func TestNewQualityLLMQuotaInitialState(t *testing.T) {
	t.Parallel()

	quota := newQualityLLMQuota()
	require.Equal(t, time.UTC, quota.day.Location(), "the counter day is a UTC day")
	require.Zero(t, quota.day.Hour())
	require.Zero(t, quota.day.Minute())
	require.Zero(t, quota.day.Second())
	analyses, columnAnalyses := qualityLLMQuotaCounts(quota)
	require.Zero(t, analyses)
	require.Zero(t, columnAnalyses)
	require.Equal(t, qualityLLMConcurrentLimit, cap(quota.sem))
}

func TestQualityLLMQuotaCountsKindsSeparately(t *testing.T) {
	t.Parallel()

	quota := newQualityLLMQuota()
	analysisRelease, err := quota.begin(context.Background(), qualityLLMAnalysis, 0)
	require.NoError(t, err)
	analysisRelease()
	columnRelease, err := quota.begin(context.Background(), qualityLLMColumnAnalysis, 2)
	require.NoError(t, err)
	columnRelease()

	analyses, columnAnalyses := qualityLLMQuotaCounts(quota)
	require.Equal(t, 1, analyses)
	require.Equal(t, 1, columnAnalyses)
}

func TestQualityLLMQuotaResetsOnNewUTCDay(t *testing.T) {
	t.Parallel()

	quota := newQualityLLMQuota()
	quota.mu.Lock()
	quota.day = qualityLLMQuotaDay(time.Now()).AddDate(0, 0, -1)
	quota.analyses = qualityLLMDailyAnalysisLimit
	quota.columnAnalyses = 7
	quota.mu.Unlock()

	release, err := quota.begin(context.Background(), qualityLLMAnalysis, 0)
	require.NoError(t, err, "a new UTC day starts with an empty quota")
	release()

	analyses, columnAnalyses := qualityLLMQuotaCounts(quota)
	require.Equal(t, 1, analyses)
	require.Zero(t, columnAnalyses)
	require.Equal(t, qualityLLMQuotaDay(time.Now()), quota.day)
}

func TestQualityLLMQuotaRejectsOverDailyLimit(t *testing.T) {
	t.Parallel()

	quota := newQualityLLMQuota()
	setQualityLLMQuotaCounts(quota, qualityLLMDailyAnalysisLimit, 0)

	_, err := quota.begin(context.Background(), qualityLLMAnalysis, 0)
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
	require.Contains(t, err.Error(), "daily")

	// The buckets are independent: the column analyses are still allowed.
	release, err := quota.begin(context.Background(), qualityLLMColumnAnalysis, 10)
	require.NoError(t, err)
	release()
}

func TestQualityLLMQuotaRejectsOverColumnLimit(t *testing.T) {
	t.Parallel()

	quota := newQualityLLMQuota()
	setQualityLLMQuotaCounts(quota, 0, 2)

	_, err := quota.begin(context.Background(), qualityLLMColumnAnalysis, 2)
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))

	// The L1/L2 bucket is unaffected by the column bucket's limit.
	release, err := quota.begin(context.Background(), qualityLLMAnalysis, 2)
	require.NoError(t, err)
	release()
}

func TestQualityLLMQuotaColumnLimitFallsBackToTheSettingsDefault(t *testing.T) {
	t.Parallel()

	quota := newQualityLLMQuota()
	setQualityLLMQuotaCounts(quota, 0, dataquality.DefaultDailyColumnAnalysisLimit)
	_, err := quota.begin(context.Background(), qualityLLMColumnAnalysis, 0)
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err), "0 means the settings default, not unlimited")

	setQualityLLMQuotaCounts(quota, 0, dataquality.DefaultDailyColumnAnalysisLimit-1)
	release, err := quota.begin(context.Background(), qualityLLMColumnAnalysis, 0)
	require.NoError(t, err)
	release()
}

func TestQualityLLMQuotaRejectsAnOverLimitRequestWithoutWaitingForASlot(t *testing.T) {
	t.Parallel()

	quota := newQualityLLMQuota()
	first, err := quota.begin(context.Background(), qualityLLMAnalysis, 0)
	require.NoError(t, err)
	t.Cleanup(first)
	second, err := quota.begin(context.Background(), qualityLLMAnalysis, 0)
	require.NoError(t, err)
	t.Cleanup(second)
	setQualityLLMQuotaCounts(quota, qualityLLMDailyAnalysisLimit, 0)

	// The semaphore is full, yet an over-quota request must fail fast rather than
	// block on it.
	done := make(chan error, 1)
	go func() {
		_, err := quota.begin(context.Background(), qualityLLMAnalysis, 0)
		done <- err
	}()
	select {
	case err := <-done:
		require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
	case <-time.After(2 * time.Second):
		t.Fatal("an over-quota request waited for a concurrency slot")
	}
}

func TestQualityLLMQuotaSerializesConcurrentAnalyses(t *testing.T) {
	t.Parallel()

	quota := newQualityLLMQuota()
	first, err := quota.begin(context.Background(), qualityLLMAnalysis, 0)
	require.NoError(t, err)
	second, err := quota.begin(context.Background(), qualityLLMColumnAnalysis, 10)
	require.NoError(t, err)
	require.Len(t, quota.sem, qualityLLMConcurrentLimit, "both kinds share the concurrency limit")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = quota.begin(ctx, qualityLLMAnalysis, 0)
	require.Equal(t, connect.CodeDeadlineExceeded, connect.CodeOf(err))
	require.Less(t, time.Since(started), time.Second, "the waiter must stop when its context ends")

	// A request that never got a slot must not consume the day's quota.
	analyses, columnAnalyses := qualityLLMQuotaCounts(quota)
	require.Equal(t, 1, analyses)
	require.Equal(t, 1, columnAnalyses)

	first()
	second()
	release, err := quota.begin(context.Background(), qualityLLMAnalysis, 0)
	require.NoError(t, err, "the released slots are reusable")
	release()
}

func TestQualityLLMQuotaReportsCancelledWaiters(t *testing.T) {
	t.Parallel()

	quota := newQualityLLMQuota()
	first, err := quota.begin(context.Background(), qualityLLMAnalysis, 0)
	require.NoError(t, err)
	t.Cleanup(first)
	second, err := quota.begin(context.Background(), qualityLLMAnalysis, 0)
	require.NoError(t, err)
	t.Cleanup(second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = quota.begin(ctx, qualityLLMAnalysis, 0)
	require.Equal(t, connect.CodeCanceled, connect.CodeOf(err))

	analyses, _ := qualityLLMQuotaCounts(quota)
	require.Equal(t, 2, analyses, "a cancelled waiter gives its quota back")
}

func TestQualityLLMQuotaReleaseIsIdempotent(t *testing.T) {
	t.Parallel()

	quota := newQualityLLMQuota()
	release, err := quota.begin(context.Background(), qualityLLMAnalysis, 0)
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
	first, err := quota.begin(context.Background(), qualityLLMAnalysis, 0)
	require.NoError(t, err)
	second, err := quota.begin(context.Background(), qualityLLMAnalysis, 0)
	require.NoError(t, err)
	first()
	second()
}

func TestQualityLLMQuotaRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	quota := newQualityLLMQuota()
	_, err := quota.begin(context.Background(), qualityLLMKind(99), 0)
	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	require.Empty(t, quota.sem)
}
