package store

import (
	"context"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// QualityAgentAdoptionOutcome is what happened to one suggestion the agent
// considered adopting.
type QualityAgentAdoptionOutcome int16

const (
	QualityAgentAdoptionOutcomeAdopted QualityAgentAdoptionOutcome = 1
	QualityAgentAdoptionOutcomeSkipped QualityAgentAdoptionOutcome = 2
	QualityAgentAdoptionOutcomeFailed  QualityAgentAdoptionOutcome = 3
)

// QualityAgentAdoptionMessage is one row of the adoption ledger.
type QualityAgentAdoptionMessage struct {
	ID             int64
	AgentID        int64
	SuggestionID   int64
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
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now().UTC()
	}
	if _, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO quality_agent_adoption
			(agent_id, suggestion_id, outcome, rule_resource_id, reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (agent_id, suggestion_id) DO UPDATE SET
			outcome = EXCLUDED.outcome,
			rule_resource_id = EXCLUDED.rule_resource_id,
			reason = EXCLUDED.reason,
			created_at = EXCLUDED.created_at
		WHERE quality_agent_adoption.outcome <> $7`,
		message.AgentID, message.SuggestionID, int16(message.Outcome),
		message.RuleResourceID, message.Reason, message.CreatedAt,
		int16(QualityAgentAdoptionOutcomeAdopted)); err != nil {
		return errors.Wrap(err, "failed to record the quality agent adoption")
	}
	return nil
}

// CountQualityAgentAdoptionsSince is the daily-cap count: adopted suggestions
// inside the guardrail window.
func (s *Store) CountQualityAgentAdoptionsSince(ctx context.Context, agentID int64, since time.Time) (int32, error) {
	var count int32
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quality_agent_adoption
		WHERE agent_id = $1 AND outcome = $2 AND created_at >= $3`,
		agentID, int16(QualityAgentAdoptionOutcomeAdopted), since).Scan(&count); err != nil {
		return 0, errors.Wrap(err, "failed to count the quality agent adoptions")
	}
	return count, nil
}
