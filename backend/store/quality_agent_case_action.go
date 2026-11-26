package store

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualityAgentCaseActionKind is the write one ledger row records.
type QualityAgentCaseActionKind int16

const (
	// QualityAgentCaseActionRevise is a definition write: the deterministic repair, or a criterion the
	// replay allowed.
	QualityAgentCaseActionRevise QualityAgentCaseActionKind = 1
	// QualityAgentCaseActionControl is a state change on the check (today only a pause).
	QualityAgentCaseActionControl QualityAgentCaseActionKind = 2
	// QualityAgentCaseActionRun is an immediate execution request.
	QualityAgentCaseActionRun QualityAgentCaseActionKind = 3
	// QualityAgentCaseActionPropose is a proposal: it writes no check, so it spends no repair budget.
	QualityAgentCaseActionPropose QualityAgentCaseActionKind = 4
)

// QualityAgentCaseActionOutcome is what the write did. Only applied writes are ledgered, so the
// column exists to keep a refused write from ever being read as a spent budget.
type QualityAgentCaseActionOutcome int16

const (
	QualityAgentCaseActionApplied QualityAgentCaseActionOutcome = 1
)

// QualityAgentCaseActionMessage is one append-only ledger row: a write an agent made to a check.
type QualityAgentCaseActionMessage struct {
	ID         int64
	CaseID     *int64
	AgentID    int64
	TargetGUID string
	TargetType storepb.MetaType
	RuleID     *int64
	Instance   string
	Action     QualityAgentCaseActionKind
	Field      string
	// FromRevision and ToRevision bound the definition the write produced. They are equal for an
	// action that wrote no definition (a run, a proposal, a pause).
	FromRevision int32
	ToRevision   int32
	ExecutionID  *int64
	Outcome      QualityAgentCaseActionOutcome
	Reason       string
	// RequestedBy is the principal that asked for the write when it was not the agent's own wake.
	RequestedBy *int32
	CreatedAt   time.Time
}

// RecordQualityAgentCaseAction appends one ledger row. It is the write the budget reads, so it is
// deliberately an insert with no conflict handling: two writes of the same verb on the same case are
// two rows, and the budget counts both.
func (s *Store) RecordQualityAgentCaseAction(ctx context.Context, message *QualityAgentCaseActionMessage) (*QualityAgentCaseActionMessage, error) {
	if message == nil || message.AgentID <= 0 || message.Action == 0 {
		return nil, common.Errorf(common.Invalid, "a ledger row needs an agent and an action")
	}
	outcome := message.Outcome
	if outcome == 0 {
		outcome = QualityAgentCaseActionApplied
	}
	createdAt := message.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	} else {
		createdAt = createdAt.UTC()
	}
	var rowID int64
	if err := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_agent_case_action
			(case_id, agent_id, target_guid, target_type, rule_id, instance, action, field,
			 from_revision, to_revision, execution_id, outcome, reason, requested_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id`,
		message.CaseID, message.AgentID, message.TargetGUID, int16(message.TargetType),
		message.RuleID, message.Instance,
		int16(message.Action), message.Field, message.FromRevision, message.ToRevision,
		message.ExecutionID, int16(outcome), message.Reason, message.RequestedBy, createdAt,
	).Scan(&rowID); err != nil {
		return nil, errors.Wrap(err, "failed to record a quality agent case action")
	}
	stored := *message
	stored.ID = rowID
	stored.Outcome = outcome
	stored.CreatedAt = createdAt
	return &stored, nil
}

// FindQualityAgentCaseActionMessage selects ledger rows for one budget question: how many writes of
// one kind has this agent spent, in one window, optionally for one check.
type FindQualityAgentCaseActionMessage struct {
	AgentID int64
	// Actions narrows the count to the kinds one budget is spent by. An empty list counts every
	// kind, which is what a report wants; a budget always names its kinds.
	Actions []QualityAgentCaseActionKind
	// RuleID narrows the count to one check. Nil counts the agent's writes across its scope, which
	// is the budget the repair allowance is expressed in.
	RuleID *int64
	// Since is the inclusive start of the window; a zero value counts everything recorded.
	Since time.Time
}

// qualityAgentCaseActionColumns lists the ledger columns every read returns, in the order the
// scanner consumes them.
const qualityAgentCaseActionColumns = `id, case_id, agent_id, target_guid, target_type, rule_id, instance,
	action, field, from_revision, to_revision, execution_id, outcome, reason, requested_by,
	created_at`

// FindQualityAgentCaseActionList narrows the ledger read: one agent, optionally one work item, one
// page. The activity view pages through everything the agent did; a case view pages one history.
type FindQualityAgentCaseActionList struct {
	AgentID int64
	// CaseID narrows the list to one work item's writes.
	CaseID *int64
	Limit  int
	Offset int
}

// ListQualityAgentCaseActions reads the ledger newest first.
func (s *Store) ListQualityAgentCaseActions(ctx context.Context, find *FindQualityAgentCaseActionList) ([]*QualityAgentCaseActionMessage, error) {
	if find == nil || find.AgentID <= 0 {
		return nil, common.Errorf(common.Invalid, "a ledger list needs an agent")
	}
	limit := find.Limit
	if limit <= 0 {
		limit = 50
	}
	where := "agent_id = $1"
	args := []any{find.AgentID}
	if find.CaseID != nil {
		args = append(args, *find.CaseID)
		where += " AND case_id = $" + strconv.Itoa(len(args))
	}
	args = append(args, limit, find.Offset)
	rows, err := s.GetDB().QueryContext(ctx,
		`SELECT `+qualityAgentCaseActionColumns+` FROM quality_agent_case_action
		 WHERE `+where+`
		 ORDER BY created_at DESC, id DESC
		 LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)),
		args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality agent case actions")
	}
	defer rows.Close()
	actions := make([]*QualityAgentCaseActionMessage, 0, limit)
	for rows.Next() {
		action, scanErr := scanQualityAgentCaseAction(rows)
		if scanErr != nil {
			return nil, errors.Wrap(scanErr, "failed to read a quality agent case action")
		}
		actions = append(actions, action)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to list the quality agent case actions")
	}
	return actions, nil
}

// scanQualityAgentCaseAction maps one ledger row.
func scanQualityAgentCaseAction(scanner interface{ Scan(dest ...any) error }) (*QualityAgentCaseActionMessage, error) {
	var (
		message     QualityAgentCaseActionMessage
		caseID      sql.NullInt64
		ruleID      sql.NullInt64
		action      int16
		executionID sql.NullInt64
		outcome     int16
		requestedBy sql.NullInt32
		targetType  int16
	)
	if err := scanner.Scan(
		&message.ID, &caseID, &message.AgentID, &message.TargetGUID, &targetType, &ruleID, &message.Instance,
		&action, &message.Field, &message.FromRevision, &message.ToRevision, &executionID,
		&outcome, &message.Reason, &requestedBy, &message.CreatedAt,
	); err != nil {
		return nil, err
	}
	if caseID.Valid {
		value := caseID.Int64
		message.CaseID = &value
	}
	if ruleID.Valid {
		value := ruleID.Int64
		message.RuleID = &value
	}
	if executionID.Valid {
		value := executionID.Int64
		message.ExecutionID = &value
	}
	if requestedBy.Valid {
		value := requestedBy.Int32
		message.RequestedBy = &value
	}
	message.TargetType = storepb.MetaType(targetType)
	message.Action = QualityAgentCaseActionKind(action)
	message.Outcome = QualityAgentCaseActionOutcome(outcome)
	return &message, nil
}

// CountQualityAgentCaseActions counts the applied writes a budget question asks about.
func (s *Store) CountQualityAgentCaseActions(ctx context.Context, find *FindQualityAgentCaseActionMessage) (int32, error) {
	if find == nil || find.AgentID <= 0 || len(find.Actions) == 0 {
		return 0, common.Errorf(common.Invalid, "a ledger count needs an agent and at least one action")
	}
	actions := make([]int16, 0, len(find.Actions))
	for _, action := range find.Actions {
		actions = append(actions, int16(action))
	}
	args := []any{find.AgentID, pq.Array(actions)}
	where := "agent_id = $1 AND action = ANY($2)"
	if find.RuleID != nil {
		args = append(args, *find.RuleID)
		where += " AND rule_id = $" + strconv.Itoa(len(args))
	}
	if !find.Since.IsZero() {
		args = append(args, find.Since.UTC())
		where += " AND created_at >= $" + strconv.Itoa(len(args))
	}
	var count int32
	if err := s.GetDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM quality_agent_case_action WHERE `+where, args...).Scan(&count); err != nil {
		return 0, errors.Wrap(err, "failed to count a quality agent case action")
	}
	return count, nil
}
