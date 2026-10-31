package store

import (
	"context"
	"strings"
	"testing"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The target directory's filter builder is unit-tested without a database: it
// carries the GUID-subtree scoping and the LIKE escaping, which are the parts a
// refactor can silently break.
func TestTargetWhere(t *testing.T) {
	t.Parallel()

	where, args, err := targetWhere(nil, 13)
	require.NoError(t, err)
	require.Equal(t, []string{"TRUE"}, where)
	require.Empty(t, args)

	// The path filters reuse the rule list's GUID prefix: an instance is the
	// first segment, and the deepest level set decides the prefix.
	where, args, err = targetWhere(&FindQualityTargetMessage{
		Instance: "instance-1",
		Database: "shop",
	}, 13)
	require.NoError(t, err)
	require.Contains(t, where, `(t.target_guid = $14 OR t.target_guid LIKE $15 ESCAPE E'\\')`)
	require.Equal(t, []any{"instance-1;shop", `instance-1;shop;%`}, args)

	// A table filter without its ancestors cannot form a GUID prefix.
	_, _, err = targetWhere(&FindQualityTargetMessage{TablePrefix: "orders"}, 13)
	require.Error(t, err)
}

// The search term is a substring over the target's path and GUID, with LIKE
// metacharacters escaped so a name containing "%" or "_" matches literally.
func TestTargetWhereSearchEscapesLikePattern(t *testing.T) {
	t.Parallel()

	search := "100%_done"
	where, args, err := targetWhere(&FindQualityTargetMessage{Search: &search}, 13)
	require.NoError(t, err)
	require.Len(t, args, 1)
	require.Equal(t, "%100\\%\\_done%", args[0])
	joined := strings.Join(where, " AND ")
	require.Contains(t, joined, "ILIKE $14 ESCAPE E'\\\\'")
	require.Contains(t, joined, "t.target_guid ILIKE $14")
}

// The attention filters bind no argument beyond the status rank; UNSPECIFIED
// keeps every target instead of binding rank zero.
func TestTargetWhereAttentionFilters(t *testing.T) {
	t.Parallel()

	where, args, err := targetWhere(&FindQualityTargetMessage{
		WorstStatus:     storepb.QualityStatus_QUALITY_STATUS_FAIL,
		HasOpenIssue:    true,
		HasPendingDraft: true,
	}, 13)
	require.NoError(t, err)
	require.Equal(t, []any{int32(5)}, args)
	require.Contains(t, where, "COALESCE(r.worst_rank, 0) = $14")
	require.Contains(t, where, "COALESCE(i.open_issue_count, 0) > 0")
	require.Contains(t, where, "COALESCE(d.pending_draft_count, 0) > 0")

	where, args, err = targetWhere(&FindQualityTargetMessage{
		WorstStatus: storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED,
	}, 0)
	require.NoError(t, err)
	require.Equal(t, []string{"TRUE"}, where)
	require.Empty(t, args)

	// The two weakest verdicts bind different ranks, so filtering one cannot match
	// a target whose latest result was the other.
	_, skippedArgs, err := targetWhere(&FindQualityTargetMessage{
		WorstStatus: storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
	}, 0)
	require.NoError(t, err)
	_, notEvaluatedArgs, err := targetWhere(&FindQualityTargetMessage{
		WorstStatus: storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
	}, 0)
	require.NoError(t, err)
	require.NotEqual(t, skippedArgs, notEvaluatedArgs)
	require.Equal(t, int32(1), skippedArgs[0])
	require.Equal(t, int32(2), notEvaluatedArgs[0])
}

// The worst-status ranking is what makes "ERROR outranks FAIL" true, and the
// inverse mapping is what puts the status back into the API response.
func TestQualityTargetStatusRankRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status storepb.QualityStatus
		rank   int32
	}{
		{storepb.QualityStatus_QUALITY_STATUS_ERROR, 6},
		{storepb.QualityStatus_QUALITY_STATUS_FAIL, 5},
		{storepb.QualityStatus_QUALITY_STATUS_WARN, 4},
		{storepb.QualityStatus_QUALITY_STATUS_PASS, 3},
		{storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, 2},
		{storepb.QualityStatus_QUALITY_STATUS_SKIPPED, 1},
		{storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, 0},
	}
	for _, tc := range cases {
		require.Equal(t, tc.rank, QualityTargetStatusRank(tc.status))
	}
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_ERROR, QualityTargetStatusFromRank(6))
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, QualityTargetStatusFromRank(5))
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_WARN, QualityTargetStatusFromRank(4))
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, QualityTargetStatusFromRank(3))
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, QualityTargetStatusFromRank(2))
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_SKIPPED, QualityTargetStatusFromRank(1))
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, QualityTargetStatusFromRank(0))
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, QualityTargetStatusFromRank(99))
	// NOT_EVALUATED and SKIPPED are distinct ranks: a directory filter must not
	// conflate "nothing to judge" with "did not run".
	require.NotEqual(t, QualityTargetStatusRank(storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED),
		QualityTargetStatusRank(storepb.QualityStatus_QUALITY_STATUS_SKIPPED))
}

// The aggregate's SQL CASE is generated from QualityTargetStatusRank, so the
// statuses it ranks cannot drift from the filter mapping. $8 is SKIPPED and $9 is
// NOT_EVALUATED, matching the argument order ListQualityTargets binds.
func TestQualityTargetWorstRankCaseMatchesTheRankFunction(t *testing.T) {
	t.Parallel()

	expr := qualityTargetWorstRankCase()
	require.Contains(t, expr, "WHEN $4 THEN 6", "ERROR")
	require.Contains(t, expr, "WHEN $5 THEN 5", "FAIL")
	require.Contains(t, expr, "WHEN $6 THEN 4", "WARN")
	require.Contains(t, expr, "WHEN $7 THEN 3", "PASS")
	require.Contains(t, expr, "WHEN $8 THEN 1", "SKIPPED")
	require.Contains(t, expr, "WHEN $9 THEN 2", "NOT_EVALUATED")
	require.Contains(t, expr, "ELSE 0")
}

// Every order ends with the GUID, so two rows that tie cannot swap between pages.
func TestQualityTargetOrderClauseIsStable(t *testing.T) {
	t.Parallel()

	for _, order := range []QualityTargetOrder{
		QualityTargetOrderAttention,
		QualityTargetOrderName,
		QualityTargetOrderRuleCount,
		QualityTargetOrderLastRun,
		QualityTargetOrder(0),
	} {
		require.Contains(t, qualityTargetOrderClause(order), "t.target_guid ASC", "order %d", order)
	}
	require.Contains(t, qualityTargetOrderClause(QualityTargetOrderAttention), "COALESCE(r.worst_rank, 0) DESC")
	require.Contains(t, qualityTargetOrderClause(QualityTargetOrderName), "COALESCE(t.target_path->>'object', '') ASC")
	require.Contains(t, qualityTargetOrderClause(QualityTargetOrderRuleCount), "t.rule_count DESC")
	require.Contains(t, qualityTargetOrderClause(QualityTargetOrderLastRun), "r.last_run_time DESC NULLS LAST")
	// An unknown order falls back to attention rather than returning no rows.
	require.Equal(t, qualityTargetOrderClause(QualityTargetOrderAttention), qualityTargetOrderClause(QualityTargetOrder(0)))
}

func TestCountPendingQualitySuggestionsRequiresTarget(t *testing.T) {
	t.Parallel()

	store := &Store{}
	_, _, err := store.CountPendingQualitySuggestions(context.Background(), "")
	require.Error(t, err)
}

// WorstStatuses is the multi-status form the failing-only export uses. It must
// bind both ranks in one ANY predicate; the single-status form must keep its
// exact-rank predicate and argument shape unchanged.
func TestTargetWhereWorstStatuses(t *testing.T) {
	t.Parallel()

	where, args, err := targetWhere(&FindQualityTargetMessage{
		WorstStatuses: []storepb.QualityStatus{
			storepb.QualityStatus_QUALITY_STATUS_FAIL,
			storepb.QualityStatus_QUALITY_STATUS_ERROR,
		},
	}, 13)
	require.NoError(t, err)
	require.Equal(t, []any{pq.Array([]int32{5, 6})}, args)
	require.Contains(t, where, "COALESCE(r.worst_rank, 0) = ANY($14::int[])")

	// The slice wins over a contradictory single status instead of ANDing two
	// predicates that cannot both hold.
	where, args, err = targetWhere(&FindQualityTargetMessage{
		WorstStatus: storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
		WorstStatuses: []storepb.QualityStatus{
			storepb.QualityStatus_QUALITY_STATUS_FAIL,
			storepb.QualityStatus_QUALITY_STATUS_ERROR,
		},
	}, 13)
	require.NoError(t, err)
	require.Equal(t, []any{pq.Array([]int32{5, 6})}, args)
	require.Contains(t, where, "COALESCE(r.worst_rank, 0) = ANY($14::int[])")

	where, args, err = targetWhere(&FindQualityTargetMessage{
		WorstStatus: storepb.QualityStatus_QUALITY_STATUS_FAIL,
	}, 13)
	require.NoError(t, err)
	require.Equal(t, []any{int32(5)}, args)
	require.Contains(t, where, "COALESCE(r.worst_rank, 0) = $14")
}
