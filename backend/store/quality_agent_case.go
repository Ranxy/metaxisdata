package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualityAgentCaseState is where one agent work item stands. The numeric order is the
// lifecycle; a state at or after Resolved is terminal and releases the subject.
type QualityAgentCaseState int16

const (
	QualityAgentCaseStateUnspecified QualityAgentCaseState = 0
	QualityAgentCaseStateOpen        QualityAgentCaseState = 1
	QualityAgentCaseStateDiagnosed   QualityAgentCaseState = 2
	QualityAgentCaseStateActed       QualityAgentCaseState = 3
	QualityAgentCaseStateAwaiting    QualityAgentCaseState = 4
	QualityAgentCaseStateResolved    QualityAgentCaseState = 5
	QualityAgentCaseStateFrozen      QualityAgentCaseState = 6
	QualityAgentCaseStateEscalated   QualityAgentCaseState = 7
)

// Terminal reports whether the state releases its subject. A frozen or escalated case
// must not keep the check out of work forever: the next occurrence opens a new case.
func (s QualityAgentCaseState) Terminal() bool {
	return s >= QualityAgentCaseStateResolved
}

// Name is the stable upper-case label the API, the report and the agent render.
func (s QualityAgentCaseState) Name() string {
	switch s {
	case QualityAgentCaseStateOpen:
		return "OPEN"
	case QualityAgentCaseStateDiagnosed:
		return "DIAGNOSED"
	case QualityAgentCaseStateActed:
		return "ACTED"
	case QualityAgentCaseStateAwaiting:
		return "AWAITING_VERIFICATION"
	case QualityAgentCaseStateResolved:
		return "RESOLVED"
	case QualityAgentCaseStateFrozen:
		return "FROZEN"
	case QualityAgentCaseStateEscalated:
		return "ESCALATED"
	default:
		return ""
	}
}

// QualityAgentCaseKind is what opened the case.
const (
	// QualityAgentCaseKindExecution is a check that cannot produce a verdict.
	QualityAgentCaseKindExecution = "EXECUTION"
)

// QualityAgentCaseMessage is one durable work item.
type QualityAgentCaseMessage struct {
	ID           int64
	ResourceID   string
	WorkspaceID  string
	AgentID      int64
	TargetGUID   string
	TargetType   storepb.MetaType
	RuleID       *int64
	SubjectKey   string
	Instance     string
	Kind         string
	FailureClass string
	Field        string
	State        QualityAgentCaseState
	Attempts     int32
	MaxAttempts  int32
	ExecutionID  *int64
	Summary      string
	NextActionAt *time.Time
	Resolution   string
	PreviousCase *int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

const qualityAgentCaseColumns = `id, resource_id, workspace_id, agent_id, target_guid,
	target_type, rule_id, subject_key, instance, kind, failure_class, field, state,
	attempts, max_attempts, execution_id, summary, next_action_at, resolution,
	previous_case, created_at, updated_at`

// QualityAgentCaseSubjectKey is the identity an open case claims. A NULL rule is part of
// the identity, so a target-level case and a rule-level case on the same target cannot
// block each other.
func QualityAgentCaseSubjectKey(targetGUID string, ruleID *int64, kind string) string {
	rule := "-"
	if ruleID != nil {
		rule = fmt.Sprintf("%d", *ruleID)
	}
	return targetGUID + "|" + rule + "|" + kind
}

// scanQualityAgentCase maps one row. The nullable columns are read through sql.Null*
// so an absent rule, execution or next action stays nil instead of becoming a zero.
func scanQualityAgentCase(row rowScanner) (*QualityAgentCaseMessage, error) {
	message := &QualityAgentCaseMessage{}
	var (
		ruleID       sql.NullInt64
		executionID  sql.NullInt64
		nextActionAt sql.NullTime
		previousCase sql.NullInt64
		state        int16
		targetType   int16
	)
	if err := row.Scan(
		&message.ID, &message.ResourceID, &message.WorkspaceID, &message.AgentID,
		&message.TargetGUID, &targetType, &ruleID, &message.SubjectKey,
		&message.Instance, &message.Kind, &message.FailureClass, &message.Field,
		&state, &message.Attempts, &message.MaxAttempts, &executionID,
		&message.Summary, &nextActionAt, &message.Resolution, &previousCase,
		&message.CreatedAt, &message.UpdatedAt,
	); err != nil {
		return nil, err
	}
	message.TargetType = storepb.MetaType(targetType)
	message.State = QualityAgentCaseState(state)
	if ruleID.Valid {
		message.RuleID = &ruleID.Int64
	}
	if executionID.Valid {
		message.ExecutionID = &executionID.Int64
	}
	if nextActionAt.Valid {
		message.NextActionAt = &nextActionAt.Time
	}
	if previousCase.Valid {
		message.PreviousCase = &previousCase.Int64
	}
	return message, nil
}

// CreateQualityAgentCaseInput is one case to open.
type CreateQualityAgentCaseInput struct {
	ResourceID   string
	WorkspaceID  string
	AgentID      int64
	TargetGUID   string
	TargetType   storepb.MetaType
	RuleID       *int64
	Instance     string
	Kind         string
	FailureClass string
	Field        string
	State        QualityAgentCaseState
	Attempts     int32
	MaxAttempts  int32
	ExecutionID  *int64
	Summary      string
	NextActionAt *time.Time
	PreviousCase *int64
}

// CreateQualityAgentCase opens one case unless an open case already claims the same
// subject. created reports whether this call inserted it; a false result returns the
// open row the partial unique index converged on, so a retried batch cannot open a
// second case for the same problem.
func (s *Store) CreateQualityAgentCase(ctx context.Context, input *CreateQualityAgentCaseInput) (*QualityAgentCaseMessage, bool, error) {
	if input == nil || input.AgentID <= 0 || input.WorkspaceID == "" || input.TargetGUID == "" {
		return nil, false, common.Errorf(common.Invalid, "a case needs an agent, a workspace and a target")
	}
	kind := input.Kind
	if kind == "" {
		kind = QualityAgentCaseKindExecution
	}
	state := input.State
	if state == QualityAgentCaseStateUnspecified {
		state = QualityAgentCaseStateOpen
	}
	maxAttempts := input.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 2
	}
	resourceID := input.ResourceID
	if resourceID == "" {
		generated, err := common.RandomString(12)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to generate the case id")
		}
		resourceID = generated
	}
	subjectKey := QualityAgentCaseSubjectKey(input.TargetGUID, input.RuleID, kind)
	now := time.Now().UTC()
	row := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_agent_case
			(resource_id, workspace_id, agent_id, target_guid, target_type, rule_id,
			 subject_key, instance, kind, failure_class, field, state, attempts,
			 max_attempts, execution_id, summary, next_action_at, previous_case,
			 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $19)
		ON CONFLICT (agent_id, subject_key) WHERE state BETWEEN 1 AND 4 DO NOTHING
		RETURNING `+qualityAgentCaseColumns,
		resourceID, input.WorkspaceID, input.AgentID, input.TargetGUID, int16(input.TargetType),
		input.RuleID, subjectKey, input.Instance, kind, input.FailureClass, input.Field,
		int16(state), input.Attempts, maxAttempts, input.ExecutionID, input.Summary,
		input.NextActionAt, input.PreviousCase, now)
	created, err := scanQualityAgentCase(row)
	if errors.Is(err, sql.ErrNoRows) {
		existing, readErr := s.GetOpenQualityAgentCase(ctx, input.AgentID, subjectKey)
		if readErr != nil {
			return nil, false, readErr
		}
		if existing == nil {
			return nil, false, errors.New("the case conflicted but no open row was found")
		}
		return existing, false, nil
	}
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to create the quality agent case")
	}
	return created, true, nil
}

// GetOpenQualityAgentCase reads the one open case that claims a subject.
func (s *Store) GetOpenQualityAgentCase(ctx context.Context, agentID int64, subjectKey string) (*QualityAgentCaseMessage, error) {
	message, err := scanQualityAgentCase(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentCaseColumns+` FROM quality_agent_case
		 WHERE agent_id = $1 AND subject_key = $2 AND state BETWEEN 1 AND 4`,
		agentID, subjectKey))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the open quality agent case")
	}
	return message, nil
}

// ListOpenQualityAgentCases lists one agent's unfinished work, soonest first. The
// wake prompt and the read-only case view both read this.
func (s *Store) ListOpenQualityAgentCases(ctx context.Context, agentID int64, limit int) ([]*QualityAgentCaseMessage, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.GetDB().QueryContext(ctx,
		`SELECT `+qualityAgentCaseColumns+` FROM quality_agent_case
		 WHERE agent_id = $1 AND state BETWEEN 1 AND 4
		 ORDER BY next_action_at ASC NULLS LAST, id ASC
		 LIMIT $2`, agentID, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the open quality agent cases")
	}
	defer rows.Close()
	cases := make([]*QualityAgentCaseMessage, 0, limit)
	for rows.Next() {
		message, scanErr := scanQualityAgentCase(rows)
		if scanErr != nil {
			return nil, errors.Wrap(scanErr, "failed to read an open quality agent case")
		}
		cases = append(cases, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to list the open quality agent cases")
	}
	return cases, nil
}

// FindQualityAgentCaseMessage selects one agent's work items for the read-only case view.
type FindQualityAgentCaseMessage struct {
	AgentID *int64
	// States restricts the lifecycle; an empty slice lists every state, closed ones included.
	States []QualityAgentCaseState
	Limit  int
	Offset int
}

// ResolveQualityAgentCasesByClass closes every open case of one failure class. It is the set-based
// settlement of a class that describes the target rather than a defect: an empty table is a fact,
// so those work items must not wait for a model or a person. Intake settles them too, but only when
// a batch runs, and an empty target may produce no execution for hours.
func (s *Store) ResolveQualityAgentCasesByClass(ctx context.Context, class, resolution string, now time.Time) (int64, error) {
	if strings.TrimSpace(class) == "" {
		return 0, common.Errorf(common.Invalid, "a failure class is required")
	}
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_case
		SET state = $2, resolution = $3, next_action_at = NULL, updated_at = $4
		WHERE failure_class = $1 AND state = ANY($5)`,
		class, int16(QualityAgentCaseStateResolved), resolution, now.UTC(),
		pq.Array([]int16{int16(QualityAgentCaseStateOpen), int16(QualityAgentCaseStateDiagnosed)}))
	if err != nil {
		return 0, errors.Wrap(err, "failed to resolve the quality agent cases of one class")
	}
	return result.RowsAffected()
}

// ListQualityAgentCases lists one agent's work items for the board: the ones still open first,
// soonest action first, then the closed ones newest first. A person reading it wants to know what
// the agent is working on now before it wants history.
func (s *Store) ListQualityAgentCases(ctx context.Context, find *FindQualityAgentCaseMessage) ([]*QualityAgentCaseMessage, error) {
	if find == nil || find.AgentID == nil || *find.AgentID <= 0 {
		return nil, common.Errorf(common.Invalid, "a quality agent case list needs an agent")
	}
	limit := find.Limit
	if limit <= 0 {
		limit = 50
	}
	where := "agent_id = $1"
	args := []any{*find.AgentID}
	if len(find.States) > 0 {
		states := make([]int16, 0, len(find.States))
		for _, state := range find.States {
			states = append(states, int16(state))
		}
		args = append(args, pq.Array(states))
		where += " AND state = ANY($" + strconv.Itoa(len(args)) + ")"
	}
	args = append(args, limit, find.Offset)
	rows, err := s.GetDB().QueryContext(ctx,
		`SELECT `+qualityAgentCaseColumns+` FROM quality_agent_case
		 WHERE `+where+`
		 ORDER BY (state BETWEEN 1 AND 4) DESC, next_action_at ASC NULLS LAST, updated_at DESC, id DESC
		 LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)),
		args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality agent cases")
	}
	defer rows.Close()
	cases := make([]*QualityAgentCaseMessage, 0, limit)
	for rows.Next() {
		message, scanErr := scanQualityAgentCase(rows)
		if scanErr != nil {
			return nil, errors.Wrap(scanErr, "failed to read a quality agent case")
		}
		cases = append(cases, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to list the quality agent cases")
	}
	return cases, nil
}

// UpdateQualityAgentCaseState moves one case and stamps the action it waits for next.
// expectedState makes the transition a compare-and-set: two wakes that both read the same
// state cannot both move it, so a resumed session cannot act on a case another wake
// already advanced.
func (s *Store) UpdateQualityAgentCaseState(ctx context.Context, resourceID string, expectedState QualityAgentCaseState, state QualityAgentCaseState, attempts int32, nextActionAt *time.Time, resolution string) (*QualityAgentCaseMessage, bool, error) {
	message, err := scanQualityAgentCase(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_case
		SET state = $3, attempts = $4, next_action_at = $5, resolution = $6,
			updated_at = NOW()
		WHERE resource_id = $1 AND state = $2
		RETURNING `+qualityAgentCaseColumns,
		resourceID, int16(expectedState), int16(state), attempts, nextActionAt, resolution))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, errors.Wrap(err, "failed to update the quality agent case")
	}
	return message, true, nil
}

// CountOpenQualityAgentCases counts one agent's unfinished work.
func (s *Store) CountOpenQualityAgentCases(ctx context.Context, agentID int64) (int32, error) {
	var count int32
	if err := s.GetDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM quality_agent_case WHERE agent_id = $1 AND state BETWEEN 1 AND 4`,
		agentID).Scan(&count); err != nil {
		return 0, errors.Wrap(err, "failed to count the open quality agent cases")
	}
	return count, nil
}

// ListOpenQualityAgentCasesByTarget lists one target's unfinished work, soonest first. A
// target session reads this to render the work it resumes from.
func (s *Store) ListOpenQualityAgentCasesByTarget(ctx context.Context, agentID int64, targetGUID string, limit int) ([]*QualityAgentCaseMessage, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.GetDB().QueryContext(ctx,
		`SELECT `+qualityAgentCaseColumns+` FROM quality_agent_case
		 WHERE agent_id = $1 AND target_guid = $2 AND state BETWEEN 1 AND 4
		 ORDER BY next_action_at ASC NULLS LAST, id ASC
		 LIMIT $3`, agentID, targetGUID, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the target's open quality agent cases")
	}
	defer rows.Close()
	cases := make([]*QualityAgentCaseMessage, 0, limit)
	for rows.Next() {
		message, scanErr := scanQualityAgentCase(rows)
		if scanErr != nil {
			return nil, errors.Wrap(scanErr, "failed to read an open quality agent case")
		}
		cases = append(cases, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to list the target's open quality agent cases")
	}
	return cases, nil
}

// DeferQualityAgentCases pushes the next look of the agent's due, untended cases. A wake that read a
// case and decided there was nothing to do used to leave its next_action_at in the past, and the
// release path merges that past-due instant into the next wake clamped to a minute: every session of
// the agent then woke every minute for work the model had already declined. Only OPEN and DIAGNOSED
// move — a case waiting for a verification run keeps its own timing, because a run really is due.
func (s *Store) DeferQualityAgentCases(ctx context.Context, agentID int64, before, until time.Time) (int64, error) {
	if agentID <= 0 || until.IsZero() {
		return 0, common.Errorf(common.Invalid, "deferring cases needs an agent and a time")
	}
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_case
		SET next_action_at = $3, updated_at = NOW()
		WHERE agent_id = $1 AND state IN ($4, $5)
			AND next_action_at IS NOT NULL AND next_action_at <= $2`,
		agentID, before.UTC(), until.UTC(),
		int16(QualityAgentCaseStateOpen), int16(QualityAgentCaseStateDiagnosed))
	if err != nil {
		return 0, errors.Wrap(err, "failed to defer the quality agent cases")
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count the deferred quality agent cases")
	}
	return rows, nil
}

// EarliestOpenQualityAgentCaseActionAt// EarliestOpenQualityAgentCaseActionAt reports the soonest next look among an agent's open
// cases. The session runner merges it into the next wake so a case opened after a wake
// began is not left sleeping until the idle cadence.
func (s *Store) EarliestOpenQualityAgentCaseActionAt(ctx context.Context, agentID int64) (*time.Time, error) {
	var earliest sql.NullTime
	if err := s.GetDB().QueryRowContext(ctx,
		`SELECT MIN(next_action_at) FROM quality_agent_case
		 WHERE agent_id = $1 AND state BETWEEN 1 AND 4`, agentID).Scan(&earliest); err != nil {
		return nil, errors.Wrap(err, "failed to read the earliest open case action")
	}
	if !earliest.Valid {
		return nil, nil
	}
	at := earliest.Time.UTC()
	return &at, nil
}

// GetQualityAgentCaseByResourceID reads one case by the resource id a wake prompt names.
func (s *Store) GetQualityAgentCaseByResourceID(ctx context.Context, resourceID string) (*QualityAgentCaseMessage, error) {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return nil, common.Errorf(common.Invalid, "a case resource id is required")
	}
	message, err := scanQualityAgentCase(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentCaseColumns+` FROM quality_agent_case WHERE resource_id = $1`, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality agent case")
	}
	return message, nil
}
