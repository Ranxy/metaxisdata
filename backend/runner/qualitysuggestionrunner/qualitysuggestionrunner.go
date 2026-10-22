// Package qualitysuggestionrunner runs the durable background AI drafting jobs.
//
// A job is one intent over 1..20 targets, stored as a single quality_suggestion_job
// row. This runner claims a PENDING job atomically, resolves the workspace LLM
// configuration once, then asks the model for one target at a time, persisting the
// per-target progress after every target. A crash leaves a RUNNING job; because the
// deployment assumes one replica, every RUNNING job found at startup belongs to a
// previous process, so the startup sweep marks all of them FAILED. Suggestions
// already queued stay in the review queue.
package qualitysuggestionrunner

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// tickInterval is how often the runner looks for a pending job. The scan is a
	// single indexed read, so the loop is cheap.
	tickInterval = 5 * time.Second
	// finalizeTimeout bounds the writes that close a job on shutdown. They use a
	// detached context because the runner's own is cancelled by the shutdown that
	// ended the wait.
	finalizeTimeout = 10 * time.Second
	// maxErrorMessageLength bounds the error text stored on a job or target. Error
	// details are for an operator, not a log file.
	maxErrorMessageLength = 1024
)

// JobStore is the slice of *store.Store the runner needs. Narrowing it here is
// what lets the loop be tested with a fake: the runner owns the order, the store
// owns the SQL.
type JobStore interface {
	AbandonStaleQualitySuggestionJobs(ctx context.Context, cutoff time.Time) (int64, error)
	ClaimPendingQualitySuggestionJob(ctx context.Context, now time.Time) (*store.QualitySuggestionJobMessage, error)
	GetQualitySuggestionJob(ctx context.Context, find *store.FindQualitySuggestionJobMessage) (*store.QualitySuggestionJobMessage, error)
	UpdateQualitySuggestionJobProgress(ctx context.Context, id int64, targets *storepb.QualitySuggestionJobTargets, provider, model, promptVersion string) error
	FinishQualitySuggestionJob(ctx context.Context, id int64, status storepb.QualitySuggestionJobStatus, errText string, at time.Time) error
}

// DraftSuggester is the drafting pipeline the runner drives. *dataquality.Suggester
// implements it; a test uses a fake to control latency, outcome and quota errors.
type DraftSuggester interface {
	ResolveConfig(ctx context.Context) (llm.ResolvedConfig, dataquality.Settings, error)
	Draft(ctx context.Context, config llm.ResolvedConfig, limits dataquality.LLMLimits, targetGUID string, metaType storepb.MetaType, columns []string, intent, language string, createdBy *int32) ([]*store.QualitySuggestionMessage, *storepb.QualityLLMDiagnostic, error)
}

// Runner claims and executes background AI drafting jobs.
type Runner struct {
	store     JobStore
	suggester DraftSuggester
	// now is time.Now.UTC in production; a test fixes it so the sweep cutoff is
	// exact.
	now func() time.Time
	// tick carries the production constant; a test overrides it.
	tick time.Duration
	// loadLimits reloads the quota policy for one target. It is a field so a test
	// can return different limits per target; production re-reads the workspace
	// settings, while the provider config stays resolved once per job.
	loadLimits func(ctx context.Context) (dataquality.LLMLimits, error)
}

// NewRunner creates the suggestion-job runner. It takes the drafting pipeline as
// the DraftSuggester interface it drives: production passes the shared
// *dataquality.Suggester, and a test can pass a fake against a real store.
func NewRunner(stores *store.Store, suggester DraftSuggester) *Runner {
	return &Runner{
		store:     stores,
		suggester: suggester,
		now:       func() time.Time { return time.Now().UTC() },
		tick:      tickInterval,
		loadLimits: func(ctx context.Context) (dataquality.LLMLimits, error) {
			settings, err := dataquality.LoadSettings(ctx, stores)
			if err != nil {
				return dataquality.LLMLimits{}, err
			}
			return dataquality.LimitsFromSettings(settings), nil
		},
	}
}

// Name identifies the runner in the server's lifecycle logs.
func (*Runner) Name() string { return "qualitysuggestionrunner" }

// Run blocks until ctx is cancelled. It first fails every job a previous process
// left RUNNING, then repeatedly claims and executes pending jobs until none is left,
// sleeping for the tick between drains.
func (r *Runner) Run(ctx context.Context) {
	r.abandonStale(ctx)

	ticker := time.NewTicker(r.tick)
	defer ticker.Stop()
	for {
		for {
			if ctx.Err() != nil {
				return
			}
			job, err := r.store.ClaimPendingQualitySuggestionJob(ctx, r.now())
			if err != nil {
				slog.Error("failed to claim a quality suggestion job", log.WithError(err))
				break
			}
			if job == nil {
				break
			}
			r.processJobIsolated(ctx, job)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// abandonStale fails every job a previous process left RUNNING. It is a startup-only
// sweep: a restart does not resume the model calls it interrupted. The cutoff is
// "now", not an age window: the deployment assumes one replica, so at startup no
// RUNNING job can have a live owner, and a job started seconds before an unclean
// restart is just as orphaned as one started hours before it. A job left RUNNING
// would otherwise be unclaimable (cancel only sets cancel_requested), unfinishable
// and unprunable forever.
func (r *Runner) abandonStale(ctx context.Context) {
	now := r.now()
	abandoned, err := r.store.AbandonStaleQualitySuggestionJobs(ctx, now)
	if err != nil {
		slog.Error("failed to abandon stale quality suggestion jobs", log.WithError(err))
		return
	}
	if abandoned > 0 {
		slog.Warn("abandoned quality suggestion jobs left behind by a previous process",
			slog.Int64("count", abandoned),
			slog.Time("started_before", now))
	}
}

// processJob executes one claimed RUNNING job. The job row is RUNNING and its
// targets are the submission snapshot.
func (r *Runner) processJob(ctx context.Context, job *store.QualitySuggestionJobMessage) {
	targets := job.Targets
	if targets == nil {
		targets = &storepb.QualitySuggestionJobTargets{}
	}

	config, settings, err := r.suggester.ResolveConfig(ctx)
	if err != nil {
		// A job-level failure: the model was never asked for any target.
		markPendingTargetsSkipped(targets, r.now())
		r.persist(ctx, job, targets)
		r.finish(ctx, job, storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED,
			fmt.Sprintf("the drafting job could not start: %v", err))
		return
	}
	// The provider config is resolved once per job; the quota policy is re-read for
	// every target so a settings edit applies to the next analysis. A failed reload
	// keeps the job's snapshot rather than failing the target.
	jobLimits := dataquality.LimitsFromSettings(settings)
	provider, model, promptVersion := dataquality.ProviderFacts(config, dataquality.RuleDraftPromptVersion)
	// The provider facts belong to the job, not to one target: persist them
	// together with the first progress write.
	job.Provider, job.Model, job.PromptVersion = provider, model, promptVersion
	r.persist(ctx, job, targets)

	for index, target := range targets.GetTargets() {
		if target == nil || target.GetState() != storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING {
			continue
		}
		if ctx.Err() != nil {
			// The process is shutting down: the current target was never asked.
			r.stopRemaining(ctx, job, targets, index, storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED,
				"the server shut down before the job finished")
			return
		}
		canceled, cancelErr := r.cancelRequested(ctx, job.ResourceID)
		if cancelErr != nil {
			// The runner could not prove the job was not cancelled. It must not
			// report COMPLETED with unvisited targets: close it as FAILED instead.
			slog.Warn("failed to re-read a quality suggestion job to check for cancellation",
				slog.String("job", job.ResourceID), log.WithError(cancelErr))
			r.stopRemaining(ctx, job, targets, index, storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED,
				fmt.Sprintf("failed to check for cancellation: %v", cancelErr))
			return
		}
		if canceled {
			r.stopRemaining(ctx, job, targets, index, storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_CANCELED, "")
			return
		}

		startedAt := r.now()
		target.State = storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_RUNNING
		target.StartedAtUnixMs = startedAt.UnixMilli()
		r.persist(ctx, job, targets)

		limits := jobLimits
		if r.loadLimits != nil {
			reloaded, loadErr := r.loadLimits(ctx)
			if loadErr != nil {
				slog.Warn("failed to reload the quality suggestion job limits; using the job's snapshot",
					slog.String("job", job.ResourceID), log.WithError(loadErr))
			} else {
				limits = reloaded
			}
		}
		created, diagnostic, draftErr := r.suggester.Draft(ctx, config, limits,
			target.GetTargetGuid(), storepb.MetaType(target.GetTargetType()), target.GetColumns(), job.Intent, job.Language, job.CreatedBy)
		if draftErr != nil {
			target.Error = truncateJobError(draftErr)
			target.Diagnostic = diagnostic
			target.State = storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_FAILED
			target.FinishedAtUnixMs = r.now().UnixMilli()
			slog.Warn("quality suggestion job target failed",
				slog.String("job", job.ResourceID),
				slog.String("target", target.GetTargetGuid()),
				slog.String("code", diagnostic.GetErrorCode()),
				slog.String("correlation_id", diagnostic.GetCorrelationId()),
				slog.Any("error", draftErr))
			if ctx.Err() != nil {
				r.stopRemaining(ctx, job, targets, index, storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED,
					"the server shut down before the job finished")
				return
			}
			if connect.CodeOf(draftErr) == connect.CodeResourceExhausted {
				// The daily budget ran out mid-job: the rest of the targets are
				// never asked, and the job says why.
				r.stopRemaining(ctx, job, targets, index, storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED,
					fmt.Sprintf("the daily data quality LLM analysis quota ran out before every target was drafted: %v", draftErr))
				return
			}
			// A per-target failure: record it and keep going.
			r.persist(ctx, job, targets)
			continue
		}

		target.Error = ""
		target.State = storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE
		target.FinishedAtUnixMs = r.now().UnixMilli()
		target.SuggestionResourceIds = make([]string, 0, len(created))
		for _, suggestion := range created {
			target.SuggestionResourceIds = append(target.SuggestionResourceIds, suggestion.ResourceID)
		}
		r.persist(ctx, job, targets)
	}

	r.finish(ctx, job, storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED, "")
	slog.Info("quality suggestion job completed",
		slog.String("job", job.ResourceID),
		slog.Int("targets", len(targets.GetTargets())),
		slog.Int("suggestions", jobSuggestionCount(targets)))
}

// processJobIsolated runs one claimed job behind a recover boundary. A panic in the
// drafting pipeline must not kill the runner loop for the rest of the process: the
// job is closed (best effort) and the loop continues with the next one.
func (r *Runner) processJobIsolated(ctx context.Context, job *store.QualitySuggestionJobMessage) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		slog.Error("quality suggestion job panicked",
			slog.String("job", job.ResourceID),
			slog.Any("panic", recovered),
			log.Stack("panic-stack"))
		r.failPanickedJob(job)
	}()
	r.processJob(ctx, job)
}

// failPanickedJob closes a job whose processing panicked: the target being drafted
// is FAILED ("interrupted"), untouched targets are SKIPPED, and the job itself is
// FAILED. It uses a detached write budget because the runner's context may already
// be cancelled, and it is best effort: if the write is lost too, the next startup
// sweep still fails the RUNNING row.
func (r *Runner) failPanickedJob(job *store.QualitySuggestionJobMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), finalizeTimeout)
	defer cancel()

	targets := job.Targets
	if targets == nil {
		targets = &storepb.QualitySuggestionJobTargets{}
	}
	now := r.now()
	for _, target := range targets.GetTargets() {
		if target == nil {
			continue
		}
		switch target.GetState() {
		case storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_RUNNING:
			target.State = storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_FAILED
			target.Error = "interrupted: the drafting job panicked"
			target.FinishedAtUnixMs = now.UnixMilli()
		case storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING,
			storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_UNSPECIFIED:
			target.State = storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED
			target.FinishedAtUnixMs = now.UnixMilli()
		case storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE,
			storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_FAILED,
			storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED:
			// The target's outcome is history; a panic does not rewrite it.
		default:
			// Unknown states are left as stored.
		}
	}
	if err := r.store.UpdateQualitySuggestionJobProgress(ctx, job.ID, targets, job.Provider, job.Model, job.PromptVersion); err != nil {
		slog.Warn("failed to persist the targets of a panicked quality suggestion job",
			slog.String("job", job.ResourceID), log.WithError(err))
	}
	finishErr := r.store.FinishQualitySuggestionJob(ctx, job.ID,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED,
		"the drafting job failed unexpectedly; queued suggestions are kept", now)
	if finishErr != nil {
		slog.Error("failed to finish a panicked quality suggestion job",
			slog.String("job", job.ResourceID), log.WithError(finishErr))
	}
}

// cancelRequested re-reads the job and reports whether a reviewer asked it to stop.
func (r *Runner) cancelRequested(ctx context.Context, resourceID string) (bool, error) {
	job, err := r.store.GetQualitySuggestionJob(ctx, &store.FindQualitySuggestionJobMessage{ResourceID: &resourceID})
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, fmt.Errorf("quality suggestion job %q disappeared", resourceID)
	}
	return job.CancelRequested, nil
}

// stopRemaining closes a job while it is between targets: the target at index is
// whatever the caller left it as, every later PENDING target is SKIPPED, the
// progress is persisted and the job is finished with status.
func (r *Runner) stopRemaining(ctx context.Context, job *store.QualitySuggestionJobMessage, targets *storepb.QualitySuggestionJobTargets, index int, status storepb.QualitySuggestionJobStatus, errText string) {
	for _, target := range targets.GetTargets()[index:] {
		if target == nil || target.GetState() != storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING {
			continue
		}
		target.State = storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED
		target.FinishedAtUnixMs = r.now().UnixMilli()
	}
	r.persist(ctx, job, targets)
	r.finish(ctx, job, status, errText)
	slog.Info("quality suggestion job stopped",
		slog.String("job", job.ResourceID),
		slog.String("status", status.String()),
		slog.Int("suggestions", jobSuggestionCount(targets)))
}

// markPendingTargetsSkipped marks every target that was never asked.
func markPendingTargetsSkipped(targets *storepb.QualitySuggestionJobTargets, at time.Time) {
	for _, target := range targets.GetTargets() {
		if target == nil || target.GetState() != storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING {
			continue
		}
		target.State = storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED
		target.FinishedAtUnixMs = at.UnixMilli()
	}
}

// persist writes the per-target snapshot and the job's provider facts. A failure
// is logged, not fatal: the next write retries the same state, and the final write
// still records the outcome.
func (r *Runner) persist(ctx context.Context, job *store.QualitySuggestionJobMessage, targets *storepb.QualitySuggestionJobTargets) {
	writeCtx := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		writeCtx, cancel = context.WithTimeout(context.Background(), finalizeTimeout)
		defer cancel()
	}
	if err := r.store.UpdateQualitySuggestionJobProgress(writeCtx, job.ID, targets, job.Provider, job.Model, job.PromptVersion); err != nil {
		slog.Warn("failed to persist quality suggestion job progress",
			slog.String("job", job.ResourceID), log.WithError(err))
	}
}

// finish writes the terminal status. It uses a detached context on shutdown so the
// job does not stay RUNNING for the next startup sweep.
func (r *Runner) finish(ctx context.Context, job *store.QualitySuggestionJobMessage, status storepb.QualitySuggestionJobStatus, errText string) {
	writeCtx := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		writeCtx, cancel = context.WithTimeout(context.Background(), finalizeTimeout)
		defer cancel()
	}
	if err := r.store.FinishQualitySuggestionJob(writeCtx, job.ID, status, truncateJobErrorText(errText), r.now()); err != nil {
		slog.Error("failed to finish a quality suggestion job",
			slog.String("job", job.ResourceID),
			slog.String("status", status.String()),
			log.WithError(err))
	}
}

// truncateJobError renders an error into the bounded text stored on a row.
func truncateJobError(err error) string {
	if err == nil {
		return ""
	}
	return truncateJobErrorText(err.Error())
}

// truncateJobErrorText bounds stored error text.
func truncateJobErrorText(text string) string {
	if len(text) <= maxErrorMessageLength {
		return text
	}
	return text[:maxErrorMessageLength]
}

// jobSuggestionCount counts the suggestions the job queued so far.
func jobSuggestionCount(targets *storepb.QualitySuggestionJobTargets) int {
	count := 0
	for _, target := range targets.GetTargets() {
		if target == nil {
			continue
		}
		count += len(target.GetSuggestionResourceIds())
	}
	return count
}
