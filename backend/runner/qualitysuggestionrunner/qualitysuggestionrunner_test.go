package qualitysuggestionrunner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// fakeJobStore is the in-memory job store the runner tests drive. Claim returns the
// queued jobs in order, then nil; every write is recorded so the test can assert
// what the runner persisted and finished.
type fakeJobStore struct {
	mu sync.Mutex

	pending []*store.QualitySuggestionJobMessage
	claimed *store.QualitySuggestionJobMessage
	// runningJobs are the jobs a previous process left RUNNING; the fake sweep
	// emulates the store predicate (status RUNNING and started before the cutoff)
	// so a test proves which cutoff the runner passes.
	runningJobs  []*store.QualitySuggestionJobMessage
	cancelOnGet  bool
	getCalls     int
	getErr       error
	progress     []*storepb.QualitySuggestionJobTargets
	statuses     []storepb.QualitySuggestionJobStatus
	errors       []string
	staleCutoffs []time.Time
	staleCount   int64
	claimErr     error

	finished chan struct{}
}

func newFakeJobStore(jobs ...*store.QualitySuggestionJobMessage) *fakeJobStore {
	return &fakeJobStore{pending: jobs, finished: make(chan struct{}, len(jobs)+1)}
}

func (f *fakeJobStore) AbandonStaleQualitySuggestionJobs(_ context.Context, cutoff time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.staleCutoffs = append(f.staleCutoffs, cutoff)
	var abandoned int64
	for _, job := range f.runningJobs {
		if job.Status != storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING {
			continue
		}
		if job.StartedAt != nil && job.StartedAt.Before(cutoff) {
			job.Status = storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED
			abandoned++
		}
	}
	return abandoned + f.staleCount, nil
}

func (f *fakeJobStore) ClaimPendingQualitySuggestionJob(_ context.Context, _ time.Time) (*store.QualitySuggestionJobMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	if len(f.pending) == 0 {
		return nil, nil
	}
	job := f.pending[0]
	f.pending = f.pending[1:]
	f.claimed = job
	return job, nil
}

func (f *fakeJobStore) GetQualitySuggestionJob(_ context.Context, _ *store.FindQualitySuggestionJobMessage) (*store.QualitySuggestionJobMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	job := cloneJobMessage(f.claimed)
	job.CancelRequested = f.cancelOnGet
	return job, nil
}

func (f *fakeJobStore) UpdateQualitySuggestionJobProgress(_ context.Context, _ int64, targets *storepb.QualitySuggestionJobTargets, _, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.progress = append(f.progress, cloneJobTargets(targets))
	return nil
}

func (f *fakeJobStore) FinishQualitySuggestionJob(_ context.Context, _ int64, status storepb.QualitySuggestionJobStatus, errText string, _ time.Time) error {
	f.mu.Lock()
	f.statuses = append(f.statuses, status)
	f.errors = append(f.errors, errText)
	f.mu.Unlock()
	f.finished <- struct{}{}
	return nil
}

// cloneJobMessage copies a job so a runner write cannot mutate the fake's stored
// state; Targets is the only proto field and gets its own clone.
func cloneJobMessage(job *store.QualitySuggestionJobMessage) *store.QualitySuggestionJobMessage {
	if job == nil {
		return &store.QualitySuggestionJobMessage{}
	}
	cloned := *job
	cloned.Targets = cloneJobTargets(job.Targets)
	return &cloned
}

// cloneJobTargets deep-copies a target snapshot.
func cloneJobTargets(targets *storepb.QualitySuggestionJobTargets) *storepb.QualitySuggestionJobTargets {
	if targets == nil {
		return nil
	}
	cloned, ok := proto.Clone(targets).(*storepb.QualitySuggestionJobTargets)
	if !ok {
		return &storepb.QualitySuggestionJobTargets{}
	}
	return cloned
}

// fakeSuggester returns a configurable draft outcome per target and records the
// order the runner visited the targets in.
type fakeSuggester struct {
	mu        sync.Mutex
	configErr error
	config    llm.ResolvedConfig
	draft     func(targetGUID string) ([]*store.QualitySuggestionMessage, *storepb.QualityLLMDiagnostic, error)
	calls     []string
}

func (f *fakeSuggester) ResolveConfig(context.Context) (llm.ResolvedConfig, dataquality.Settings, error) {
	if f.configErr != nil {
		return llm.ResolvedConfig{}, dataquality.Settings{}, f.configErr
	}
	return f.config, dataquality.Settings{}, nil
}

func (f *fakeSuggester) Draft(_ context.Context, _ llm.ResolvedConfig, targetGUID string, _ storepb.MetaType, _ []string, _, _ string, _ *int32) ([]*store.QualitySuggestionMessage, *storepb.QualityLLMDiagnostic, error) {
	f.mu.Lock()
	f.calls = append(f.calls, targetGUID)
	f.mu.Unlock()
	if f.draft != nil {
		return f.draft(targetGUID)
	}
	return []*store.QualitySuggestionMessage{{ResourceID: targetGUID + "-s1"}}, nil, nil
}

func (f *fakeSuggester) draftedTargets() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// jobTestMessage builds one RUNNING job with the given target GUIDs in PENDING
// state, as a claim would return it.
func jobTestMessage(guids ...string) *store.QualitySuggestionJobMessage {
	return jobTestMessageFor(7, "job1", guids...)
}

// jobTestMessageFor builds one RUNNING job with an explicit row id and resource id.
func jobTestMessageFor(id int64, resourceID string, guids ...string) *store.QualitySuggestionJobMessage {
	targets := &storepb.QualitySuggestionJobTargets{}
	for _, guid := range guids {
		targets.Targets = append(targets.Targets, &storepb.QualitySuggestionJobTarget{
			TargetGuid: guid,
			TargetType: int32(storepb.MetaType_TABLE),
			State:      storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING,
		})
	}
	return &store.QualitySuggestionJobMessage{
		ID:         id,
		ResourceID: resourceID,
		Status:     storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING,
		Intent:     "email must not be empty",
		Language:   "en",
		Targets:    targets,
	}
}

func newTestRunner(stores JobStore, suggester DraftSuggester, now time.Time) *Runner {
	return &Runner{
		store:     stores,
		suggester: suggester,
		now:       func() time.Time { return now },
		tick:      time.Millisecond,
	}
}

// A claimed job visits its targets in order, persists progress after every target,
// records the queued suggestions and finishes COMPLETED.
func TestProcessJobCompletesEveryTargetInOrder(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 9, 0, 0, 0, time.UTC)
	stores := newFakeJobStore(jobTestMessage("target-a", "target-b"))
	suggester := &fakeSuggester{config: llm.ResolvedConfig{ProfileName: "profiles/openai", ModelName: "gpt-4"}}
	runner := newTestRunner(stores, suggester, now)

	job, err := stores.ClaimPendingQualitySuggestionJob(context.Background(), now)
	require.NoError(t, err)
	runner.processJob(context.Background(), job)

	require.Equal(t, []string{"target-a", "target-b"}, suggester.draftedTargets())
	require.Len(t, stores.statuses, 1)
	require.Equal(t, storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED, stores.statuses[0])
	require.Empty(t, stores.errors[0])

	final := stores.progress[len(stores.progress)-1].GetTargets()
	require.Len(t, final, 2)
	for index, guid := range []string{"target-a", "target-b"} {
		require.Equal(t, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE, final[index].GetState(), guid)
		require.Equal(t, []string{guid + "-s1"}, final[index].GetSuggestionResourceIds())
		require.Equal(t, now.UnixMilli(), final[index].GetStartedAtUnixMs())
		require.Equal(t, now.UnixMilli(), final[index].GetFinishedAtUnixMs())
	}
	// Progress is written before every target and after it, so a poll never sees a
	// stale snapshot for long.
	require.GreaterOrEqual(t, len(stores.progress), 3)
}

// A failed target keeps the diagnostic the suggester classified, so the job panel
// can explain the failure without the server log.
func TestProcessJobPersistsTheFailureDiagnostic(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 9, 0, 0, 0, time.UTC)
	stores := newFakeJobStore(jobTestMessage("target-a"))
	diagnostic := &storepb.QualityLLMDiagnostic{
		ErrorCode:       "TRUNCATED",
		Provider:        "profiles/openai",
		Model:           "gpt-4",
		PromptVersion:   "dq.rule_draft.v1",
		MaxOutputTokens: 8192,
		FinishReason:    "length",
		CorrelationId:   "corr-1",
		PromptChars:     1234,
	}
	suggester := &fakeSuggester{draft: func(string) ([]*store.QualitySuggestionMessage, *storepb.QualityLLMDiagnostic, error) {
		return nil, diagnostic, errors.New("the model stopped at the output token limit")
	}}
	runner := newTestRunner(stores, suggester, now)

	job, err := stores.ClaimPendingQualitySuggestionJob(context.Background(), now)
	require.NoError(t, err)
	runner.processJob(context.Background(), job)

	persisted := stores.progress[len(stores.progress)-1].GetTargets()[0]
	require.Equal(t, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_FAILED, persisted.GetState())
	require.Contains(t, persisted.GetError(), "output token limit", "the human error text is kept")
	require.NotNil(t, persisted.GetDiagnostic())
	require.Equal(t, "TRUNCATED", persisted.GetDiagnostic().GetErrorCode())
	require.Equal(t, "length", persisted.GetDiagnostic().GetFinishReason())
	require.Equal(t, "corr-1", persisted.GetDiagnostic().GetCorrelationId())
	require.Equal(t, int32(8192), persisted.GetDiagnostic().GetMaxOutputTokens())
	require.Equal(t, "profiles/openai", persisted.GetDiagnostic().GetProvider())
}

// A target the model could not draft is recorded FAILED and the rest still run.
func TestProcessJobContinuesAfterATargetFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 9, 0, 0, 0, time.UTC)
	stores := newFakeJobStore(jobTestMessage("target-a", "target-b"))
	suggester := &fakeSuggester{draft: func(guid string) ([]*store.QualitySuggestionMessage, *storepb.QualityLLMDiagnostic, error) {
		if guid == "target-a" {
			return nil, nil, errors.New("the model returned nonsense")
		}
		return []*store.QualitySuggestionMessage{{ResourceID: guid + "-s1"}}, nil, nil
	}}
	runner := newTestRunner(stores, suggester, now)

	job, err := stores.ClaimPendingQualitySuggestionJob(context.Background(), now)
	require.NoError(t, err)
	runner.processJob(context.Background(), job)

	require.Equal(t, []string{"target-a", "target-b"}, suggester.draftedTargets())
	require.Equal(t, storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED, stores.statuses[0])
	final := stores.progress[len(stores.progress)-1].GetTargets()
	require.Equal(t, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_FAILED, final[0].GetState())
	require.Contains(t, final[0].GetError(), "nonsense")
	require.Equal(t, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE, final[1].GetState())
}

// A cancel request observed between targets stops the job: the current target's
// outcome stands, the rest are SKIPPED and the job ends CANCELED.
func TestProcessJobStopsWhenCancellationIsRequested(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 9, 0, 0, 0, time.UTC)
	stores := newFakeJobStore(jobTestMessage("target-a", "target-b"))
	suggester := &fakeSuggester{}
	runner := newTestRunner(stores, suggester, now)

	job, err := stores.ClaimPendingQualitySuggestionJob(context.Background(), now)
	require.NoError(t, err)
	// The reviewer cancels while the first target is being drafted: the runner
	// observes it before visiting the second target.
	suggester.draft = func(guid string) ([]*store.QualitySuggestionMessage, *storepb.QualityLLMDiagnostic, error) {
		stores.mu.Lock()
		stores.cancelOnGet = true
		stores.mu.Unlock()
		return []*store.QualitySuggestionMessage{{ResourceID: guid + "-s1"}}, nil, nil
	}
	runner.processJob(context.Background(), job)

	require.Equal(t, []string{"target-a"}, suggester.draftedTargets())
	require.Equal(t, storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_CANCELED, stores.statuses[0])
	final := stores.progress[len(stores.progress)-1].GetTargets()
	require.Equal(t, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE, final[0].GetState())
	require.Equal(t, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED, final[1].GetState())
	require.Empty(t, final[1].GetSuggestionResourceIds())
}

// When the daily budget runs out mid-job the current target FAILED, the rest
// SKIPPED and the job FAILED with the quota message: the model is never asked again.
func TestProcessJobStopsWhenTheQuotaRunsOut(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 9, 0, 0, 0, time.UTC)
	stores := newFakeJobStore(jobTestMessage("target-a", "target-b", "target-c"))
	suggester := &fakeSuggester{draft: func(string) ([]*store.QualitySuggestionMessage, *storepb.QualityLLMDiagnostic, error) {
		return nil, nil, connect.NewError(connect.CodeResourceExhausted, errors.New("the daily quota is used"))
	}}
	runner := newTestRunner(stores, suggester, now)

	job, err := stores.ClaimPendingQualitySuggestionJob(context.Background(), now)
	require.NoError(t, err)
	runner.processJob(context.Background(), job)

	require.Equal(t, []string{"target-a"}, suggester.draftedTargets(), "a quota stop never asks the model again")
	require.Equal(t, storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED, stores.statuses[0])
	require.Contains(t, stores.errors[0], "quota")
	final := stores.progress[len(stores.progress)-1].GetTargets()
	require.Equal(t, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_FAILED, final[0].GetState())
	require.Equal(t, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED, final[1].GetState())
	require.Equal(t, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED, final[2].GetState())
}

// A shutdown before the job starts never asks the model: every target is SKIPPED
// and the job FAILED, so a poll after the restart does not wait forever.
func TestProcessJobClosesOnShutdown(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 9, 0, 0, 0, time.UTC)
	stores := newFakeJobStore(jobTestMessage("target-a"))
	suggester := &fakeSuggester{}
	runner := newTestRunner(stores, suggester, now)

	job, err := stores.ClaimPendingQualitySuggestionJob(context.Background(), now)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner.processJob(ctx, job)

	require.Empty(t, suggester.draftedTargets())
	require.Equal(t, storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED, stores.statuses[0])
	require.Contains(t, stores.errors[0], "shut down")
	require.Equal(t, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED,
		stores.progress[len(stores.progress)-1].GetTargets()[0].GetState())
}

// A job whose provider configuration cannot be resolved fails before any model
// call, with every target SKIPPED.
func TestProcessJobFailsWhenConfigIsUnavailable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 9, 0, 0, 0, time.UTC)
	stores := newFakeJobStore(jobTestMessage("target-a", "target-b"))
	suggester := &fakeSuggester{configErr: connect.NewError(connect.CodeFailedPrecondition, errors.New("LLM analysis is disabled"))}
	runner := newTestRunner(stores, suggester, now)

	job, err := stores.ClaimPendingQualitySuggestionJob(context.Background(), now)
	require.NoError(t, err)
	runner.processJob(context.Background(), job)

	require.Empty(t, suggester.draftedTargets())
	require.Equal(t, storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED, stores.statuses[0])
	require.Contains(t, stores.errors[0], "disabled")
	for _, target := range stores.progress[len(stores.progress)-1].GetTargets() {
		require.Equal(t, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED, target.GetState())
	}
}

// The startup sweep asks the store to abandon every RUNNING job: the cutoff is the
// startup instant, not an age window.
func TestAbandonStaleUsesNowAsTheCutoff(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 9, 0, 0, 0, time.UTC)
	stores := newFakeJobStore()
	stores.staleCount = 2
	runner := newTestRunner(stores, &fakeSuggester{}, now)

	runner.abandonStale(context.Background())

	require.Len(t, stores.staleCutoffs, 1)
	require.Equal(t, now, stores.staleCutoffs[0])
}

// A RUNNING job started a minute before an unclean restart is orphaned too: at
// startup, no RUNNING job can have a live owner (single replica), so Run fails it.
// The old now-15m window left exactly this job RUNNING forever.
func TestRunFailsRunningJobsLeftByAPreviousProcess(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 9, 0, 0, 0, time.UTC)
	startedAt := now.Add(-time.Minute)
	orphan := jobTestMessageFor(9, "orphan", "target-a")
	orphan.StartedAt = &startedAt

	stores := newFakeJobStore()
	stores.runningJobs = append(stores.runningJobs, orphan)
	runner := newTestRunner(stores, &fakeSuggester{}, now)
	runner.tick = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()
	require.Eventually(t, func() bool {
		stores.mu.Lock()
		defer stores.mu.Unlock()
		return orphan.Status == storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED
	}, 5*time.Second, 10*time.Millisecond, "the orphaned RUNNING job was not failed at startup")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the runner did not stop after its context was cancelled")
	}
	require.NotEmpty(t, stores.runningJobs)
}

// A cancel re-read that errors must not fall through to COMPLETED with unvisited
// targets: the remaining targets are SKIPPED and the job ends FAILED.
func TestProcessJobFailsWhenTheCancelRecheckErrors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 9, 0, 0, 0, time.UTC)
	stores := newFakeJobStore(jobTestMessage("target-a", "target-b"))
	suggester := &fakeSuggester{}
	runner := newTestRunner(stores, suggester, now)
	stores.getErr = errors.New("connection refused")

	job, err := stores.ClaimPendingQualitySuggestionJob(context.Background(), now)
	require.NoError(t, err)
	runner.processJob(context.Background(), job)

	require.Empty(t, suggester.draftedTargets(), "the job must not start a target it cannot re-check")
	require.Equal(t, storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED, stores.statuses[0])
	require.Contains(t, stores.errors[0], "failed to check for cancellation")
	final := stores.progress[len(stores.progress)-1].GetTargets()
	for _, target := range final {
		require.Equal(t, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED, target.GetState())
	}
}

// A panic in the drafting pipeline closes the claimed job and does not kill the
// runner loop: the next queued job still completes.
func TestRunSurvivesAPanickingSuggester(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 9, 0, 0, 0, time.UTC)
	panicking := jobTestMessageFor(7, "job-panic", "target-panic")
	following := jobTestMessageFor(8, "job-next", "target-b")
	stores := newFakeJobStore(panicking, following)
	suggester := &fakeSuggester{draft: func(guid string) ([]*store.QualitySuggestionMessage, *storepb.QualityLLMDiagnostic, error) {
		if guid == "target-panic" {
			panic("the drafting pipeline exploded")
		}
		return []*store.QualitySuggestionMessage{{ResourceID: guid + "-s1"}}, nil, nil
	}}
	runner := newTestRunner(stores, suggester, now)
	runner.tick = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()
	for range 2 {
		select {
		case <-stores.finished:
		case <-time.After(5 * time.Second):
			t.Fatal("the runner stopped after the panic instead of finishing both jobs")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the runner did not stop after its context was cancelled")
	}

	require.Equal(t, []storepb.QualitySuggestionJobStatus{
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED,
	}, stores.statuses, "the panicked job is failed and the following job still completes")
	require.Contains(t, stores.errors[0], "unexpectedly")
	require.Equal(t, []string{"target-panic", "target-b"}, suggester.draftedTargets(),
		"the panicking target was attempted; the following job still ran")
}

// Run sweeps stale jobs, drains the pending queue and then waits for the next tick;
// cancelling it returns.
func TestRunSweepsThenDrains(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 9, 0, 0, 0, time.UTC)
	stores := newFakeJobStore(jobTestMessage("target-a"))
	suggester := &fakeSuggester{}
	runner := newTestRunner(stores, suggester, now)
	runner.tick = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()

	select {
	case <-stores.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the runner never finished the queued job")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the runner did not stop after its context was cancelled")
	}
	require.Len(t, stores.staleCutoffs, 1)
	require.Equal(t, []string{"target-a"}, suggester.draftedTargets())
	require.Equal(t, storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED, stores.statuses[0])
}

// A claim error is logged and the loop waits for the next tick instead of spinning.
func TestRunSurvivesAClaimError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 9, 0, 0, 0, time.UTC)
	stores := newFakeJobStore()
	stores.claimErr = errors.New("connection refused")
	runner := newTestRunner(stores, &fakeSuggester{}, now)
	runner.tick = time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	require.NotPanics(t, func() { runner.Run(ctx) })
	require.Empty(t, stores.statuses)
}
