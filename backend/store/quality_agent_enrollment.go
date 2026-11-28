package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualityAgentEnrollmentMessage is one target's place in the quality agent runner's
// queue: the desired enrollment state derived from the database scope, the priority
// admission reads and the session the queue created. The session row remains the
// execution state; this row is the queue.
type QualityAgentEnrollmentMessage struct {
	ID              int64
	ResourceID      string
	WorkspaceID     string
	AgentID         int64
	TargetGUID      string
	TargetType      storepb.MetaType
	DatabaseRef     string
	State           storepb.QualityAgentEnrollmentState
	Mode            storepb.QualityAgentSessionMode
	Priority        int32
	PriorityReason  string
	BlockedReason   string
	SessionID       *int64
	TriageCount     int32
	LastTriagedAt   *time.Time
	NextReviewAt    *time.Time
	TriageStartedAt *time.Time
	DiscoveredAt    time.Time
	LastSeenAt      time.Time
	// Participation is the per-target exception; nil inherits the database level.
	Participation *QualityAgentParticipation
	Revision      int32
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

const qualityAgentEnrollmentColumns = `id, resource_id, workspace_id, agent_id, target_guid, target_type,
	database_ref, state, mode, priority, priority_reason, blocked_reason, session_id,
	triage_count, last_triaged_at, next_review_at, triage_started_at, discovered_at,
	last_seen_at, participation, revision, created_at, updated_at`

// qualityAgentEnrollmentColumnsQualified is qualityAgentEnrollmentColumns qualified with
// the enrollment alias. The qualification is load-bearing: a read that aliases the queue
// next to a table repeating its column names (the recoverable read joins
// quality_agent_session) is ambiguous to PostgreSQL (SQLSTATE 42702) and fails entirely.
// Keep the column order identical to qualityAgentEnrollmentColumns;
// scanQualityAgentEnrollment depends on it.
const qualityAgentEnrollmentColumnsQualified = `e.id, e.resource_id, e.workspace_id,
	e.agent_id, e.target_guid, e.target_type, e.database_ref, e.state, e.mode, e.priority,
	e.priority_reason, e.blocked_reason, e.session_id, e.triage_count, e.last_triaged_at,
	e.next_review_at, e.triage_started_at, e.discovered_at, e.last_seen_at,
	e.participation, e.revision, e.created_at, e.updated_at`

// QualityAgentEnrollmentUpsert is one discovered object. The store mints a resource id
// for a row it inserts; an existing row keeps its id.
type QualityAgentEnrollmentUpsert struct {
	AgentID     int64
	TargetGUID  string
	TargetType  storepb.MetaType
	DatabaseRef string
}

// UpsertQualityAgentEnrollments records discovered objects. A new row is PENDING; an
// existing row only refreshes last_seen_at and database_ref, while OUT_OF_SCOPE and
// GONE rows come back as PENDING. ACTIVE and DORMANT rows are never reset by
// discovery: the queue's own progress outranks a rescan.
func (s *Store) UpsertQualityAgentEnrollments(ctx context.Context, workspaceID string, updates []*QualityAgentEnrollmentUpsert) (int64, error) {
	if len(updates) == 0 {
		return 0, nil
	}
	resourceIDs := make([]string, 0, len(updates))
	agentIDs := make([]int64, 0, len(updates))
	guids := make([]string, 0, len(updates))
	types := make([]int16, 0, len(updates))
	databases := make([]string, 0, len(updates))
	for _, update := range updates {
		if update == nil || update.AgentID <= 0 || strings.TrimSpace(update.TargetGUID) == "" || update.DatabaseRef == "" {
			return 0, common.Errorf(common.Invalid, "an enrollment needs an agent, a target guid and a database")
		}
		resourceID, err := common.RandomString(12)
		if err != nil {
			return 0, errors.Wrap(err, "failed to generate an enrollment id")
		}
		resourceIDs = append(resourceIDs, resourceID)
		agentIDs = append(agentIDs, update.AgentID)
		guids = append(guids, update.TargetGUID)
		types = append(types, int16(update.TargetType))
		databases = append(databases, update.DatabaseRef)
	}
	result, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO quality_agent_enrollment
			(resource_id, workspace_id, agent_id, target_guid, target_type, database_ref, state, mode)
		SELECT unnest($1::text[]), $2, unnest($3::int8[]), unnest($4::text[]),
			unnest($5::int2[]), unnest($6::text[]), $7, $8
		ON CONFLICT (agent_id, target_guid) DO UPDATE SET
			last_seen_at = NOW(),
			target_type = EXCLUDED.target_type,
			database_ref = EXCLUDED.database_ref,
			state = CASE
				WHEN quality_agent_enrollment.state IN ($9, $10) THEN $7
				ELSE quality_agent_enrollment.state
			END,
			next_review_at = CASE
				WHEN quality_agent_enrollment.state IN ($9, $10) THEN NULL
				ELSE quality_agent_enrollment.next_review_at
			END,
			updated_at = NOW()`,
		pq.Array(resourceIDs), workspaceID, pq.Array(agentIDs), pq.Array(guids), pq.Array(types),
		pq.Array(databases),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING),
		int16(storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_OUT_OF_SCOPE),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_GONE))
	if err != nil {
		return 0, errors.Wrap(err, "failed to upsert the quality agent enrollments")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count the upserted enrollments")
	}
	return affected, nil
}

// MarkQualityAgentEnrollmentsGone records the objects a completed database scan did
// not see and archives the sessions the queue created for them. A row already GONE is
// never touched again.
func (s *Store) MarkQualityAgentEnrollmentsGone(ctx context.Context, workspaceID string, agentID int64, databaseRef string, scanStartedAt time.Time) (int64, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return 0, errors.Wrap(err, "failed to begin the enrollment sweep transaction")
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE quality_agent_enrollment
		SET state = $5, next_review_at = NULL, blocked_reason = '',
			revision = revision + 1, updated_at = NOW()
		WHERE workspace_id = $1 AND agent_id = $2 AND database_ref = $3 AND last_seen_at < $4
			AND state <> $5`,
		workspaceID, agentID, databaseRef, scanStartedAt,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_GONE))
	if err != nil {
		return 0, errors.Wrap(err, "failed to mark the enrollments gone")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count the gone enrollments")
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE quality_agent_session s
		SET state = $4, archived_at = NOW(), next_wake_at = NULL, lease_until = NULL,
			revision = revision + 1, updated_at = NOW()
		WHERE s.workspace_id = $1 AND s.archived_at IS NULL
			AND s.id IN (
				SELECT e.session_id FROM quality_agent_enrollment e
				WHERE e.workspace_id = $1 AND e.agent_id = $2 AND e.database_ref = $3
					AND e.state = $5 AND e.session_id IS NOT NULL
			)`,
		workspaceID, agentID, databaseRef,
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ARCHIVED),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_GONE)); err != nil {
		return 0, errors.Wrap(err, "failed to archive the sessions of gone targets")
	}
	// A gone target's coordination records must not outlive it: the lead would keep
	// listing requests for an archived session and the owner would keep a dead ask.
	if _, err := tx.ExecContext(ctx, `
		UPDATE quality_agent_request r
		SET status = $4, resolved_at = NOW(), updated_at = NOW()
		WHERE r.agent_id = $2 AND r.status IN ($5, $6, $7)
			AND r.origin_session_id IN (
				SELECT e.session_id FROM quality_agent_enrollment e
				WHERE e.workspace_id = $1 AND e.agent_id = $2 AND e.database_ref = $3
					AND e.state = $8 AND e.session_id IS NOT NULL
			)`,
		workspaceID, agentID, databaseRef,
		int16(QualityAgentRequestStatusResolved),
		int16(QualityAgentRequestStatusOpen), int16(QualityAgentRequestStatusForwarded),
		int16(QualityAgentRequestStatusAnswered),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_GONE)); err != nil {
		return 0, errors.Wrap(err, "failed to close the gone targets' requests")
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE quality_agent_ask a
		SET status = $4, resolved_at = NOW(), updated_at = NOW()
		WHERE a.agent_id = $2 AND a.status IN ($5, $6)
			AND (
				a.target_guid IN (
					SELECT e.target_guid FROM quality_agent_enrollment e
					WHERE e.workspace_id = $1 AND e.agent_id = $2 AND e.database_ref = $3
						AND e.state = $7
				)
				OR (
					EXISTS (SELECT 1 FROM quality_agent_request owned WHERE owned.ask_id = a.id)
					AND NOT EXISTS (
						SELECT 1 FROM quality_agent_request r
						WHERE r.ask_id = a.id AND r.status IN ($8, $9)
					)
				)
			)`,
		workspaceID, agentID, databaseRef,
		int16(QualityAgentAskStatusResolved),
		int16(QualityAgentAskStatusOpen), int16(QualityAgentAskStatusAnswered),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_GONE),
		int16(QualityAgentRequestStatusOpen), int16(QualityAgentRequestStatusForwarded)); err != nil {
		return 0, errors.Wrap(err, "failed to close the gone targets' asks")
	}
	if err := tx.Commit(); err != nil {
		return 0, errors.Wrap(err, "failed to commit the enrollment sweep")
	}
	return affected, nil
}

// reconcileOutOfScopeEnrollmentsQuery marks the rows whose database left the scope.
const reconcileOutOfScopeEnrollmentsQuery = `
		UPDATE quality_agent_enrollment
		SET state = $4, blocked_reason = '', next_review_at = NULL,
			revision = revision + 1, updated_at = NOW()
		WHERE workspace_id = $1 AND agent_id = $2 AND state <> $4 AND NOT (database_ref = ANY($3))`

// blockOutOfScopeSessionsQuery blocks only sessions the queue may safely park: a
// needs-human block keeps its state, so re-adding the database cannot undo it. It
// takes no database list: the enrollment rows were already marked OUT_OF_SCOPE by the
// preceding statement, and a parameter that the statement never references would be
// rejected by PostgreSQL as an untyped parameter (SQLSTATE 42P18).
const blockOutOfScopeSessionsQuery = `
		UPDATE quality_agent_session s
		SET state = $3, blocked_code = 'out-of-scope',
			blocked_reason = 'the session''s database is no longer in the runner''s allowlist',
			next_wake_at = NULL, lease_until = NULL,
			revision = revision + 1, updated_at = NOW()
		WHERE s.workspace_id = $1 AND s.archived_at IS NULL
			AND (s.state <> $3 OR s.blocked_code IN ('', 'out-of-scope', 'agent-disabled'))
			AND s.id IN (
				SELECT e.session_id FROM quality_agent_enrollment e
				WHERE e.workspace_id = $1 AND e.agent_id = $4 AND e.state = $2
					AND e.session_id IS NOT NULL
			)`

// restoreInScopeEnrollmentsQuery puts the rows of a database that came back in scope
// into the pending set; admission then resumes their sessions.
const restoreInScopeEnrollmentsQuery = `
		UPDATE quality_agent_enrollment
		SET state = $4, next_review_at = NULL, revision = revision + 1, updated_at = NOW()
		WHERE workspace_id = $1 AND agent_id = $2 AND state = $5 AND database_ref = ANY($3)`

// scopeDatabaseArray encodes the runner's database allowlist for the scope statements.
// An empty allowlist means no database is in scope, so it must encode as an empty array:
// pq encodes a nil slice as NULL, and `= ANY(NULL)` matches no row, which would turn
// clearing the scope into a no-op instead of parking the queue.
func scopeDatabaseArray(keepDatabases []string) pq.StringArray {
	return pq.StringArray(append([]string{}, keepDatabases...))
}

// activateRestoredMonitorEnrollmentsQuery returns restored covered targets to the
// active monitor set. A restored row is PENDING only so the unblock can find it; a
// MONITOR row has no triage step left and must not sit in the pending queue for ever.
const activateRestoredMonitorEnrollmentsQuery = `
		UPDATE quality_agent_enrollment
		SET state = $4, next_review_at = NULL, blocked_reason = '',
			revision = revision + 1, updated_at = NOW()
		WHERE workspace_id = $1 AND agent_id = $2 AND database_ref = ANY($3)
			AND state = $5 AND mode = $6 AND session_id IS NOT NULL
			AND EXISTS (
				SELECT 1 FROM quality_agent_session s
				WHERE s.id = session_id AND s.state = $7 AND s.archived_at IS NULL
			)`

// unblockRestoredSessionsQuery releases the reversible out-of-scope block on sessions
// whose database just came back. The queue row alone is not enough: materialize only
// looks for a missing session, triage only looks at TRIAGE rows, and recoverBlocked only
// looks at ACTIVE rows, so a restored PENDING row with a blocked session would otherwise
// never run again.
const unblockRestoredSessionsQuery = `
		UPDATE quality_agent_session s
		SET state = $2, blocked_code = '', blocked_reason = '',
			next_wake_at = LEAST(COALESCE(s.next_wake_at, NOW()), NOW()),
			revision = s.revision + 1, updated_at = NOW()
		WHERE s.archived_at IS NULL AND s.state = $3 AND s.blocked_code = 'out-of-scope'
			AND s.id IN (
				SELECT e.session_id FROM quality_agent_enrollment e
				WHERE e.workspace_id = $1 AND e.agent_id = $4
				  AND e.database_ref = ANY($5) AND e.state = $6 AND e.mode = $7
				  AND e.session_id IS NOT NULL
			)`

// ReconcileQualityAgentEnrollmentScope aligns the queue with the settings' database
// list: rows outside the list become OUT_OF_SCOPE and their sessions are blocked with
// the reversible out-of-scope code; rows that came back become PENDING again, so
// admission can resume them without a person. An empty list means no database is in
// scope, so every row is parked.
func (s *Store) ReconcileQualityAgentEnrollmentScope(ctx context.Context, workspaceID string, agentID int64, keepDatabases []string) (outOfScope, restored int64, err error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to begin the enrollment scope transaction")
	}
	defer func() { _ = tx.Rollback() }()
	keep := scopeDatabaseArray(keepDatabases)
	outResult, err := tx.ExecContext(ctx, reconcileOutOfScopeEnrollmentsQuery,
		workspaceID, agentID, keep,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_OUT_OF_SCOPE))
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to mark the enrollments out of scope")
	}
	outOfScope, err = outResult.RowsAffected()
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to count the out-of-scope enrollments")
	}
	if _, err := tx.ExecContext(ctx, blockOutOfScopeSessionsQuery,
		workspaceID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_OUT_OF_SCOPE),
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED),
		agentID); err != nil {
		return 0, 0, errors.Wrap(err, "failed to block the out-of-scope sessions")
	}
	restoreResult, err := tx.ExecContext(ctx, restoreInScopeEnrollmentsQuery,
		workspaceID, agentID, keep,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_OUT_OF_SCOPE))
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to restore the in-scope enrollments")
	}
	restored, err = restoreResult.RowsAffected()
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to count the restored enrollments")
	}
	if _, err := tx.ExecContext(ctx, unblockRestoredSessionsQuery,
		workspaceID, int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED),
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED),
		agentID, keep,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING),
		int16(storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR)); err != nil {
		return 0, 0, errors.Wrap(err, "failed to unblock the restored sessions")
	}
	if _, err := tx.ExecContext(ctx, activateRestoredMonitorEnrollmentsQuery,
		workspaceID, agentID, keep,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_ACTIVE),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING),
		int16(storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR),
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED)); err != nil {
		return 0, 0, errors.Wrap(err, "failed to reactivate the restored monitor enrollments")
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, errors.Wrap(err, "failed to commit the enrollment scope reconciliation")
	}
	return outOfScope, restored, nil
}

// FindQualityAgentEnrollmentMessage filters an enrollment list; every field is
// optional.
type FindQualityAgentEnrollmentMessage struct {
	WorkspaceID string
	AgentID     int64
	DatabaseRef *string
	States      []storepb.QualityAgentEnrollmentState
	Mode        storepb.QualityAgentSessionMode
	Search      string
	// OrderByTarget walks a stable target order. Priority recomputation updates
	// priority while paging, so ordering by it would skip rows between pages.
	OrderByTarget bool
	Limit         *int
	Offset        *int
}

// ListQualityAgentEnrollments lists the queue, most urgent first.
func (s *Store) ListQualityAgentEnrollments(ctx context.Context, find *FindQualityAgentEnrollmentMessage) ([]*QualityAgentEnrollmentMessage, error) {
	if find == nil || find.WorkspaceID == "" {
		return nil, common.Errorf(common.Invalid, "a workspace id is required")
	}
	where := "workspace_id = $1"
	args := []any{find.WorkspaceID}
	if find.AgentID > 0 {
		args = append(args, find.AgentID)
		where += " AND agent_id = $" + strconv.Itoa(len(args))
	}
	if find.DatabaseRef != nil && *find.DatabaseRef != "" {
		args = append(args, *find.DatabaseRef)
		where += " AND database_ref = $" + strconv.Itoa(len(args))
	}
	if len(find.States) > 0 {
		states := make([]int16, 0, len(find.States))
		for _, state := range find.States {
			states = append(states, int16(state))
		}
		args = append(args, pq.Array(states))
		where += " AND state = ANY($" + strconv.Itoa(len(args)) + "::int2[])"
	}
	if find.Mode != storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_UNSPECIFIED {
		args = append(args, int16(find.Mode))
		where += " AND mode = $" + strconv.Itoa(len(args))
	}
	if search := strings.TrimSpace(find.Search); search != "" {
		args = append(args, "%"+likePatternEscaper.Replace(search)+"%")
		where += " AND target_guid ILIKE $" + strconv.Itoa(len(args)) + " ESCAPE E'\\\\'"
	}
	limit, offset := 100, 0
	if find.Limit != nil && *find.Limit > 0 {
		limit = *find.Limit
	}
	if find.Offset != nil && *find.Offset > 0 {
		offset = *find.Offset
	}
	args = append(args, limit, offset)
	order := "priority DESC, target_guid ASC"
	if find.OrderByTarget {
		order = "target_guid ASC"
	}
	rows, err := s.GetDB().QueryContext(ctx, `SELECT `+qualityAgentEnrollmentColumns+`
		FROM quality_agent_enrollment WHERE `+where+`
		ORDER BY `+order+`
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality agent enrollments")
	}
	defer rows.Close()
	var enrollments []*QualityAgentEnrollmentMessage
	for rows.Next() {
		enrollment, err := scanQualityAgentEnrollment(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality agent enrollment")
		}
		enrollments = append(enrollments, enrollment)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality agent enrollments")
	}
	return enrollments, nil
}

// GetQualityAgentEnrollmentByTarget reads a target's enrollment; nil means absent.
func (s *Store) GetQualityAgentEnrollmentByTarget(ctx context.Context, workspaceID string, agentID int64, targetGUID string) (*QualityAgentEnrollmentMessage, error) {
	enrollment, err := scanQualityAgentEnrollment(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentEnrollmentColumns+` FROM quality_agent_enrollment
		 WHERE workspace_id = $1 AND agent_id = $2 AND target_guid = $3`, workspaceID, agentID, targetGUID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the target's quality agent enrollment")
	}
	return enrollment, nil
}

// admissibleQualityAgentEnrollmentsQuery is the admission read: pending rows, ranked
// round robin by database so one large database cannot starve the others. Keeping it in
// one constant lets the store test pin the predicate.
const admissibleQualityAgentEnrollmentsQuery = `
		SELECT ` + qualityAgentEnrollmentColumns + ` FROM (
			SELECT *, ROW_NUMBER() OVER (
				PARTITION BY database_ref ORDER BY priority DESC, target_guid ASC
			) AS db_rank
			FROM quality_agent_enrollment
			WHERE workspace_id = $1 AND agent_id = $2 AND state = $3
		) ranked
		ORDER BY db_rank ASC, priority DESC, target_guid ASC
		LIMIT $4`

// ListAdmissibleQualityAgentEnrollments returns the next pending candidates, round
// robin across databases: one database with a huge backlog cannot starve the others.
func (s *Store) ListAdmissibleQualityAgentEnrollments(ctx context.Context, workspaceID string, agentID int64, limit int) ([]*QualityAgentEnrollmentMessage, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := s.GetDB().QueryContext(ctx, admissibleQualityAgentEnrollmentsQuery,
		workspaceID, agentID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING),
		limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the admissible enrollments")
	}
	defer rows.Close()
	var enrollments []*QualityAgentEnrollmentMessage
	for rows.Next() {
		enrollment, err := scanQualityAgentEnrollment(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan an admissible enrollment")
		}
		enrollments = append(enrollments, enrollment)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the admissible enrollments")
	}
	return enrollments, nil
}

// unmaterializedQualityAgentEnrollmentsQuery finds pending targets that have no live
// session. A session is the target's fixed identity, so it is created once and then
// reused; this query is what makes "every target has a subagent" converge.
const unmaterializedQualityAgentEnrollmentsQuery = `
		SELECT ` + qualityAgentEnrollmentColumnsQualified + ` FROM quality_agent_enrollment e
		LEFT JOIN quality_agent_session s ON s.id = e.session_id AND s.archived_at IS NULL
		WHERE e.workspace_id = $1 AND e.agent_id = $2 AND e.state = $3 AND s.id IS NULL
		ORDER BY e.priority DESC, e.target_guid ASC
		LIMIT $4`

// ListUnmaterializedQualityAgentEnrollments returns pending targets still waiting for
// their subagent, highest priority first.
func (s *Store) ListUnmaterializedQualityAgentEnrollments(ctx context.Context, workspaceID string, agentID int64, limit int) ([]*QualityAgentEnrollmentMessage, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := s.GetDB().QueryContext(ctx, unmaterializedQualityAgentEnrollmentsQuery,
		workspaceID, agentID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING), limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the unmaterialized enrollments")
	}
	defer rows.Close()
	var enrollments []*QualityAgentEnrollmentMessage
	for rows.Next() {
		enrollment, err := scanQualityAgentEnrollment(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan an unmaterialized enrollment")
		}
		enrollments = append(enrollments, enrollment)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the unmaterialized enrollments")
	}
	return enrollments, nil
}

// AttachQualityAgentEnrollmentSession links a pending target to its subagent without
// admitting it. Materialization creates the identity; the mode-specific admission
// (monitor or triage) decides when the session first runs.
func (s *Store) AttachQualityAgentEnrollmentSession(ctx context.Context, resourceID string, sessionID int64, mode storepb.QualityAgentSessionMode) (*QualityAgentEnrollmentMessage, bool, error) {
	enrollment, err := scanQualityAgentEnrollment(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_enrollment
		SET session_id = $2, mode = $3, blocked_reason = '',
			revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND state = $4
		RETURNING `+qualityAgentEnrollmentColumns,
		resourceID, sessionID, int16(mode),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, errors.Wrap(err, "failed to attach the enrollment session")
	}
	return enrollment, true, nil
}

// pendingTriageQualityAgentEnrollmentsQuery is the triage queue: materialized targets
// that are uncovered and waiting for a model slot.
const pendingTriageQualityAgentEnrollmentsQuery = `
		SELECT ` + qualityAgentEnrollmentColumns + ` FROM quality_agent_enrollment
		WHERE workspace_id = $1 AND agent_id = $2 AND state = $3 AND mode = $4
			AND session_id IS NOT NULL
		ORDER BY priority DESC, target_guid ASC
		LIMIT $5`

// ListPendingTriageQualityAgentEnrollments returns the materialized targets waiting for
// their one-time triage.
func (s *Store) ListPendingTriageQualityAgentEnrollments(ctx context.Context, workspaceID string, agentID int64, limit int) ([]*QualityAgentEnrollmentMessage, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := s.GetDB().QueryContext(ctx, pendingTriageQualityAgentEnrollmentsQuery,
		workspaceID, agentID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING),
		int16(storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE),
		limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the pending triage enrollments")
	}
	defer rows.Close()
	var enrollments []*QualityAgentEnrollmentMessage
	for rows.Next() {
		enrollment, err := scanQualityAgentEnrollment(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a pending triage enrollment")
		}
		enrollments = append(enrollments, enrollment)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the pending triage enrollments")
	}
	return enrollments, nil
}

// CountActiveTriageQualityAgentEnrollments counts the triage targets currently holding
// a slot. Monitor targets have their own, effectively unlimited, existence.
func (s *Store) CountActiveTriageQualityAgentEnrollments(ctx context.Context, workspaceID string, agentID int64) (int, error) {
	var total int
	// A session parked for a person (needs-human, restart-limit) must not keep a triage
	// slot. Only a session that can still run counts; a retryable budget block does,
	// because the window will reopen.
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quality_agent_enrollment e
		JOIN quality_agent_session s ON s.id = e.session_id
		WHERE e.workspace_id = $1 AND e.agent_id = $2
			AND e.state = $3 AND e.mode = $4 AND s.archived_at IS NULL
			AND (s.state = $5 OR (s.state = $6 AND s.blocked_code = ANY($7)))`,
		workspaceID, agentID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_ACTIVE),
		int16(storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE),
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED),
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED),
		pq.Array(retryableSessionBlockedCodes())).Scan(&total); err != nil {
		return 0, errors.Wrap(err, "failed to count the active triage enrollments")
	}
	return total, nil
}

// CountQualityAgentTriageStartsSince counts the targets that entered triage in a
// window. Counting session activity instead let a previously triaged target's event
// wake consume the hourly admission rate and starve the real queue.
func (s *Store) CountQualityAgentTriageStartsSince(ctx context.Context, agentID int64, since time.Time) (int, error) {
	var total int
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quality_agent_enrollment
		WHERE agent_id = $1 AND triage_started_at IS NOT NULL AND triage_started_at > $2`,
		agentID, since).Scan(&total); err != nil {
		return 0, errors.Wrap(err, "failed to count the triage starts")
	}
	return total, nil
}

// ListDueDormantQualityAgentEnrollments returns dormant rows whose review is due.
func (s *Store) ListDueDormantQualityAgentEnrollments(ctx context.Context, workspaceID string, agentID int64, now time.Time, limit int) ([]*QualityAgentEnrollmentMessage, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT `+qualityAgentEnrollmentColumns+` FROM quality_agent_enrollment
		WHERE workspace_id = $1 AND agent_id = $2 AND state = $3
			AND next_review_at IS NOT NULL AND next_review_at <= $4
		ORDER BY next_review_at ASC, target_guid ASC
		LIMIT $5`,
		workspaceID, agentID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_DORMANT),
		now, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the dormant enrollments")
	}
	defer rows.Close()
	var enrollments []*QualityAgentEnrollmentMessage
	for rows.Next() {
		enrollment, err := scanQualityAgentEnrollment(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a dormant enrollment")
		}
		enrollments = append(enrollments, enrollment)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the dormant enrollments")
	}
	return enrollments, nil
}

// coveredDormantQualityAgentEnrollmentsQuery finds dormant rows whose target gained
// coverage: an enabled check or a published contract. The queue is aliased for the
// correlated subqueries, so the projection is the alias-qualified one.
const coveredDormantQualityAgentEnrollmentsQuery = `
		SELECT ` + qualityAgentEnrollmentColumnsQualified + ` FROM quality_agent_enrollment e
		WHERE e.workspace_id = $1 AND e.agent_id = $2 AND e.state = $3
			AND (
				EXISTS (
					SELECT 1 FROM quality_rule r
					WHERE r.target_guid = e.target_guid AND r.deleted = FALSE AND r.state = $4
				)
				OR EXISTS (
					SELECT 1 FROM quality_contract c
					WHERE c.target_guid = e.target_guid AND c.target_type = e.target_type
						AND c.published_version > 0
				)
			)
		ORDER BY e.priority DESC, e.target_guid ASC
		LIMIT $5`

// ListCoveredDormantQualityAgentEnrollments returns dormant rows whose target has
// gained coverage: an enabled check or a published contract. Admission promotes them
// back to monitoring without a person doing anything.
func (s *Store) ListCoveredDormantQualityAgentEnrollments(ctx context.Context, workspaceID string, agentID int64, limit int) ([]*QualityAgentEnrollmentMessage, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := s.GetDB().QueryContext(ctx, coveredDormantQualityAgentEnrollmentsQuery,
		workspaceID, agentID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_DORMANT),
		int16(storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED),
		limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the covered dormant enrollments")
	}
	defer rows.Close()
	var enrollments []*QualityAgentEnrollmentMessage
	for rows.Next() {
		enrollment, err := scanQualityAgentEnrollment(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a covered dormant enrollment")
		}
		enrollments = append(enrollments, enrollment)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the covered dormant enrollments")
	}
	return enrollments, nil
}

// CountActiveQualityAgentEnrollments counts the queue's active set, the admission cap.
func (s *Store) CountActiveQualityAgentEnrollments(ctx context.Context, workspaceID string, agentID int64) (int, error) {
	var count int
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quality_agent_enrollment
		WHERE workspace_id = $1 AND agent_id = $2 AND state = $3`,
		workspaceID, agentID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_ACTIVE)).Scan(&count); err != nil {
		return 0, errors.Wrap(err, "failed to count the active enrollments")
	}
	return count, nil
}

// QualityAgentDatabaseEnrollmentSummary is one database's slice of the queue.
type QualityAgentDatabaseEnrollmentSummary struct {
	DatabaseRef string
	Pending     int
	Active      int
	Dormant     int
	OutOfScope  int
	Gone        int
}

// QualityAgentEnrollmentSummary counts the queue per database.
func (s *Store) QualityAgentEnrollmentSummary(ctx context.Context, workspaceID string, agentID int64) ([]*QualityAgentDatabaseEnrollmentSummary, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT database_ref,
			COUNT(*) FILTER (WHERE state = $3)::INT,
			COUNT(*) FILTER (WHERE state = $4)::INT,
			COUNT(*) FILTER (WHERE state = $5)::INT,
			COUNT(*) FILTER (WHERE state = $6)::INT,
			COUNT(*) FILTER (WHERE state = $7)::INT
		FROM quality_agent_enrollment
		WHERE workspace_id = $1 AND agent_id = $2
		GROUP BY database_ref
		ORDER BY database_ref`,
		workspaceID, agentID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_ACTIVE),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_DORMANT),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_OUT_OF_SCOPE),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_GONE))
	if err != nil {
		return nil, errors.Wrap(err, "failed to summarize the enrollments")
	}
	defer rows.Close()
	var summaries []*QualityAgentDatabaseEnrollmentSummary
	for rows.Next() {
		var summary QualityAgentDatabaseEnrollmentSummary
		if err := rows.Scan(&summary.DatabaseRef, &summary.Pending, &summary.Active,
			&summary.Dormant, &summary.OutOfScope, &summary.Gone); err != nil {
			return nil, errors.Wrap(err, "failed to scan an enrollment summary")
		}
		summaries = append(summaries, &summary)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the enrollment summaries")
	}
	return summaries, nil
}

// UpdateQualityAgentEnrollmentPriority writes a batch of computed priorities. The
// caller keeps the enrollment's state untouched; only the ordering hints move.
func (s *Store) UpdateQualityAgentEnrollmentPriority(ctx context.Context, updates []*QualityAgentEnrollmentPriorityUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	resourceIDs := make([]string, 0, len(updates))
	priorities := make([]int32, 0, len(updates))
	reasons := make([]string, 0, len(updates))
	modes := make([]int16, 0, len(updates))
	for _, update := range updates {
		if update == nil || update.ResourceID == "" {
			continue
		}
		resourceIDs = append(resourceIDs, update.ResourceID)
		priorities = append(priorities, update.Priority)
		reasons = append(reasons, update.PriorityReason)
		modes = append(modes, int16(update.Mode))
	}
	if len(resourceIDs) == 0 {
		return nil
	}
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_enrollment e
		SET priority = v.priority, priority_reason = v.priority_reason,
			mode = CASE WHEN e.state = $5 THEN v.mode ELSE e.mode END,
			updated_at = NOW()
		FROM (
			SELECT unnest($1::text[]) AS resource_id,
				unnest($2::int4[]) AS priority,
				unnest($3::text[]) AS priority_reason,
				unnest($4::int2[]) AS mode
		) v
		WHERE e.resource_id = v.resource_id`,
		pq.Array(resourceIDs), pq.Array(priorities), pq.Array(reasons), pq.Array(modes),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING))
	if err != nil {
		return errors.Wrap(err, "failed to update the enrollment priorities")
	}
	return nil
}

// QualityAgentEnrollmentPriorityUpdate is one computed priority.
type QualityAgentEnrollmentPriorityUpdate struct {
	ResourceID     string
	Priority       int32
	PriorityReason string
	// Mode is what admission should create: MONITOR when the target already has an
	// enabled check, TRIAGE otherwise. It is only applied while the row is waiting.
	Mode storepb.QualityAgentSessionMode
}

// LinkQualityAgentEnrollmentSession marks a pending enrollment active and records the
// session admission created or resumed. It only applies while the row is still
// PENDING, so a person's exclusion or another tick cannot be resurrected.
func (s *Store) LinkQualityAgentEnrollmentSession(ctx context.Context, resourceID string, sessionID int64, mode storepb.QualityAgentSessionMode) (*QualityAgentEnrollmentMessage, bool, error) {
	enrollment, err := scanQualityAgentEnrollment(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_enrollment
		SET state = $2, session_id = $3, mode = $4, blocked_reason = '',
			next_review_at = NULL, revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND state = $5
		RETURNING `+qualityAgentEnrollmentColumns,
		resourceID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_ACTIVE),
		sessionID, int16(mode),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, errors.Wrap(err, "failed to link the enrollment session")
	}
	return enrollment, true, nil
}

// AdmitQualityAgentTriage moves one materialized target into triage in one
// transaction: the queue row becomes active, the session switches mode, and its wake
// becomes due. A partial write would leave a TRIAGE session waking without a slot or an
// active enrollment whose session never runs.
func (s *Store) AdmitQualityAgentTriage(ctx context.Context, enrollmentResourceID, sessionResourceID string, now time.Time) (bool, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return false, errors.Wrap(err, "failed to begin the triage admission")
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE quality_agent_enrollment
		SET state = $2, mode = $3, next_review_at = NULL, triage_started_at = NOW(),
			blocked_reason = '', revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND state = $4`,
		enrollmentResourceID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_ACTIVE),
		int16(storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING))
	if err != nil {
		return false, errors.Wrap(err, "failed to activate the triage enrollment")
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
		return false, errors.Wrap(rowsErr, "failed to count the triage enrollment")
	} else if affected == 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE quality_agent_session
		SET mode = $2, next_wake_at = LEAST(COALESCE(next_wake_at, $3), $3),
			revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND archived_at IS NULL`,
		sessionResourceID,
		int16(storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE), now.UTC()); err != nil {
		return false, errors.Wrap(err, "failed to start the triage session")
	}
	if err := tx.Commit(); err != nil {
		return false, errors.Wrap(err, "failed to commit the triage admission")
	}
	return true, nil
}

// SetQualityAgentEnrollmentBlockedReason records why admission passed a pending row
// over. It only writes when the reason actually changes, so a persistent gate does not
// rewrite every row on every tick.
func (s *Store) SetQualityAgentEnrollmentBlockedReason(ctx context.Context, resourceID, reason string) error {
	if _, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_enrollment
		SET blocked_reason = $2, updated_at = NOW()
		WHERE resource_id = $1 AND blocked_reason <> $2`,
		resourceID, reason); err != nil {
		return errors.Wrap(err, "failed to record the enrollment blocked reason")
	}
	return nil
}

// UpdateQualityAgentEnrollmentAfterTriage parks a triaged enrollment until its next
// review. The session stays as the target's conversation; only the queue state moves.
func (s *Store) UpdateQualityAgentEnrollmentAfterTriage(ctx context.Context, resourceID string, nextReviewAt time.Time) (*QualityAgentEnrollmentMessage, bool, error) {
	enrollment, err := scanQualityAgentEnrollment(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_enrollment
		SET state = $2, triage_count = triage_count + 1, last_triaged_at = NOW(),
			next_review_at = $3, blocked_reason = '',
			revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND state = $4
		RETURNING `+qualityAgentEnrollmentColumns,
		resourceID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_DORMANT),
		nextReviewAt,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_ACTIVE)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, errors.Wrap(err, "failed to park the triaged enrollment")
	}
	return enrollment, true, nil
}

// PromoteQualityAgentEnrollment moves a dormant row back to active monitoring after
// its target gained coverage.
func (s *Store) PromoteQualityAgentEnrollment(ctx context.Context, resourceID string, mode storepb.QualityAgentSessionMode) (*QualityAgentEnrollmentMessage, bool, error) {
	enrollment, err := scanQualityAgentEnrollment(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_enrollment
		SET state = $2, mode = $3, next_review_at = NULL, blocked_reason = '',
			revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND state = $4
		RETURNING `+qualityAgentEnrollmentColumns,
		resourceID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_ACTIVE),
		int16(mode),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_DORMANT)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, errors.Wrap(err, "failed to promote the enrollment")
	}
	return enrollment, true, nil
}

// RequeueQualityAgentEnrollments moves dormant rows back to pending so admission can
// review them again. The priority bump keeps a due row ahead of untouched backlog.
func (s *Store) RequeueQualityAgentEnrollments(ctx context.Context, resourceIDs []string, _ string) (int64, error) {
	if len(resourceIDs) == 0 {
		return 0, nil
	}
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_enrollment
		SET state = $2, next_review_at = NULL, blocked_reason = '',
			priority = LEAST(1000, priority + 50),
			revision = revision + 1, updated_at = NOW()
		WHERE resource_id = ANY($1) AND state = $3`,
		pq.Array(resourceIDs),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_DORMANT))
	if err != nil {
		return 0, errors.Wrap(err, "failed to requeue the dormant enrollments")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count the requeued enrollments")
	}
	return affected, nil
}

// scanQualityAgentEnrollment scans one enrollment row.
func scanQualityAgentEnrollment(scanner interface{ Scan(dest ...any) error }) (*QualityAgentEnrollmentMessage, error) {
	var (
		enrollment    QualityAgentEnrollmentMessage
		targetType    int16
		state         int16
		mode          int16
		participation sql.NullInt16
	)
	if err := scanner.Scan(
		&enrollment.ID, &enrollment.ResourceID, &enrollment.WorkspaceID, &enrollment.AgentID,
		&enrollment.TargetGUID, &targetType, &enrollment.DatabaseRef, &state, &mode,
		&enrollment.Priority, &enrollment.PriorityReason, &enrollment.BlockedReason,
		&enrollment.SessionID, &enrollment.TriageCount, &enrollment.LastTriagedAt,
		&enrollment.NextReviewAt, &enrollment.TriageStartedAt, &enrollment.DiscoveredAt,
		&enrollment.LastSeenAt, &participation, &enrollment.Revision, &enrollment.CreatedAt,
		&enrollment.UpdatedAt,
	); err != nil {
		return nil, err
	}
	enrollment.TargetType = storepb.MetaType(targetType)
	enrollment.State = storepb.QualityAgentEnrollmentState(state)
	enrollment.Mode = storepb.QualityAgentSessionMode(mode)
	if participation.Valid {
		level := QualityAgentParticipation(participation.Int16)
		enrollment.Participation = &level
	}
	return &enrollment, nil
}

// ListQualityAgentSessionResourceIDs maps session ids to their API-facing resource
// ids in one query, so an enrollment list can render session names without a query
// per row.
func (s *Store) ListQualityAgentSessionResourceIDs(ctx context.Context, workspaceID string, sessionIDs []int64) (map[int64]string, error) {
	names := map[int64]string{}
	if len(sessionIDs) == 0 {
		return names, nil
	}
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT id, resource_id FROM quality_agent_session
		WHERE workspace_id = $1 AND id = ANY($2)`,
		workspaceID, pq.Array(sessionIDs))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the enrollment session ids")
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var resourceID string
		if err := rows.Scan(&id, &resourceID); err != nil {
			return nil, errors.Wrap(err, "failed to scan an enrollment session id")
		}
		names[id] = resourceID
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the enrollment session ids")
	}
	return names, nil
}

// recoverableQualityAgentEnrollmentsQuery is the recovery read: active enrollments
// whose session a reversible condition blocked. The projection must stay qualified with
// the enrollment alias because the join partner repeats the queue's column names.
const recoverableQualityAgentEnrollmentsQuery = `
		SELECT ` + qualityAgentEnrollmentColumnsQualified + ` FROM quality_agent_enrollment e
		JOIN quality_agent_session s ON s.id = e.session_id
		WHERE e.workspace_id = $1 AND e.agent_id = $2 AND e.state = $3
			AND s.archived_at IS NULL AND s.state = $4
			AND s.blocked_code IN ('out-of-scope', 'agent-disabled')
		ORDER BY e.priority DESC, e.target_guid ASC
		LIMIT $5`

// ListRecoverableQualityAgentEnrollments lists active enrollments whose session was
// blocked by a reversible condition (the database left the scope or the agent was
// disabled). The enrollment runner re-enables them once the condition clears.
func (s *Store) ListRecoverableQualityAgentEnrollments(ctx context.Context, workspaceID string, agentID int64, limit int) ([]*QualityAgentEnrollmentMessage, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := s.GetDB().QueryContext(ctx, recoverableQualityAgentEnrollmentsQuery,
		workspaceID, agentID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_ACTIVE),
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED),
		limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the recoverable enrollments")
	}
	defer rows.Close()
	var enrollments []*QualityAgentEnrollmentMessage
	for rows.Next() {
		enrollment, err := scanQualityAgentEnrollment(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a recoverable enrollment")
		}
		enrollments = append(enrollments, enrollment)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the recoverable enrollments")
	}
	return enrollments, nil
}

// CurrentDatabaseTime reads the metadata database's clock. A discovery scan uses it as
// the GONE watermark, because comparing a Go clock with the NOW() the upsert writes
// would mark freshly seen rows gone whenever the application clock runs ahead.
func (s *Store) CurrentDatabaseTime(ctx context.Context) (time.Time, error) {
	var now time.Time
	if err := s.GetDB().QueryRowContext(ctx, `SELECT NOW()`).Scan(&now); err != nil {
		return time.Time{}, errors.Wrap(err, "failed to read the database time")
	}
	return now.UTC(), nil
}
