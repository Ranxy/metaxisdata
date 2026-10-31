package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualityRuleStateCounts counts the workspace's live rules per lifecycle state.
// Archived rules are excluded: the overview reports what is being kept, not what
// was deleted.
func (s *Store) QualityRuleStateCounts(ctx context.Context) (map[storepb.QualityRuleState]int, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT state, COUNT(*) FROM quality_rule WHERE deleted = FALSE GROUP BY state
	`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to count quality rules by state")
	}
	defer rows.Close()

	counts := make(map[storepb.QualityRuleState]int, 3)
	for rows.Next() {
		var (
			state int16
			count int
		)
		if err := rows.Scan(&state, &count); err != nil {
			return nil, errors.Wrap(err, "failed to scan quality rule state counts")
		}
		counts[storepb.QualityRuleState(state)] = count
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate quality rule state counts")
	}
	return counts, nil
}

// qualityCoverageObjectTypes is the coverage denominator's object-type set: the
// registry objects a rule can monitor. A column is not a target, and a database or
// schema is only a container.
var qualityCoverageObjectTypes = []int16{
	int16(storepb.MetaType_TABLE),
	int16(storepb.MetaType_EXTERNAL_TABLE),
	int16(storepb.MetaType_VIEW),
	int16(storepb.MetaType_MATERIALIZED_VIEW),
}

// qualityCoverageQuery counts, per live instance, the registry objects of the
// monitorable types and their columns. Views and external tables carry their
// columns inside their own metadata (they have no COLUMN registry rows), so the
// four metadata column arrays are summed instead of counting COLUMN objects. The
// instance join is the live-instance filter: objects under a deleted instance are
// no longer monitored.
const qualityCoverageQuery = `
	SELECT i.resource_id,
		COALESCE(i.metadata->>'title', ''),
		COALESCE(i.environment, ''),
		COUNT(*)::INT,
		COALESCE(SUM(
			COALESCE(jsonb_array_length(r.metadata->'tableMetadata'->'columns'), 0) +
			COALESCE(jsonb_array_length(r.metadata->'externalTableMetadata'->'columns'), 0) +
			COALESCE(jsonb_array_length(r.metadata->'viewMetadata'->'columns'), 0) +
			COALESCE(jsonb_array_length(r.metadata->'materializedViewMetadata'->'columns'), 0)
		), 0)::INT
	FROM meta_registry_resource r
	JOIN instance i ON i.resource_id = split_part(r.guid, ';', 1)
	WHERE i.deleted = FALSE AND r.object_type = ANY($1::int2[])
	GROUP BY i.id
	ORDER BY i.resource_id
`

// qualityTargetColumnQuery returns each requested target's current registry
// columns, i.e. the column names the coverage denominator counts for it. It is
// the numerator's guard: an enabled rule outlives a dropped or renamed column
// just as it outlives a dropped table, and a column that is no longer in the
// registry cannot be covered. The live-instance join and the object-type
// predicate are the coverage denominator's own, so both sides count the same
// objects; the LEFT JOIN LATERAL keeps a zero-column object as a row with a NULL
// name, so its key is still known to exist.
const qualityTargetColumnQuery = `
	SELECT k.guid, k.object_type, c.name
	FROM unnest($1::text[], $2::int2[]) AS k(guid, object_type)
	JOIN instance i ON i.resource_id = split_part(k.guid, ';', 1) AND i.deleted = FALSE
	JOIN meta_registry_resource r ON r.guid = k.guid AND r.object_type = k.object_type
	LEFT JOIN LATERAL (
		SELECT cols.value->>'name' AS name
		FROM (
			SELECT jsonb_array_elements(COALESCE(r.metadata->'tableMetadata'->'columns', '[]'::jsonb)) AS value
			UNION ALL
			SELECT jsonb_array_elements(COALESCE(r.metadata->'externalTableMetadata'->'columns', '[]'::jsonb))
			UNION ALL
			SELECT jsonb_array_elements(COALESCE(r.metadata->'viewMetadata'->'columns', '[]'::jsonb))
			UNION ALL
			SELECT jsonb_array_elements(COALESCE(r.metadata->'materializedViewMetadata'->'columns', '[]'::jsonb))
		) AS cols
	) c ON TRUE
	WHERE r.object_type = ANY($3::int2[])
	ORDER BY k.guid, k.object_type, c.name
`

// ListMetaRegistryTargetColumns returns each target's current registry column
// names, keyed by target identity. A key is present exactly when the target has a
// coverage denominator row under a live instance; a target with no columns maps
// to an empty set. One batch statement serves a whole rule set, and an empty input
// is a no-op.
func (s *Store) ListMetaRegistryTargetColumns(ctx context.Context, keys []MetaGUIDKey) (map[MetaGUIDKey]map[string]struct{}, error) {
	columns := make(map[MetaGUIDKey]map[string]struct{}, len(keys))
	if len(keys) == 0 {
		return columns, nil
	}
	guids := make([]string, 0, len(keys))
	objectTypes := make([]int16, 0, len(keys))
	for _, key := range keys {
		guids = append(guids, key.GUID)
		objectTypes = append(objectTypes, int16(key.ObjectType))
	}
	rows, err := s.GetDB().QueryContext(ctx, qualityTargetColumnQuery,
		pq.Array(guids), pq.Array(objectTypes), pq.Array(qualityCoverageObjectTypes))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the targets' registry columns")
	}
	defer rows.Close()

	for rows.Next() {
		var (
			guid       string
			objectType int16
			name       sql.NullString
		)
		scanErr := rows.Scan(&guid, &objectType, &name)
		if scanErr != nil {
			return nil, errors.Wrap(scanErr, "failed to scan a target's registry column")
		}
		key := MetaGUIDKey{GUID: guid, ObjectType: storepb.MetaType(objectType)}
		targetColumns := columns[key]
		if targetColumns == nil {
			targetColumns = map[string]struct{}{}
			columns[key] = targetColumns
		}
		if name.Valid {
			targetColumns[name.String] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the targets' registry columns")
	}
	return columns, nil
}

// QualityCoverageRow is one live instance's registry coverage, the denominator of
// the workspace coverage. Environment is empty when the instance has none, and
// InstanceTitle is empty when the instance never recorded one.
type QualityCoverageRow struct {
	InstanceResourceID string
	InstanceTitle      string
	Environment        string
	TableCount         int32
	ColumnCount        int32
}

// ListQualityCoverage counts the registry objects and their columns per live
// instance, which is the coverage denominator. One grouped query serves the
// whole overview: the workspace totals and the per-instance and per-environment
// breakdowns are sums over these rows.
func (s *Store) ListQualityCoverage(ctx context.Context) ([]*QualityCoverageRow, error) {
	rows, err := s.GetDB().QueryContext(ctx, qualityCoverageQuery, pq.Array(qualityCoverageObjectTypes))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the registry coverage")
	}
	defer rows.Close()

	var coverage []*QualityCoverageRow
	for rows.Next() {
		row := &QualityCoverageRow{}
		scanErr := rows.Scan(&row.InstanceResourceID, &row.InstanceTitle, &row.Environment,
			&row.TableCount, &row.ColumnCount)
		if scanErr != nil {
			return nil, errors.Wrap(scanErr, "failed to scan the registry coverage")
		}
		coverage = append(coverage, row)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the registry coverage")
	}
	return coverage, nil
}

// QualityOpenIssueSeverityCounts counts the unresolved issues per severity.
func (s *Store) QualityOpenIssueSeverityCounts(ctx context.Context) (map[storepb.QualitySeverity]int, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT severity, COUNT(*) FROM quality_issue WHERE status <> $1 GROUP BY severity
	`, int16(storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED))
	if err != nil {
		return nil, errors.Wrap(err, "failed to count open quality issues")
	}
	defer rows.Close()

	counts := make(map[storepb.QualitySeverity]int, 3)
	for rows.Next() {
		var (
			severity int16
			count    int
		)
		if err := rows.Scan(&severity, &count); err != nil {
			return nil, errors.Wrap(err, "failed to scan open quality issue counts")
		}
		counts[storepb.QualitySeverity(severity)] = count
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate open quality issue counts")
	}
	return counts, nil
}

// CountDistinctFailedRulesSince counts the rules whose completed execution at or
// after since reported FAIL. ERROR is deliberately not counted: a check that
// could not run failed no data.
func (s *Store) CountDistinctFailedRulesSince(ctx context.Context, since time.Time) (int, error) {
	if since.IsZero() {
		return 0, common.Errorf(common.Invalid, "a start time is required")
	}
	var count int
	scanErr := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT rule_id) FROM quality_execution
		WHERE status = $1 AND state = $2 AND started_at >= $3
	`, int16(storepb.QualityStatus_QUALITY_STATUS_FAIL),
		int16(storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED), since).Scan(&count)
	if scanErr != nil {
		return 0, errors.Wrap(scanErr, "failed to count failing rules")
	}
	return count, nil
}

// QualityExecutionStatusCountsSince tallies the completed executions started at
// or after since. PENDING, RUNNING and ABANDONED rows are excluded: only a
// completed execution is an observation, so a pending row can never inflate the
// total or leave a half-written day in the trend.
func (s *Store) QualityExecutionStatusCountsSince(ctx context.Context, since time.Time) (*storepb.QualityStatusCounts, error) {
	if since.IsZero() {
		return nil, common.Errorf(common.Invalid, "a start time is required")
	}
	counts := &storepb.QualityStatusCounts{}
	var err error
	if counts.Total, err = s.countQualityExecutions(ctx, since, nil); err != nil {
		return nil, err
	}
	if counts.Pass, err = s.countQualityExecutions(ctx, since, ptrStatus(storepb.QualityStatus_QUALITY_STATUS_PASS)); err != nil {
		return nil, err
	}
	if counts.Warn, err = s.countQualityExecutions(ctx, since, ptrStatus(storepb.QualityStatus_QUALITY_STATUS_WARN)); err != nil {
		return nil, err
	}
	if counts.Fail, err = s.countQualityExecutions(ctx, since, ptrStatus(storepb.QualityStatus_QUALITY_STATUS_FAIL)); err != nil {
		return nil, err
	}
	if counts.Error, err = s.countQualityExecutions(ctx, since, ptrStatus(storepb.QualityStatus_QUALITY_STATUS_ERROR)); err != nil {
		return nil, err
	}
	if counts.Skipped, err = s.countQualityExecutions(ctx, since, ptrStatus(storepb.QualityStatus_QUALITY_STATUS_SKIPPED)); err != nil {
		return nil, err
	}
	if counts.NotEvaluated, err = s.countQualityExecutions(ctx, since, ptrStatus(storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED)); err != nil {
		return nil, err
	}
	return counts, nil
}

// countQualityExecutions counts one status among the completed executions, or
// every completed execution for a nil status.
func (s *Store) countQualityExecutions(ctx context.Context, since time.Time, status *storepb.QualityStatus) (int32, error) {
	var count int32
	query := `SELECT COUNT(*) FROM quality_execution WHERE state = $1 AND started_at >= $2`
	args := []any{int16(storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED), since}
	if status != nil {
		query += ` AND status = $3`
		args = append(args, int16(*status))
	}
	if err := s.GetDB().QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, errors.Wrap(err, "failed to count quality executions")
	}
	return count, nil
}

// QualityTrendDay is one day of completed-execution tallies.
type QualityTrendDay struct {
	// Date is the UTC day, formatted as YYYY-MM-DD.
	Date   string
	Counts *storepb.QualityStatusCounts
}

// QualityExecutionTrend groups the completed executions started at or after
// since into UTC days. Days without executions are absent; the caller fills the
// gaps so a chart does not skip a quiet day.
func (s *Store) QualityExecutionTrend(ctx context.Context, since time.Time) ([]QualityTrendDay, error) {
	if since.IsZero() {
		return nil, common.Errorf(common.Invalid, "a start time is required")
	}
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT to_char(date_trunc('day', started_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS day,
			status, COUNT(*)
		FROM quality_execution
		WHERE state = $1 AND started_at >= $2
		GROUP BY day, status
		ORDER BY day
	`, int16(storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED), since)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the quality execution trend")
	}
	defer rows.Close()

	byDay := make(map[string]*storepb.QualityStatusCounts)
	var days []string
	for rows.Next() {
		var (
			day    string
			status int16
			count  int32
		)
		if err := rows.Scan(&day, &status, &count); err != nil {
			return nil, errors.Wrap(err, "failed to scan the quality execution trend")
		}
		counts, ok := byDay[day]
		if !ok {
			counts = &storepb.QualityStatusCounts{}
			byDay[day] = counts
			days = append(days, day)
		}
		counts.Total += count
		addQualityStatusCount(counts, storepb.QualityStatus(status), count)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality execution trend")
	}

	trend := make([]QualityTrendDay, 0, len(days))
	for _, day := range days {
		trend = append(trend, QualityTrendDay{Date: day, Counts: byDay[day]})
	}
	return trend, nil
}

// addQualityStatusCount adds count to the tally of one status.
func addQualityStatusCount(counts *storepb.QualityStatusCounts, status storepb.QualityStatus, count int32) {
	switch status {
	case storepb.QualityStatus_QUALITY_STATUS_PASS:
		counts.Pass += count
	case storepb.QualityStatus_QUALITY_STATUS_WARN:
		counts.Warn += count
	case storepb.QualityStatus_QUALITY_STATUS_FAIL:
		counts.Fail += count
	case storepb.QualityStatus_QUALITY_STATUS_ERROR:
		counts.Error += count
	case storepb.QualityStatus_QUALITY_STATUS_SKIPPED:
		counts.Skipped += count
	case storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED:
		counts.NotEvaluated += count
	case storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED:
	default:
	}
}

// ptrStatus returns a pointer to status.
func ptrStatus(status storepb.QualityStatus) *storepb.QualityStatus {
	return &status
}
