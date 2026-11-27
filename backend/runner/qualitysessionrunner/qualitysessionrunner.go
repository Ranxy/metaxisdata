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
	// concurrencyRetryWake is how soon a session skipped because its agent is at
	// its concurrent limit is looked at again.
	concurrencyRetryWake = 30 * time.Second
	// restartLimit is how many times a session may be recovered after a crash before it
	// is parked for a person.
	restartLimit int32 = 3
	// restartEscalationLimit bounds how many restart-limit sessions one sweep escalates.
	restartEscalationLimit = 100
	// caseWakeFloor is the soonest a due case may wake a session, so a past-due case cannot
	// make the three-second tick spin.
	caseWakeFloor = time.Minute
	// unattendedCaseDelay is how far a wake pushes a case it looked at and did not advance. It is an
	// hour because the case's own evidence only changes when the check runs again, which is the
	// target's cadence at the fastest.
	unattendedCaseDelay = time.Hour
)

// SessionStore is the slice of *store.Store the runner needs.
// Waker runs one wake of one session.
type Waker interface {
	Wake(ctx context.Context, session *store.QualityAgentSessionMessage, options agentrunner.Options) (agentrunner.Outcome, error)
	NeedsModel(ctx context.Context, session *store.QualityAgentSessionMessage) (bool, error)
	Review(ctx context.Context, session *store.QualityAgentSessionMessage) agentrunner.Outcome
}

type SessionStore interface {
	ResetStaleQualityAgentSessionLeases(ctx context.Context, now time.Time, restartLimit int32) (int64, error)
	ListQualityAgentSessionsByBlockedCode(ctx context.Context, code string, limit int) ([]*store.QualityAgentSessionMessage, error)
	ClaimDueQualityAgentSessions(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]*store.QualityAgentSessionMessage, error)
	GetQualityAgentByID(ctx context.Context, id int64) (*store.QualityAgentMessage, error)
	ListQualityAgentDatabases(ctx context.Context, agentID int64) ([]string, error)
	UpdateQualityAgentSessionProgress(ctx context.Context, resourceID string, result *store.QualityAgentSessionWakeResult) (*store.QualityAgentSessionMessage, error)
	RecordQualityAgentReview(ctx context.Context, resourceID string, reviewedAt, nextWakeAt time.Time) (bool, error)
	UpdateQualityAgentSessionState(ctx context.Context, resourceID string, expectedRevision int32, state storepb.QualityAgentSessionState, blockedCode, blockedReason string, nextWakeAt *time.Time) (*store.QualityAgentSessionMessage, bool, error)
	ReleaseQualityAgentSessionLease(ctx context.Context, resourceID string, nextWakeAt *time.Time) (bool, error)
	SumQualityAgentSessionTokensSince(ctx context.Context, workspaceID string, agentID int64, since time.Time) (int64, error)
	ClearQualityAgentSessionManualResume(ctx context.Context, resourceID string) error
	CountQualityAgentSessionWakesSince(ctx context.Context, workspaceID string, agentID int64, since time.Time) (int64, error)
	MaxQualityAgentInputSeqAfter(ctx context.Context, sessionID int64, afterSeq int64) (int64, error)
	EarliestOpenQualityAgentCaseActionAt(ctx context.Context, agentID int64) (*time.Time, error)
	DeferQualityAgentCases(ctx context.Context, agentID int64, before, until time.Time) (int64, error)
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
	tick                   time.Duration
	swept                  bool
	escalatedRestartLimits bool
	inFlight               atomic.Int32
	wg                     sync.WaitGroup
	// agentSlots counts the sessions this process is running for one agent, so a
	// claim batch larger than the agent's concurrent budget does not make every
	// session of the batch park at once.
	agentSlotsMu sync.Mutex
	agentSlots   map[int64]int
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
		now:        func() time.Time { return time.Now().UTC() },
		tick:       tickInterval,
		agentSlots: map[int64]int{},
	}
}

// acquireAgentSlot reserves one concurrent-session slot for an agent. It reports
// false when the agent is already at its limit; the caller returns the session to
// the queue instead of parking it, so a claim batch larger than the limit does not
// livelock every session in the batch.
func (r *Runner) acquireAgentSlot(agentID int64, limit int) bool {
	r.agentSlotsMu.Lock()
	defer r.agentSlotsMu.Unlock()
	if r.agentSlots == nil {
		r.agentSlots = map[int64]int{}
	}
	if r.agentSlots[agentID] >= limit {
		return false
	}
	r.agentSlots[agentID]++
	return true
}

// releaseAgentSlot frees one concurrent-session slot for an agent.
func (r *Runner) releaseAgentSlot(agentID int64) {
	r.agentSlotsMu.Lock()
	defer r.agentSlotsMu.Unlock()
	if r.agentSlots[agentID] <= 1 {
		delete(r.agentSlots, agentID)
		return
	}
	r.agentSlots[agentID]--
}

// Name identifies the runner in logs.
func (*Runner) Name() string {
	return "qualitysession"
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
	if !r.escalatedRestartLimits {
		r.escalatedRestartLimits = true
		r.escalateRestartLimits(ctx)
	}
	// The worker pool is bounded by the workspace hard cap; each agent's own
	// concurrency budget is enforced when its session starts.
	slots := dataquality.MaxAgentRunnerConcurrentSessions - int(r.inFlight.Load())
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
			r.host(ctx, session)
		}(session)
	}
}

// host runs one wake of one claimed session and persists its outcome.
func (r *Runner) host(ctx context.Context, session *store.QualityAgentSessionMessage) {
	agent, code, reason, next := r.scopeBlocker(ctx, session)
	if code != "" {
		r.block(ctx, session, code, reason, next)
		return
	}
	concurrencyLimit := agent.MaxConcurrentSessions
	if concurrencyLimit <= 0 {
		concurrencyLimit = dataquality.DefaultAgentRunnerMaxConcurrentSessions
	}
	if concurrencyLimit < 1 {
		concurrencyLimit = 1
	}
	if !r.acquireAgentSlot(agent.ID, int(concurrencyLimit)) {
		nextWake := ptrTime(r.now().Add(concurrencyRetryWake))
		if _, err := r.store.ReleaseQualityAgentSessionLease(ctx, session.ResourceID, nextWake); err != nil {
			slog.Error("failed to requeue a session over its agent concurrency limit",
				slog.String("session", session.ResourceID), log.WithError(err))
		}
		return
	}
	defer r.releaseAgentSlot(agent.ID)
	needsModel, err := r.engine.NeedsModel(ctx, session)
	if err != nil {
		r.block(ctx, session, "internal-error", "the session review check failed: "+err.Error(),
			ptrTime(r.now().Add(retryWake)))
		return
	}
	if !needsModel {
		outcome := r.engine.Review(ctx, session)
		next := r.idleWake()
		if outcome.NextWakeAt != nil {
			next = outcome.NextWakeAt
		}
		if _, err := r.store.RecordQualityAgentReview(ctx, session.ResourceID, r.now(), *next); err != nil {
			slog.Warn("failed to record a deterministic review",
				slog.String("session", session.ResourceID), log.WithError(err))
		}
		return
	}
	tokenLimit := agent.TokensPerHour
	if tokenLimit <= 0 {
		tokenLimit = dataquality.DefaultAgentRunnerTokensPerHour
	}
	// A person pressed "resume". The budgets still describe the hour, but the decision to run now is
	// theirs, so the marker is spent here and the wake proceeds. It is cleared immediately: one wake,
	// not a standing exemption.
	manual := session.ManualResumeAt != nil
	if manual {
		if clearErr := r.store.ClearQualityAgentSessionManualResume(ctx, session.ResourceID); clearErr != nil {
			slog.Warn("failed to clear a manual resume", slog.String("session", session.ResourceID), log.WithError(clearErr))
		}
	}
	spent, err := r.store.SumQualityAgentSessionTokensSince(ctx, session.WorkspaceID, session.AgentID, r.now().Add(-time.Hour))
	if err != nil {
		r.block(ctx, session, "internal-error", "the agent token spend could not be read",
			ptrTime(r.now().Add(retryWake)))
		return
	}
	if spent >= tokenLimit && !manual {
		nextWindow := r.now().Truncate(time.Hour).Add(time.Hour)
		r.block(ctx, session, "token-budget", "the hourly agent token budget is spent", &nextWindow)
		return
	}
	wakeLimit := int64(agent.WakesPerHour)
	if wakeLimit <= 0 {
		wakeLimit = int64(dataquality.DefaultAgentRunnerWakesPerHour)
	}
	wakes, err := r.store.CountQualityAgentSessionWakesSince(ctx, session.WorkspaceID, session.AgentID, r.now().Add(-time.Hour))
	if err != nil {
		r.block(ctx, session, "internal-error", "the agent wake count could not be read",
			ptrTime(r.now().Add(retryWake)))
		return
	}
	if wakes >= wakeLimit && !manual {
		nextWindow := r.now().Truncate(time.Hour).Add(time.Hour)
		r.block(ctx, session, "wake-budget", "the hourly agent wake budget is spent", &nextWindow)
		return
	}

	wakeCtx, cancel := context.WithTimeout(ctx, wakeDeadline)
	defer cancel()
	outcome, err := r.engine.Wake(wakeCtx, session, agentrunner.Options{})
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
			ProcessedSeq:       outcome.ProcessedSeq,
			EngineState:        outcome.EngineState,
			LastReviewedAt:     outcome.ReviewedAt,
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
		r.parkEnrollmentAfterTriage(ctx, session)
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
	next = r.idleWake()
	if outcome.NextWakeAt != nil {
		next = outcome.NextWakeAt
	}
	if outcome.TriageComplete {
		// A triaged target sleeps until its review, not on the idle cadence: otherwise
		// every DORMANT target would wake and re-triage once a day.
		review := r.triageReviewAt()
		if next == nil || review.Before(*next) {
			next = &review
		}
	}
	// A case nobody advanced is not due work, however past its time: push its next look out before the
	// merge reads it. Without this the release path merged the same stale instant into every wake and
	// the clamp turned it into one wake a minute, each of which read the same evidence and concluded
	// nothing had changed.
	if session.Kind != 2 {
		if _, deferErr := r.store.DeferQualityAgentCases(ctx, session.AgentID, r.now(), r.now().Add(unattendedCaseDelay)); deferErr != nil {
			slog.Warn("failed to defer the agent's untended cases",
				slog.String("session", session.ResourceID), log.WithError(deferErr))
		}
	}
	// A case is due work. The engine merges what it saw before the wake; this read catches a
	// case another pass opened while the wake was running, which would otherwise sleep until
	// the idle cadence. A past-due case is clamped so the three-second tick does not spin.
	if session.Kind != 2 {
		if earliest, caseErr := r.store.EarliestOpenQualityAgentCaseActionAt(ctx, session.AgentID); caseErr != nil {
			slog.Warn("failed to read the earliest open case action",
				slog.String("session", session.ResourceID), log.WithError(caseErr))
		} else if earliest != nil {
			at := earliest.UTC()
			if floor := r.now().Add(caseWakeFloor); at.Before(floor) {
				at = floor
			}
			if next == nil || at.Before(*next) {
				next = &at
			}
		}
	}
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

// escalateRestartLimits raises one owner attention for every session the lease sweep
// just parked on the restart limit. The attention sink dedupes per session, so a
// repeated pass converges on the open to-do.
func (r *Runner) escalateRestartLimits(ctx context.Context) {
	if r.attention == nil {
		return
	}
	sessions, err := r.store.ListQualityAgentSessionsByBlockedCode(ctx, "restart-limit", restartEscalationLimit)
	if err != nil {
		slog.Error("failed to read the restart-limit sessions", log.WithError(err))
		return
	}
	for _, session := range sessions {
		agent, err := r.store.GetQualityAgentByID(ctx, session.AgentID)
		if err != nil || agent == nil || agent.State != store.QualityAgentStateActive {
			continue
		}
		r.attention(ctx, session, "restart-limit", session.BlockedReason)
	}
}

// scopeBlocker reports why a session may not run any more, or "" when it is in scope,
// together with the agent the session runs for.
func (r *Runner) scopeBlocker(ctx context.Context, session *store.QualityAgentSessionMessage) (*store.QualityAgentMessage, string, string, *time.Time) {
	agent, err := r.store.GetQualityAgentByID(ctx, session.AgentID)
	if err != nil {
		return nil, "internal-error", "the session's agent could not be read", ptrTime(r.now().Add(retryWake))
	}
	if agent == nil || agent.State == store.QualityAgentStateDisabled {
		return nil, "agent-disabled", "the session's agent is disabled", nil
	}
	if session.Kind == 2 {
		// The front-office conversation is read-only and may run while the agent is a
		// draft or paused; only a disabled agent (terminal) stops answering.
		return agent, "", "", nil
	}
	if agent.State != store.QualityAgentStateActive {
		return nil, "agent-disabled", "the session's agent is not active", nil
	}
	if session.DatabaseRef == "" {
		return agent, "out-of-scope", "the session's database is no longer in the agent's scope", nil
	}
	databases, err := r.store.ListQualityAgentDatabases(ctx, session.AgentID)
	if err != nil {
		return agent, "internal-error", "the agent's scope could not be read", ptrTime(r.now().Add(retryWake))
	}
	if !slices.Contains(databases, session.DatabaseRef) {
		return agent, "out-of-scope", "the session's database is no longer in the agent's scope", nil
	}
	return agent, "", "", nil
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
	case "out-of-scope", "agent-disabled":
		return false
	default:
		return true
	}
}

// parkEnrollmentAfterTriage marks the target's queue row dormant until its review. A
// failure to update the queue row must not fail the wake: the transcript is already
// recorded and the next review tick retries the queue side.
func (r *Runner) parkEnrollmentAfterTriage(ctx context.Context, session *store.QualityAgentSessionMessage) {
	enrollment, err := r.store.GetQualityAgentEnrollmentByTarget(ctx, session.WorkspaceID, session.AgentID, session.TargetGUID)
	if err != nil {
		slog.Warn("failed to read the enrollment of a triaged session",
			slog.String("session", session.ResourceID), log.WithError(err))
		return
	}
	if enrollment == nil {
		return
	}
	nextReview := r.triageReviewAt()
	if _, applied, err := r.store.UpdateQualityAgentEnrollmentAfterTriage(ctx, enrollment.ResourceID, nextReview); err != nil {
		slog.Warn("failed to park a triaged enrollment",
			slog.String("enrollment", enrollment.ResourceID), log.WithError(err))
	} else if !applied {
		slog.Debug("the enrollment changed before it could be parked",
			slog.String("enrollment", enrollment.ResourceID))
	}
}

// triageReviewAt is when a triaged target is looked at again.
func (r *Runner) triageReviewAt() time.Time {
	return r.now().Add(time.Duration(dataquality.DefaultAgentRunnerDormantReviewDays) * 24 * time.Hour)
}

// idleWake is the cadence a session sleeps on when nothing asks for an earlier wake.
func (r *Runner) idleWake() *time.Time {
	return ptrTime(r.now().Add(time.Duration(dataquality.DefaultAgentRunnerIdleWakeSeconds) * time.Second))
}

func ptrTime(value time.Time) *time.Time { return &value }
