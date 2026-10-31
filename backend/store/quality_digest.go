package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualityDigestOwnerMessage is one owner's share of a digest period: the open
// problems whose rules belong to that owner, most first, unassigned last. It is
// the store-side shape; the runner maps it to the notification detail.
type QualityDigestOwnerMessage struct {
	// UserID is the owner's principal id. Zero is the unassigned bucket: rules
	// without an owner, which nobody is asked to look at.
	UserID int32
	// Name is the owner's display name at aggregation time, empty when the
	// account is gone. It is never an address: the outbound payload carries no
	// contact details.
	Name string
	// OpenIssueCount is how many unresolved issues the owner's rules carried at
	// the end of the period.
	OpenIssueCount int32
	// FailingRuleCount is how many distinct rules those issues belong to.
	FailingRuleCount int32
}

// QualityDigestTargetMessage is one target's share of a digest period: the
// targets with the most failed checks in the period.
type QualityDigestTargetMessage struct {
	TargetGUID string
	TargetType storepb.MetaType
	// DisplayPath is the target path snapshot: instance/database/schema/table.
	DisplayPath string
	// FailedExecutionCount and ErroredExecutionCount are the completed
	// executions of the period whose verdict was FAIL and ERROR.
	FailedExecutionCount  int32
	ErroredExecutionCount int32
	// OpenIssueCount is how many unresolved issues the target carried at the end
	// of the period.
	OpenIssueCount int32
}

// QualityDigestSnapshot is the aggregate one weekly digest period reports: how
// the checks ran, how the problem ledger moved, and how the open state splits by
// owner and target. Every count is a point-in-time or in-window answer, so a
// digest is a pure read of the period and cannot disagree with the pages that
// show the same numbers.
//
// The reads are independent rather than one transaction: the issue and open-state
// queries are pinned to the period end by their predicates, while the coverage
// counts and the execution tally read live tables. A rule created or a batch
// completed between two of them can therefore make them disagree slightly inside
// one digest. Weekly, low impact, and a repeatable-read transaction would not
// change what a reader sees in a document that is a summary; accepted.
type QualityDigestSnapshot struct {
	// TargetCount is how many distinct targets had an enabled check as the digest
	// was aggregated, and RuleCount how many enabled checks there were then. The
	// current state is counted because there is no history to reconstruct the
	// period-end state; for a timely digest the difference is minutes.
	TargetCount int32
	RuleCount   int32

	// The verdict tally of the period's completed executions. ExecutionCount is
	// the sum of the four verdicts, not every completed row: a SKIPPED or
	// NOT_EVALUATED execution observed no data, and counting it would make the
	// headline number disagree with the four tallies that explain it.
	ExecutionCount int32
	PassedCount    int32
	WarnedCount    int32
	FailedCount    int32
	ErroredCount   int32

	// The problem ledger's movement inside the period.
	NewIssueCount       int32
	RecoveredIssueCount int32

	// The open state at the end of the period.
	OpenIssueCount    int32
	OpenWarningCount  int32
	OpenErrorCount    int32
	OpenCriticalCount int32

	// Owners and Targets are capped by their queries. They carry no omitted count,
	// so a reader cannot tell from them alone whether five rows are the whole list;
	// the rendered headings say "top", which is the contract. Adding a cheaper
	// omitted count is possible when a consumer needs it; accepted for now.
	Owners  []*QualityDigestOwnerMessage
	Targets []*QualityDigestTargetMessage
}

// qualityDigestOwnerLimit and qualityDigestTargetLimit bound how many owners and
// targets one digest names. A digest is a summary; the pages behind it list
// everything.
const (
	qualityDigestOwnerLimit  = 5
	qualityDigestTargetLimit = 5
)

// tryRecordQualityDigestSendQuery claims one digest period. The unique index on
// period_key is what makes the claim atomic: a second tick, a restart in the
// middle of the schedule hour or a catch-up pass cannot queue the same week
// twice, and DO NOTHING makes a repeat a cheap no-op instead of an error.
const tryRecordQualityDigestSendQuery = `
	INSERT INTO quality_digest_send (period_key, channel_count)
	VALUES ($1, $2)
	ON CONFLICT (period_key) DO NOTHING`

const qualityDigestSendRecordedQuery = `
	SELECT EXISTS (SELECT 1 FROM quality_digest_send WHERE period_key = $1)`

// qualityDigestTargetRuleCountsQuery counts the enabled checks and the targets
// that carry them. It uses the same "enabled and not deleted" predicate as the
// overview, so a digest and the dashboard cannot report different coverage.
const qualityDigestTargetRuleCountsQuery = `
	SELECT COUNT(DISTINCT target_guid), COUNT(*)
	FROM quality_rule
	WHERE state = $1 AND deleted = FALSE`

// qualityDigestExecutionCountsQuery tallies the period's completed executions by
// verdict. Only a completed execution is an observation: PENDING, RUNNING and
// ABANDONED rows are excluded so a batch in flight cannot inflate the numbers,
// and SKIPPED/NOT_EVALUATED rows are excluded on purpose because they observed
// no data. execution_count is consequently the sum of the four tallies.
// The period is keyed on started_at, the same axis the overview and the trend
// use, so the digest and the dashboard report the same week. That attribution has
// one asymmetry worth knowing: an execution that starts inside the period but is
// still running when the digest aggregates has no verdict yet, so it is absent
// here; the next digest starts at period end, so it is absent there too. It is
// attributed to the period it started in and only shows up once a later digest or
// the dashboard reads it after completion, so a boundary-crossing execution can be
// missing from both digests. Delaying aggregation by a settle window would trade
// that for a late digest and a different skew; accepted.
const qualityDigestExecutionCountsQuery = `
	SELECT status, COUNT(*)
	FROM quality_execution
	WHERE state = $1 AND started_at >= $2 AND started_at < $3
	  AND status IN ($4, $5, $6, $7)
	GROUP BY status`

// qualityDigestIssueMovementQuery counts the problems that appeared and those
// that resolved inside the period.
const qualityDigestIssueMovementQuery = `
	SELECT COUNT(*) FILTER (WHERE first_seen_at >= $1 AND first_seen_at < $2),
	       COUNT(*) FILTER (WHERE resolved_at >= $1 AND resolved_at < $2)
	FROM quality_issue`

// qualityDigestOpenIssueCountsQuery counts the unresolved issues at the period's
// end by severity. A resolution after the window's end does not remove the issue
// from it: the digest describes the end of the period, not the moment it runs.
const qualityDigestOpenIssueCountsQuery = `
	SELECT severity, COUNT(*)
	FROM quality_issue
	WHERE first_seen_at < $1 AND (resolved_at IS NULL OR resolved_at >= $1)
	GROUP BY severity`

// qualityDigestOwnerQuery splits the open state by rule owner. The LEFT JOIN is
// what produces the unassigned bucket: rules without an owner group under a NULL
// owner_id, which the scan turns into UserID 0 rather than dropping the issues
// from the digest. The cap is by open issues, the ones somebody should act on.
const qualityDigestOwnerQuery = `
	SELECT r.owner_id, COALESCE(p.name, ''), COUNT(*), COUNT(DISTINCT i.rule_id)
	FROM quality_issue i
	JOIN quality_rule r ON r.id = i.rule_id
	LEFT JOIN principal p ON p.id = r.owner_id
	WHERE i.first_seen_at < $1 AND (i.resolved_at IS NULL OR i.resolved_at >= $1)
	GROUP BY r.owner_id, p.name
	ORDER BY COUNT(*) DESC, r.owner_id NULLS LAST
	LIMIT $2`

// qualityDigestTargetQuery names the targets with the most failed checks in the
// period. The target path comes from the newest execution snapshot, and the
// open-issue count is joined per target rather than fetched per row; the
// aggregate only runs over the period's failed and errored executions, so the
// scan is bounded by what actually went wrong.
const qualityDigestTargetQuery = `
	SELECT e.target_guid,
	       e.target_type,
	       (array_agg(e.target_path ORDER BY e.started_at DESC))[1],
	       COUNT(*) FILTER (WHERE e.status = $1),
	       COUNT(*) FILTER (WHERE e.status = $2),
	       COALESCE(o.open_count, 0)
	FROM quality_execution e
	LEFT JOIN (
		SELECT target_guid, target_type, COUNT(*) AS open_count
		FROM quality_issue
		WHERE first_seen_at < $3 AND (resolved_at IS NULL OR resolved_at >= $3)
		GROUP BY target_guid, target_type
	) o ON o.target_guid = e.target_guid AND o.target_type = e.target_type
	WHERE e.state = $4 AND e.started_at >= $5 AND e.started_at < $6
	  AND e.status IN ($1, $2)
	GROUP BY e.target_guid, e.target_type, o.open_count
	ORDER BY COUNT(*) FILTER (WHERE e.status = $1) DESC,
	         COUNT(*) FILTER (WHERE e.status = $2) DESC,
	         e.target_guid
	LIMIT $7`

// TryRecordQualityDigestSend records one digest period as queued. It reports
// whether this call recorded it: false means the period was already there, so
// the caller's own pass was a repeat. channelCount is stored even when it is
// zero, because a week with no subscribed channel is still a finished period.
func (s *Store) TryRecordQualityDigestSend(ctx context.Context, periodKey string, channelCount int) (bool, error) {
	if strings.TrimSpace(periodKey) == "" {
		return false, common.Errorf(common.Invalid, "a digest period key is required")
	}
	result, err := s.GetDB().ExecContext(ctx, tryRecordQualityDigestSendQuery, periodKey, int32(channelCount))
	if err != nil {
		return false, errors.Wrap(err, "failed to record the quality digest period")
	}
	recorded, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to count the recorded quality digest periods")
	}
	return recorded > 0, nil
}

// QualityDigestSendRecorded reports whether one digest period was already
// queued. It is the read half of the period guard: the runner consults it before
// it aggregates anything, so a finished week costs one indexed probe.
func (s *Store) QualityDigestSendRecorded(ctx context.Context, periodKey string) (bool, error) {
	if strings.TrimSpace(periodKey) == "" {
		return false, common.Errorf(common.Invalid, "a digest period key is required")
	}
	var recorded bool
	if err := s.GetDB().QueryRowContext(ctx, qualityDigestSendRecordedQuery, periodKey).Scan(&recorded); err != nil {
		return false, errors.Wrap(err, "failed to read the quality digest period")
	}
	return recorded, nil
}

// QualityDigestSnapshot aggregates the digest period [since, until). The counts
// are independent indexed reads: a digest runs once a week, and each one answers
// a different question (coverage, verdicts, ledger movement, ownership).
func (s *Store) QualityDigestSnapshot(ctx context.Context, since, until time.Time) (*QualityDigestSnapshot, error) {
	if since.IsZero() || until.IsZero() {
		return nil, common.Errorf(common.Invalid, "a digest period requires a start and an end")
	}
	if !since.Before(until) {
		return nil, common.Errorf(common.Invalid, "the digest period start must be before its end")
	}

	snapshot := &QualityDigestSnapshot{}
	if err := s.qualityDigestCoverage(ctx, snapshot); err != nil {
		return nil, err
	}
	if err := s.qualityDigestExecutions(ctx, snapshot, since, until); err != nil {
		return nil, err
	}
	if err := s.qualityDigestIssues(ctx, snapshot, since, until); err != nil {
		return nil, err
	}
	if err := s.qualityDigestOwners(ctx, snapshot, until); err != nil {
		return nil, err
	}
	if err := s.qualityDigestTargets(ctx, snapshot, since, until); err != nil {
		return nil, err
	}
	return snapshot, nil
}

// qualityDigestCoverage fills the enabled check and target counts.
func (s *Store) qualityDigestCoverage(ctx context.Context, snapshot *QualityDigestSnapshot) error {
	scanErr := s.GetDB().QueryRowContext(ctx, qualityDigestTargetRuleCountsQuery,
		int16(storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED),
	).Scan(&snapshot.TargetCount, &snapshot.RuleCount)
	if scanErr != nil {
		return errors.Wrap(scanErr, "failed to count the quality digest coverage")
	}
	return nil
}

// qualityDigestExecutions fills the period's verdict tally.
func (s *Store) qualityDigestExecutions(ctx context.Context, snapshot *QualityDigestSnapshot, since, until time.Time) error {
	rows, err := s.GetDB().QueryContext(ctx, qualityDigestExecutionCountsQuery,
		int16(storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED), since, until,
		int16(storepb.QualityStatus_QUALITY_STATUS_PASS),
		int16(storepb.QualityStatus_QUALITY_STATUS_WARN),
		int16(storepb.QualityStatus_QUALITY_STATUS_FAIL),
		int16(storepb.QualityStatus_QUALITY_STATUS_ERROR),
	)
	if err != nil {
		return errors.Wrap(err, "failed to count the quality digest executions")
	}
	defer rows.Close()

	for rows.Next() {
		var (
			status int16
			count  int32
		)
		if err := rows.Scan(&status, &count); err != nil {
			return errors.Wrap(err, "failed to scan the quality digest executions")
		}
		addQualityDigestExecutionCount(snapshot, storepb.QualityStatus(status), count)
	}
	if err := rows.Err(); err != nil {
		return errors.Wrap(err, "failed to iterate the quality digest executions")
	}
	return nil
}

// addQualityDigestExecutionCount tallies one verdict. Only PASS, WARN, FAIL and
// ERROR count toward ExecutionCount and their own tally: SKIPPED and
// NOT_EVALUATED observed no data, so they are deliberately absent from the
// digest's headline number (the query does not even return them).
func addQualityDigestExecutionCount(snapshot *QualityDigestSnapshot, status storepb.QualityStatus, count int32) {
	switch status {
	case storepb.QualityStatus_QUALITY_STATUS_PASS:
		snapshot.PassedCount += count
		snapshot.ExecutionCount += count
	case storepb.QualityStatus_QUALITY_STATUS_WARN:
		snapshot.WarnedCount += count
		snapshot.ExecutionCount += count
	case storepb.QualityStatus_QUALITY_STATUS_FAIL:
		snapshot.FailedCount += count
		snapshot.ExecutionCount += count
	case storepb.QualityStatus_QUALITY_STATUS_ERROR:
		snapshot.ErroredCount += count
		snapshot.ExecutionCount += count
	case storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
		storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED:
		// Deliberately not counted: no data was observed.
	case storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED:
		// A completed execution always has a verdict; an out-of-band row with none
		// is not a verdict the digest reports.
	default:
	}
}

// qualityDigestIssues fills the ledger movement and the open state.
func (s *Store) qualityDigestIssues(ctx context.Context, snapshot *QualityDigestSnapshot, since, until time.Time) error {
	movementErr := s.GetDB().QueryRowContext(ctx, qualityDigestIssueMovementQuery, since, until).
		Scan(&snapshot.NewIssueCount, &snapshot.RecoveredIssueCount)
	if movementErr != nil {
		return errors.Wrap(movementErr, "failed to count the quality digest issue movement")
	}

	rows, err := s.GetDB().QueryContext(ctx, qualityDigestOpenIssueCountsQuery, until)
	if err != nil {
		return errors.Wrap(err, "failed to count the open quality issues")
	}
	defer rows.Close()

	for rows.Next() {
		var (
			severity int16
			count    int32
		)
		if err := rows.Scan(&severity, &count); err != nil {
			return errors.Wrap(err, "failed to scan the open quality issues")
		}
		snapshot.OpenIssueCount += count
		switch storepb.QualitySeverity(severity) {
		case storepb.QualitySeverity_QUALITY_SEVERITY_WARNING:
			snapshot.OpenWarningCount += count
		case storepb.QualitySeverity_QUALITY_SEVERITY_ERROR:
			snapshot.OpenErrorCount += count
		case storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL:
			snapshot.OpenCriticalCount += count
		case storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED:
			// Counted in the total only: an issue with no severity has no bucket
			// to show, but dropping it would understate the open count.
		default:
		}
	}
	if err := rows.Err(); err != nil {
		return errors.Wrap(err, "failed to iterate the open quality issues")
	}
	return nil
}

// qualityDigestOwners fills the owner breakdown, most open issues first.
func (s *Store) qualityDigestOwners(ctx context.Context, snapshot *QualityDigestSnapshot, until time.Time) error {
	rows, err := s.GetDB().QueryContext(ctx, qualityDigestOwnerQuery, until, qualityDigestOwnerLimit)
	if err != nil {
		return errors.Wrap(err, "failed to read the quality digest owners")
	}
	defer rows.Close()

	for rows.Next() {
		var (
			ownerID sql.NullInt32
			name    string
			open    int32
			failing int32
		)
		if err := rows.Scan(&ownerID, &name, &open, &failing); err != nil {
			return errors.Wrap(err, "failed to scan a quality digest owner")
		}
		snapshot.Owners = append(snapshot.Owners, qualityDigestOwner(ownerID, name, open, failing))
	}
	if err := rows.Err(); err != nil {
		return errors.Wrap(err, "failed to iterate the quality digest owners")
	}
	return nil
}

// qualityDigestTargets fills the failing targets, most failed checks first.
func (s *Store) qualityDigestTargets(ctx context.Context, snapshot *QualityDigestSnapshot, since, until time.Time) error {
	rows, err := s.GetDB().QueryContext(ctx, qualityDigestTargetQuery,
		int16(storepb.QualityStatus_QUALITY_STATUS_FAIL),
		int16(storepb.QualityStatus_QUALITY_STATUS_ERROR),
		until,
		int16(storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED),
		since,
		until,
		qualityDigestTargetLimit,
	)
	if err != nil {
		return errors.Wrap(err, "failed to read the quality digest targets")
	}
	defer rows.Close()

	for rows.Next() {
		var (
			target     QualityDigestTargetMessage
			targetType int16
			rawPath    []byte
			open       int32
		)
		scanErr := rows.Scan(&target.TargetGUID, &targetType, &rawPath,
			&target.FailedExecutionCount, &target.ErroredExecutionCount, &open)
		if scanErr != nil {
			return errors.Wrap(scanErr, "failed to scan a quality digest target")
		}
		target.TargetType = storepb.MetaType(targetType)
		target.OpenIssueCount = open
		target.DisplayPath = qualityDigestTargetDisplayPath(rawPath)
		snapshot.Targets = append(snapshot.Targets, &target)
	}
	if err := rows.Err(); err != nil {
		return errors.Wrap(err, "failed to iterate the quality digest targets")
	}
	return nil
}

// qualityDigestOwner maps one owner row. A NULL owner id is the unassigned
// bucket, which is a real group in the digest rather than a row to drop: the
// issues are still open and nobody has been asked to look at them.
func qualityDigestOwner(ownerID sql.NullInt32, name string, openIssueCount, failingRuleCount int32) *QualityDigestOwnerMessage {
	owner := &QualityDigestOwnerMessage{
		Name:             name,
		OpenIssueCount:   openIssueCount,
		FailingRuleCount: failingRuleCount,
	}
	if ownerID.Valid {
		owner.UserID = ownerID.Int32
	}
	return owner
}

// qualityDigestTargetDisplayPath renders a stored target path snapshot as
// instance/database/schema/table, skipping the containers an engine does not
// have. It is display text, not an identity.
func qualityDigestTargetDisplayPath(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	path := &storepb.QualityTargetPath{}
	if err := common.ProtojsonUnmarshaler.Unmarshal(raw, path); err != nil {
		return ""
	}
	var segments []string
	for _, segment := range []string{path.GetInstance(), path.GetDatabase(), path.GetSchema(), path.GetObject()} {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	return strings.Join(segments, "/")
}
