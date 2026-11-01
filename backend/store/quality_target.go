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

// QualityTargetOrder is how ListQualityTargets orders its rows.
type QualityTargetOrder int

const (
	// QualityTargetOrderAttention puts the targets that need attention first: the
	// worst latest result, then open issues, then pending drafts.
	QualityTargetOrderAttention QualityTargetOrder = iota + 1
	// QualityTargetOrderName orders by the target's object name.
	QualityTargetOrderName
	// QualityTargetOrderRuleCount orders by the number of rules, most first.
	QualityTargetOrderRuleCount
	// QualityTargetOrderLastRun orders by the most recent run, newest first.
	QualityTargetOrderLastRun
)

// FindQualityTargetMessage is the query filter for the target directory. The
// four path fields are GUID prefixes, the same shape as FindQualityRuleMessage:
// a target matches when its GUID starts at or under the prefix.
type FindQualityTargetMessage struct {
	Instance    string
	Database    string
	Schema      string
	TablePrefix string
	// Search matches the target's path or GUID as a case-insensitive substring.
	Search *string
	// WorstStatus keeps only targets whose worst latest result is this status.
	// UNSPECIFIED keeps every target.
	WorstStatus storepb.QualityStatus
	// WorstStatuses keeps only targets whose worst latest result is any of these
	// statuses. It is the multi-status form of WorstStatus: when non-empty it
	// replaces the single-status predicate, so a caller can express "failing"
	// (FAIL or ERROR) in one query.
	WorstStatuses []storepb.QualityStatus
	// HasOpenIssue keeps only targets with at least one unresolved issue.
	HasOpenIssue bool
	// HasPendingDraft keeps only targets with at least one pending AI draft.
	HasPendingDraft bool
	// TargetKeys keeps only these exact targets. It is an exact selection, not a
	// prefix filter, and it is how the metadata browser reads one page's trust
	// signals in a single call. The aggregate query binds it into the rule scan,
	// the per-rule execution probe and the issue/draft tallies, so a selection
	// costs its own targets rather than the workspace's whole history. An empty
	// slice means no selection.
	TargetKeys []MetaGUIDKey
	Order      QualityTargetOrder
	Limit      *int
	Offset     *int
}

// QualityTargetMessage is one row of the target directory: the target identity
// plus the aggregates the directory and the workspace header render.
type QualityTargetMessage struct {
	GUID string
	Type storepb.MetaType
	// Path is the snapshot stored with the rules; nil when the rules never
	// recorded one.
	Path *storepb.QualityTargetPath

	RuleCount    int32
	EnabledCount int32
	PausedCount  int32
	// ColumnCount is the number of distinct columns a column rule names.
	ColumnCount int32

	OpenIssueCount    int32
	PendingDraftCount int32
	PendingLabelCount int32

	// WorstStatus is the worst latest result across the target's rules;
	// UNSPECIFIED when none of them has run.
	WorstStatus storepb.QualityStatus
	LastRunTime *time.Time
	// SchemaChanged is true when at least one rule's saved target hash differs
	// from the current registry schema hash.
	SchemaChanged bool
	// Orphaned is true when the target no longer resolves. It is filled by the
	// API, which owns the registry read, not by the aggregate query.
	Orphaned bool
	// LatestRun is the target's slice of the most recent dispatch batch that
	// planned any of its checks; nil when it has never run. RunCount is how many
	// distinct runs planned an execution for it. Both are filled by the API from
	// ListQualityTargetRunSummaries.
	LatestRun *QualityTargetRunSummaryMessage
	RunCount  int32

	// ScheduleInterval and NextRunAt are the target's own schedule: every enabled
	// check on the target runs together on this cadence. NextRunAt is nil while
	// the target has no enabled rule.
	ScheduleInterval time.Duration
	NextRunAt        *time.Time

	// QualityScore is the target's 0-100 weighted score over its enabled rules'
	// latest judged results; nil when none was judged. ScoredRuleCount is how many
	// enabled rules contributed. Both are filled by the API from the rules and
	// their latest executions, which the aggregate query cannot score.
	QualityScore    *float64
	ScoredRuleCount int32
	// RecentPassRate is the 0-1 pass share (PASS / PASS+WARN+FAIL) of the target's
	// newest completed executions, and RecentExecutionCount is how many of those
	// were judged. Nil when none was judged. The API fills both from
	// ListQualityTargetExecutionTallies.
	RecentPassRate       *float64
	RecentExecutionCount int32

	// CoveredColumnCount and TotalColumnCount are the target's column coverage:
	// how many of its current registry columns at least one enabled rule reads, and
	// how many columns the registry currently records for it. Both are filled by
	// the API from one registry read, so the numerator can never exceed the
	// denominator; a target that no longer resolves reports 0/0.
	CoveredColumnCount int32
	TotalColumnCount   int32
}

// qualityTargetStatusRank maps a quality status onto the ordering used to pick a
// target's worst latest result. ERROR outranks FAIL: a check that could not run
// needs attention more than one whose data failed. SKIPPED is the weakest verdict
// (the check was deliberately not run) and NOT_EVALUATED sits just above it, so a
// directory filter can tell "nothing to judge" apart from "did not run".
func QualityTargetStatusRank(status storepb.QualityStatus) int32 {
	switch status {
	case storepb.QualityStatus_QUALITY_STATUS_ERROR:
		return 6
	case storepb.QualityStatus_QUALITY_STATUS_FAIL:
		return 5
	case storepb.QualityStatus_QUALITY_STATUS_WARN:
		return 4
	case storepb.QualityStatus_QUALITY_STATUS_PASS:
		return 3
	case storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED:
		return 2
	case storepb.QualityStatus_QUALITY_STATUS_SKIPPED:
		return 1
	case storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED:
		return 0
	default:
		return 0
	}
}

// qualityStatusFromRank is the inverse of qualityTargetStatusRank. A rank below
// one means no rule has run.
func QualityTargetStatusFromRank(rank int32) storepb.QualityStatus {
	switch rank {
	case 6:
		return storepb.QualityStatus_QUALITY_STATUS_ERROR
	case 5:
		return storepb.QualityStatus_QUALITY_STATUS_FAIL
	case 4:
		return storepb.QualityStatus_QUALITY_STATUS_WARN
	case 3:
		return storepb.QualityStatus_QUALITY_STATUS_PASS
	case 2:
		return storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED
	case 1:
		return storepb.QualityStatus_QUALITY_STATUS_SKIPPED
	default:
		return storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED
	}
}

// qualityTargetWorstRankCase renders the CASE expression the aggregate applies to
// each rule's latest status. The rank numbers come from qualityTargetStatusRank,
// so the SQL mapping cannot drift from the filter mapping; the placeholders are
// the six status arguments the query binds at $4..$9, in this order: ERROR, FAIL,
// WARN, PASS, SKIPPED, NOT_EVALUATED.
func qualityTargetWorstRankCase() string {
	return qualityTargetWorstRankCaseAt("latest.status", 4)
}

// qualityTargetWorstRankCaseAt renders the same CASE for a query whose status
// arguments start at firstPlaceholder. The argument order is fixed: ERROR, FAIL,
// WARN, PASS, SKIPPED, NOT_EVALUATED. Callers that aggregate a different row set
// (the run slice) reuse it so the rank mapping never has a second implementation.
func qualityTargetWorstRankCaseAt(statusExpr string, firstPlaceholder int) string {
	return fmt.Sprintf(`CASE %s
				WHEN $%d THEN %d WHEN $%d THEN %d WHEN $%d THEN %d
				WHEN $%d THEN %d WHEN $%d THEN %d WHEN $%d THEN %d ELSE 0 END`,
		statusExpr,
		firstPlaceholder, QualityTargetStatusRank(storepb.QualityStatus_QUALITY_STATUS_ERROR),
		firstPlaceholder+1, QualityTargetStatusRank(storepb.QualityStatus_QUALITY_STATUS_FAIL),
		firstPlaceholder+2, QualityTargetStatusRank(storepb.QualityStatus_QUALITY_STATUS_WARN),
		firstPlaceholder+3, QualityTargetStatusRank(storepb.QualityStatus_QUALITY_STATUS_PASS),
		firstPlaceholder+4, QualityTargetStatusRank(storepb.QualityStatus_QUALITY_STATUS_SKIPPED),
		firstPlaceholder+5, QualityTargetStatusRank(storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED))
}

// qualityTargetSchemaChangedSQL is the directory aggregate's structure-change
// predicate, the SQL twin of qualitySchemaChangedSince in the API layer: a rule's
// saved snapshot is current when it equals the target's fingerprint, and also when
// it equals the legacy DDL hash a pre-fingerprint rule stored. The latter branch
// is what keeps an upgrade from reporting every existing StarRocks/Doris rule as
// changed.
func qualityTargetSchemaChangedSQL() string {
	return `BOOL_OR(
			l.target_meta_hash IS NOT NULL
			AND f.fingerprint IS NOT NULL
			AND l.target_meta_hash <> f.fingerprint
			AND (s.schema_hash IS NULL OR l.target_meta_hash <> s.schema_hash)
		)`
}

// targetWhere renders the predicate over the joined aggregate query. It is a
// pure function so the filter shape can be unit-tested without a database.
// `base` is the number of parameters the query has already bound, so the
// placeholders continue from there instead of restarting at $1.
func targetWhere(find *FindQualityTargetMessage, base int) ([]string, []any, error) {
	if find == nil {
		find = &FindQualityTargetMessage{}
	}
	where := []string{"TRUE"}
	args := []any{}
	// The four path filters reuse the rule list's GUID prefix builder: the
	// deepest level set decides how many segments the subtree condition carries.
	prefix, err := qualityRuleGUIDPrefix(&FindQualityRuleMessage{
		Instance:    find.Instance,
		Database:    find.Database,
		Schema:      find.Schema,
		TablePrefix: find.TablePrefix,
	})
	if err != nil {
		return nil, nil, err
	}
	if prefix != "" {
		// Same subtree semantics as appendGUIDSubtreeCondition, but the query has
		// already bound its aggregate constants, so the placeholders start at base.
		args = append(args, prefix)
		exactIndex := base + len(args)
		args = append(args, likePatternEscaper.Replace(prefix+common.MetaGUIDSplit)+"%")
		descendantIndex := base + len(args)
		where = append(where, fmt.Sprintf(
			`(t.target_guid = $%d OR t.target_guid LIKE $%d ESCAPE E'\\')`,
			exactIndex, descendantIndex))
	}
	if find.Search != nil && strings.TrimSpace(*find.Search) != "" {
		pattern := "%" + likePatternEscaper.Replace(strings.TrimSpace(*find.Search)) + "%"
		args = append(args, pattern)
		where = append(where, fmt.Sprintf(
			`(concat_ws(' ', t.target_path->>'instance', t.target_path->>'database', t.target_path->>'schema', t.target_path->>'object') ILIKE $%d ESCAPE E'\\' OR t.target_guid ILIKE $%d ESCAPE E'\\')`,
			base+len(args), base+len(args)))
	}
	if len(find.WorstStatuses) > 0 {
		ranks := make([]int32, len(find.WorstStatuses))
		for i, status := range find.WorstStatuses {
			ranks[i] = QualityTargetStatusRank(status)
		}
		args = append(args, pq.Array(ranks))
		where = append(where, fmt.Sprintf("COALESCE(r.worst_rank, 0) = ANY($%d::int[])", base+len(args)))
	} else if find.WorstStatus != storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED {
		args = append(args, QualityTargetStatusRank(find.WorstStatus))
		where = append(where, fmt.Sprintf("COALESCE(r.worst_rank, 0) = $%d", base+len(args)))
	}
	if find.HasOpenIssue {
		where = append(where, "COALESCE(i.open_issue_count, 0) > 0")
	}
	if find.HasPendingDraft {
		where = append(where, "COALESCE(d.pending_draft_count, 0) > 0")
	}
	return where, args, nil
}

// ListQualityTargets aggregates the workspace's rules by target. One query
// returns the page: the per-target counts, the worst latest result, the last run
// and the issue/draft tallies, so the directory never reads every rule row to
// build its rows.
//
// An exact-target selection is pushed into the query rather than applied to its
// result: the rule scan, the per-rule latest-execution probe and the issue/draft
// tallies all run for the selected targets alone, so a metadata page costs its
// own objects and not the workspace's retained execution history.
func (s *Store) ListQualityTargets(ctx context.Context, find *FindQualityTargetMessage) ([]*QualityTargetMessage, error) {
	query, args, err := qualityTargetsStatement(find)
	if err != nil {
		return nil, err
	}
	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality targets")
	}
	defer rows.Close()

	targets := []*QualityTargetMessage{}
	for rows.Next() {
		var (
			message         QualityTargetMessage
			targetGUID      string
			targetType      int16
			targetPath      []byte
			worstRank       int32
			lastRunTime     sql.NullTime
			schemaChange    bool
			scheduleSeconds sql.NullInt32
			scheduleNext    sql.NullTime
		)
		scanErr := rows.Scan(
			&targetGUID, &targetType, &targetPath,
			&message.RuleCount, &message.EnabledCount, &message.PausedCount, &message.ColumnCount,
			&worstRank, &lastRunTime,
			&message.OpenIssueCount, &message.PendingDraftCount, &message.PendingLabelCount,
			&schemaChange, &scheduleSeconds, &scheduleNext,
		)
		if scanErr != nil {
			return nil, errors.Wrap(scanErr, "failed to scan a quality target")
		}
		message.GUID = targetGUID
		message.Type = storepb.MetaType(targetType)
		message.WorstStatus = QualityTargetStatusFromRank(worstRank)
		message.SchemaChanged = schemaChange
		message.ScheduleInterval = time.Duration(scheduleSeconds.Int32) * time.Second
		if scheduleNext.Valid {
			message.NextRunAt = &scheduleNext.Time
		}
		if lastRunTime.Valid {
			message.LastRunTime = &lastRunTime.Time
		}
		if len(targetPath) > 0 {
			path := &storepb.QualityTargetPath{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(targetPath, path); err != nil {
				return nil, errors.Wrap(err, "failed to unmarshal a quality target path")
			}
			message.Path = path
		}
		targets = append(targets, &message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality targets")
	}
	return targets, nil
}

// qualityTargetsStatement renders the aggregate query and its arguments. It is a
// pure function so the pushed-down selection, the bounded execution probe and the
// placeholder layout can be pinned without a database.
func qualityTargetsStatement(find *FindQualityTargetMessage) (string, []any, error) {
	if find == nil {
		find = &FindQualityTargetMessage{}
	}
	args := []any{
		int16(storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED),
		int16(storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED),
		int16(storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED),
		int16(storepb.QualityStatus_QUALITY_STATUS_ERROR),
		int16(storepb.QualityStatus_QUALITY_STATUS_FAIL),
		int16(storepb.QualityStatus_QUALITY_STATUS_WARN),
		int16(storepb.QualityStatus_QUALITY_STATUS_PASS),
		int16(storepb.QualityStatus_QUALITY_STATUS_SKIPPED),
		int16(storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED),
		int16(storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED),
		int16(storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT),
		int16(storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL),
		int16(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING),
	}
	// The selection has to restrict the scans themselves. Applied only to the
	// outer SELECT it would leave the rule aggregation and the execution probe
	// reading the whole workspace — the exact whole-history scan a metadata page
	// must not pay. The outer predicate stays as a safety net around the result.
	liveSelection, issuesSelection, draftsSelection, outerSelection := "", "", "", ""
	if len(find.TargetKeys) > 0 {
		guids := make([]string, 0, len(find.TargetKeys))
		objectTypes := make([]int16, 0, len(find.TargetKeys))
		for _, key := range find.TargetKeys {
			guids = append(guids, key.GUID)
			objectTypes = append(objectTypes, int16(key.ObjectType))
		}
		args = append(args, pq.Array(guids), pq.Array(objectTypes))
		selected := fmt.Sprintf("(SELECT * FROM unnest($%d::text[], $%d::int2[]))", len(args)-1, len(args))
		liveSelection = " AND (r.target_guid, r.target_type) IN " + selected
		issuesSelection = " AND (i.target_guid, i.target_type) IN " + selected
		draftsSelection = " AND (target_guid, target_type) IN " + selected
		outerSelection = "(t.target_guid, t.target_type) IN " + selected
	}
	filter, filterArgs, err := targetWhere(find, len(args))
	if err != nil {
		return "", nil, err
	}
	if outerSelection != "" {
		filter = append(filter, outerSelection)
	}
	args = append(args, filterArgs...)
	query := `WITH live AS (
		SELECT r.id, r.target_guid, r.target_type, r.target_path, r.state, r.target_meta_hash
		FROM quality_rule r
		WHERE r.deleted = FALSE AND r.state <> $1` + liveSelection + `
	),
	targets AS (
		SELECT l.target_guid, l.target_type,
			(COUNT(*))::INT AS rule_count,
			(COUNT(*) FILTER (WHERE l.state = $2))::INT AS enabled_count,
			(COUNT(*) FILTER (WHERE l.state = $3))::INT AS paused_count,
			(COUNT(DISTINCT NULLIF(l.target_path->>'column', '')))::INT AS column_count,
			(ARRAY_AGG(l.target_path ORDER BY l.id DESC))[1] AS target_path,
			` + qualityTargetSchemaChangedSQL() + ` AS schema_changed
		FROM live l
		LEFT JOIN meta_registry_schema_fingerprint f
			ON f.guid = l.target_guid AND f.object_type = l.target_type
		LEFT JOIN meta_registry_resource_schema s
			ON s.guid = l.target_guid AND s.object_type = l.target_type
		GROUP BY l.target_guid, l.target_type
	),
	latest AS (
		SELECT l.id AS rule_id, e.status, e.finished_at
		FROM live l
		JOIN LATERAL (
			SELECT e.status, e.finished_at
			FROM quality_execution e
			WHERE e.rule_id = l.id AND e.status <> 0
			ORDER BY e.started_at DESC, e.id DESC
			LIMIT 1
		) e ON TRUE
	),
	results AS (
		SELECT l.target_guid, l.target_type,
			MAX(` + qualityTargetWorstRankCase() + `)::INT AS worst_rank,
			MAX(latest.finished_at) AS last_run_time
		FROM latest
		JOIN live l ON l.id = latest.rule_id
		GROUP BY l.target_guid, l.target_type
	),
	issues AS (
		SELECT i.target_guid, i.target_type, (COUNT(*))::INT AS open_issue_count
		FROM quality_issue i
		JOIN quality_rule r ON r.id = i.rule_id
		WHERE i.status <> $10 AND r.deleted = FALSE AND r.state <> $1` + issuesSelection + `
		GROUP BY i.target_guid, i.target_type
	),
	drafts AS (
		SELECT target_guid, target_type,
			(COUNT(*) FILTER (WHERE kind = $11))::INT AS pending_draft_count,
			(COUNT(*) FILTER (WHERE kind = $12))::INT AS pending_label_count
		FROM quality_suggestion
		WHERE status = $13` + draftsSelection + `
		GROUP BY target_guid, target_type
	)
	SELECT t.target_guid, t.target_type, t.target_path, t.rule_count, t.enabled_count,
		t.paused_count, t.column_count, COALESCE(r.worst_rank, 0), r.last_run_time,
		COALESCE(i.open_issue_count, 0), COALESCE(d.pending_draft_count, 0),
		COALESCE(d.pending_label_count, 0), COALESCE(t.schema_changed, FALSE),
		sch.schedule_interval_seconds, sch.next_run_at
	FROM targets t
	LEFT JOIN results r ON r.target_guid = t.target_guid AND r.target_type = t.target_type
	LEFT JOIN issues i ON i.target_guid = t.target_guid AND i.target_type = t.target_type
	LEFT JOIN drafts d ON d.target_guid = t.target_guid AND d.target_type = t.target_type
	LEFT JOIN quality_target_schedule sch
		ON sch.target_guid = t.target_guid AND sch.target_type = t.target_type
	WHERE ` + strings.Join(filter, " AND ") + `
	ORDER BY ` + qualityTargetOrderClause(find.Order)
	// Limit and offset are optional, like the other list stores: omitting them
	// must mean "no bound", not LIMIT 0.
	if find.Limit != nil {
		args = append(args, max(*find.Limit, 0))
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if find.Offset != nil {
		args = append(args, max(*find.Offset, 0))
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}
	return query, args, nil
}

// qualityTargetOrderClause is the ORDER BY for one directory order. Every order
// ends with the GUID so pagination is stable when two rows tie.
func qualityTargetOrderClause(order QualityTargetOrder) string {
	switch order {
	case QualityTargetOrderName:
		return "COALESCE(t.target_path->>'object', '') ASC, t.target_guid ASC"
	case QualityTargetOrderRuleCount:
		return "t.rule_count DESC, COALESCE(t.target_path->>'object', '') ASC, t.target_guid ASC"
	case QualityTargetOrderLastRun:
		return "r.last_run_time DESC NULLS LAST, t.target_guid ASC"
	case QualityTargetOrderAttention:
		return "COALESCE(r.worst_rank, 0) DESC, COALESCE(i.open_issue_count, 0) DESC, " +
			"COALESCE(d.pending_draft_count, 0) DESC, COALESCE(t.target_path->>'object', '') ASC, t.target_guid ASC"
	default:
		return "COALESCE(r.worst_rank, 0) DESC, COALESCE(i.open_issue_count, 0) DESC, " +
			"COALESCE(d.pending_draft_count, 0) DESC, COALESCE(t.target_path->>'object', '') ASC, t.target_guid ASC"
	}
}

// CountPendingQualitySuggestions tallies one target's pending AI proposals. The
// workspace header shows the counts, not the payloads, so this reads no JSONB.
func (s *Store) CountPendingQualitySuggestions(ctx context.Context, targetGUID string) (int32, int32, error) {
	if targetGUID == "" {
		return 0, 0, common.Errorf(common.Invalid, "a target GUID is required")
	}
	var drafts, labels int32
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT (COUNT(*) FILTER (WHERE kind = $1))::INT,
			(COUNT(*) FILTER (WHERE kind = $2))::INT
		FROM quality_suggestion
		WHERE target_guid = $3 AND status = $4
	`,
		int16(storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT),
		int16(storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL),
		targetGUID,
		int16(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING),
	).Scan(&drafts, &labels)
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to count the target's pending suggestions")
	}
	return drafts, labels, nil
}
