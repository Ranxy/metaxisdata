package store

import (
	"context"
	"time"

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

// CountMonitoredTables counts the distinct tables at least one enabled rule
// watches. It is the coverage numerator of the overview.
func (s *Store) CountMonitoredTables(ctx context.Context) (int, error) {
	var count int
	scanErr := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT target_guid) FROM quality_rule
		WHERE state = $1 AND deleted = FALSE
	`, int16(storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED)).Scan(&count)
	if scanErr != nil {
		return 0, errors.Wrap(scanErr, "failed to count monitored tables")
	}
	return count, nil
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

// CountDistinctFailedRulesSince counts the rules that reported a FAIL result at
// or after since. ERROR is deliberately not counted: a check that could not run
// failed no data.
func (s *Store) CountDistinctFailedRulesSince(ctx context.Context, since time.Time) (int, error) {
	if since.IsZero() {
		return 0, common.Errorf(common.Invalid, "a start time is required")
	}
	var count int
	scanErr := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT rule_id) FROM quality_rule_result
		WHERE status = $1 AND started_at >= $2
	`, int16(storepb.QualityStatus_QUALITY_STATUS_FAIL), since).Scan(&count)
	if scanErr != nil {
		return 0, errors.Wrap(scanErr, "failed to count failing rules")
	}
	return count, nil
}

// QualityResultCountsSince tallies the results started at or after since.
func (s *Store) QualityResultCountsSince(ctx context.Context, since time.Time) (*storepb.QualityRunCounts, error) {
	if since.IsZero() {
		return nil, common.Errorf(common.Invalid, "a start time is required")
	}
	counts := &storepb.QualityRunCounts{}
	var err error
	if counts.Total, err = s.countQualityResults(ctx, since, nil); err != nil {
		return nil, err
	}
	if counts.Pass, err = s.countQualityResults(ctx, since, ptrStatus(storepb.QualityStatus_QUALITY_STATUS_PASS)); err != nil {
		return nil, err
	}
	if counts.Warn, err = s.countQualityResults(ctx, since, ptrStatus(storepb.QualityStatus_QUALITY_STATUS_WARN)); err != nil {
		return nil, err
	}
	if counts.Fail, err = s.countQualityResults(ctx, since, ptrStatus(storepb.QualityStatus_QUALITY_STATUS_FAIL)); err != nil {
		return nil, err
	}
	if counts.Error, err = s.countQualityResults(ctx, since, ptrStatus(storepb.QualityStatus_QUALITY_STATUS_ERROR)); err != nil {
		return nil, err
	}
	if counts.Skipped, err = s.countQualityResults(ctx, since, ptrStatus(storepb.QualityStatus_QUALITY_STATUS_SKIPPED)); err != nil {
		return nil, err
	}
	if counts.NotEvaluated, err = s.countQualityResults(ctx, since, ptrStatus(storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED)); err != nil {
		return nil, err
	}
	return counts, nil
}

// countQualityResults counts one status, or every result for a nil status.
func (s *Store) countQualityResults(ctx context.Context, since time.Time, status *storepb.QualityStatus) (int32, error) {
	var count int32
	query := `SELECT COUNT(*) FROM quality_rule_result WHERE started_at >= $1`
	args := []any{since}
	if status != nil {
		query += ` AND status = $2`
		args = append(args, int16(*status))
	}
	if err := s.GetDB().QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, errors.Wrap(err, "failed to count quality results")
	}
	return count, nil
}

// QualityTrendDay is one day of result tallies.
type QualityTrendDay struct {
	// Date is the UTC day, formatted as YYYY-MM-DD.
	Date   string
	Counts *storepb.QualityRunCounts
}

// QualityResultTrend groups the results started at or after since into UTC days.
// Days without results are absent; the caller fills the gaps so a chart does not
// skip a quiet day.
func (s *Store) QualityResultTrend(ctx context.Context, since time.Time) ([]QualityTrendDay, error) {
	if since.IsZero() {
		return nil, common.Errorf(common.Invalid, "a start time is required")
	}
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT to_char(date_trunc('day', started_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS day,
			status, COUNT(*)
		FROM quality_rule_result
		WHERE started_at >= $1
		GROUP BY day, status
		ORDER BY day
	`, since)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the quality result trend")
	}
	defer rows.Close()

	byDay := make(map[string]*storepb.QualityRunCounts)
	var days []string
	for rows.Next() {
		var (
			day    string
			status int16
			count  int32
		)
		if err := rows.Scan(&day, &status, &count); err != nil {
			return nil, errors.Wrap(err, "failed to scan the quality result trend")
		}
		counts, ok := byDay[day]
		if !ok {
			counts = &storepb.QualityRunCounts{}
			byDay[day] = counts
			days = append(days, day)
		}
		counts.Total += count
		addQualityStatusCount(counts, storepb.QualityStatus(status), count)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality result trend")
	}

	trend := make([]QualityTrendDay, 0, len(days))
	for _, day := range days {
		trend = append(trend, QualityTrendDay{Date: day, Counts: byDay[day]})
	}
	return trend, nil
}

// addQualityStatusCount adds count to the tally of one status.
func addQualityStatusCount(counts *storepb.QualityRunCounts, status storepb.QualityStatus, count int32) {
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
