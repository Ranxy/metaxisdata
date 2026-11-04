// Package qualityagentrunner is the host loop of the built-in quality agent runner.
//
// It starts, resets leases left by a previous process, claims due goals up to the
// workspace's concurrency limit, and gives each claimed goal one turn of the engine.
// M2's turn is deterministic (the model turn arrives in M3). The goal's durable phase is
// the product state; "running" is only the lease this loop holds.
package qualityagentrunner

import (
	"context"
	"log/slog"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentrunner"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// tickInterval bounds how long a due goal waits for the next look. The claim is
	// one indexed update, so the loop is cheap.
	tickInterval = 3 * time.Second
	// claimLease is how long one claim is protected from other claims before a crash
	// can be assumed. A turn that legitimately runs longer renews it in M3.
	claimLease = 2 * time.Minute
	// restartLimit is how many times a goal may be recovered after a crash before it
	// is blocked for a person.
	restartLimit int32 = 3
)

// GoalStore is the slice of *store.Store the runner needs.
type GoalStore interface {
	ResetStaleQualityAgentGoalLeases(ctx context.Context, now time.Time, restartLimit int32) (int64, error)
	ClaimDueQualityAgentGoals(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]*store.QualityAgentGoalMessage, error)
	AppendQualityAgentGoalStep(ctx context.Context, step *store.QualityAgentGoalStepMessage) (*store.QualityAgentGoalStepMessage, error)
	ReleaseQualityAgentGoalLease(ctx context.Context, resourceID string, nextWakeAt *time.Time) (bool, error)
	UpdateQualityAgentGoalState(ctx context.Context, resourceID string, expectedRevision int32, phase storepb.QualityAgentGoalPhase, blockedCode, blockedReason string, nextWakeAt *time.Time) (*store.QualityAgentGoalMessage, bool, error)
	UpdateQualityAgentGoalProgress(ctx context.Context, resourceID string, roundsStarted int32, tokensUsed int64, checkpoint *storepb.QualityAgentGoalCheckpoint) (*store.QualityAgentGoalMessage, error)
	GetQualityAgentByID(ctx context.Context, id int64) (*store.QualityAgentMessage, error)
}

// GoalEngine runs one turn of a goal. Production passes the deterministic engine; M3
// swaps in the model-driven one.
type GoalEngine interface {
	Run(ctx context.Context, goal *store.QualityAgentGoalMessage) (agentrunner.Outcome, error)
}

// Runner is the goal-host loop.
type Runner struct {
	store        GoalStore
	engine       GoalEngine
	loadSettings func(ctx context.Context) (dataquality.Settings, error)
	// now is time.Now.UTC in production; a test fixes it.
	now func() time.Time
	// tick carries the production constant; a test overrides it.
	tick  time.Duration
	swept bool
}

// NewRunner creates the quality agent goal runner.
func NewRunner(stores *store.Store) *Runner {
	return &Runner{
		store:  stores,
		engine: agentrunner.NewEngine(stores),
		loadSettings: func(ctx context.Context) (dataquality.Settings, error) {
			return dataquality.LoadSettings(ctx, stores)
		},
		now:  func() time.Time { return time.Now().UTC() },
		tick: tickInterval,
	}
}

// Name identifies the runner in logs.
func (*Runner) Name() string {
	return "qualityagentgoal"
}

// Run drives the loop until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
	ticker := time.NewTicker(r.tick)
	defer ticker.Stop()
	for {
		r.runOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runOnce sweeps stale leases on the first pass, then claims and hosts the due goals
// while the workspace allows the runner.
func (r *Runner) runOnce(ctx context.Context) {
	now := r.now()
	if !r.swept {
		r.swept = true
		if reset, err := r.store.ResetStaleQualityAgentGoalLeases(ctx, now, restartLimit); err != nil {
			slog.Error("failed to reset stale quality agent goal leases", log.WithError(err))
		} else if reset > 0 {
			slog.Info("recovered quality agent goals after a restart", slog.Int64("goals", reset))
		}
	}
	settings, err := r.loadSettings(ctx)
	if err != nil {
		slog.Error("failed to load data quality settings for the goal runner", log.WithError(err))
		return
	}
	if !settings.AgentRunnerEnabled {
		return
	}
	limit := settings.AgentRunnerMaxConcurrentGoals
	if limit <= 0 {
		limit = dataquality.DefaultAgentRunnerMaxConcurrentGoals
	}
	goals, err := r.store.ClaimDueQualityAgentGoals(ctx, now, claimLease, limit)
	if err != nil {
		slog.Error("failed to claim quality agent goals", log.WithError(err))
		return
	}
	for _, goal := range goals {
		r.hostGoal(ctx, settings, goal)
	}
}

// hostGoal gives one claimed goal one turn: it checks the run-time scope (database
// allowlist and agent enabled), journals the reservation, runs the engine, records the
// progress and applies the decision.
func (r *Runner) hostGoal(ctx context.Context, settings dataquality.Settings, goal *store.QualityAgentGoalMessage) {
	if reason := r.scopeBlocker(ctx, settings, goal); reason != "" {
		r.blockGoal(ctx, goal, reason)
		return
	}
	step := &store.QualityAgentGoalStepMessage{
		GoalID:      goal.ID,
		WorkspaceID: goal.WorkspaceID,
		Kind:        storepb.QualityAgentGoalStepKind_QUALITY_AGENT_GOAL_STEP_KIND_TURN_RESERVED,
		Payload: &storepb.QualityAgentGoalStepPayload{
			Tool:    "runner",
			Summary: "goal turn reserved",
		},
	}
	if _, err := r.store.AppendQualityAgentGoalStep(ctx, step); err != nil {
		slog.Error("failed to append a quality agent goal step",
			slog.String("goal", goal.ResourceID), log.WithError(err))
	}
	if goal.MaxRounds > 0 && goal.RoundsStarted >= goal.MaxRounds {
		r.stopWithBlocker(ctx, goal, "round-limit", "the goal reached its round limit")
		return
	}
	outcome, err := r.engine.Run(ctx, goal)
	if err != nil {
		slog.Error("quality agent goal turn failed",
			slog.String("goal", goal.ResourceID), log.WithError(err))
		if _, journalErr := r.store.AppendQualityAgentGoalStep(ctx, &store.QualityAgentGoalStepMessage{
			GoalID: goal.ID, WorkspaceID: goal.WorkspaceID,
			Kind:    storepb.QualityAgentGoalStepKind_QUALITY_AGENT_GOAL_STEP_KIND_STOP,
			Payload: &storepb.QualityAgentGoalStepPayload{Tool: "runner", Summary: "the turn failed", Error: err.Error()},
		}); journalErr != nil {
			slog.Error("failed to journal the failed goal turn",
				slog.String("goal", goal.ResourceID), log.WithError(journalErr))
		}
		r.stopWithBlocker(ctx, goal, "internal-error", "the turn failed: "+err.Error())
		return
	}
	progress, err := r.store.UpdateQualityAgentGoalProgress(ctx, goal.ResourceID,
		goal.RoundsStarted+1, 0, outcome.Checkpoint)
	if err != nil {
		slog.Error("failed to record the goal progress",
			slog.String("goal", goal.ResourceID), log.WithError(err))
		r.stopWithBlocker(ctx, goal, "internal-error", "the turn progress could not be recorded")
		return
	}
	if _, err := r.store.AppendQualityAgentGoalStep(ctx, &store.QualityAgentGoalStepMessage{
		GoalID: goal.ID, WorkspaceID: goal.WorkspaceID,
		Kind:    storepb.QualityAgentGoalStepKind_QUALITY_AGENT_GOAL_STEP_KIND_GOAL_CHANGE,
		Payload: &storepb.QualityAgentGoalStepPayload{Tool: "runner", Summary: outcome.Summary},
	}); err != nil {
		slog.Error("failed to journal the goal decision",
			slog.String("goal", goal.ResourceID), log.WithError(err))
	}
	if outcome.Phase != storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_ACTIVE {
		if _, _, err := r.store.UpdateQualityAgentGoalState(ctx, progress.ResourceID, progress.Revision,
			outcome.Phase, outcome.BlockedCode, outcome.BlockedReason, outcome.NextWakeAt); err != nil {
			slog.Error("failed to apply the goal decision",
				slog.String("goal", goal.ResourceID), log.WithError(err))
		}
		_, _ = r.store.ReleaseQualityAgentGoalLease(ctx, goal.ResourceID, nil)
		return
	}
	if _, err := r.store.ReleaseQualityAgentGoalLease(ctx, goal.ResourceID, outcome.NextWakeAt); err != nil {
		slog.Error("failed to release a quality agent goal lease",
			slog.String("goal", goal.ResourceID), log.WithError(err))
	}
}

// stopWithBlocker parks a goal whose turn failed before it could decide, so the runner
// does not claim it again in a hot loop.
func (r *Runner) stopWithBlocker(ctx context.Context, goal *store.QualityAgentGoalMessage, code, reason string) {
	if _, _, err := r.store.UpdateQualityAgentGoalState(ctx, goal.ResourceID, goal.Revision,
		storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_BLOCKED, code, reason, nil); err != nil {
		slog.Error("failed to block the failed goal",
			slog.String("goal", goal.ResourceID), log.WithError(err))
	}
	_, _ = r.store.ReleaseQualityAgentGoalLease(ctx, goal.ResourceID, nil)
}

// scopeBlocker reports why a goal may not run any more, or "" when it is in scope.
// It is the run-time half of the allowlist: an administrator can remove a database or
// disable the agent after the goal was created, and the next round must stop.
func (r *Runner) scopeBlocker(ctx context.Context, settings dataquality.Settings, goal *store.QualityAgentGoalMessage) string {
	if goal.DatabaseRef == "" || !settings.AgentRunnerDatabaseEnabled(goal.DatabaseRef) {
		return "the goal's database is no longer in the runner's allowlist"
	}
	agent, err := r.store.GetQualityAgentByID(ctx, goal.AgentID)
	if err != nil {
		return "the goal's agent could not be read"
	}
	if agent == nil || !agent.Enabled {
		return "the goal's agent is disabled"
	}
	return ""
}

// blockGoal parks a goal that may not run with a stable needs-human code, keeping the
// journal and the durable state for a person to inspect.
func (r *Runner) blockGoal(ctx context.Context, goal *store.QualityAgentGoalMessage, reason string) {
	if _, err := r.store.AppendQualityAgentGoalStep(ctx, &store.QualityAgentGoalStepMessage{
		GoalID:      goal.ID,
		WorkspaceID: goal.WorkspaceID,
		Kind:        storepb.QualityAgentGoalStepKind_QUALITY_AGENT_GOAL_STEP_KIND_STOP,
		Payload:     &storepb.QualityAgentGoalStepPayload{Tool: "runner", Summary: reason, Error: reason},
	}); err != nil {
		slog.Error("failed to append the blocked-goal step",
			slog.String("goal", goal.ResourceID), log.WithError(err))
	}
	if _, _, err := r.store.UpdateQualityAgentGoalState(ctx, goal.ResourceID, goal.Revision,
		storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_BLOCKED, "needs-human", reason, nil); err != nil {
		slog.Error("failed to block an out-of-scope goal",
			slog.String("goal", goal.ResourceID), log.WithError(err))
	}
	if _, err := r.store.ReleaseQualityAgentGoalLease(ctx, goal.ResourceID, nil); err != nil {
		slog.Error("failed to release a blocked goal lease",
			slog.String("goal", goal.ResourceID), log.WithError(err))
	}
}
