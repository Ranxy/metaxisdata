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
	ID             int64
	ResourceID     string
	WorkspaceID    string
	TargetGUID     string
	TargetType     storepb.MetaType
	DatabaseRef    string
	State          storepb.QualityAgentEnrollmentState
	Mode           storepb.QualityAgentSessionMode
	Priority       int32
	PriorityReason string
	BlockedReason  string
	SessionID      *int64
	TriageCount    int32
	LastTriagedAt  *time.Time
	NextReviewAt   *time.Time
	DiscoveredAt   time.Time
	LastSeenAt     time.Time
	ExcludedBy     *int32
	ExcludedAt     *time.Time
	Revision       int32
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

const qualityAgentEnrollmentColumns = `id, resource_id, workspace_id, target_guid, target_type,
	database_ref, state, mode, priority, priority_reason, blocked_reason, session_id,
	triage_count, last_triaged_at, next_review_at, discovered_at, last_seen_at,
	excluded_by, excluded_at, revision, created_at, updated_at`

// QualityAgentEnrollmentUpsert is one discovered object. The store mints a resource id
// for a row it inserts; an existing row keeps its id.
type QualityAgentEnrollmentUpsert struct {
	TargetGUID  string
	TargetType  storepb.MetaType
	DatabaseRef string
}

// UpsertQualityAgentEnrollments records discovered objects. A new row is PENDING; an
// existing row only refreshes last_seen_at and database_ref, while OUT_OF_SCOPE and
// GONE rows come back as PENDING. ACTIVE, DORMANT and EXCLUDED rows are never reset by
// discovery: a person's exclusion and the queue's own progress outrank a rescan.
func (s *Store) UpsertQualityAgentEnrollments(ctx context.Context, workspaceID string, updates []*QualityAgentEnrollmentUpsert) (int64, error) {
	if len(updates) == 0 {
		return 0, nil
	}
	resourceIDs := make([]string, 0, len(updates))
	guids := make([]string, 0, len(updates))
	types := make([]int16, 0, len(updates))
	databases := make([]string, 0, len(updates))
	for _, update := range updates {
		if update == nil || strings.TrimSpace(update.TargetGUID) == "" || update.DatabaseRef == "" {
			return 0, common.Errorf(common.Invalid, "an enrollment needs a target guid and a database")
		}
		resourceID, err := common.RandomString(12)
		if err != nil {
			return 0, errors.Wrap(err, "failed to generate an enrollment id")
		}
		resourceIDs = append(resourceIDs, resourceID)
		guids = append(guids, update.TargetGUID)
		types = append(types, int16(update.TargetType))
		databases = append(databases, update.DatabaseRef)
	}
	result, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO quality_agent_enrollment
			(resource_id, workspace_id, target_guid, target_type, database_ref, state, mode)
		SELECT unnest($1::text[]), $2, unnest($3::text[]), unnest($4::int2[]),
			unnest($5::text[]), $6, $7
		ON CONFLICT (workspace_id, target_guid) DO UPDATE SET
			last_seen_at = NOW(),
			target_type = EXCLUDED.target_type,
			database_ref = EXCLUDED.database_ref,
			state = CASE
				WHEN quality_agent_enrollment.state IN ($8, $9) THEN $6
				ELSE quality_agent_enrollment.state
			END,
			next_review_at = CASE
				WHEN quality_agent_enrollment.state IN ($8, $9) THEN NULL
				ELSE quality_agent_enrollment.next_review_at
			END,
			updated_at = NOW()`,
		pq.Array(resourceIDs), workspaceID, pq.Array(guids), pq.Array(types),
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
// not see and archives the sessions the queue created for them. A row a person
// excluded is never touched: the exclusion is a decision, not a discovery result.
func (s *Store) MarkQualityAgentEnrollmentsGone(ctx context.Context, workspaceID, databaseRef string, scanStartedAt time.Time) (int64, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return 0, errors.Wrap(err, "failed to begin the enrollment sweep transaction")
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE quality_agent_enrollment
		SET state = $4, next_review_at = NULL, blocked_reason = '',
			revision = revision + 1, updated_at = NOW()
		WHERE workspace_id = $1 AND database_ref = $2 AND last_seen_at < $3
			AND state NOT IN ($5, $4)`,
		workspaceID, databaseRef, scanStartedAt,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_GONE),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_EXCLUDED))
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
				WHERE e.workspace_id = $1 AND e.database_ref = $2 AND e.state = $3
					AND e.session_id IS NOT NULL
			)`,
		workspaceID, databaseRef,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_GONE),
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ARCHIVED)); err != nil {
		return 0, errors.Wrap(err, "failed to archive the sessions of gone targets")
	}
	if err := tx.Commit(); err != nil {
		return 0, errors.Wrap(err, "failed to commit the enrollment sweep")
	}
	return affected, nil
}

// reconcileOutOfScopeEnrollmentsQuery marks the rows whose database left the scope. A
// person's EXCLUDED row is left alone: the exclusion outranks a scope change.
const reconcileOutOfScopeEnrollmentsQuery = `
		UPDATE quality_agent_enrollment
		SET state = $3, blocked_reason = '', next_review_at = NULL,
			revision = revision + 1, updated_at = NOW()
		WHERE workspace_id = $1 AND state <> $4 AND NOT (database_ref = ANY($2))`

// blockOutOfScopeSessionsQuery blocks only sessions the queue may safely park: a human
// pause and a needs-human block keep their state, so re-adding the database cannot undo
// them. $2 is the settings database list, used by the surrounding restore statement only.
const blockOutOfScopeSessionsQuery = `
		UPDATE quality_agent_session s
		SET state = $4, blocked_code = 'out-of-scope',
			blocked_reason = 'the session''s database is no longer in the runner''s allowlist',
			next_wake_at = NULL, lease_until = NULL,
			revision = revision + 1, updated_at = NOW()
		WHERE s.workspace_id = $1 AND s.archived_at IS NULL
			AND s.state <> $5
			AND (s.state <> $4 OR s.blocked_code IN ('', 'out-of-scope', 'agent-disabled'))
			AND s.id IN (
				SELECT e.session_id FROM quality_agent_enrollment e
				WHERE e.workspace_id = $1 AND e.state = $3 AND e.session_id IS NOT NULL
			)`

// restoreInScopeEnrollmentsQuery puts the rows of a database that came back in scope
// into the pending set; admission then resumes their sessions.
const restoreInScopeEnrollmentsQuery = `
		UPDATE quality_agent_enrollment
		SET state = $3, next_review_at = NULL, revision = revision + 1, updated_at = NOW()
		WHERE workspace_id = $1 AND state = $4 AND database_ref = ANY($2)`

// ReconcileQualityAgentEnrollmentScope aligns the queue with the settings' database
// list: rows outside the list become OUT_OF_SCOPE and their sessions are blocked with
// the reversible out-of-scope code; rows that came back become PENDING again, so
// admission can resume them without a person.
func (s *Store) ReconcileQualityAgentEnrollmentScope(ctx context.Context, workspaceID string, keepDatabases []string) (outOfScope, restored int64, err error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to begin the enrollment scope transaction")
	}
	defer func() { _ = tx.Rollback() }()
	keep := pq.Array(keepDatabases)
	outResult, err := tx.ExecContext(ctx, reconcileOutOfScopeEnrollmentsQuery,
		workspaceID, keep,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_OUT_OF_SCOPE),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_EXCLUDED))
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to mark the enrollments out of scope")
	}
	outOfScope, err = outResult.RowsAffected()
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to count the out-of-scope enrollments")
	}
	if _, err := tx.ExecContext(ctx, blockOutOfScopeSessionsQuery,
		workspaceID,
		keep,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_OUT_OF_SCOPE),
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED),
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_PAUSED)); err != nil {
		return 0, 0, errors.Wrap(err, "failed to block the out-of-scope sessions")
	}
	restoreResult, err := tx.ExecContext(ctx, restoreInScopeEnrollmentsQuery,
		workspaceID, keep,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_OUT_OF_SCOPE))
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to restore the in-scope enrollments")
	}
	restored, err = restoreResult.RowsAffected()
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to count the restored enrollments")
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
	DatabaseRef *string
	States      []storepb.QualityAgentEnrollmentState
	Mode        storepb.QualityAgentSessionMode
	Search      string
	Limit       *int
	Offset      *int
}

// ListQualityAgentEnrollments lists the queue, most urgent first.
func (s *Store) ListQualityAgentEnrollments(ctx context.Context, find *FindQualityAgentEnrollmentMessage) ([]*QualityAgentEnrollmentMessage, error) {
	if find == nil || find.WorkspaceID == "" {
		return nil, common.Errorf(common.Invalid, "a workspace id is required")
	}
	where := "workspace_id = $1"
	args := []any{find.WorkspaceID}
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
	rows, err := s.GetDB().QueryContext(ctx, `SELECT `+qualityAgentEnrollmentColumns+`
		FROM quality_agent_enrollment WHERE `+where+`
		ORDER BY priority DESC, target_guid ASC
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

// GetQualityAgentEnrollment reads one enrollment by resource id; nil means absent.
func (s *Store) GetQualityAgentEnrollment(ctx context.Context, resourceID string) (*QualityAgentEnrollmentMessage, error) {
	enrollment, err := scanQualityAgentEnrollment(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentEnrollmentColumns+` FROM quality_agent_enrollment WHERE resource_id = $1`, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality agent enrollment")
	}
	return enrollment, nil
}

// GetQualityAgentEnrollmentByTarget reads a target's enrollment; nil means absent.
func (s *Store) GetQualityAgentEnrollmentByTarget(ctx context.Context, workspaceID, targetGUID string) (*QualityAgentEnrollmentMessage, error) {
	enrollment, err := scanQualityAgentEnrollment(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentEnrollmentColumns+` FROM quality_agent_enrollment
		 WHERE workspace_id = $1 AND target_guid = $2`, workspaceID, targetGUID))
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
			WHERE workspace_id = $1 AND state = $2
		) ranked
		ORDER BY db_rank ASC, priority DESC, target_guid ASC
		LIMIT $3`

// ListAdmissibleQualityAgentEnrollments returns the next pending candidates, round
// robin across databases: one database with a huge backlog cannot starve the others.
func (s *Store) ListAdmissibleQualityAgentEnrollments(ctx context.Context, workspaceID string, limit int) ([]*QualityAgentEnrollmentMessage, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := s.GetDB().QueryContext(ctx, admissibleQualityAgentEnrollmentsQuery,
		workspaceID,
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

// ListDueDormantQualityAgentEnrollments returns dormant rows whose review is due.
func (s *Store) ListDueDormantQualityAgentEnrollments(ctx context.Context, workspaceID string, now time.Time, limit int) ([]*QualityAgentEnrollmentMessage, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT `+qualityAgentEnrollmentColumns+` FROM quality_agent_enrollment
		WHERE workspace_id = $1 AND state = $2
			AND next_review_at IS NOT NULL AND next_review_at <= $3
		ORDER BY next_review_at ASC, target_guid ASC
		LIMIT $4`,
		workspaceID,
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

// ListCoveredDormantQualityAgentEnrollments returns dormant rows whose target has
// gained coverage: an enabled check or a published contract. Admission promotes them
// back to monitoring without a person doing anything.
func (s *Store) ListCoveredDormantQualityAgentEnrollments(ctx context.Context, workspaceID string, limit int) ([]*QualityAgentEnrollmentMessage, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT `+qualityAgentEnrollmentColumns+` FROM quality_agent_enrollment e
		WHERE e.workspace_id = $1 AND e.state = $2
			AND (
				EXISTS (
					SELECT 1 FROM quality_rule r
					WHERE r.target_guid = e.target_guid AND r.deleted = FALSE AND r.state = $3
				)
				OR EXISTS (
					SELECT 1 FROM quality_contract c
					WHERE c.target_guid = e.target_guid AND c.target_type = e.target_type
						AND c.published_version > 0
				)
			)
		ORDER BY e.priority DESC, e.target_guid ASC
		LIMIT $4`,
		workspaceID,
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
func (s *Store) CountActiveQualityAgentEnrollments(ctx context.Context, workspaceID string) (int, error) {
	var count int
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quality_agent_enrollment
		WHERE workspace_id = $1 AND state = $2`,
		workspaceID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_ACTIVE)).Scan(&count); err != nil {
		return 0, errors.Wrap(err, "failed to count the active enrollments")
	}
	return count, nil
}

// QualityAgentEnrollmentStateCounts is one count per non-empty state.
type QualityAgentEnrollmentStateCounts map[storepb.QualityAgentEnrollmentState]int

// CountQualityAgentEnrollmentsByState counts the queue by state.
func (s *Store) CountQualityAgentEnrollmentsByState(ctx context.Context, workspaceID string) (QualityAgentEnrollmentStateCounts, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT state, COUNT(*) FROM quality_agent_enrollment
		WHERE workspace_id = $1 GROUP BY state`, workspaceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to count the enrollments by state")
	}
	defer rows.Close()
	counts := QualityAgentEnrollmentStateCounts{}
	for rows.Next() {
		var state int16
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			return nil, errors.Wrap(err, "failed to scan an enrollment state count")
		}
		counts[storepb.QualityAgentEnrollmentState(state)] = count
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the enrollment state counts")
	}
	return counts, nil
}

// QualityAgentDatabaseEnrollmentSummary is one database's slice of the queue.
type QualityAgentDatabaseEnrollmentSummary struct {
	DatabaseRef string
	Pending     int
	Active      int
	Dormant     int
	Excluded    int
	OutOfScope  int
	Gone        int
}

// QualityAgentEnrollmentSummary counts the queue per database.
func (s *Store) QualityAgentEnrollmentSummary(ctx context.Context, workspaceID string) ([]*QualityAgentDatabaseEnrollmentSummary, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT database_ref,
			COUNT(*) FILTER (WHERE state = $2)::INT,
			COUNT(*) FILTER (WHERE state = $3)::INT,
			COUNT(*) FILTER (WHERE state = $4)::INT,
			COUNT(*) FILTER (WHERE state = $5)::INT,
			COUNT(*) FILTER (WHERE state = $6)::INT,
			COUNT(*) FILTER (WHERE state = $7)::INT
		FROM quality_agent_enrollment
		WHERE workspace_id = $1
		GROUP BY database_ref
		ORDER BY database_ref`,
		workspaceID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_ACTIVE),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_DORMANT),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_EXCLUDED),
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
			&summary.Dormant, &summary.Excluded, &summary.OutOfScope, &summary.Gone); err != nil {
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

// ExcludeQualityAgentEnrollment opts a target out of automatic admission. The scan
// never re-admits an excluded row.
func (s *Store) ExcludeQualityAgentEnrollment(ctx context.Context, resourceID string, expectedRevision int32, excludedBy int32) (*QualityAgentEnrollmentMessage, bool, error) {
	enrollment, err := scanQualityAgentEnrollment(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_enrollment
		SET state = $2, excluded_by = $3, excluded_at = NOW(), next_review_at = NULL,
			blocked_reason = '', revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND revision = $4 AND state <> $5
		RETURNING `+qualityAgentEnrollmentColumns,
		resourceID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_EXCLUDED),
		excludedBy, expectedRevision,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_GONE)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, errors.Wrap(err, "failed to exclude the enrollment")
	}
	return enrollment, true, nil
}

// IncludeQualityAgentEnrollment puts an excluded or out-of-scope target back into the
// queue.
func (s *Store) IncludeQualityAgentEnrollment(ctx context.Context, resourceID string, expectedRevision int32) (*QualityAgentEnrollmentMessage, bool, error) {
	enrollment, err := scanQualityAgentEnrollment(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_enrollment
		SET state = $2, excluded_by = NULL, excluded_at = NULL, next_review_at = NULL,
			blocked_reason = '', revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND revision = $3
			AND state IN ($4, $5, $6)
		RETURNING `+qualityAgentEnrollmentColumns,
		resourceID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING),
		expectedRevision,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_EXCLUDED),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_GONE),
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_OUT_OF_SCOPE)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, errors.Wrap(err, "failed to include the enrollment")
	}
	return enrollment, true, nil
}

// RetryQualityAgentEnrollment requeues one row now with a priority bump.
func (s *Store) RetryQualityAgentEnrollment(ctx context.Context, resourceID string, expectedRevision int32) (*QualityAgentEnrollmentMessage, bool, error) {
	enrollment, err := scanQualityAgentEnrollment(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_enrollment
		SET state = $2, next_review_at = NULL, blocked_reason = '',
			priority = LEAST(1000, priority + 100),
			revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND revision = $3
		RETURNING `+qualityAgentEnrollmentColumns,
		resourceID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING),
		expectedRevision))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, errors.Wrap(err, "failed to retry the enrollment")
	}
	return enrollment, true, nil
}

// SetQualityAgentEnrollmentMode switches how admission will treat the target.
func (s *Store) SetQualityAgentEnrollmentMode(ctx context.Context, resourceID string, expectedRevision int32, mode storepb.QualityAgentSessionMode) (*QualityAgentEnrollmentMessage, bool, error) {
	enrollment, err := scanQualityAgentEnrollment(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_enrollment
		SET mode = $2, revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND revision = $3
		RETURNING `+qualityAgentEnrollmentColumns,
		resourceID, int16(mode), expectedRevision))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, errors.Wrap(err, "failed to set the enrollment mode")
	}
	return enrollment, true, nil
}

// scanQualityAgentEnrollment scans one enrollment row.
func scanQualityAgentEnrollment(scanner interface{ Scan(dest ...any) error }) (*QualityAgentEnrollmentMessage, error) {
	var (
		enrollment QualityAgentEnrollmentMessage
		targetType int16
		state      int16
		mode       int16
	)
	if err := scanner.Scan(
		&enrollment.ID, &enrollment.ResourceID, &enrollment.WorkspaceID,
		&enrollment.TargetGUID, &targetType, &enrollment.DatabaseRef, &state, &mode,
		&enrollment.Priority, &enrollment.PriorityReason, &enrollment.BlockedReason,
		&enrollment.SessionID, &enrollment.TriageCount, &enrollment.LastTriagedAt,
		&enrollment.NextReviewAt, &enrollment.DiscoveredAt, &enrollment.LastSeenAt,
		&enrollment.ExcludedBy, &enrollment.ExcludedAt, &enrollment.Revision,
		&enrollment.CreatedAt, &enrollment.UpdatedAt,
	); err != nil {
		return nil, err
	}
	enrollment.TargetType = storepb.MetaType(targetType)
	enrollment.State = storepb.QualityAgentEnrollmentState(state)
	enrollment.Mode = storepb.QualityAgentSessionMode(mode)
	return &enrollment, nil
}

// AdoptQualityAgentSession records an external (manual or resumed) session as the
// active enrollment of its target. The unique target key makes it idempotent; a person
// creating a session explicitly also lifts a previous exclusion, because the session
// is a stronger statement than the queue row.
func (s *Store) AdoptQualityAgentSession(ctx context.Context, workspaceID, targetGUID string, targetType storepb.MetaType, databaseRef string, sessionID int64, mode storepb.QualityAgentSessionMode) (*QualityAgentEnrollmentMessage, error) {
	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate an enrollment id")
	}
	enrollment, err := scanQualityAgentEnrollment(s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_agent_enrollment
			(resource_id, workspace_id, target_guid, target_type, database_ref, state, mode,
			 session_id, excluded_by, excluded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULL, NULL)
		ON CONFLICT (workspace_id, target_guid) DO UPDATE SET
			target_type = EXCLUDED.target_type,
			database_ref = EXCLUDED.database_ref,
			state = EXCLUDED.state,
			mode = EXCLUDED.mode,
			session_id = EXCLUDED.session_id,
			excluded_by = NULL,
			excluded_at = NULL,
			next_review_at = NULL,
			blocked_reason = '',
			revision = quality_agent_enrollment.revision + 1,
			updated_at = NOW()
		RETURNING `+qualityAgentEnrollmentColumns,
		resourceID, workspaceID, targetGUID, int16(targetType), databaseRef,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_ACTIVE),
		int16(mode), sessionID))
	if err != nil {
		return nil, errors.Wrap(err, "failed to adopt the quality agent session")
	}
	return enrollment, nil
}

// MarkQualityAgentEnrollmentArchived excludes a target's enrollment after a person
// archived its session, so discovery does not create a new one on the next scan.
func (s *Store) MarkQualityAgentEnrollmentArchived(ctx context.Context, workspaceID, targetGUID string) error {
	if _, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_enrollment
		SET state = $3, excluded_at = NOW(), next_review_at = NULL,
			blocked_reason = '', revision = revision + 1, updated_at = NOW()
		WHERE workspace_id = $1 AND target_guid = $2 AND state <> $3`,
		workspaceID, targetGUID,
		int16(storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_EXCLUDED)); err != nil {
		return errors.Wrap(err, "failed to mark the enrollment archived")
	}
	return nil
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

// ListRecoverableQualityAgentEnrollments lists active enrollments whose session was
// blocked by a reversible condition (the database left the scope or the agent was
// disabled). The enrollment runner re-enables them once the condition clears.
func (s *Store) ListRecoverableQualityAgentEnrollments(ctx context.Context, workspaceID string, limit int) ([]*QualityAgentEnrollmentMessage, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT `+qualityAgentEnrollmentColumns+` FROM quality_agent_enrollment e
		JOIN quality_agent_session s ON s.id = e.session_id
		WHERE e.workspace_id = $1 AND e.state = $2
			AND s.archived_at IS NULL AND s.state = $3
			AND s.blocked_code IN ('out-of-scope', 'agent-disabled')
		ORDER BY e.priority DESC, e.target_guid ASC
		LIMIT $4`,
		workspaceID,
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
