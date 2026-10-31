package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The period guard's shape is the invariant: one row per period, and a repeat is
// a no-op rather than an error, so a second tick or a catch-up pass cannot queue
// the same week twice.
func TestTryRecordQualityDigestSendQuery(t *testing.T) {
	t.Parallel()

	require.Contains(t, tryRecordQualityDigestSendQuery, "INSERT INTO quality_digest_send (period_key, channel_count)")
	require.Contains(t, tryRecordQualityDigestSendQuery, "ON CONFLICT (period_key) DO NOTHING")
	require.Contains(t, qualityDigestSendRecordedQuery, "FROM quality_digest_send WHERE period_key = $1")
}

// A digest reads the same "enabled and not deleted" coverage the overview does,
// and tallies only completed executions: a batch in flight is not an observation.
func TestQualityDigestCountQueries(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityDigestTargetRuleCountsQuery, "COUNT(DISTINCT target_guid), COUNT(*)")
	require.Contains(t, qualityDigestTargetRuleCountsQuery, "WHERE state = $1 AND deleted = FALSE")

	require.Contains(t, qualityDigestExecutionCountsQuery, "WHERE state = $1 AND started_at >= $2 AND started_at < $3")
	require.Contains(t, qualityDigestExecutionCountsQuery, "AND status IN ($4, $5, $6, $7)")
	require.Contains(t, qualityDigestExecutionCountsQuery, "GROUP BY status")
	require.NotContains(t, qualityDigestExecutionCountsQuery, "ABANDONED")
	require.NotContains(t, qualityDigestExecutionCountsQuery, "SKIPPED",
		"a skipped execution observed no data and is not a digest verdict")

	// New and recovered problems are both bounded by the period, so an issue that
	// resolved after it belongs to neither.
	require.Contains(t, qualityDigestIssueMovementQuery, "first_seen_at >= $1 AND first_seen_at < $2")
	require.Contains(t, qualityDigestIssueMovementQuery, "resolved_at >= $1 AND resolved_at < $2")

	// The open state is the state at the period's end, not at the moment the
	// digest runs: an issue resolved later still existed when the week closed.
	require.Contains(t, qualityDigestOpenIssueCountsQuery, "first_seen_at < $1 AND (resolved_at IS NULL OR resolved_at >= $1)")
	require.Contains(t, qualityDigestOpenIssueCountsQuery, "GROUP BY severity")
}

// The owner breakdown must keep rules without an owner: their issues are open and
// nobody has been asked to look at them, so they form the unassigned bucket
// rather than being filtered out. The cap is by open issues.
func TestQualityDigestOwnerQuery(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityDigestOwnerQuery, "LEFT JOIN principal p ON p.id = r.owner_id")
	require.Contains(t, qualityDigestOwnerQuery, "COALESCE(p.name, '')")
	require.Contains(t, qualityDigestOwnerQuery, "GROUP BY r.owner_id, p.name")
	require.Contains(t, qualityDigestOwnerQuery, "first_seen_at < $1 AND (i.resolved_at IS NULL OR i.resolved_at >= $1)")
	require.Contains(t, qualityDigestOwnerQuery, "ORDER BY COUNT(*) DESC, r.owner_id NULLS LAST")
	require.Contains(t, qualityDigestOwnerQuery, "LIMIT $2")
	require.NotContains(t, qualityDigestOwnerQuery, "owner_id IS NOT NULL",
		"the unassigned bucket must survive the query")
	require.NotContains(t, qualityDigestOwnerQuery, "deleted",
		"an issue's rule being soft-deleted does not close its problem")
	require.Equal(t, 5, qualityDigestOwnerLimit)
}

// The target breakdown is ordered by failed checks and capped, and the open
// issue count is joined per target rather than fetched per row.
func TestQualityDigestTargetQuery(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityDigestTargetQuery, "LEFT JOIN (")
	require.Contains(t, qualityDigestTargetQuery, "COALESCE(o.open_count, 0)")
	require.Contains(t, qualityDigestTargetQuery, "COUNT(*) FILTER (WHERE e.status = $1)")
	require.Contains(t, qualityDigestTargetQuery, "COUNT(*) FILTER (WHERE e.status = $2)")
	require.Contains(t, qualityDigestTargetQuery, "e.status IN ($1, $2)")
	require.Contains(t, qualityDigestTargetQuery, "array_agg(e.target_path ORDER BY e.started_at DESC)")
	require.Contains(t, qualityDigestTargetQuery, "ORDER BY COUNT(*) FILTER (WHERE e.status = $1) DESC")
	require.Contains(t, qualityDigestTargetQuery, "LIMIT $7")
	require.Equal(t, 5, qualityDigestTargetLimit)
}

// execution_count is the sum of the four verdicts: a SKIPPED or NOT_EVALUATED
// execution observed no data, so it neither gets a tally of its own nor moves the
// headline number the tallies explain.
func TestAddQualityDigestExecutionCount(t *testing.T) {
	t.Parallel()

	snapshot := &QualityDigestSnapshot{}
	addQualityDigestExecutionCount(snapshot, storepb.QualityStatus_QUALITY_STATUS_PASS, 2)
	addQualityDigestExecutionCount(snapshot, storepb.QualityStatus_QUALITY_STATUS_WARN, 1)
	addQualityDigestExecutionCount(snapshot, storepb.QualityStatus_QUALITY_STATUS_FAIL, 3)
	addQualityDigestExecutionCount(snapshot, storepb.QualityStatus_QUALITY_STATUS_ERROR, 4)
	addQualityDigestExecutionCount(snapshot, storepb.QualityStatus_QUALITY_STATUS_SKIPPED, 5)
	addQualityDigestExecutionCount(snapshot, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, 6)
	addQualityDigestExecutionCount(snapshot, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, 7)

	require.Equal(t, int32(2), snapshot.PassedCount)
	require.Equal(t, int32(1), snapshot.WarnedCount)
	require.Equal(t, int32(3), snapshot.FailedCount)
	require.Equal(t, int32(4), snapshot.ErroredCount)
	require.Equal(t, int32(10), snapshot.ExecutionCount, "the four verdicts, not every completed row")
	require.Equal(t,
		snapshot.ExecutionCount,
		snapshot.PassedCount+snapshot.WarnedCount+snapshot.FailedCount+snapshot.ErroredCount)
}

// A NULL owner id is the unassigned bucket, not a row to skip, and its name and
// counts survive the mapping unchanged.
func TestQualityDigestOwner(t *testing.T) {
	t.Parallel()

	unassigned := qualityDigestOwner(sql.NullInt32{}, "", 4, 2)
	require.Equal(t, int32(0), unassigned.UserID, "0 is the unassigned bucket")
	require.Empty(t, unassigned.Name)
	require.Equal(t, int32(4), unassigned.OpenIssueCount)
	require.Equal(t, int32(2), unassigned.FailingRuleCount)

	owned := qualityDigestOwner(sql.NullInt32{Int32: 7, Valid: true}, "Alice", 3, 3)
	require.Equal(t, int32(7), owned.UserID)
	require.Equal(t, "Alice", owned.Name)
	require.Equal(t, int32(3), owned.OpenIssueCount)
	require.Equal(t, int32(3), owned.FailingRuleCount)
}

func TestQualityDigestTargetDisplayPath(t *testing.T) {
	t.Parallel()

	require.Equal(t, "prod/shop/public/orders", qualityDigestTargetDisplayPath([]byte(
		`{"instance":"prod","database":"shop","schema":"public","object":"orders"}`)))
	// An engine without a schema segment skips it, and a missing snapshot is not
	// an error: it renders as no path rather than failing the whole digest.
	require.Equal(t, "prod/shop/orders", qualityDigestTargetDisplayPath([]byte(
		`{"instance":"prod","database":"shop","object":"orders"}`)))
	require.Empty(t, qualityDigestTargetDisplayPath(nil))
	require.Empty(t, qualityDigestTargetDisplayPath([]byte(`not-json`)))
}

// A digest with no period, or an inverted one, is a caller bug: refuse it before
// any query runs.
func TestQualityDigestSnapshotValidatesThePeriod(t *testing.T) {
	t.Parallel()

	stores := &Store{}
	_, err := stores.QualityDigestSnapshot(context.Background(), time.Time{}, time.Now())
	require.Error(t, err)
	_, err = stores.QualityDigestSnapshot(context.Background(), time.Now(), time.Time{})
	require.Error(t, err)
	_, err = stores.QualityDigestSnapshot(context.Background(), time.Now(), time.Now().Add(-time.Hour))
	require.Error(t, err)

	_, err = stores.TryRecordQualityDigestSend(context.Background(), "", 0)
	require.Error(t, err)
	_, err = stores.QualityDigestSendRecorded(context.Background(), "  ")
	require.Error(t, err)
}
