package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualityRuleStalenessMessage is one enabled check and the instant it last completed, as a stall
// sweep reads it. It is raw material rather than a verdict: whether the gap is a stall depends on
// the target's cadence and the rule's age, which the domain decides.
type QualityRuleStalenessMessage struct {
	RuleID         int64
	RuleResourceID string
	Title          string
	TargetGUID     string
	TargetType     storepb.MetaType
	// CreatedAt is when the check was saved; a check younger than its stall window has simply not
	// had a chance to run.
	CreatedAt time.Time
	// LastCompletedAt is the newest completed execution, nil when the check never completed one.
	LastCompletedAt *time.Time
	// Cadence is the target's nominal schedule spacing.
	Cadence time.Duration
}

// ListQualityRuleStaleness returns the enabled checks of one target with the instant each last
// completed and the target's cadence.
//
// It reads the newest completion rather than the executions themselves because a sweep asks one
// question — is this check running at all? — and the batch path, which holds the executions, asks
// the finer ones. A check whose schedule row is missing is left out: without a cadence there is no
// stall window to judge against.
func (s *Store) ListQualityRuleStaleness(ctx context.Context, targetGUID string) ([]*QualityRuleStalenessMessage, error) {
	if targetGUID == "" {
		return nil, nil
	}
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT r.id, r.resource_id, r.title, r.target_guid, r.target_type, r.created_at,
			MAX(e.finished_at), s.schedule_interval_seconds
		FROM quality_rule r
		JOIN quality_target_schedule s
			ON s.target_guid = r.target_guid AND s.target_type = r.target_type
		LEFT JOIN quality_execution e
			ON e.rule_id = r.id AND e.state = $2
		WHERE r.target_guid = $1 AND r.state = $3
		GROUP BY r.id, r.resource_id, r.title, r.target_guid, r.target_type, r.created_at,
			s.schedule_interval_seconds
		ORDER BY r.id`,
		targetGUID,
		int16(storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED),
		int16(storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's check staleness")
	}
	defer rows.Close()
	var messages []*QualityRuleStalenessMessage
	for rows.Next() {
		var (
			message         QualityRuleStalenessMessage
			targetType      int16
			lastCompletedAt sql.NullTime
			intervalSeconds int32
		)
		if err := rows.Scan(&message.RuleID, &message.RuleResourceID, &message.Title,
			&message.TargetGUID, &targetType, &message.CreatedAt, &lastCompletedAt,
			&intervalSeconds); err != nil {
			return nil, errors.Wrap(err, "failed to scan a check's staleness")
		}
		message.TargetType = storepb.MetaType(targetType)
		message.Cadence = time.Duration(intervalSeconds) * time.Second
		if lastCompletedAt.Valid {
			value := lastCompletedAt.Time
			message.LastCompletedAt = &value
		}
		messages = append(messages, &message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate a target's check staleness")
	}
	return messages, nil
}
