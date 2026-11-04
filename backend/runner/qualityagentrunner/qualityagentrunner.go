// Package qualityagentrunner is the host loop of the built-in quality agent runner.
//
// M1 is the goal substrate: it starts, resets leases left by a previous process, and
// claims due goals up to the workspace's concurrency limit. Each claimed goal records
// one journal step and is scheduled for its next look; the deterministic and model
// driven turns arrive in the next slices. The goal's durable phase is the product
// state; "running" is only the lease this loop holds.
package qualityagentrunner

import (
	"context"
	"log/slog"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
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
	// placeholderWake is how long a claimed goal waits in M1, where no turn runs yet.
	placeholderWake = time.Hour
)

// GoalStore is the slice of *store.Store the runner needs.
type GoalStore interface {
	ResetStaleQualityAgentGoalLeases(ctx context.Context, now time.Time, restartLimit int32) (int64, error)
	ClaimDueQualityAgentGoals(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]*store.QualityAgentGoalMessage, error)
	AppendQualityAgentGoalStep(ctx context.Context, step *store.QualityAgentGoalStepMessage) (*store.QualityAgentGoalStepMessage, error)
	ReleaseQualityAgentGoalLease(ctx context.Context, resourceID string, nextWakeAt *time.Time) (bool, error)
}

// Runner is the goal-host loop.
type Runner struct {
	store        GoalStore
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
		store: stores,
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
		r.hostGoal(ctx, goal)
	}
}

// hostGoal is M1's placeholder turn: it journals the claim, checks the goal's scope,
// and schedules the next look. The deterministic survey lands in M2 and the model
// loop in M3.
func (r *Runner) hostGoal(ctx context.Context, goal *store.QualityAgentGoalMessage) {
	step := &store.QualityAgentGoalStepMessage{
		GoalID:      goal.ID,
		WorkspaceID: goal.WorkspaceID,
		Kind:        storepb.QualityAgentGoalStepKind_QUALITY_AGENT_GOAL_STEP_KIND_GOAL_CHANGE,
		Payload: &storepb.QualityAgentGoalStepPayload{
			Tool:    "runner",
			Summary: "claimed by the quality agent runner",
		},
	}
	if _, err := r.store.AppendQualityAgentGoalStep(ctx, step); err != nil {
		slog.Error("failed to append a quality agent goal step",
			slog.String("goal", goal.ResourceID), log.WithError(err))
	}
	next := r.now().Add(placeholderWake)
	if _, err := r.store.ReleaseQualityAgentGoalLease(ctx, goal.ResourceID, &next); err != nil {
		slog.Error("failed to release a quality agent goal lease",
			slog.String("goal", goal.ResourceID), log.WithError(err))
	}
}
