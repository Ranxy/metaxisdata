package store

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualityIssueScannerStub stands in for a *sql.Row. The column count check is the
// guard that qualityIssueColumns and scanQualityIssue stay in the same order and
// the same length: a schema change that adds a column without touching the scan
// fails here instead of mis-assigning values at runtime.
type qualityIssueScannerStub struct {
	id               int64
	resourceID       string
	ruleID           int64
	targetGUID       string
	targetType       int16
	status           int16
	severity         int16
	firstSeenRunID   sql.NullInt64
	lastSeenRunID    sql.NullInt64
	firstSeenAt      time.Time
	lastSeenAt       time.Time
	resolvedAt       sql.NullTime
	resolutionReason sql.NullInt32
	resolutionNote   string
	acknowledgedBy   sql.NullInt32
	acknowledgedAt   sql.NullTime
	mutedUntil       sql.NullTime
	occurrenceCount  int32
	updatedAt        time.Time
}

func (s qualityIssueScannerStub) Scan(dest ...any) error {
	if len(dest) != 19 {
		return fmt.Errorf("scanQualityIssue scanned %d destinations, want 19", len(dest))
	}
	*(dest[0].(*int64)) = s.id
	*(dest[1].(*string)) = s.resourceID
	*(dest[2].(*int64)) = s.ruleID
	*(dest[3].(*string)) = s.targetGUID
	*(dest[4].(*int16)) = s.targetType
	*(dest[5].(*int16)) = s.status
	*(dest[6].(*int16)) = s.severity
	*(dest[7].(*sql.NullInt64)) = s.firstSeenRunID
	*(dest[8].(*sql.NullInt64)) = s.lastSeenRunID
	*(dest[9].(*time.Time)) = s.firstSeenAt
	*(dest[10].(*time.Time)) = s.lastSeenAt
	*(dest[11].(*sql.NullTime)) = s.resolvedAt
	*(dest[12].(*sql.NullInt32)) = s.resolutionReason
	*(dest[13].(*string)) = s.resolutionNote
	*(dest[14].(*sql.NullInt32)) = s.acknowledgedBy
	*(dest[15].(*sql.NullTime)) = s.acknowledgedAt
	*(dest[16].(*sql.NullTime)) = s.mutedUntil
	*(dest[17].(*int32)) = s.occurrenceCount
	*(dest[18].(*time.Time)) = s.updatedAt
	return nil
}

// The DDL hard-codes the state machine as INT2 values ("1 OPEN, 2 ACKNOWLEDGED,
// 3 RESOLVED" and the partial unique index's "<> 3"), and the resolution reasons
// are stored as numbers. Renumbering a proto enum would silently change what every
// row means, so the values the table was written against are pinned here.
func TestQualityIssueEnumsMatchTheDDLComments(t *testing.T) {
	t.Parallel()

	require.Equal(t, storepb.QualityIssueStatus(1), storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN)
	require.Equal(t, storepb.QualityIssueStatus(2), storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_ACKNOWLEDGED)
	require.Equal(t, storepb.QualityIssueStatus(3), storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED)

	require.Equal(t, storepb.QualityResolutionReason(1), storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_FIXED)
	require.Equal(t, storepb.QualityResolutionReason(2), storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_EXPECTED)
	require.Equal(t, storepb.QualityResolutionReason(3), storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_NO_ACTION_NEEDED)
	require.Equal(t, storepb.QualityResolutionReason(4), storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_DUPLICATE)
	require.Equal(t, storepb.QualityResolutionReason(5), storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_AUTO_RECOVERED)

	require.Equal(t, storepb.QualitySeverity(1), storepb.QualitySeverity_QUALITY_SEVERITY_WARNING)
	require.Equal(t, storepb.QualitySeverity(2), storepb.QualitySeverity_QUALITY_SEVERITY_ERROR)
	require.Equal(t, storepb.QualitySeverity(3), storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL)
}

// A NULL timestamp is not the zero time: resolved_at, acknowledged_at and
// muted_until have to read back as nil, and a NULL resolution_reason has to mean
// UNSPECIFIED rather than a reason value 0 that the UI would render as a name.
func TestScanQualityIssueReadsNullColumnsAsNil(t *testing.T) {
	t.Parallel()

	msg, err := scanQualityIssue(qualityIssueScannerStub{
		targetType: int16(storepb.MetaType_TABLE),
		status:     int16(storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN),
		severity:   int16(storepb.QualitySeverity_QUALITY_SEVERITY_WARNING),
	})
	require.NoError(t, err)

	require.Nil(t, msg.FirstSeenRunID)
	require.Nil(t, msg.LastSeenRunID)
	require.Nil(t, msg.ResolvedAt)
	require.Nil(t, msg.AcknowledgedBy)
	require.Nil(t, msg.AcknowledgedAt)
	require.Nil(t, msg.MutedUntil)
	require.Equal(t, storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_UNSPECIFIED, msg.ResolutionReason)
	require.Equal(t, storepb.MetaType_TABLE, msg.TargetType)
}

func TestScanQualityIssueReadsEveryColumn(t *testing.T) {
	t.Parallel()

	firstSeen := time.Date(2026, time.October, 19, 9, 0, 0, 0, time.UTC)
	lastSeen := firstSeen.Add(2 * time.Hour)
	resolvedAt := lastSeen.Add(time.Hour)

	msg, err := scanQualityIssue(qualityIssueScannerStub{
		id:               7,
		resourceID:       "abc123",
		ruleID:           42,
		targetGUID:       "inst;db;;orders",
		targetType:       int16(storepb.MetaType_TABLE),
		status:           int16(storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_ACKNOWLEDGED),
		severity:         int16(storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL),
		firstSeenRunID:   sql.NullInt64{Int64: 100, Valid: true},
		lastSeenRunID:    sql.NullInt64{Int64: 205, Valid: true},
		firstSeenAt:      firstSeen,
		lastSeenAt:       lastSeen,
		resolvedAt:       sql.NullTime{Time: resolvedAt, Valid: true},
		resolutionReason: sql.NullInt32{Int32: int32(storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_FIXED), Valid: true},
		resolutionNote:   "backfilled the missing rows",
		acknowledgedBy:   sql.NullInt32{Int32: 9, Valid: true},
		acknowledgedAt:   sql.NullTime{Time: lastSeen, Valid: true},
		mutedUntil:       sql.NullTime{Time: lastSeen.Add(24 * time.Hour), Valid: true},
		occurrenceCount:  3,
		updatedAt:        resolvedAt,
	})
	require.NoError(t, err)

	require.Equal(t, int64(7), msg.ID)
	require.Equal(t, "abc123", msg.ResourceID)
	require.Equal(t, int64(42), msg.RuleID)
	require.Equal(t, "inst;db;;orders", msg.TargetGUID)
	require.Equal(t, storepb.MetaType_TABLE, msg.TargetType)
	require.Equal(t, storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_ACKNOWLEDGED, msg.Status)
	require.Equal(t, storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL, msg.Severity)
	require.Equal(t, int64(100), *msg.FirstSeenRunID)
	require.Equal(t, int64(205), *msg.LastSeenRunID)
	require.Equal(t, firstSeen, msg.FirstSeenAt)
	require.Equal(t, lastSeen, msg.LastSeenAt)
	require.Equal(t, resolvedAt, *msg.ResolvedAt)
	require.Equal(t, storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_FIXED, msg.ResolutionReason)
	require.Equal(t, "backfilled the missing rows", msg.ResolutionNote)
	require.Equal(t, int32(9), *msg.AcknowledgedBy)
	require.Equal(t, lastSeen, *msg.AcknowledgedAt)
	require.Equal(t, lastSeen.Add(24*time.Hour), *msg.MutedUntil)
	require.Equal(t, int32(3), msg.OccurrenceCount)
	require.Equal(t, resolvedAt, msg.UpdatedAt)
}

func TestBuildQualityIssueWhereAppliesEveryFilter(t *testing.T) {
	t.Parallel()

	where, args := buildQualityIssueWhere(nil)
	require.Equal(t, []string{"TRUE"}, where)
	require.Empty(t, args)

	startAt := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	endAt := startAt.Add(7 * 24 * time.Hour)
	where, args = buildQualityIssueWhere(&FindQualityIssueMessage{
		ResourceID: ptr("  abc123 "),
		RuleID:     ptr(int64(42)),
		StartAt:    &startAt,
		EndAt:      &endAt,
	})
	require.Equal(t, []string{
		"TRUE",
		"resource_id = $1",
		"rule_id = $2",
		"first_seen_at >= $3",
		"first_seen_at <= $4",
	}, where)
	require.Equal(t, []any{"abc123", int64(42), startAt, endAt}, args)
}

// OpenOnly and Statuses are both predicates on status, and they are ANDed: the
// "open" view must not silently widen to resolved rows when a caller also passes a
// status set, and the store must not build an OR that would.
func TestBuildQualityIssueWhereOpenOnlyIsAdditive(t *testing.T) {
	t.Parallel()

	where, args := buildQualityIssueWhere(&FindQualityIssueMessage{
		Statuses: []storepb.QualityIssueStatus{
			storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN,
			storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED,
		},
		OpenOnly:   true,
		Severities: []storepb.QualitySeverity{storepb.QualitySeverity_QUALITY_SEVERITY_ERROR},
	})
	require.Len(t, where, 4)
	require.Equal(t, "TRUE", where[0])
	require.Equal(t, "status = ANY($1::int2[])", where[1])
	require.Equal(t, "status <> $2", where[2])
	require.Equal(t, "severity = ANY($3::int2[])", where[3])

	statuses, ok := args[0].(*pq.Int32Array)
	require.True(t, ok)
	require.Equal(t, pq.Int32Array{1, 3}, *statuses)
	require.Equal(t, int16(storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED), args[1])
	severities, ok := args[2].(*pq.Int32Array)
	require.True(t, ok)
	require.Equal(t, pq.Int32Array{2}, *severities)
}

// A target filter is a GUID subtree, so an instance or database prefix selects every
// issue under it, and LIKE metacharacters in the names have to be escaped rather than
// act as wildcards.
func TestBuildQualityIssueWhereTargetGUIDIsASubtree(t *testing.T) {
	t.Parallel()

	where, args := buildQualityIssueWhere(&FindQualityIssueMessage{TargetGUID: ptr(`inst;db_1%prod`)})
	require.Equal(t, []string{"TRUE", `(target_guid = $1 OR target_guid LIKE $2 ESCAPE E'\\')`}, where)
	require.Equal(t, []any{`inst;db_1%prod`, `inst;db\_1\%prod;%`}, args)

	// An empty prefix would compile to "guid = '' OR guid LIKE ';%'", which matches
	// nothing; the filter is skipped so an unfilled field does not empty the list.
	where, args = buildQualityIssueWhere(&FindQualityIssueMessage{TargetGUID: ptr("  ")})
	require.Equal(t, []string{"TRUE"}, where)
	require.Empty(t, args)
}

// The list is ordered by when the problem appeared, not by updated_at: touch,
// acknowledge and resolve bump updated_at, so ordering by it would reshuffle the
// list every time somebody touched a long-standing issue.
func TestBuildListQualityIssuesQueryKeepsFirstSeenOrder(t *testing.T) {
	t.Parallel()

	query, args := buildListQualityIssuesQuery(&FindQualityIssueMessage{
		RuleID: ptr(int64(42)),
		Limit:  ptr(25),
		Offset: ptr(50),
	})
	require.Contains(t, query, `FROM quality_issue`)
	require.Contains(t, query, `ORDER BY first_seen_at DESC, id DESC`)
	require.Contains(t, query, "LIMIT $2")
	require.Contains(t, query, "OFFSET $3")
	require.Equal(t, []any{int64(42), 25, 50}, args)

	// A nil filter lists everything; negative pagination is clamped rather than
	// emitted as a negative LIMIT.
	query, args = buildListQualityIssuesQuery(nil)
	require.Equal(t, `SELECT `+qualityIssueColumns+` FROM quality_issue WHERE TRUE ORDER BY first_seen_at DESC, id DESC`, query)
	require.Empty(t, args)
}

// RESOLVED is terminal. Reopening it would let a rule accumulate two unresolved
// issues and race the partial unique index, so the transition is refused before any
// SQL is built.
func TestQualityIssueStatusChangeIsTerminalAtResolved(t *testing.T) {
	t.Parallel()

	resolved := storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED
	for _, target := range []storepb.QualityIssueStatus{
		storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN,
		storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_ACKNOWLEDGED,
		storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED,
	} {
		err := qualityIssueStatusChangeError(resolved, target)
		require.Error(t, err)
		require.Equal(t, common.Conflict, common.ErrorCode(err))
	}

	open := storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN
	acknowledged := storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_ACKNOWLEDGED
	require.NoError(t, qualityIssueStatusChangeError(open, open))
	require.NoError(t, qualityIssueStatusChangeError(open, acknowledged))
	require.NoError(t, qualityIssueStatusChangeError(open, resolved))
	require.NoError(t, qualityIssueStatusChangeError(acknowledged, open))

	// UNSPECIFIED and unknown numbers are not transitions: a row always carries a
	// real status.
	for _, target := range []storepb.QualityIssueStatus{
		storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_UNSPECIFIED,
		storepb.QualityIssueStatus(9),
	} {
		err := qualityIssueStatusChangeError(open, target)
		require.Error(t, err)
		require.Equal(t, common.Invalid, common.ErrorCode(err))
	}
}

// The issue state machine is written against real severities. A producer that
// forgot to copy the rule's severity must not store "unspecified" and render as a
// problem with no ranking.
func TestQualityIssueSeverityMustBeRanked(t *testing.T) {
	t.Parallel()

	require.True(t, qualityIssueSeverityValid(storepb.QualitySeverity_QUALITY_SEVERITY_WARNING))
	require.True(t, qualityIssueSeverityValid(storepb.QualitySeverity_QUALITY_SEVERITY_ERROR))
	require.True(t, qualityIssueSeverityValid(storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL))
	require.False(t, qualityIssueSeverityValid(storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED))
	require.False(t, qualityIssueSeverityValid(storepb.QualitySeverity(9)))
}

func TestBuildQualityIssueUpdateSetsLeavesNilFieldsUntouched(t *testing.T) {
	t.Parallel()

	current := &QualityIssueMessage{Status: storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN}
	sets, args, err := buildQualityIssueUpdateSets(current, &UpdateQualityIssueMessage{
		ResolutionNote: ptr("  known bad data  "),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"updated_at = NOW()", "resolution_note = $1"}, sets)
	require.Equal(t, []any{"known bad data"}, args)

	// An explicit zero time is the only way a field mask can clear a nullable
	// column, and UNSPECIFIED clears the reason rather than storing enum 0.
	sets, args, err = buildQualityIssueUpdateSets(current, &UpdateQualityIssueMessage{
		ResolutionReason: ptr(storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_UNSPECIFIED),
		MutedUntil:       ptr(time.Time{}),
		AcknowledgedAt:   ptr(time.Time{}),
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		"updated_at = NOW()",
		"resolution_reason = NULL",
		"muted_until = NULL",
		"acknowledged_at = NULL",
	}, sets)
	require.Empty(t, args)

	// Resolving through the management path stamps resolved_at once: a second update
	// that sets RESOLVED must not move it.
	sets, _, err = buildQualityIssueUpdateSets(current, &UpdateQualityIssueMessage{
		Status: ptr(storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED),
	})
	require.NoError(t, err)
	require.Contains(t, sets, "status = $1")
	require.Contains(t, sets, "resolved_at = COALESCE(resolved_at, NOW())")

	// A resolved issue rejects a status change, so no SET list is built at all.
	_, _, err = buildQualityIssueUpdateSets(&QualityIssueMessage{
		Status: storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED,
	}, &UpdateQualityIssueMessage{Status: ptr(storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN)})
	require.Error(t, err)
	require.Equal(t, common.Conflict, common.ErrorCode(err))

	_, _, err = buildQualityIssueUpdateSets(current, nil)
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

// The open insert and the transitions encode the invariants in SQL. The partial
// unique index is what makes "at most one unresolved issue per rule" true under
// concurrency, so the insert must rely on it (ON CONFLICT DO NOTHING) instead of
// overwriting an existing issue; the transition statements must refuse to touch a
// resolved row.
func TestQualityIssueStatementShapesCarryTheirInvariants(t *testing.T) {
	t.Parallel()

	insertPart := strings.SplitN(qualityIssueOpenStatement, "RETURNING", 2)[0]
	require.Contains(t, insertPart, "INSERT INTO quality_issue")
	require.Contains(t, insertPart, "occurrence_count")
	require.Contains(t, insertPart, "first_seen_run_id")
	require.Contains(t, insertPart, "last_seen_run_id")
	require.NotContains(t, insertPart, "resolved_at")
	require.NotContains(t, insertPart, "acknowledged")
	require.Contains(t, qualityIssueOpenStatement, "ON CONFLICT DO NOTHING")
	require.NotContains(t, qualityIssueOpenStatement, "DO UPDATE")

	require.Contains(t, qualityIssueTouchStatement, "last_seen_at = $1")
	require.Contains(t, qualityIssueTouchStatement, "last_seen_run_id = $2")
	require.Contains(t, qualityIssueTouchStatement, "severity = $3")
	require.Contains(t, qualityIssueTouchStatement, "occurrence_count = occurrence_count + 1")
	require.Contains(t, qualityIssueTouchStatement, "WHERE id = $4 AND status <> $5")
	require.NotContains(t, qualityIssueTouchStatement, "first_seen_at")

	require.Contains(t, qualityIssueResolveStatement, "SET status = $1")
	require.Contains(t, qualityIssueResolveStatement, "resolved_at = $2")
	require.Contains(t, qualityIssueResolveStatement, "resolution_reason = $3")
	require.Contains(t, qualityIssueResolveStatement, "resolution_note = $4")
	require.Contains(t, qualityIssueResolveStatement, "WHERE id = $5 AND status <> $1")

	require.Contains(t, qualityIssueAcknowledgeStatement, "SET status = $1")
	require.Contains(t, qualityIssueAcknowledgeStatement, "acknowledged_by = $2")
	require.Contains(t, qualityIssueAcknowledgeStatement, "acknowledged_at = $3")
	require.Contains(t, qualityIssueAcknowledgeStatement, "WHERE id = $4 AND status <> $1")

	// The management update decides against the locked row, so the read has to lock
	// it: a read without FOR UPDATE would let a concurrent resolve be overwritten.
	require.Contains(t, qualityIssueLockStatement, "FROM quality_issue")
	require.Contains(t, qualityIssueLockStatement, "WHERE resource_id = $1")
	require.Contains(t, qualityIssueLockStatement, "FOR UPDATE")
}

// The projection and the scan are one contract: this pins the order of every column
// scanQualityIssue consumes.
func TestQualityIssueColumnsMatchTheScan(t *testing.T) {
	t.Parallel()

	columns := strings.Split(qualityIssueColumns, ", ")
	require.Equal(t, []string{
		"id",
		"resource_id",
		"rule_id",
		"target_guid",
		"target_type",
		"status",
		"severity",
		"first_seen_run_id",
		"last_seen_run_id",
		"first_seen_at",
		"last_seen_at",
		"resolved_at",
		"resolution_reason",
		"resolution_note",
		"acknowledged_by",
		"acknowledged_at",
		"muted_until",
		"occurrence_count",
		"updated_at",
	}, columns)
	require.NotContains(t, qualityIssueColumns, "*")
}

// Input validation happens before any database access, so a nil Store is enough to
// prove the bad request is refused instead of reaching the driver.
func TestQualityIssueValidationRunsBeforeDatabaseAccess(t *testing.T) {
	t.Parallel()

	var s *Store
	ctx := t.Context()

	_, err := s.OpenQualityIssue(ctx, nil)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	for _, issue := range []*QualityIssueMessage{
		{TargetGUID: "inst;db;;orders", TargetType: storepb.MetaType_TABLE, Severity: storepb.QualitySeverity_QUALITY_SEVERITY_WARNING},
		{RuleID: 1, TargetType: storepb.MetaType_TABLE, Severity: storepb.QualitySeverity_QUALITY_SEVERITY_WARNING},
		{RuleID: 1, TargetGUID: "inst;db;;orders", Severity: storepb.QualitySeverity_QUALITY_SEVERITY_WARNING},
		{RuleID: 1, TargetGUID: "inst;db;;orders", TargetType: storepb.MetaType_TABLE},
	} {
		_, err := s.OpenQualityIssue(ctx, issue)
		require.Error(t, err)
		require.Equal(t, common.Invalid, common.ErrorCode(err), "%+v", issue)
	}

	require.Equal(t, common.Invalid, common.ErrorCode(s.TouchQualityIssue(ctx, 0, 1, storepb.QualitySeverity_QUALITY_SEVERITY_ERROR, time.Now())))
	require.Equal(t, common.Invalid, common.ErrorCode(s.TouchQualityIssue(ctx, 1, 0, storepb.QualitySeverity_QUALITY_SEVERITY_ERROR, time.Now())))
	require.Equal(t, common.Invalid, common.ErrorCode(s.TouchQualityIssue(ctx, 1, 1, storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED, time.Now())))

	require.Equal(t, common.Invalid, common.ErrorCode(s.ResolveQualityIssue(ctx, 0, storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_FIXED, "", time.Now())))
	require.Equal(t, common.Invalid, common.ErrorCode(s.AcknowledgeQualityIssue(ctx, 0, 1, time.Now())))
	require.Equal(t, common.Invalid, common.ErrorCode(s.AcknowledgeQualityIssue(ctx, 1, 0, time.Now())))

	_, err = s.UpdateQualityIssue(ctx, "  ", &UpdateQualityIssueMessage{})
	require.Equal(t, common.Invalid, common.ErrorCode(err))
	_, err = s.UpdateQualityIssue(ctx, "abc123", nil)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
	_, err = s.GetOpenQualityIssueForRule(ctx, 0)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}
