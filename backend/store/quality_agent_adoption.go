package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualityAgentAdoptionOutcome is what happened to one suggestion the agent
// considered adopting.
type QualityAgentAdoptionOutcome int16

const (
	QualityAgentAdoptionOutcomeAdopted QualityAgentAdoptionOutcome = 1
	QualityAgentAdoptionOutcomeSkipped QualityAgentAdoptionOutcome = 2
	QualityAgentAdoptionOutcomeFailed  QualityAgentAdoptionOutcome = 3
)

// QualityAgentAdoptionOrigin says who started the adoption. Only the first two
// spend the agent's autonomous daily ceiling; an approval is a person's decision
// written through the internal create path, and is recorded for accounting instead.
type QualityAgentAdoptionOrigin int16

const (
	QualityAgentAdoptionOriginAuto     QualityAgentAdoptionOrigin = 1
	QualityAgentAdoptionOriginTool     QualityAgentAdoptionOrigin = 2
	QualityAgentAdoptionOriginApproval QualityAgentAdoptionOrigin = 3
)

// QualityAgentAdoptionMessage is one row of the adoption ledger.
type QualityAgentAdoptionMessage struct {
	ID             int64
	AgentID        int64
	SuggestionID   int64
	Origin         QualityAgentAdoptionOrigin
	Outcome        QualityAgentAdoptionOutcome
	RuleResourceID string
	Reason         string
	CreatedAt      time.Time
}

// RecordQualityAgentAdoption appends one ledger row. The unique suggestion index
// makes a retried adoption pass idempotent.
func (s *Store) RecordQualityAgentAdoption(ctx context.Context, message *QualityAgentAdoptionMessage) error {
	if message == nil || message.AgentID <= 0 || message.SuggestionID <= 0 {
		return common.Errorf(common.Invalid, "an adoption needs an agent and a suggestion")
	}
	if message.Origin != QualityAgentAdoptionOriginAuto &&
		message.Origin != QualityAgentAdoptionOriginTool &&
		message.Origin != QualityAgentAdoptionOriginApproval {
		return common.Errorf(common.Invalid, "an adoption needs an origin")
	}
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now().UTC()
	}
	if _, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO quality_agent_adoption
			(agent_id, suggestion_id, origin, outcome, rule_resource_id, reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (agent_id, suggestion_id) DO UPDATE SET
			origin = EXCLUDED.origin,
			outcome = EXCLUDED.outcome,
			rule_resource_id = EXCLUDED.rule_resource_id,
			reason = EXCLUDED.reason,
			created_at = EXCLUDED.created_at
		WHERE quality_agent_adoption.outcome <> $8`,
		message.AgentID, message.SuggestionID, int16(message.Origin),
		int16(message.Outcome), message.RuleResourceID, message.Reason, message.CreatedAt,
		int16(QualityAgentAdoptionOutcomeAdopted)); err != nil {
		return errors.Wrap(err, "failed to record the quality agent adoption")
	}
	return nil
}

// CountQualityAgentAdoptionsSince is the daily-cap count: adoptions the agent
// started itself or through the owner-requested tool. A person's approval is a
// decision, not autonomous adoption, so it never spends this ceiling.
func (s *Store) CountQualityAgentAdoptionsSince(ctx context.Context, agentID int64, since time.Time) (int32, error) {
	var count int32
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quality_agent_adoption
		WHERE agent_id = $1 AND outcome = $2 AND origin <> $3 AND created_at >= $4`,
		agentID, int16(QualityAgentAdoptionOutcomeAdopted),
		int16(QualityAgentAdoptionOriginApproval), since).Scan(&count); err != nil {
		return 0, errors.Wrap(err, "failed to count the quality agent adoptions")
	}
	return count, nil
}

// QualityAgentAdoptionStatusMessage is the read-side view of one agent's adoption
// guardrails: what they actually did, not what they are configured to do.
type QualityAgentAdoptionStatusMessage struct {
	AdoptedLast24h    int32
	FailedLast24h     int32
	ApprovedLast24h   int32
	PendingDraftCount int32
	LastFailureReason string
	LastFailureTime   *time.Time
}

// GetQualityAgentAdoptionStatus reads the recent outcomes and the queue depth of
// one agent. It is a read model: it writes nothing and is safe to call on every
// agent view.
func (s *Store) GetQualityAgentAdoptionStatus(ctx context.Context, agentID int64, now time.Time) (*QualityAgentAdoptionStatusMessage, error) {
	status := &QualityAgentAdoptionStatusMessage{}
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE outcome = $2 AND origin <> $5 AND created_at >= $3),
			COUNT(*) FILTER (WHERE outcome = $4 AND created_at >= $3),
			COUNT(*) FILTER (WHERE outcome = $2 AND origin = $5 AND created_at >= $3)
		FROM quality_agent_adoption
		WHERE agent_id = $1`,
		agentID, int16(QualityAgentAdoptionOutcomeAdopted), now.Add(-24*time.Hour),
		int16(QualityAgentAdoptionOutcomeFailed), int16(QualityAgentAdoptionOriginApproval)).Scan(
		&status.AdoptedLast24h, &status.FailedLast24h, &status.ApprovedLast24h)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the agent adoption status")
	}
	err = s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quality_suggestion
		WHERE agent_id = $1 AND status = $2 AND kind = $3`,
		agentID, int16(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING),
		int16(storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT)).Scan(&status.PendingDraftCount)
	if err != nil {
		return nil, errors.Wrap(err, "failed to count the agent pending drafts")
	}
	var (
		reason string
		at     time.Time
	)
	err = s.GetDB().QueryRowContext(ctx, `
		SELECT reason, created_at FROM quality_agent_adoption
		WHERE agent_id = $1 AND outcome = $2
		ORDER BY created_at DESC LIMIT 1`,
		agentID, int16(QualityAgentAdoptionOutcomeFailed)).Scan(&reason, &at)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, errors.Wrap(err, "failed to read the last adoption failure")
	}
	if err == nil {
		status.LastFailureReason = reason
		status.LastFailureTime = &at
	}
	return status, nil
}
