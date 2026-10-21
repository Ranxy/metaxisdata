package maintenance

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// passTime is the fixed clock the tests give the runner, so every cutoff can be
// asserted exactly.
var passTime = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// fakeStore records the cutoffs the pass asks for and returns the configured
// counts and errors. A delete whose name is in block waits for the pass deadline,
// which is how a timeout is tested without a real slow query.
type fakeStore struct {
	mu sync.Mutex

	calls   map[string]int
	cutoffs map[string]time.Time
	deleted map[string]int64
	errs    map[string]error
	block   string

	setting    *storepb.WorkspaceProfileSetting
	settingErr error

	// touched is closed by the first delete call, so a test can observe Run
	// without sleeping.
	touched chan struct{}
	once    sync.Once
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		calls:   make(map[string]int),
		cutoffs: make(map[string]time.Time),
		deleted: make(map[string]int64),
		errs:    make(map[string]error),
		touched: make(chan struct{}),
	}
}

// delete is the shared body of the fake delete methods.
func (f *fakeStore) delete(ctx context.Context, name string, cutoff time.Time) (int64, error) {
	f.mu.Lock()
	f.calls[name]++
	f.cutoffs[name] = cutoff
	block := f.block == name
	f.mu.Unlock()
	f.once.Do(func() { close(f.touched) })

	if block {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deleted[name], f.errs[name]
}

func (f *fakeStore) DeleteExpiredExplainSQLCache(ctx context.Context, olderThan time.Time) (int64, error) {
	return f.delete(ctx, "explain_sql_cache", olderThan)
}

func (f *fakeStore) DeleteExpiredLLMDebugLog(ctx context.Context, olderThan time.Time) (int64, error) {
	return f.delete(ctx, "llm_debug_log", olderThan)
}

func (f *fakeStore) DeleteExpiredRevokedTokens(ctx context.Context, before time.Time) (int64, error) {
	return f.delete(ctx, "revoked_tokens", before)
}

func (f *fakeStore) DeleteExpiredOAuthGrants(ctx context.Context, before time.Time) (int64, error) {
	return f.delete(ctx, "oauth_grants", before)
}

func (f *fakeStore) DeleteExpiredWebRefreshTokens(ctx context.Context, before time.Time) (int64, error) {
	return f.delete(ctx, "web_refresh_tokens", before)
}

func (f *fakeStore) DeleteExpiredQualityRuns(ctx context.Context, cutoff time.Time) (int64, error) {
	return f.delete(ctx, "quality_runs", cutoff)
}

func (f *fakeStore) DeleteExpiredQualityIssues(ctx context.Context, cutoff time.Time) (int64, error) {
	return f.delete(ctx, "quality_issues", cutoff)
}

func (f *fakeStore) DeleteExpiredQualitySuggestions(ctx context.Context, cutoff time.Time) (int64, error) {
	return f.delete(ctx, "quality_suggestions", cutoff)
}

func (f *fakeStore) DeleteExpiredColumnProfiles(ctx context.Context, cutoff time.Time) (int64, error) {
	return f.delete(ctx, "column_profiles", cutoff)
}

func (f *fakeStore) DeleteExpiredDataQualityLLMCache(ctx context.Context, cutoff time.Time) (int64, error) {
	return f.delete(ctx, "data_quality_llm_cache", cutoff)
}

func (f *fakeStore) DeleteEmptyOpenLineageDatasets(ctx context.Context, olderThan time.Time) (int64, error) {
	return f.delete(ctx, "empty_openlineage_datasets", olderThan)
}

func (f *fakeStore) DeleteOpenLineageRunsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	return f.delete(ctx, "openlineage_runs", cutoff)
}

func (f *fakeStore) GetWorkspaceGeneralSetting(ctx context.Context) (*storepb.WorkspaceProfileSetting, error) {
	f.mu.Lock()
	f.calls["workspace_setting"]++
	setting, err := f.setting, f.settingErr
	f.mu.Unlock()

	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	return setting, err
}

func (f *fakeStore) callCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[name]
}

func (f *fakeStore) cutoff(name string) time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cutoffs[name]
}

// fakeRevalidator returns the configured counts, can report a full batch a
// configured number of times per method so a test can model a backlog, and can run
// a hook after each batch so a test can end the pass between batches.
type fakeRevalidator struct {
	mu sync.Mutex

	unresolvedCalls   int
	contradictedCalls int
	// unresolvedHasMore and contradictedHasMore are how many of each method's
	// first calls report a full batch; the call after that reports no more.
	unresolvedHasMore   int
	contradictedHasMore int

	err          error
	revalidated  int
	stillUnknown int
	onBatch      func()
}

func (f *fakeRevalidator) batch(ctx context.Context, calls *int, hasMoreCalls int) (int, int, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, false, err
	}

	f.mu.Lock()
	*calls++
	hasMore := *calls <= hasMoreCalls
	revalidated, stillUnknown, err, onBatch := f.revalidated, f.stillUnknown, f.err, f.onBatch
	f.mu.Unlock()

	if onBatch != nil {
		onBatch()
	}
	if err != nil {
		return 0, 0, false, err
	}
	return revalidated, stillUnknown, hasMore, nil
}

func (f *fakeRevalidator) RevalidateUnresolvedLineage(ctx context.Context, _ int) (int, int, bool, error) {
	return f.batch(ctx, &f.unresolvedCalls, f.unresolvedHasMore)
}

func (f *fakeRevalidator) RevalidateContradictedColumnClaims(ctx context.Context, _ int) (int, int, bool, error) {
	return f.batch(ctx, &f.contradictedCalls, f.contradictedHasMore)
}

func (f *fakeRevalidator) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unresolvedCalls + f.contradictedCalls
}

func (f *fakeRevalidator) unresolvedCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unresolvedCalls
}

func (f *fakeRevalidator) contradictedCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.contradictedCalls
}

// newTestRunner wires the pass to the fakes with the production fields replaced by
// deterministic ones.
func newTestRunner(stores maintenanceStore, revalidator LineageRevalidator) *Runner {
	return &Runner{
		store:       stores,
		revalidator: revalidator,
		now:         func() time.Time { return passTime },
		interval:    time.Millisecond,
		passTimeout: time.Minute,
	}
}

// An item that fails must not abort the pass: the tables after it are pruned and
// the summary names the failure.
func TestFailedItemDoesNotStopLaterItems(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	stores.errs["explain_sql_cache"] = errors.New("table is locked")
	stores.deleted["llm_debug_log"] = 3
	stores.deleted["revoked_tokens"] = 5
	revalidator := &fakeRevalidator{revalidated: 2}

	summary := newTestRunner(stores, revalidator).runOnce(context.Background())

	require.Equal(t, []string{"explain_sql_cache"}, summary.failed)
	require.NotContains(t, summary.counts, "explain_sql_cache")
	require.Equal(t, int64(3), summary.counts["llm_debug_log"])
	require.Equal(t, int64(5), summary.counts["revoked_tokens"])
	for _, name := range []string{"llm_debug_log", "revoked_tokens", "oauth_grants", "web_refresh_tokens", "empty_openlineage_datasets"} {
		require.Equal(t, 1, stores.callCount(name), name)
	}
	require.Equal(t, 2, revalidator.callCount(), "both revalidations must still run")
}

// A failed setting read skips the retention item only: the sweep of what other
// deletions left behind and both revalidations still run.
func TestSettingErrorSkipsOnlyRetention(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	stores.settingErr = errors.New("connection refused")
	stores.deleted["llm_debug_log"] = 1
	stores.deleted["empty_openlineage_datasets"] = 2
	revalidator := &fakeRevalidator{revalidated: 1}

	summary := newTestRunner(stores, revalidator).runOnce(context.Background())

	for _, name := range []string{"explain_sql_cache", "llm_debug_log", "revoked_tokens", "oauth_grants", "web_refresh_tokens", "empty_openlineage_datasets"} {
		require.Equal(t, 1, stores.callCount(name), name)
	}
	require.Equal(t, 2, revalidator.callCount())
	require.Equal(t, 0, stores.callCount("openlineage_runs"))
	require.Equal(t, "workspace setting unavailable", summary.retentionSkipped)
	require.Empty(t, summary.failed, "a setting read failure is a skipped item, not a failed pass")
}

// Without a retention window the runs are kept: the pass says why it skipped them
// and still sweeps the empty datasets.
func TestUnsetRetentionWindowDoesNotDeleteRuns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		days int32
	}{
		{name: "zero days", days: 0},
		{name: "negative days", days: -3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stores := newFakeStore()
			stores.setting = &storepb.WorkspaceProfileSetting{OpenlineageRetentionDays: tt.days}

			summary := newTestRunner(stores, nil).runOnce(context.Background())

			require.Equal(t, 0, stores.callCount("openlineage_runs"))
			require.Equal(t, "no retention window set", summary.retentionSkipped)
			require.Equal(t, 1, stores.callCount("empty_openlineage_datasets"))
		})
	}
}

// Every cutoff is the pass start minus its window, computed once so one pass
// cannot use two different "now" values.
func TestCutoffsAreBasedOnThePassStart(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	stores.setting = &storepb.WorkspaceProfileSetting{OpenlineageRetentionDays: 7}

	newTestRunner(stores, nil).runOnce(context.Background())

	want := map[string]time.Time{
		"explain_sql_cache":          passTime.Add(-store.ExplainSQLCacheTTL),
		"llm_debug_log":              passTime.Add(-llmDebugLogRetention),
		"revoked_tokens":             passTime,
		"oauth_grants":               passTime.Add(-connectedAppRetention),
		"web_refresh_tokens":         passTime,
		"empty_openlineage_datasets": passTime.Add(-openLineageDatasetSweepGrace),
		"openlineage_runs":           passTime.AddDate(0, 0, -7),
	}
	for name, cutoff := range want {
		require.Equalf(t, cutoff, stores.cutoff(name), "cutoff for %s", name)
	}
}

// A runner wired without a revalidator must still prune; only the revalidation
// items are absent.
func TestNilRevalidatorIsSafe(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	stores.deleted["llm_debug_log"] = 4

	var summary *passSummary
	require.NotPanics(t, func() {
		summary = newTestRunner(stores, nil).runOnce(context.Background())
	})

	require.Equal(t, int64(4), summary.counts["llm_debug_log"])
	require.NotContains(t, summary.counts, "unresolved_lineage")
	require.NotContains(t, summary.counts, "contradicted_column_claims")
}

// A backlog larger than one implementation batch is drained in one pass, and the
// summary records the sum over the batches instead of only the last one.
func TestBacklogIsDrainedInOnePass(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	revalidator := &fakeRevalidator{
		revalidated:         2,
		stillUnknown:        3,
		unresolvedHasMore:   1,
		contradictedHasMore: 1,
	}

	summary := newTestRunner(stores, revalidator).runOnce(context.Background())

	require.Equal(t, 2, revalidator.unresolvedCallCount(), "a full batch earns a second call")
	require.Equal(t, 2, revalidator.contradictedCallCount())
	require.Equal(t, int64(4), summary.counts["unresolved_lineage"])
	require.Equal(t, int64(4), summary.counts["contradicted_column_claims"])
}

// A drain must stop when the pass context ends instead of calling forever, and the
// batches that already ran stay counted.
func TestBacklogDrainStopsWhenThePassEnds(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	ctx, cancel := context.WithCancel(context.Background())
	revalidator := &fakeRevalidator{revalidated: 1, unresolvedHasMore: 10}
	revalidator.onBatch = cancel

	summary := newTestRunner(stores, revalidator).runOnce(ctx)

	require.Equal(t, 1, revalidator.unresolvedCallCount(), "the drain must stop asking once its context ends")
	require.Equal(t, int64(1), summary.counts["unresolved_lineage"], "the batch that ran is still counted")
}

// A drain is bounded per pass even when the backlog never shrinks: a still unknown
// relation stays eligible, so an unbudgeted drain would rewrite the same edges for
// the whole pass.
func TestBacklogDrainIsBoundedPerPass(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	revalidator := &fakeRevalidator{
		revalidated:         1,
		unresolvedHasMore:   math.MaxInt,
		contradictedHasMore: math.MaxInt,
	}

	summary := newTestRunner(stores, revalidator).runOnce(context.Background())

	require.Equal(t, maxRevalidationBatches, revalidator.unresolvedCallCount())
	require.Equal(t, maxRevalidationBatches, revalidator.contradictedCallCount())
	require.Equal(t, int64(maxRevalidationBatches), summary.counts["unresolved_lineage"])
	require.Equal(t, int64(maxRevalidationBatches), summary.counts["contradicted_column_claims"])
}

// Run re-arms its jittered timer after each pass, so the periodic pass keeps
// running while the context is live.
func TestRunTicksAfterTheInterval(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	runner := newTestRunner(stores, nil)
	runner.interval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		return stores.callCount("explain_sql_cache") >= 2
	}, 5*time.Second, time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return when the context was cancelled")
	}
}

// The pass deadline reaches every item: once it fires on a stuck delete the items
// after it fail fast instead of running unbounded.
func TestPassTimeoutBoundsTheWholePass(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	stores.block = "explain_sql_cache"

	runner := newTestRunner(stores, &fakeRevalidator{revalidated: 1})
	runner.passTimeout = 5 * time.Millisecond

	started := time.Now()
	summary := runner.runOnce(context.Background())
	require.Less(t, time.Since(started), 5*time.Second, "the pass must end at its deadline")

	for _, name := range []string{
		"explain_sql_cache", "llm_debug_log", "revoked_tokens",
		"oauth_grants", "web_refresh_tokens",
	} {
		require.Contains(t, summary.failed, name, "the item after the stuck one still ran")
	}
	require.Equal(t, "workspace setting unavailable", summary.retentionSkipped)
}

// Run exits on context cancellation even with a fast injected interval.
func TestRunStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	runner := newTestRunner(stores, nil)
	runner.interval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.Run(ctx)
	}()

	select {
	case <-stores.touched:
	case <-time.After(5 * time.Second):
		t.Fatal("the startup pass did not run")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return when the context was cancelled")
	}
}

// TestRunOncePrunesDataQualityRetention pins the data quality cutoffs: results go
// with their run, only resolved issues and decided suggestions are pruned, and
// profiles and the LLM cache expire on their shorter window.
func TestRunOncePrunesDataQualityRetention(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	stores.deleted["quality_runs"] = 2
	stores.deleted["quality_issues"] = 3
	stores.deleted["column_profiles"] = 4
	stores.deleted["data_quality_llm_cache"] = 5
	stores.deleted["quality_suggestions"] = 6

	summary := newTestRunner(stores, &fakeRevalidator{}).runOnce(context.Background())

	require.Empty(t, summary.failed)
	require.Equal(t, int64(2), summary.counts["quality_runs"])
	require.Equal(t, int64(3), summary.counts["quality_issues"])
	require.Equal(t, int64(4), summary.counts["column_profiles"])
	require.Equal(t, int64(5), summary.counts["data_quality_llm_cache"])
	require.Equal(t, int64(6), summary.counts["quality_suggestions"])

	require.Equal(t, passTime.Add(-qualityRunRetention), stores.cutoff("quality_runs"))
	require.Equal(t, passTime.Add(-qualityIssueRetention), stores.cutoff("quality_issues"))
	require.Equal(t, passTime.Add(-qualityProfileRetention), stores.cutoff("column_profiles"))
	require.Equal(t, passTime.Add(-qualityLLMCacheRetention), stores.cutoff("data_quality_llm_cache"))
	require.Equal(t, passTime.Add(-qualitySuggestionRetention), stores.cutoff("quality_suggestions"))
}

// The deleted-rows counter is a row counter: the two lineage items count edges
// re-checked, so folding them in would make the value meaningless.
func TestRowCountsDropsLineageEdgeCounts(t *testing.T) {
	t.Parallel()

	rows := rowCounts(map[string]int64{
		"llm_debug_log":              3,
		"unresolved_lineage":         5,
		"contradicted_column_claims": 7,
	})
	require.Equal(t, map[string]int64{"llm_debug_log": 3}, rows)
	require.Empty(t, rowCounts(nil))
}

// One pass records one pass counter and one deleted-rows entry per item; the
// lineage edge counts stay out of it, and a failed item is visible as such.
func TestPassMetricsRecordOutcomeAndItems(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	stores.deleted["llm_debug_log"] = 3
	stores.errs["revoked_tokens"] = errors.New("table is locked")

	registry := prometheus.NewRegistry()
	m := metrics.New()
	require.NoError(t, m.Register(registry))

	runner := newTestRunner(stores, &fakeRevalidator{revalidated: 2})
	runner.metrics = m
	runner.runOnce(context.Background())

	require.Equal(t, 1.0, gatheredCounter(t, registry, "metaxisdata_maintenance_passes_total", map[string]string{"outcome": "partial"}))
	require.Equal(t, 3.0, gatheredCounter(t, registry, "metaxisdata_maintenance_deleted_rows_total", map[string]string{"item": "llm_debug_log"}))
	require.Equal(t, 1.0, gatheredCounter(t, registry, "metaxisdata_maintenance_item_failures_total", map[string]string{"item": "revoked_tokens"}))
	require.Equal(t, 0.0, gatheredCounter(t, registry, "metaxisdata_maintenance_deleted_rows_total", map[string]string{"item": "unresolved_lineage"}),
		"an edge count must not be reported as deleted rows")
}

// gatheredCounter returns one counter's value from a gathered registry, matched on
// its full label set; a series that was never recorded is 0.
func gatheredCounter(t *testing.T, registry *prometheus.Registry, name string, labels map[string]string) float64 {
	t.Helper()

	families, err := registry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if len(metric.GetLabel()) != len(labels) {
				continue
			}
			matches := true
			for _, pair := range metric.GetLabel() {
				value, ok := labels[pair.GetName()]
				if !ok || value != pair.GetValue() {
					matches = false
					break
				}
			}
			if matches && metric.GetCounter() != nil {
				return metric.GetCounter().GetValue()
			}
		}
	}
	return 0
}
