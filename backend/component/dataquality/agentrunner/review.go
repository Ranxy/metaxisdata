package agentrunner

import (
	"context"
	"time"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// NeedsModel reports whether this scheduled wake has actionable evidence. A false
// result means the daily review can run deterministically: no model, no wake
// transcript row, no token or wake-budget spend.
func (e *Engine) NeedsModel(ctx context.Context, session *store.QualityAgentSessionMessage) (bool, error) {
	if session == nil || session.ID == 0 {
		return false, errors.New("a session is required")
	}
	pending, err := e.pendingMessages(ctx, session)
	if err != nil {
		return false, err
	}
	for _, message := range pending {
		switch message.Origin {
		case storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK,
			storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT,
			storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_NOTICE,
			storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_EVENT:
			return true, nil
		}
	}
	if isLeadSession(session) {
		return false, nil
	}
	if session.Mode == storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE {
		enrollment, err := e.store.GetQualityAgentEnrollmentByTarget(ctx, session.WorkspaceID, session.AgentID, session.TargetGUID)
		if err != nil {
			return false, err
		}
		if enrollment != nil && enrollment.NextReviewAt != nil && enrollment.NextReviewAt.After(e.now()) {
			return false, nil
		}
		return true, nil
	}
	now := e.now()
	openCases := e.openCases(ctx, session)
	for _, openCase := range openCases {
		if openCase.NextActionAt == nil || !openCase.NextActionAt.After(now) {
			return true, nil
		}
	}
	// The read is bounded. If it filled the bound, a due case may be just past it, so
	// the review errs toward a model wake instead of silently skipping one.
	if len(openCases) >= 3 {
		return true, nil
	}
	since := session.CreatedAt
	if session.LastReviewedAt != nil {
		since = *session.LastReviewedAt
	}
	actionable, err := e.store.HasActionableQualityExecutionsSince(ctx, session.TargetGUID, since)
	if err != nil {
		return false, err
	}
	return actionable, nil
}

// Review is the deterministic daily pass for a target with nothing actionable. It
// returns the next daily wake without writing a transcript row.
func (e *Engine) Review(ctx context.Context, session *store.QualityAgentSessionMessage) Outcome {
	now := e.now()
	// A no-op review is silent, but a newly uncovered declared property is durable
	// knowledge: only that case runs the deterministic survey and records its note.
	if gaps, err := e.coverageGaps(ctx, session); err == nil && len(gaps) > 0 {
		if _, maintainErr := e.maintain(ctx, session, 0); maintainErr != nil {
			return Outcome{
				NextWakeAt:  ptrTime(now.Add(retryWake)),
				EngineState: cloneEngineState(session.EngineState),
				ReviewedAt:  ptrTime(now),
				Summary:     "review deferred: the coverage survey failed",
			}
		}
	}
	outcome := Outcome{
		NextWakeAt:   ptrTime(now.Add(maintainWake)),
		ProcessedSeq: session.ProcessedSeq,
		EngineState:  cloneEngineState(session.EngineState),
		ReviewedAt:   ptrTime(now),
		Summary:      "reviewed: nothing actionable",
	}
	if session.Mode == storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE {
		outcome.NextWakeAt = ptrTime(now.Add(triageReviewDelay))
		if enrollment, err := e.store.GetQualityAgentEnrollmentByTarget(ctx, session.WorkspaceID, session.AgentID, session.TargetGUID); err == nil && enrollment != nil && enrollment.NextReviewAt != nil {
			outcome.NextWakeAt = enrollment.NextReviewAt
		}
	}
	return outcome
}

// triageReviewDelay keeps a triage target that could not run from spinning on the
// daily review path; the runner's triage rate is the real controller.
const triageReviewDelay = time.Hour
