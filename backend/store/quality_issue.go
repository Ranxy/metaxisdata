package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualityIssueResourceIDLength is the length of a generated issue resource id. The
// resource id is public (it names the resource) and never recycled, so it is random
// rather than sequential; the BIGSERIAL primary key stays internal.
const qualityIssueResourceIDLength = 12

// QualityIssueMessage is the store representation of one persistent problem for one
// rule.
//
// An issue is the durable history of a rule failing, not a per-run record: a run opens
// at most one issue and later failing runs only touch it. The state machine is
//
//	OPEN --acknowledge--> ACKNOWLEDGED --resolve--> RESOLVED
//	OPEN ------------------------------resolve-----> RESOLVED
//	ACKNOWLEDGED ----------------------resolve-----> RESOLVED
//
// RESOLVED is terminal for the store: a resolved issue is never reopened, because the
// next failing run opens a new issue. That is what keeps "at most one unresolved issue
// per rule" true over time, with the partial unique index idx_quality_issue_open_rule
// as the database-level backstop.
type QualityIssueMessage struct {
	ID         int64
	ResourceID string
	RuleID     int64
	TargetGUID string
	TargetType storepb.MetaType
	Status     storepb.QualityIssueStatus
	Severity   storepb.QualitySeverity
	// FirstSeenExecutionID and LastSeenExecutionID are nullable in the table: an
	// issue can be opened by a path that has no execution (a manual import, a
	// backfill) even though the runner always has one.
	FirstSeenExecutionID *int64
	LastSeenExecutionID  *int64
	FirstSeenAt          time.Time
	LastSeenAt           time.Time
	ResolvedAt           *time.Time
	// ResolutionReason is NULL in the table when it is UNSPECIFIED, so an issue
	// resolved before reasons existed reads back as UNSPECIFIED rather than as a
	// made-up value.
	ResolutionReason storepb.QualityResolutionReason
	ResolutionNote   string
	AcknowledgedBy   *int32
	AcknowledgedAt   *time.Time
	MutedUntil       *time.Time
	OccurrenceCount  int32
	UpdatedAt        time.Time
}

// FindQualityIssueMessage is the query filter for quality issues.
//
// OpenOnly and Statuses are additive: both predicates are ANDed, so asking for
// OpenOnly with Statuses={RESOLVED} matches nothing. OpenOnly means "not RESOLVED"
// (status <> 3), which is the same predicate the partial unique index uses, so the
// issue list and the uniqueness guarantee agree on what "open" means.
type FindQualityIssueMessage struct {
	ResourceID *string
	RuleID     *int64
	// RuleIDs keeps only issues of the listed rules. It is how the API resolves an
	// owner filter: ownership lives on the rule, not on the issue, so the handler
	// reads the owner's rules first and passes their ids here. An empty slice adds
	// no predicate; it is ANDed with RuleID, so passing both requires both.
	RuleIDs    []int64
	Statuses   []storepb.QualityIssueStatus
	OpenOnly   bool
	Severities []storepb.QualitySeverity
	// TargetGUID matches the object or any of its descendants: the GUID is a
	// ';'-separated path, so an instance prefix selects every issue under it.
	TargetGUID *string
	// StartAt and EndAt bound first_seen_at, the moment the problem appeared. The
	// list is ordered by it, so a range filter and the ordering agree.
	StartAt *time.Time
	EndAt   *time.Time
	Limit   *int
	Offset  *int
}

// UpdateQualityIssueMessage contains the mutable fields of an issue for the
// management update path. A nil field is left untouched; an explicit zero time for
// MutedUntil or AcknowledgedAt clears the column (the API has no other way to
// express NULL through a field mask).
type UpdateQualityIssueMessage struct {
	Status           *storepb.QualityIssueStatus
	ResolutionReason *storepb.QualityResolutionReason
	ResolutionNote   *string
	MutedUntil       *time.Time
	AcknowledgedBy   *int32
	AcknowledgedAt   *time.Time
}

// qualityIssueColumns is the projection every read in this file uses, in the order
// scanQualityIssue consumes it. Listing the columns explicitly keeps a schema change
// from silently shifting a scan.
const qualityIssueColumns = `id, resource_id, rule_id, target_guid, target_type, status, severity, ` +
	`first_seen_execution_id, last_seen_execution_id, first_seen_at, last_seen_at, resolved_at, ` +
	`resolution_reason, resolution_note, acknowledged_by, acknowledged_at, muted_until, ` +
	`occurrence_count, updated_at`

// scanQualityIssue reads one row of qualityIssueColumns. The nullable timestamps and
// ids scan through sql.Null* values, so a NULL column becomes a nil pointer rather
// than the zero time (which is a valid TIMESTAMPTZ and would be indistinguishable from
// a real value).
func scanQualityIssue(row interface{ Scan(...any) error }) (*QualityIssueMessage, error) {
	var (
		msg                              QualityIssueMessage
		targetType, status, severity     int16
		firstSeenExecutionID             sql.NullInt64
		lastSeenExecutionID              sql.NullInt64
		resolvedAt                       sql.NullTime
		resolutionReason, acknowledgedBy sql.NullInt32
		acknowledgedAt, mutedUntil       sql.NullTime
		occurrenceCount                  int32
	)
	if err := row.Scan(
		&msg.ID, &msg.ResourceID, &msg.RuleID, &msg.TargetGUID, &targetType, &status, &severity,
		&firstSeenExecutionID, &lastSeenExecutionID, &msg.FirstSeenAt, &msg.LastSeenAt, &resolvedAt,
		&resolutionReason, &msg.ResolutionNote, &acknowledgedBy, &acknowledgedAt, &mutedUntil,
		&occurrenceCount, &msg.UpdatedAt,
	); err != nil {
		return nil, err
	}

	msg.TargetType = storepb.MetaType(targetType)
	msg.Status = storepb.QualityIssueStatus(status)
	msg.Severity = storepb.QualitySeverity(severity)
	msg.OccurrenceCount = occurrenceCount

	if firstSeenExecutionID.Valid {
		value := firstSeenExecutionID.Int64
		msg.FirstSeenExecutionID = &value
	}
	if lastSeenExecutionID.Valid {
		value := lastSeenExecutionID.Int64
		msg.LastSeenExecutionID = &value
	}
	if resolvedAt.Valid {
		value := resolvedAt.Time
		msg.ResolvedAt = &value
	}
	if resolutionReason.Valid {
		msg.ResolutionReason = storepb.QualityResolutionReason(resolutionReason.Int32)
	}
	if acknowledgedBy.Valid {
		value := acknowledgedBy.Int32
		msg.AcknowledgedBy = &value
	}
	if acknowledgedAt.Valid {
		value := acknowledgedAt.Time
		msg.AcknowledgedAt = &value
	}
	if mutedUntil.Valid {
		value := mutedUntil.Time
		msg.MutedUntil = &value
	}

	return &msg, nil
}

// buildQualityIssueWhere builds the scoped predicate of every issue read. It is a
// pure function so the scoping shape (a resource id, a rule id, a status/severity
// set, a GUID subtree and a time range) is testable without a database.
func buildQualityIssueWhere(find *FindQualityIssueMessage) ([]string, []any) {
	where, args := []string{"TRUE"}, []any{}
	if find == nil {
		return where, args
	}

	if v := find.ResourceID; v != nil {
		where = append(where, fmt.Sprintf("resource_id = $%d", len(args)+1))
		args = append(args, strings.TrimSpace(*v))
	}
	if v := find.RuleID; v != nil {
		where = append(where, fmt.Sprintf("rule_id = $%d", len(args)+1))
		args = append(args, *v)
	}
	if len(find.RuleIDs) > 0 {
		where = append(where, fmt.Sprintf("rule_id = ANY($%d::bigint[])", len(args)+1))
		args = append(args, pq.Array(find.RuleIDs))
	}
	if len(find.Statuses) > 0 {
		statuses := make([]int32, 0, len(find.Statuses))
		for _, status := range find.Statuses {
			statuses = append(statuses, int32(status))
		}
		where = append(where, fmt.Sprintf("status = ANY($%d::int2[])", len(args)+1))
		args = append(args, pq.Array(statuses))
	}
	if find.OpenOnly {
		where = append(where, fmt.Sprintf("status <> $%d", len(args)+1))
		args = append(args, int16(storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED))
	}
	if len(find.Severities) > 0 {
		severities := make([]int32, 0, len(find.Severities))
		for _, severity := range find.Severities {
			severities = append(severities, int32(severity))
		}
		where = append(where, fmt.Sprintf("severity = ANY($%d::int2[])", len(args)+1))
		args = append(args, pq.Array(severities))
	}
	if v := find.TargetGUID; v != nil && strings.TrimSpace(*v) != "" {
		where, args = appendGUIDSubtreeCondition(where, args, "target_guid", strings.TrimSpace(*v))
	}
	if v := find.StartAt; v != nil {
		where = append(where, fmt.Sprintf("first_seen_at >= $%d", len(args)+1))
		args = append(args, *v)
	}
	if v := find.EndAt; v != nil {
		where = append(where, fmt.Sprintf("first_seen_at <= $%d", len(args)+1))
		args = append(args, *v)
	}

	return where, args
}

// buildListQualityIssuesQuery builds the issue list statement: the filter, the
// stable order and the pagination in one place.
//
// The order is first_seen_at DESC, id DESC rather than updated_at DESC: what the list
// answers is "which problems appeared most recently", and touch/acknowledge/resolve
// bump updated_at, so ordering by it would make a long-standing issue jump around
// whenever anybody touched it. first_seen_at is also the column the time range filter
// uses, so filtering and ordering agree.
func buildListQualityIssuesQuery(find *FindQualityIssueMessage) (string, []any) {
	where, args := buildQualityIssueWhere(find)
	query := `SELECT ` + qualityIssueColumns + ` FROM quality_issue WHERE ` + strings.Join(where, " AND ") +
		` ORDER BY first_seen_at DESC, id DESC`
	if find != nil && find.Limit != nil {
		args = append(args, max(*find.Limit, 0))
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if find != nil && find.Offset != nil {
		args = append(args, max(*find.Offset, 0))
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}
	return query, args
}

// qualityIssueStatusValid reports whether status is one of the three states the table
// and the partial unique index are written against. UNSPECIFIED is not a stored state:
// a row always has a real status, and the API maps an unset filter field to "no
// filter", not to status 0.
func qualityIssueStatusValid(status storepb.QualityIssueStatus) bool {
	switch status {
	case
		storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN,
		storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_ACKNOWLEDGED,
		storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED:
		return true
	default:
		return false
	}
}

// qualityIssueSeverityValid reports whether severity is a real ranking. An issue with
// UNSPECIFIED severity would sort and render as "none", so a producer that forgot to
// pass the rule's severity is rejected instead of stored.
func qualityIssueSeverityValid(severity storepb.QualitySeverity) bool {
	switch severity {
	case
		storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL:
		return true
	default:
		return false
	}
}

// qualityIssueStatusChangeError decides whether a management update may move an issue
// from current to target. It is the pure half of the RESOLVED-is-terminal invariant:
// a resolved issue cannot go back to OPEN, because the next failing run opens a new
// issue, and un-resolving one would let two unresolved issues for the same rule race
// against the partial unique index.
func qualityIssueStatusChangeError(current, target storepb.QualityIssueStatus) error {
	if !qualityIssueStatusValid(target) {
		return common.Errorf(common.Invalid, "invalid quality issue status %d", target)
	}
	if current == storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED {
		return common.Errorf(common.Conflict, "a resolved quality issue cannot change status")
	}
	return nil
}

// GetQualityIssue returns the one issue matching the filter, or nil when none does.
func (s *Store) GetQualityIssue(ctx context.Context, find *FindQualityIssueMessage) (*QualityIssueMessage, error) {
	list, err := s.ListQualityIssues(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	if len(list) > 1 {
		return nil, errors.Errorf("found %d quality issues with filter %+v, expect 1", len(list), find)
	}
	return list[0], nil
}

// GetOpenQualityIssueForRule returns the rule's single unresolved issue, or nil when
// the rule has no open or acknowledged problem. "Unresolved" is status <> RESOLVED,
// the predicate of idx_quality_issue_open_rule, so this lookup is what makes the
// runner's "FAIL with an existing issue" branch and the index agree.
func (s *Store) GetOpenQualityIssueForRule(ctx context.Context, ruleID int64) (*QualityIssueMessage, error) {
	if ruleID <= 0 {
		return nil, common.Errorf(common.Invalid, "quality rule id is required")
	}

	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin the quality issue read transaction")
	}
	defer tx.Rollback()

	msg, err := scanQualityIssue(tx.QueryRowContext(ctx, `
		SELECT `+qualityIssueColumns+`
		FROM quality_issue
		WHERE rule_id = $1 AND status <> $2
	`, ruleID, int16(storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the open quality issue of rule %d", ruleID)
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit the quality issue read transaction")
	}
	return msg, nil
}

// qualityIssueOpenStatement inserts the first issue of a rule.
//
// It is deliberately INSERT ... ON CONFLICT DO NOTHING: PostgreSQL cannot express
// "ON CONFLICT (rule_id) WHERE status <> 3" directly (an ON CONFLICT target cannot
// carry the index predicate of a partial unique index), so the conflict is left
// unspecified and DO NOTHING swallows it. idx_quality_issue_open_rule is the
// backstop for invariant I-4 ("at most one unresolved issue per rule"): the caller
// reads FOR UPDATE and branches, and this statement guarantees that a racing run
// which skipped that read still cannot create a second unresolved issue. A conflict
// means "the issue already exists" and no row is returned.
const qualityIssueOpenStatement = `
	INSERT INTO quality_issue (
		resource_id, rule_id, target_guid, target_type, status, severity,
		first_seen_execution_id, last_seen_execution_id, first_seen_at, last_seen_at,
		occurrence_count, updated_at
	) VALUES (
		$1, $2, $3, $4, $5, $6,
		$7, $8, $9, $10,
		1, $11
	)
	ON CONFLICT DO NOTHING
	RETURNING ` + qualityIssueColumns

// OpenQualityIssue opens an issue for a rule.
//
// The status is forced to OPEN and occurrence_count to 1: an issue is born open, and
// letting a caller insert an already-acknowledged or resolved row would bypass the
// state machine and the partial unique index's definition of "unresolved".
//
// It returns (nil, nil) when the rule already has an unresolved issue, so the caller
// takes the touch branch instead. That is the only expected conflict; a duplicate
// caller-supplied resource id is reported as Conflict instead of being mistaken for
// an existing issue.
func (s *Store) OpenQualityIssue(ctx context.Context, issue *QualityIssueMessage) (*QualityIssueMessage, error) {
	if issue == nil {
		return nil, common.Errorf(common.Invalid, "quality issue is required")
	}
	if issue.RuleID <= 0 {
		return nil, common.Errorf(common.Invalid, "quality rule id is required")
	}
	targetGUID := strings.TrimSpace(issue.TargetGUID)
	if targetGUID == "" {
		return nil, common.Errorf(common.Invalid, "quality issue target guid is required")
	}
	if issue.TargetType == storepb.MetaType_UNSPECIFIED {
		return nil, common.Errorf(common.Invalid, "quality issue target type is required")
	}
	if !qualityIssueSeverityValid(issue.Severity) {
		return nil, common.Errorf(common.Invalid, "invalid quality issue severity %d", issue.Severity)
	}

	resourceID := strings.TrimSpace(issue.ResourceID)
	generatedResourceID := false
	if resourceID == "" {
		generated, err := common.RandomString(qualityIssueResourceIDLength)
		if err != nil {
			return nil, errors.Wrap(err, "failed to generate a quality issue resource id")
		}
		resourceID = generated
		generatedResourceID = true
	}

	now := time.Now().UTC()
	firstSeenAt := issue.FirstSeenAt
	if firstSeenAt.IsZero() {
		firstSeenAt = now
	}
	lastSeenAt := issue.LastSeenAt
	if lastSeenAt.IsZero() {
		lastSeenAt = firstSeenAt
	}
	firstSeenExecutionID := issue.FirstSeenExecutionID
	lastSeenExecutionID := issue.LastSeenExecutionID
	if lastSeenExecutionID == nil {
		lastSeenExecutionID = firstSeenExecutionID
	}

	msg, err := scanQualityIssue(s.GetDB().QueryRowContext(ctx, qualityIssueOpenStatement,
		resourceID,
		issue.RuleID,
		targetGUID,
		int16(issue.TargetType),
		int16(storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN),
		int16(issue.Severity),
		firstSeenExecutionID,
		lastSeenExecutionID,
		firstSeenAt,
		lastSeenAt,
		now,
	))
	if errors.Is(err, sql.ErrNoRows) {
		// The rule already has an unresolved issue: the partial unique index rejected
		// the insert. Distinguish that from a duplicated caller-supplied resource id,
		// which is a request error rather than "already open".
		if !generatedResourceID {
			existing, findErr := s.GetQualityIssue(ctx, &FindQualityIssueMessage{ResourceID: &resourceID})
			if findErr != nil {
				return nil, findErr
			}
			if existing != nil {
				return nil, common.Errorf(common.Conflict, "quality issue resource id %q already exists", resourceID)
			}
		}
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to open the quality issue")
	}
	return msg, nil
}

// qualityIssueTouchStatement records another observation of an unresolved issue. The
// status guard is the invariant "a resolved issue is never touched": if the issue was
// resolved between the caller's read and this write, the update matches nothing and
// the next failing execution opens a new issue instead of resurrecting the old one.
const qualityIssueTouchStatement = `
	UPDATE quality_issue
	SET last_seen_at = $1, last_seen_execution_id = $2, severity = $3,
		occurrence_count = occurrence_count + 1, updated_at = $1
	WHERE id = $4 AND status <> $5
	RETURNING id
`

// TouchQualityIssue records that an execution saw the same problem again: it
// advances the last-seen fields, refreshes the severity (a rule's severity may
// have been edited) and increments occurrence_count. first_seen_* is never
// overwritten.
//
// A no-op when the issue no longer exists or has been resolved: the caller read it a
// moment earlier and the next failing execution will open a fresh one, so this is not
// an error condition.
func (s *Store) TouchQualityIssue(ctx context.Context, issueID int64, executionID int64, severity storepb.QualitySeverity, seenAt time.Time) error {
	if issueID <= 0 {
		return common.Errorf(common.Invalid, "quality issue id is required")
	}
	if executionID <= 0 {
		return common.Errorf(common.Invalid, "quality execution id is required")
	}
	if !qualityIssueSeverityValid(severity) {
		return common.Errorf(common.Invalid, "invalid quality issue severity %d", severity)
	}
	if seenAt.IsZero() {
		seenAt = time.Now().UTC()
	}

	if _, err := s.GetDB().ExecContext(ctx, qualityIssueTouchStatement,
		seenAt, executionID, int16(severity), issueID,
		int16(storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED),
	); err != nil {
		return errors.Wrapf(err, "failed to touch quality issue %d", issueID)
	}
	return nil
}

// qualityIssueResolveStatement closes an issue. $1 is both the new status and the
// guard: the update only matches rows that are not already RESOLVED, so resolving an
// already-resolved issue is a no-op rather than a rewrite of its resolution reason or
// note. updated_at shares resolved_at: the moment the transition happened.
const qualityIssueResolveStatement = `
	UPDATE quality_issue
	SET status = $1, resolved_at = $2, resolution_reason = $3,
		resolution_note = $4, updated_at = $2
	WHERE id = $5 AND status <> $1
	RETURNING id
`

// ResolveQualityIssue closes an issue with a reason and an optional note. Resolving an
// already-resolved issue is idempotent and leaves the original reason, note and time
// intact.
func (s *Store) ResolveQualityIssue(ctx context.Context, issueID int64, reason storepb.QualityResolutionReason, note string, at time.Time) error {
	if issueID <= 0 {
		return common.Errorf(common.Invalid, "quality issue id is required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}

	// UNSPECIFIED is stored as NULL: the column is nullable and "no reason recorded"
	// must not read back as a named reason.
	var reasonArg any
	if reason != storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_UNSPECIFIED {
		reasonArg = int16(reason)
	}

	result, err := s.GetDB().ExecContext(ctx, qualityIssueResolveStatement,
		int16(storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED), at, reasonArg,
		strings.TrimSpace(note), issueID,
	)
	if err != nil {
		return errors.Wrapf(err, "failed to resolve quality issue %d", issueID)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.Wrapf(err, "failed to report the resolved quality issue %d", issueID)
	}
	if affected > 0 {
		return nil
	}
	return s.qualityIssueEnsureResolved(ctx, issueID)
}

// qualityIssueAcknowledgeStatement marks an issue acknowledged. The status guard
// lists what an acknowledgement may act on rather than what it may not: an issue is
// either OPEN or ACKNOWLEDGED, and a RESOLVED one keeps its resolution. The earlier
// `status <> ACKNOWLEDGED` form let a resolved issue be resurrected, because
// RESOLVED differs from ACKNOWLEDGED.
const qualityIssueAcknowledgeStatement = `
	UPDATE quality_issue
	SET status = $1, acknowledged_by = $2, acknowledged_at = $3, updated_at = $3
	WHERE id = $4 AND status IN ($5, $1)
	RETURNING id
`

// AcknowledgeQualityIssue marks an unresolved issue as acknowledged by a principal.
// Acknowledging an already-acknowledged issue updates who acknowledged it and when; a
// resolved issue is not overwritten.
func (s *Store) AcknowledgeQualityIssue(ctx context.Context, issueID int64, principalID int32, at time.Time) error {
	if issueID <= 0 {
		return common.Errorf(common.Invalid, "quality issue id is required")
	}
	if principalID <= 0 {
		return common.Errorf(common.Invalid, "the acknowledging principal is required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}

	result, err := s.GetDB().ExecContext(ctx, qualityIssueAcknowledgeStatement,
		int16(storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_ACKNOWLEDGED), principalID, at, issueID,
		int16(storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN),
	)
	if err != nil {
		return errors.Wrapf(err, "failed to acknowledge quality issue %d", issueID)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.Wrapf(err, "failed to report the acknowledged quality issue %d", issueID)
	}
	if affected > 0 {
		return nil
	}
	return s.qualityIssueEnsureResolved(ctx, issueID)
}

// qualityIssueEnsureResolved runs after a resolve/acknowledge statement matched no row.
// No row means either the issue does not exist (NotFound) or it is already RESOLVED
// (the transition happened, so the request is satisfied). Anything else would mean the
// row changed concurrently in a way the guard did not anticipate, which is an internal
// error rather than a silent success.
func (s *Store) qualityIssueEnsureResolved(ctx context.Context, issueID int64) error {
	var status int16
	if err := s.GetDB().QueryRowContext(ctx, `SELECT status FROM quality_issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.Errorf(common.NotFound, "quality issue %d not found", issueID)
		}
		return errors.Wrapf(err, "failed to read the status of quality issue %d", issueID)
	}
	if storepb.QualityIssueStatus(status) != storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED {
		return common.Errorf(common.Internal, "quality issue %d changed concurrently", issueID)
	}
	return nil
}

// appendQualityIssueSet appends one `column = $n` assignment and its bind value.
func appendQualityIssueSet(sets []string, args []any, column string, value any) ([]string, []any) {
	return append(sets, fmt.Sprintf("%s = $%d", column, len(args)+1)), append(args, value)
}

// buildQualityIssueUpdateSets turns a management patch into the SET list of one
// UPDATE. It is pure, so the two rules that matter are testable without a database:
// a resolved issue cannot change status (see qualityIssueStatusChangeError), and a nil
// field is left untouched. An explicit zero time clears a nullable timestamp, because
// a field mask has no way to express NULL otherwise.
func buildQualityIssueUpdateSets(current *QualityIssueMessage, patch *UpdateQualityIssueMessage) ([]string, []any, error) {
	if patch == nil {
		return nil, nil, common.Errorf(common.Invalid, "an update patch is required")
	}

	sets, args := []string{"updated_at = NOW()"}, []any{}

	if patch.Status != nil {
		if err := qualityIssueStatusChangeError(current.Status, *patch.Status); err != nil {
			return nil, nil, err
		}
		sets, args = appendQualityIssueSet(sets, args, "status", int16(*patch.Status))
		if *patch.Status == storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED {
			// Preserve the first resolution time if the row already carries one (an
			// update that sets RESOLVED twice must not rewrite when it happened).
			sets = append(sets, "resolved_at = COALESCE(resolved_at, NOW())")
		}
	}

	if patch.ResolutionReason != nil {
		if *patch.ResolutionReason == storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_UNSPECIFIED {
			sets = append(sets, "resolution_reason = NULL")
		} else {
			sets, args = appendQualityIssueSet(sets, args, "resolution_reason", int16(*patch.ResolutionReason))
		}
	}

	if patch.ResolutionNote != nil {
		sets, args = appendQualityIssueSet(sets, args, "resolution_note", strings.TrimSpace(*patch.ResolutionNote))
	}

	if patch.MutedUntil != nil {
		if patch.MutedUntil.IsZero() {
			sets = append(sets, "muted_until = NULL")
		} else {
			sets, args = appendQualityIssueSet(sets, args, "muted_until", *patch.MutedUntil)
		}
	}

	if patch.AcknowledgedBy != nil {
		sets, args = appendQualityIssueSet(sets, args, "acknowledged_by", *patch.AcknowledgedBy)
	}
	if patch.AcknowledgedAt != nil {
		if patch.AcknowledgedAt.IsZero() {
			sets = append(sets, "acknowledged_at = NULL")
		} else {
			sets, args = appendQualityIssueSet(sets, args, "acknowledged_at", *patch.AcknowledgedAt)
		}
	}

	return sets, args, nil
}

// qualityIssueLockStatement reads the issue the management update is about to
// change, with a row lock. The lock is part of the update contract: the
// RESOLVED-is-terminal decision must be made against the row that is then written,
// not against a snapshot a concurrent resolve can invalidate.
const qualityIssueLockStatement = `
	SELECT ` + qualityIssueColumns + `
	FROM quality_issue
	WHERE resource_id = $1
	FOR UPDATE
`

// UpdateQualityIssue patches one issue by its public resource id. It is the
// management path (M2 API): status transitions, resolution reason/note, mute and
// acknowledgement.
//
// The read and the write run in one transaction with the row locked FOR UPDATE, so
// the RESOLVED-is-terminal check cannot be bypassed by a concurrent resolve: the
// decision is made against the row the update then writes.
func (s *Store) UpdateQualityIssue(ctx context.Context, resourceID string, patch *UpdateQualityIssueMessage) (*QualityIssueMessage, error) {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return nil, common.Errorf(common.Invalid, "quality issue resource id is required")
	}
	if patch == nil {
		return nil, common.Errorf(common.Invalid, "an update patch is required")
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin the quality issue update transaction")
	}
	defer tx.Rollback()

	current, err := scanQualityIssue(tx.QueryRowContext(ctx, qualityIssueLockStatement, resourceID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, common.Errorf(common.NotFound, "quality issue %q not found", resourceID)
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read quality issue %q", resourceID)
	}

	sets, args, err := buildQualityIssueUpdateSets(current, patch)
	if err != nil {
		return nil, err
	}
	args = append(args, resourceID)
	msg, err := scanQualityIssue(tx.QueryRowContext(ctx, `
		UPDATE quality_issue
		SET `+strings.Join(sets, ", ")+`
		WHERE resource_id = $`+fmt.Sprintf("%d", len(args))+`
		RETURNING `+qualityIssueColumns, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, common.Errorf(common.NotFound, "quality issue %q not found", resourceID)
	}
	if err != nil {
		if isUniqueViolation(err) {
			// Reopening an issue while another unresolved issue for the same rule
			// exists (or setting an unresolved status without resolving the current
			// one) violates idx_quality_issue_open_rule.
			return nil, common.Errorf(common.Conflict, "another unresolved quality issue already exists for the rule")
		}
		return nil, errors.Wrapf(err, "failed to update quality issue %q", resourceID)
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit the quality issue update transaction")
	}
	return msg, nil
}

// ListQualityIssues lists issues matching the filter, newest first by first_seen_at.
// See buildListQualityIssuesQuery for why the order is not updated_at.
func (s *Store) ListQualityIssues(ctx context.Context, find *FindQualityIssueMessage) ([]*QualityIssueMessage, error) {
	query, args := buildListQualityIssuesQuery(find)

	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin the quality issue list transaction")
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query quality issues")
	}
	defer rows.Close()

	var result []*QualityIssueMessage
	for rows.Next() {
		msg, err := scanQualityIssue(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality issue")
		}
		result = append(result, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for quality issues")
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit the quality issue list transaction")
	}
	return result, nil
}

// CountOpenQualityIssues counts the unresolved issues of the workspace. "Open" here is
// status <> RESOLVED, the same definition the issue list's OpenOnly filter and the
// partial unique index use; an acknowledged issue is still an open problem, only a
// resolved one is closed.
func (s *Store) CountOpenQualityIssues(ctx context.Context) (int, error) {
	var count int
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quality_issue WHERE status <> $1
	`, int16(storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED)).Scan(&count); err != nil {
		return 0, errors.Wrap(err, "failed to count open quality issues")
	}
	return count, nil
}

// DeleteExpiredQualityIssues removes the resolved issues whose resolution is
// older than cutoff. An unresolved issue is never pruned: it is the current
// state of the rule, not history.
func (s *Store) DeleteExpiredQualityIssues(ctx context.Context, cutoff time.Time) (int64, error) {
	if cutoff.IsZero() {
		return 0, common.Errorf(common.Invalid, "a cutoff is required")
	}
	result, err := s.GetDB().ExecContext(ctx, `
		DELETE FROM quality_issue
		WHERE resolved_at IS NOT NULL AND resolved_at < $1
	`, cutoff)
	if err != nil {
		return 0, errors.Wrap(err, "failed to delete expired quality issues")
	}
	return result.RowsAffected()
}
