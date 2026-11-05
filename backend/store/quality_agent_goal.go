package store

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualityAgentGoalMessage is one durable objective of the built-in quality agent
// runner. Every human-visible mutation bumps Revision so a stale writer is rejected;
// Phase is the durable lifecycle, while "running" is expressed by the lease only.
type QualityAgentGoalMessage struct {
	ID          int64
	ResourceID  string
	WorkspaceID string
	Kind        storepb.QualityAgentGoalKind
	Trigger     storepb.QualityAgentGoalTrigger
	TriggerRef  string
	TargetGUID  string
	TargetType  storepb.MetaType
	DatabaseRef string
	AgentID     int64
	// AgentPrincipalID is the principal the agent authenticates as, joined for
	// rendering users/{id} without a second read.
	AgentPrincipalID int32
	Objective        string
	Phase            storepb.QualityAgentGoalPhase
	BlockedCode      string
	BlockedReason    string
	Revision         int32
	Checkpoint       *storepb.QualityAgentGoalCheckpoint
	RoundsStarted    int32
	MaxRounds        int32
	TokensUsed       int64
	NextWakeAt       *time.Time
	LeaseUntil       *time.Time
	RestartCount     int32
	CreatedBy        *int32
	ClearedAt        *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// QualityAgentGoalStepMessage is one append-only journal entry of a goal.
type QualityAgentGoalStepMessage struct {
	ID             int64
	GoalID         int64
	WorkspaceID    string
	Seq            int32
	Kind           storepb.QualityAgentGoalStepKind
	Payload        *storepb.QualityAgentGoalStepPayload
	Tokens         int32
	IdempotencyKey string
	CreatedAt      time.Time
}

const qualityAgentGoalColumns = `id, resource_id, workspace_id, kind, "trigger", trigger_ref,
	target_guid, target_type, database_ref, agent_id,
	COALESCE((SELECT principal_id FROM quality_agent WHERE quality_agent.id = agent_id), 0) AS agent_principal_id,
	objective, phase, blocked_code,
	blocked_reason, revision, checkpoint, rounds_started, max_rounds, tokens_used,
	next_wake_at, lease_until, restart_count, created_by, cleared_at, created_at, updated_at`

const qualityAgentGoalStepColumns = `id, goal_id, workspace_id, seq, kind, payload, tokens,
	idempotency_key, created_at`

// CreateQualityAgentGoalInput is one goal creation. A duplicate active goal for the
// same scope is reported as a conflict by the partial unique indexes.
type CreateQualityAgentGoalInput struct {
	WorkspaceID string
	Kind        storepb.QualityAgentGoalKind
	Trigger     storepb.QualityAgentGoalTrigger
	TriggerRef  string
	TargetGUID  string
	TargetType  storepb.MetaType
	DatabaseRef string
	AgentID     int64
	Objective   string
	MaxRounds   int32
	CreatedBy   *int32
}

// CreateQualityAgentGoal records one goal in ACTIVE phase.
func (s *Store) CreateQualityAgentGoal(ctx context.Context, input *CreateQualityAgentGoalInput) (*QualityAgentGoalMessage, error) {
	if input == nil || input.WorkspaceID == "" {
		return nil, common.Errorf(common.Invalid, "a workspace id is required")
	}
	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate the goal id")
	}
	if input.MaxRounds <= 0 {
		input.MaxRounds = 256
	}
	row := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_agent_goal
			(resource_id, workspace_id, kind, "trigger", trigger_ref, target_guid,
			 target_type, database_ref, agent_id, objective, phase, max_rounds, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING `+qualityAgentGoalColumns,
		resourceID, input.WorkspaceID, int16(input.Kind), int16(input.Trigger),
		input.TriggerRef, input.TargetGUID, int16(input.TargetType), input.DatabaseRef,
		input.AgentID, input.Objective,
		int16(storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_ACTIVE),
		input.MaxRounds, input.CreatedBy)
	message, err := scanQualityAgentGoal(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, common.Errorf(common.Conflict, "an active goal already covers this scope")
		}
		return nil, errors.Wrap(err, "failed to create the quality agent goal")
	}
	return message, nil
}

// GetQualityAgentGoal reads one goal by resource id; nil means absent or cleared.
func (s *Store) GetQualityAgentGoal(ctx context.Context, resourceID string) (*QualityAgentGoalMessage, error) {
	message, err := scanQualityAgentGoal(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentGoalColumns+` FROM quality_agent_goal
		 WHERE resource_id = $1 AND cleared_at IS NULL`, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality agent goal")
	}
	return message, nil
}

// FindQualityAgentGoalMessage filters a goal list; every field is optional.
type FindQualityAgentGoalMessage struct {
	WorkspaceID *string
	Phases      []storepb.QualityAgentGoalPhase
	TargetGUID  *string
	DatabaseRef *string
	Limit       *int
	Offset      *int
}

// ListQualityAgentGoals lists goals newest first.
func (s *Store) ListQualityAgentGoals(ctx context.Context, find *FindQualityAgentGoalMessage) ([]*QualityAgentGoalMessage, error) {
	if find == nil {
		find = &FindQualityAgentGoalMessage{}
	}
	where := "cleared_at IS NULL"
	args := []any{}
	arg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	if find.WorkspaceID != nil && *find.WorkspaceID != "" {
		where += ` AND workspace_id = ` + arg(*find.WorkspaceID)
	}
	if find.TargetGUID != nil && *find.TargetGUID != "" {
		where += ` AND target_guid = ` + arg(*find.TargetGUID)
	}
	if find.DatabaseRef != nil && *find.DatabaseRef != "" {
		where += ` AND database_ref = ` + arg(*find.DatabaseRef)
	}
	if len(find.Phases) > 0 {
		phases := make([]int16, 0, len(find.Phases))
		for _, phase := range find.Phases {
			phases = append(phases, int16(phase))
		}
		where += ` AND phase = ANY(` + arg(pq.Array(phases)) + `::int2[])`
	}
	limit, offset := 100, 0
	if find.Limit != nil && *find.Limit > 0 {
		limit = *find.Limit
	}
	if find.Offset != nil && *find.Offset > 0 {
		offset = *find.Offset
	}
	rows, err := s.GetDB().QueryContext(ctx, `SELECT `+qualityAgentGoalColumns+`
		FROM quality_agent_goal WHERE `+where+`
		ORDER BY created_at DESC, id DESC LIMIT `+arg(limit)+` OFFSET `+arg(offset), args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality agent goals")
	}
	defer rows.Close()
	var messages []*QualityAgentGoalMessage
	for rows.Next() {
		message, err := scanQualityAgentGoal(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality agent goal")
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality agent goals")
	}
	return messages, nil
}

// ClaimDueQualityAgentGoals leases up to limit runnable goals. The lease (not a
// phase) expresses "running", and an expired lease is how a crashed process's goal
// becomes claimable again.
func (s *Store) ClaimDueQualityAgentGoals(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]*QualityAgentGoalMessage, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := s.GetDB().QueryContext(ctx, `
		UPDATE quality_agent_goal g
		SET lease_until = $2, updated_at = NOW()
		WHERE g.id IN (
			SELECT id FROM quality_agent_goal
			WHERE phase = $3 AND cleared_at IS NULL
				AND (next_wake_at IS NULL OR next_wake_at <= $1)
				AND (lease_until IS NULL OR lease_until <= $1)
			ORDER BY COALESCE(next_wake_at, created_at) ASC, id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $4
		)
		RETURNING `+qualityAgentGoalColumns,
		now, now.Add(lease),
		int16(storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_ACTIVE), limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to claim the quality agent goals")
	}
	defer rows.Close()
	var messages []*QualityAgentGoalMessage
	for rows.Next() {
		message, err := scanQualityAgentGoal(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a claimed goal")
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the claimed goals")
	}
	return messages, nil
}

// UpdateQualityAgentGoalState applies a human phase change (pause/resume/complete) or
// a blocked transition with a revision compare-and-set. It reports false when the
// revision is stale.
func (s *Store) UpdateQualityAgentGoalState(ctx context.Context, resourceID string, expectedRevision int32, phase storepb.QualityAgentGoalPhase, blockedCode, blockedReason string, nextWakeAt *time.Time) (*QualityAgentGoalMessage, bool, error) {
	if phase == storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_ACTIVE {
		blockedCode, blockedReason = "", ""
	}
	message, err := scanQualityAgentGoal(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_goal
		SET phase = $2, blocked_code = $3, blocked_reason = $4, next_wake_at = $5,
			lease_until = NULL, revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND revision = $6 AND cleared_at IS NULL
		RETURNING `+qualityAgentGoalColumns,
		resourceID, int16(phase), blockedCode, blockedReason, nextWakeAt, expectedRevision))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, errors.Wrap(err, "failed to update the quality agent goal")
	}
	return message, true, nil
}

// UpdateQualityAgentGoalProgress records what a runner turn produced. It only applies
// while the goal is still ACTIVE, so a pause that landed meanwhile wins.
func (s *Store) UpdateQualityAgentGoalProgress(ctx context.Context, resourceID string, roundsStarted int32, tokensUsed int64, checkpoint *storepb.QualityAgentGoalCheckpoint) (*QualityAgentGoalMessage, error) {
	payload, err := protojson.Marshal(checkpoint)
	if err != nil {
		return nil, err
	}
	message, err := scanQualityAgentGoal(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_goal
		SET rounds_started = $2, tokens_used = tokens_used + $3, checkpoint = $4,
			revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND phase = $5 AND cleared_at IS NULL
		RETURNING `+qualityAgentGoalColumns,
		resourceID, roundsStarted, tokensUsed, payload,
		int16(storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_ACTIVE)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.Conflict, "the goal is no longer active")
		}
		return nil, errors.Wrap(err, "failed to update the goal progress")
	}
	return message, nil
}

// ReleaseQualityAgentGoalLease clears the lease and schedules the next wakeup. It
// reports false when the goal is not leased any more (for example it was paused).
func (s *Store) ReleaseQualityAgentGoalLease(ctx context.Context, resourceID string, nextWakeAt *time.Time) (bool, error) {
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_goal
		SET lease_until = NULL, next_wake_at = $2, updated_at = NOW()
		WHERE resource_id = $1 AND lease_until IS NOT NULL`,
		resourceID, nextWakeAt)
	if err != nil {
		return false, errors.Wrap(err, "failed to release the goal lease")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to count the released goal")
	}
	return affected > 0, nil
}

// ResetStaleQualityAgentGoalLeases clears leases left by a previous process and counts
// the restart. Goals over the restart limit are blocked and need a person.
func (s *Store) ResetStaleQualityAgentGoalLeases(ctx context.Context, now time.Time, restartLimit int32) (int64, error) {
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_goal
		SET lease_until = NULL,
			restart_count = restart_count + 1,
			phase = CASE WHEN restart_count + 1 > $2 THEN $3 ELSE phase END,
			blocked_code = CASE WHEN restart_count + 1 > $2 THEN 'restart-limit' ELSE blocked_code END,
			blocked_reason = CASE WHEN restart_count + 1 > $2
				THEN 'the process restarted while this goal was running too many times'
				ELSE blocked_reason END,
			revision = revision + 1,
			updated_at = NOW()
		WHERE phase = $4 AND cleared_at IS NULL AND lease_until IS NOT NULL AND lease_until <= $1`,
		now, restartLimit,
		int16(storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_BLOCKED),
		int16(storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_ACTIVE))
	if err != nil {
		return 0, errors.Wrap(err, "failed to reset the stale goal leases")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count the reset goal leases")
	}
	return affected, nil
}

// ClearQualityAgentGoal tombstones a goal. The journal stays for the audit trail.
func (s *Store) ClearQualityAgentGoal(ctx context.Context, resourceID string, expectedRevision int32, now time.Time) (bool, error) {
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_goal
		SET cleared_at = $2, lease_until = NULL, revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND revision = $3 AND cleared_at IS NULL`,
		resourceID, now, expectedRevision)
	if err != nil {
		return false, errors.Wrap(err, "failed to clear the quality agent goal")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to count the cleared goal")
	}
	return affected > 0, nil
}

// AppendQualityAgentGoalStep appends one journal entry, allocating the next sequence
// number for the goal. The goal is leased by one runner, so the max+1 allocation is
// safe; the unique index still protects against a duplicate.
func (s *Store) AppendQualityAgentGoalStep(ctx context.Context, input *QualityAgentGoalStepMessage) (*QualityAgentGoalStepMessage, error) {
	if input == nil || input.GoalID == 0 {
		return nil, common.Errorf(common.Invalid, "a goal id is required")
	}
	if input.Payload == nil {
		input.Payload = &storepb.QualityAgentGoalStepPayload{}
	}
	payload, err := protojson.Marshal(input.Payload)
	if err != nil {
		return nil, err
	}
	message, err := scanQualityAgentGoalStep(s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_agent_goal_step
			(goal_id, workspace_id, seq, kind, payload, tokens, idempotency_key)
		VALUES ($1, $2,
			(SELECT COALESCE(MAX(seq), 0) + 1 FROM quality_agent_goal_step WHERE goal_id = $1),
			$3, $4, $5, $6)
		RETURNING `+qualityAgentGoalStepColumns,
		input.GoalID, input.WorkspaceID, int16(input.Kind), payload, input.Tokens,
		input.IdempotencyKey))
	if err != nil {
		if isUniqueViolation(err) && input.IdempotencyKey != "" {
			// A retried effect reuses its journal entry instead of failing; the
			// effect itself is made idempotent by its own fingerprint.
			existing, findErr := scanQualityAgentGoalStep(s.GetDB().QueryRowContext(ctx,
				`SELECT `+qualityAgentGoalStepColumns+` FROM quality_agent_goal_step
				 WHERE goal_id = $1 AND idempotency_key = $2`, input.GoalID, input.IdempotencyKey))
			if findErr != nil {
				return nil, errors.Wrap(findErr, "failed to read the existing goal step")
			}
			return existing, nil
		}
		return nil, errors.Wrap(err, "failed to append the goal step")
	}
	return message, nil
}

// SumQualityAgentGoalTokensSince is the workspace's token spend since one instant,
// the hourly budget the runner enforces.
func (s *Store) SumQualityAgentGoalTokensSince(ctx context.Context, workspaceID string, since time.Time) (int64, error) {
	var total int64
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COALESCE(SUM(tokens), 0) FROM quality_agent_goal_step
		WHERE workspace_id = $1 AND created_at > $2`, workspaceID, since).Scan(&total); err != nil {
		return 0, errors.Wrap(err, "failed to sum the goal tokens")
	}
	return total, nil
}

// CountQualityAgentGoalsSince counts the goals a workspace created since one instant,
// the hourly goal-creation budget.
func (s *Store) CountQualityAgentGoalsSince(ctx context.Context, workspaceID string, since time.Time) (int64, error) {
	var total int64
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quality_agent_goal
		WHERE workspace_id = $1 AND created_at > $2`, workspaceID, since).Scan(&total); err != nil {
		return 0, errors.Wrap(err, "failed to count the created goals")
	}
	return total, nil
}

// scanQualityAgentGoal reads one quality_agent_goal row.
func scanQualityAgentGoal(scanner interface{ Scan(dest ...any) error }) (*QualityAgentGoalMessage, error) {
	var (
		message     QualityAgentGoalMessage
		kind        int16
		trigger     int16
		targetType  int16
		phase       int16
		agentID     int64
		agentPrinID int32
		checkpoint  []byte
		nextWakeAt  sql.NullTime
		leaseUntil  sql.NullTime
		createdBy   sql.NullInt32
		clearedAt   sql.NullTime
	)
	if err := scanner.Scan(&message.ID, &message.ResourceID, &message.WorkspaceID, &kind,
		&trigger, &message.TriggerRef, &message.TargetGUID, &targetType,
		&message.DatabaseRef, &agentID, &agentPrinID, &message.Objective, &phase,
		&message.BlockedCode, &message.BlockedReason, &message.Revision, &checkpoint,
		&message.RoundsStarted, &message.MaxRounds, &message.TokensUsed, &nextWakeAt,
		&leaseUntil, &message.RestartCount, &createdBy, &clearedAt,
		&message.CreatedAt, &message.UpdatedAt); err != nil {
		return nil, err
	}
	message.AgentID = agentID
	message.AgentPrincipalID = agentPrinID
	message.Kind = storepb.QualityAgentGoalKind(kind)
	message.Trigger = storepb.QualityAgentGoalTrigger(trigger)
	message.TargetType = storepb.MetaType(targetType)
	message.Phase = storepb.QualityAgentGoalPhase(phase)
	message.Checkpoint = &storepb.QualityAgentGoalCheckpoint{}
	if len(checkpoint) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(checkpoint, message.Checkpoint); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal the goal checkpoint")
		}
	}
	if nextWakeAt.Valid {
		value := nextWakeAt.Time
		message.NextWakeAt = &value
	}
	if leaseUntil.Valid {
		value := leaseUntil.Time
		message.LeaseUntil = &value
	}
	if createdBy.Valid {
		id := createdBy.Int32
		message.CreatedBy = &id
	}
	if clearedAt.Valid {
		value := clearedAt.Time
		message.ClearedAt = &value
	}
	return &message, nil
}

// scanQualityAgentGoalStep reads one quality_agent_goal_step row.
func scanQualityAgentGoalStep(scanner interface{ Scan(dest ...any) error }) (*QualityAgentGoalStepMessage, error) {
	var (
		message QualityAgentGoalStepMessage
		kind    int16
		payload []byte
	)
	if err := scanner.Scan(&message.ID, &message.GoalID, &message.WorkspaceID,
		&message.Seq, &kind, &payload, &message.Tokens, &message.IdempotencyKey,
		&message.CreatedAt); err != nil {
		return nil, err
	}
	message.Kind = storepb.QualityAgentGoalStepKind(kind)
	message.Payload = &storepb.QualityAgentGoalStepPayload{}
	if len(payload) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(payload, message.Payload); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal the goal step payload")
		}
	}
	return &message, nil
}
