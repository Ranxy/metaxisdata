// Package qualitysessionrunner hosts the built-in quality agent's sessions.
//
// One session is one target's long-lived conversation. The runner claims sessions whose
// wake is due, runs one bounded wake each on a worker pool, persists the wake result and
// schedules the next wake. "Running" is the lease; a state is never used for it.
package qualitysessionrunner

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentrunner"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// tickInterval bounds how long a due session waits for the next look.
	tickInterval = 3 * time.Second
	// claimLease protects one claimed session from another claim before a crash can be
	// assumed. A wake is bounded well inside it.
	claimLease = 15 * time.Minute
	// wakeDeadline bounds one wake.
	wakeDeadline = 10 * time.Minute
	// retryWake is how soon a transient failure is retried.
	retryWake = 10 * time.Minute
	// restartLimit is how many times a session may be recovered after a crash before it
	// is parked for a person.
	restartLimit int32 = 3
)

// SessionStore is the slice of *store.Store the runner needs.
// Waker runs one wake of one session.
type Waker interface {
	Wake(ctx context.Context, session *store.QualityAgentSessionMessage, options agentrunner.Options) (agentrunner.Outcome, error)
}

type SessionStore interface {
	ResetStaleQualityAgentSessionLeases(ctx context.Context, now time.Time, restartLimit int32) (int64, error)
	ClaimDueQualityAgentSessions(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]*store.QualityAgentSessionMessage, error)
	GetQualityAgentByID(ctx context.Context, id int64) (*store.QualityAgentMessage, error)
	GetQualityAgentConfig(ctx context.Context, agentID int64) (*store.QualityAgentConfigMessage, error)
	ListQualityAgentDatabases(ctx context.Context, agentID int64) ([]string, error)
	UpdateQualityAgentSessionProgress(ctx context.Context, resourceID string, result *store.QualityAgentSessionWakeResult) (*store.QualityAgentSessionMessage, error)
	UpdateQualityAgentSessionState(ctx context.Context, resourceID string, expectedRevision int32, state storepb.QualityAgentSessionState, blockedCode, blockedReason string, nextWakeAt *time.Time) (*store.QualityAgentSessionMessage, bool, error)
	ReleaseQualityAgentSessionLease(ctx context.Context, resourceID string, nextWakeAt *time.Time) (bool, error)
	SumQualityAgentSessionTokensSince(ctx context.Context, workspaceID string, agentID int64, since time.Time) (int64, error)
	CountQualityAgentSessionWakesSince(ctx context.Context, workspaceID string, agentID int64, since time.Time) (int64, error)
	SumQualityAgentSessionTokensSinceAll(ctx context.Context, workspaceID string, since time.Time) (int64, error)
	CountQualityAgentSessionWakesSinceAll(ctx context.Context, workspaceID string, since time.Time) (int64, error)
	CountQualityAgentSessionsByLease(ctx context.Context, workspaceID string, agentID int64, now time.Time) (int32, error)
	CountQualityAgentSessionsByLeaseAll(ctx context.Context, workspaceID string, now time.Time) (int32, error)
	MaxQualityAgentInputSeqAfter(ctx context.Context, sessionID int64, afterSeq int64) (int64, error)
	GetQualityAgentEnrollmentByTarget(ctx context.Context, workspaceID string, agentID int64, targetGUID string) (*store.QualityAgentEnrollmentMessage, error)
	UpdateQualityAgentEnrollmentAfterTriage(ctx context.Context, resourceID string, nextReviewAt time.Time) (*store.QualityAgentEnrollmentMessage, bool, error)
}

// AttentionSink is called after a session is parked for a person. The wiring loads
// the agent owner, writes the attention to-do and sends the inbox notification.
type AttentionSink func(ctx context.Context, session *store.QualityAgentSessionMessage, code, reason string)

// Runner hosts the agent's sessions.
type Runner struct {
	store        SessionStore
	engine       Waker
	loadSettings func(ctx context.Context) (dataquality.Settings, error)
	// now is time.Now.UTC in production; a test fixes it.
	now func() time.Time
	// tick carries the production constant; a test overrides it.
	tick     time.Duration
	swept    bool
	inFlight atomic.Int32
	wg       sync.WaitGroup
	// lastTick is process-local liveness for the status read; a restart resets it.
	lastTick atomic.Int64
	// attention escalates a session parked for a person. Nil keeps the runner
	// usable without notifications (tests, embedded use).
	attention AttentionSink
}

// SetAttentionSink wires the owner escalation. It is called once before Run.
func (r *Runner) SetAttentionSink(sink AttentionSink) {
	r.attention = sink
}

// NewRunner creates the session runner. Production passes the model engine; a nil
// engine falls back to the deterministic survey.
func NewRunner(stores *store.Store, engine Waker) *Runner {
	if engine == nil {
		engine = agentrunner.NewEngine(stores)
	}
	return &Runner{
		store:  stores,
		engine: engine,
		loadSettings: func(ctx context.Context) (dataquality.Settings, error) {
			return dataquality.LoadSettings(ctx, stores)
		},
		now:  func() time.Time { return time.Now().UTC() },
		tick: tickInterval,
	}
}

// Name identifies the runner in logs.
func (*Runner) Name() string {
	return "qualitysession"
}

// LastTick is when the loop last looked for due sessions. A zero value means the
// process has not ticked yet (fresh start) or the runner stopped.
func (r *Runner) LastTick() time.Time {
	millis := r.lastTick.Load()
	if millis == 0 {
		return time.Time{}
	}
	return time.Unix(0, millis).UTC()
}

// Run drives the loop until ctx is cancelled, then waits for in-flight wakes.
func (r *Runner) Run(ctx context.Context) {
	ticker := time.NewTicker(r.tick)
	defer ticker.Stop()
	for {
		r.runOnce(ctx)
		select {
		case <-ctx.Done():
			r.wg.Wait()
			return
		case <-ticker.C:
		}
	}
}

// runOnce sweeps stale leases on the first pass, then claims due sessions while the
// workspace allows the runner and the worker pool has room.
func (r *Runner) runOnce(ctx context.Context) {
	now := r.now()
	r.lastTick.Store(now.UnixNano())
	if !r.swept {
		r.swept = true
		if reset, err := r.store.ResetStaleQualityAgentSessionLeases(ctx, now, restartLimit); err != nil {
			slog.Error("failed to reset stale quality agent session leases", log.WithError(err))
		} else if reset > 0 {
			slog.Info("recovered quality agent sessions after a restart", slog.Int64("sessions", reset))
		}
	}
	settings, err := r.loadSettings(ctx)
	if err != nil {
		slog.Error("failed to load data quality settings for the session runner", log.WithError(err))
		return
	}
	if !settings.AgentRunnerEnabled {
		return
	}
	limit := settings.AgentRunnerMaxConcurrentSessions
	if limit <= 0 {
		limit = dataquality.DefaultAgentRunnerMaxConcurrentSessions
	}
	slots := limit - int(r.inFlight.Load())
	if slots <= 0 {
		return
	}
	sessions, err := r.store.ClaimDueQualityAgentSessions(ctx, now, claimLease, slots)
	if err != nil {
		slog.Error("failed to claim quality agent sessions", log.WithError(err))
		return
	}
	for _, session := range sessions {
		r.inFlight.Add(1)
		r.wg.Add(1)
		go func(session *store.QualityAgentSessionMessage) {
			defer r.wg.Done()
			defer r.inFlight.Add(-1)
			r.host(ctx, settings, session)
		}(session)
	}
}

// host runs one wake of one claimed session and persists its outcome.
func (r *Runner) host(ctx context.Context, settings dataquality.Settings, session *store.QualityAgentSessionMessage) {
	if code, reason, next := r.scopeBlocker(ctx, settings, session); code != "" {
		r.block(ctx, session, code, reason, next)
		return
	}
	// Autonomy is the workspace write switch. It pauses autonomous target wakes;
	// the owner's conversation (kind=2) stays available read-only.
	if session.Kind == 1 && !settings.AgentAutonomyEnabled {
		r.block(ctx, session, "autonomy-off", "agent autonomy is disabled in this workspace",
			ptrTime(r.now().Add(5*time.Minute)))
		return
	}
	config, err := r.store.GetQualityAgentConfig(ctx, session.AgentID)
	if err != nil {
		r.block(ctx, session, "internal-error", "the agent's configuration could not be read",
			ptrTime(r.now().Add(retryWake)))
		return
	}
	if config == nil || config.State != store.QualityAgentStateActive {
		r.block(ctx, session, "agent-disabled", "the agent is not active", nil)
		return
	}
	if config.MaxConcurrentSessions > 0 {
		running, err := r.store.CountQualityAgentSessionsByLease(ctx, session.WorkspaceID, session.AgentID, r.now())
		if err != nil {
			r.block(ctx, session, "internal-error", "the agent concurrency could not be read",
				ptrTime(r.now().Add(retryWake)))
			return
		}
		if running > config.MaxConcurrentSessions {
			r.block(ctx, session, "concurrency-limit", "the agent reached its concurrent session limit",
				ptrTime(r.now().Add(time.Minute)))
			return
		}
	}
	workspaceTokenLimit := settings.AgentRunnerTokensPerHour
	if workspaceTokenLimit <= 0 {
		workspaceTokenLimit = dataquality.DefaultAgentRunnerTokensPerHour
	}
	agentTokenLimit := config.TokensPerHour
	if agentTokenLimit <= 0 {
		agentTokenLimit = workspaceTokenLimit
	}
	spent, err := r.store.SumQualityAgentSessionTokensSince(ctx, session.WorkspaceID, session.AgentID, r.now().Add(-time.Hour))
	if err != nil {
		r.block(ctx, session, "internal-error", "the agent token spend could not be read",
			ptrTime(r.now().Add(retryWake)))
		return
	}
	spentAll, err := r.store.SumQualityAgentSessionTokensSinceAll(ctx, session.WorkspaceID, r.now().Add(-time.Hour))
	if err != nil {
		r.block(ctx, session, "internal-error", "the workspace token spend could not be read",
			ptrTime(r.now().Add(retryWake)))
		return
	}
	if spent >= agentTokenLimit || spentAll >= workspaceTokenLimit {
		nextWindow := r.now().Truncate(time.Hour).Add(time.Hour)
		r.block(ctx, session, "token-budget", "the hourly agent token budget is spent", &nextWindow)
		return
	}
	workspaceWakeLimit := settings.AgentRunnerWakesPerHour
	if workspaceWakeLimit <= 0 {
		workspaceWakeLimit = dataquality.DefaultAgentRunnerWakesPerHour
	}
	agentWakeLimit := int(config.WakesPerHour)
	if agentWakeLimit <= 0 {
		agentWakeLimit = workspaceWakeLimit
	}
	wakes, err := r.store.CountQualityAgentSessionWakesSince(ctx, session.WorkspaceID, session.AgentID, r.now().Add(-time.Hour))
	if err != nil {
		r.block(ctx, session, "internal-error", "the agent wake count could not be read",
			ptrTime(r.now().Add(retryWake)))
		return
	}
	wakesAll, err := r.store.CountQualityAgentSessionWakesSinceAll(ctx, session.WorkspaceID, r.now().Add(-time.Hour))
	if err != nil {
		r.block(ctx, session, "internal-error", "the workspace wake count could not be read",
			ptrTime(r.now().Add(retryWake)))
		return
	}
	if wakes >= int64(agentWakeLimit) || wakesAll >= int64(workspaceWakeLimit) {
		nextWindow := r.now().Truncate(time.Hour).Add(time.Hour)
		r.block(ctx, session, "wake-budget", "the hourly agent wake budget is spent", &nextWindow)
		return
	}

	wakeCtx, cancel := context.WithTimeout(ctx, wakeDeadline)
	defer cancel()
	outcome, err := r.engine.Wake(wakeCtx, session, agentrunner.Options{
		CompactionTriggerTokens: settings.AgentRunnerCompactionTriggerTokens,
	})
	if err != nil {
		slog.Error("quality agent session wake failed",
			slog.String("session", session.ResourceID), log.WithError(err))
		r.block(ctx, session, "internal-error", "the wake failed: "+err.Error(),
			ptrTime(r.now().Add(retryWake)))
		return
	}
	progress, err := r.store.UpdateQualityAgentSessionProgress(ctx, session.ResourceID,
		&store.QualityAgentSessionWakeResult{
			WakeCountIncrement: 1,
			TokensUsed:         outcome.TokensUsed,
			ContextTokens:      outcome.ContextTokens,
			ProcessedSeq:       outcome.ProcessedSeq,
			EngineState:        outcome.EngineState,
		})
	if err != nil {
		slog.Error("failed to record the quality agent session progress",
			slog.String("session", session.ResourceID), log.WithError(err))
		return
	}
	if outcome.TriageComplete {
		// The triage analysis is done: park the queue row until its review. The session
		// stays as the target's conversation and wakes again only when something new
		// happens (a failure event, a person, or the review).
		r.parkEnrollmentAfterTriage(ctx, session, settings)
	}
	if outcome.Blocked {
		_, parked, parkErr := r.store.UpdateQualityAgentSessionState(ctx, progress.ResourceID, progress.Revision,
			storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED,
			outcome.BlockedCode, outcome.BlockedReason, nil)
		if parkErr != nil {
			slog.Error("failed to park the quality agent session",
				slog.String("session", session.ResourceID), log.WithError(parkErr))
		} else if !parked {
			slog.Warn("the quality agent session changed before it could be parked",
				slog.String("session", session.ResourceID))
		} else if r.attention != nil && attentionWorthy(outcome.BlockedCode) {
			r.attention(ctx, session, outcome.BlockedCode, outcome.BlockedReason)
		}
		return
	}
	next := r.idleWake(settings)
	if outcome.NextWakeAt != nil {
		next = outcome.NextWakeAt
	}
	if outcome.TriageComplete {
		// A triaged target sleeps until its review, not on the idle cadence: otherwise
		// every DORMANT target would wake and re-triage once a day.
		review := r.triageReviewAt(settings)
		if next == nil || review.Before(*next) {
			next = &review
		}
	}
	// An event or question that arrived while the wake ran makes the session due again
	// immediately, so it is never lost behind the idle cadence.
	// Only an event or question that arrived after the consumed watermark makes another
	// wake immediately due; rows the wake itself wrote must not keep the session hot.
	if pendingSeq, err := r.store.MaxQualityAgentInputSeqAfter(ctx, session.ID, outcome.ProcessedSeq); err == nil && pendingSeq > 0 {
		next = ptrTime(r.now())
	}
	if _, err := r.store.ReleaseQualityAgentSessionLease(ctx, session.ResourceID, next); err != nil {
		slog.Error("failed to release a quality agent session lease",
			slog.String("session", session.ResourceID), log.WithError(err))
	}
}

// scopeBlocker reports why a session may not run any more, or "" when it is in scope.
// It is the run-time half of the allowlist: an administrator can remove a database or
// disable the agent after the session was created.
func (r *Runner) scopeBlocker(ctx context.Context, _ dataquality.Settings, session *store.QualityAgentSessionMessage) (code, reason string, nextWake *time.Time) {
	if session.Kind == 2 {
		// The agent conversation has no target database.
		if code, reason, next := r.agentStateBlocker(ctx, session.AgentID); code != "" {
			return code, reason, next
		}
		return "", "", nil
	}
	if session.DatabaseRef == "" {
		return "out-of-scope", "the session's database is no longer in the agent's scope", nil
	}
	config, err := r.store.GetQualityAgentConfig(ctx, session.AgentID)
	if err != nil {
		return "internal-error", "the agent's configuration could not be read", ptrTime(r.now().Add(retryWake))
	}
	if config == nil {
		return "agent-disabled", "the session's agent has no runtime configuration", nil
	}
	if config.State != store.QualityAgentStateActive {
		return "agent-disabled", "the agent is not active", nil
	}
	databases, err := r.store.ListQualityAgentDatabases(ctx, session.AgentID)
	if err != nil {
		return "internal-error", "the agent's scope could not be read", ptrTime(r.now().Add(retryWake))
	}
	if !slices.Contains(databases, session.DatabaseRef) {
		return "out-of-scope", "the session's database is no longer in the agent's scope", nil
	}
	return r.agentStateBlocker(ctx, session.AgentID)
}

// agentStateBlocker reports whether the session's agent identity is enabled.
func (r *Runner) agentStateBlocker(ctx context.Context, agentID int64) (code, reason string, nextWake *time.Time) {
	agent, err := r.store.GetQualityAgentByID(ctx, agentID)
	if err != nil {
		return "internal-error", "the session's agent could not be read", ptrTime(r.now().Add(retryWake))
	}
	if agent == nil || !agent.Enabled {
		return "agent-disabled", "the session's agent is disabled", nil
	}
	return "", "", nil
}

// block parks a session before or instead of a wake, keeping the transcript for a person
// to inspect. A retryable block sets the next wake; a needs-human one leaves it empty.
func (r *Runner) block(ctx context.Context, session *store.QualityAgentSessionMessage, code, reason string, nextWake *time.Time) {
	_, parked, err := r.store.UpdateQualityAgentSessionState(ctx, session.ResourceID, session.Revision,
		storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED, code, reason, nextWake)
	if err != nil {
		slog.Error("failed to park the quality agent session",
			slog.String("session", session.ResourceID), log.WithError(err))
	} else if !parked {
		slog.Warn("the quality agent session changed before it could be parked",
			slog.String("session", session.ResourceID))
	} else if r.attention != nil && attentionWorthy(code) {
		r.attention(ctx, session, code, reason)
	}
}

// attentionWorthy reports whether a blocked code needs the owner's decision rather
// than the next retry. Retryable budget/concurrency codes and runner-managed scope
// or disabled-agent parks stay silent.
func attentionWorthy(code string) bool {
	if store.QualityAgentSessionBlockedCodeRetryable(code) {
		return false
	}
	switch code {
	case "out-of-scope", "agent-disabled", "autonomy-off":
		return false
	default:
		return true
	}
}

// parkEnrollmentAfterTriage marks the target's queue row dormant until its review. A
// failure to update the queue row must not fail the wake: the transcript is already
// recorded and the next review tick retries the queue side.
func (r *Runner) parkEnrollmentAfterTriage(ctx context.Context, session *store.QualityAgentSessionMessage, settings dataquality.Settings) {
	enrollment, err := r.store.GetQualityAgentEnrollmentByTarget(ctx, session.WorkspaceID, session.AgentID, session.TargetGUID)
	if err != nil {
		slog.Warn("failed to read the enrollment of a triaged session",
			slog.String("session", session.ResourceID), log.WithError(err))
		return
	}
	if enrollment == nil {
		return
	}
	nextReview := r.triageReviewAt(settings)
	if _, applied, err := r.store.UpdateQualityAgentEnrollmentAfterTriage(ctx, enrollment.ResourceID, nextReview); err != nil {
		slog.Warn("failed to park a triaged enrollment",
			slog.String("enrollment", enrollment.ResourceID), log.WithError(err))
	} else if !applied {
		slog.Debug("the enrollment changed before it could be parked",
			slog.String("enrollment", enrollment.ResourceID))
	}
}

// triageReviewAt is when a triaged target is looked at again.
func (r *Runner) triageReviewAt(settings dataquality.Settings) time.Time {
	days := settings.AgentRunnerDormantReviewDays
	if days <= 0 {
		days = dataquality.DefaultAgentRunnerDormantReviewDays
	}
	return r.now().Add(time.Duration(days) * 24 * time.Hour)
}

// idleWake is the cadence a session sleeps on when nothing asks for an earlier wake.
func (r *Runner) idleWake(settings dataquality.Settings) *time.Time {
	seconds := settings.AgentRunnerIdleWakeSeconds
	if seconds <= 0 {
		seconds = dataquality.DefaultAgentRunnerIdleWakeSeconds
	}
	return ptrTime(r.now().Add(time.Duration(seconds) * time.Second))
}

func ptrTime(value time.Time) *time.Time { return &value }
