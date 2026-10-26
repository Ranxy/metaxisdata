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

// qualityTargetScheduleColumns is the column list every quality_target_schedule
// read returns, in the order scanQualityTargetSchedule consumes it.
const qualityTargetScheduleColumns = `target_guid, target_type,
	schedule_interval_seconds, next_run_at, created_at, updated_at`

// QualityTargetScheduleMessage is the store representation of one target's
// schedule row. Scheduling is per target: every enabled rule of the target runs
// together when next_run_at comes due, at schedule_interval_seconds apart.
type QualityTargetScheduleMessage struct {
	TargetGUID string
	TargetType storepb.MetaType
	// ScheduleInterval is how often the target's checks run. The column stores
	// whole seconds.
	ScheduleInterval time.Duration
	// NextRunAt is when the scheduler next considers the target due; nil while the
	// target has no enabled rule.
	NextRunAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// scheduleScanner is the row shape scanned from either a *sql.Row or *sql.Rows,
// so the mapping is testable without a database.
type scheduleScanner interface {
	Scan(dest ...any) error
}

// scanQualityTargetSchedule maps one quality_target_schedule row to the message.
func scanQualityTargetSchedule(row scheduleScanner) (*QualityTargetScheduleMessage, error) {
	var (
		msg             QualityTargetScheduleMessage
		targetType      int16
		intervalSeconds int32
		nextRunAt       sql.NullTime
	)
	if err := row.Scan(
		&msg.TargetGUID,
		&targetType,
		&intervalSeconds,
		&nextRunAt,
		&msg.CreatedAt,
		&msg.UpdatedAt,
	); err != nil {
		return nil, err
	}
	msg.TargetType = storepb.MetaType(targetType)
	msg.ScheduleInterval = time.Duration(intervalSeconds) * time.Second
	if nextRunAt.Valid {
		msg.NextRunAt = &nextRunAt.Time
	}
	return &msg, nil
}

// GetQualityTargetSchedule reads one target's schedule. A nil message means the
// target has no schedule row yet; rules created through the API always create
// one.
func (s *Store) GetQualityTargetSchedule(ctx context.Context, targetGUID string, targetType storepb.MetaType) (*QualityTargetScheduleMessage, error) {
	if targetGUID == "" {
		return nil, common.Errorf(common.Invalid, "a target GUID is required")
	}
	if targetType == storepb.MetaType_UNSPECIFIED {
		return nil, common.Errorf(common.Invalid, "a target type is required")
	}

	message, err := scanQualityTargetSchedule(s.GetDB().QueryRowContext(ctx, `
		SELECT `+qualityTargetScheduleColumns+`
		FROM quality_target_schedule
		WHERE target_guid = $1 AND target_type = $2`, targetGUID, int16(targetType)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality target schedule")
	}
	return message, nil
}

// ensureQualityTargetSchedule writes one target's schedule row inside an
// existing transaction, creating it with the given interval when absent. The row
// starts due one interval from now only when the target already has an enabled
// rule, so a paused rule never schedules its target.
func ensureQualityTargetSchedule(ctx context.Context, tx *sql.Tx, targetGUID string, targetType storepb.MetaType, interval time.Duration, now time.Time) (*QualityTargetScheduleMessage, error) {
	if targetGUID == "" {
		return nil, common.Errorf(common.Invalid, "a target GUID is required")
	}
	if targetType == storepb.MetaType_UNSPECIFIED {
		return nil, common.Errorf(common.Invalid, "a target type is required")
	}
	if interval < time.Second {
		return nil, common.Errorf(common.Invalid, "a quality target schedule interval is required")
	}

	message, err := scanQualityTargetSchedule(tx.QueryRowContext(ctx, `
		INSERT INTO quality_target_schedule (target_guid, target_type, schedule_interval_seconds, next_run_at)
		SELECT $1::text, $2::int2, $3::int4,
			CASE WHEN EXISTS (
				SELECT 1 FROM quality_rule r
				WHERE r.target_guid = $1 AND r.target_type = $2
					AND r.state = $4 AND r.deleted = FALSE
			) THEN $5::timestamptz ELSE NULL END
		ON CONFLICT (target_guid, target_type) DO UPDATE
		SET next_run_at = COALESCE(quality_target_schedule.next_run_at, EXCLUDED.next_run_at),
			updated_at = NOW()
		RETURNING `+qualityTargetScheduleColumns,
		targetGUID,
		int16(targetType),
		int32(interval/time.Second),
		storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
		now.Add(interval),
	))
	if err != nil {
		return nil, errors.Wrap(err, "failed to ensure the quality target schedule")
	}
	return message, nil
}

// EnsureQualityTargetSchedule creates the target's schedule row if it is absent,
// and schedules a target whose checks are all paused when it gains an enabled
// rule. An existing future next_run_at is never moved by a new rule: the target
// keeps its cadence and the new check joins the next slot.
func (s *Store) EnsureQualityTargetSchedule(ctx context.Context, targetGUID string, targetType storepb.MetaType, interval time.Duration, now time.Time) (*QualityTargetScheduleMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin the quality target schedule transaction")
	}
	defer tx.Rollback()

	message, err := ensureQualityTargetSchedule(ctx, tx, targetGUID, targetType, interval, now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit the quality target schedule transaction")
	}
	return message, nil
}

// UpdateQualityTargetSchedule replaces one target's interval and reschedules it
// from now. A target with no enabled rule keeps a NULL next_run_at: its interval
// is remembered until a check is enabled again. The target must already have a
// schedule row, which every target with rules has.
func (s *Store) UpdateQualityTargetSchedule(ctx context.Context, targetGUID string, targetType storepb.MetaType, interval time.Duration, now time.Time) (*QualityTargetScheduleMessage, error) {
	if targetGUID == "" {
		return nil, common.Errorf(common.Invalid, "a target GUID is required")
	}
	if targetType == storepb.MetaType_UNSPECIFIED {
		return nil, common.Errorf(common.Invalid, "a target type is required")
	}
	if interval < time.Second {
		return nil, common.Errorf(common.Invalid, "a quality target schedule interval is required")
	}

	message, err := scanQualityTargetSchedule(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_target_schedule s
		SET schedule_interval_seconds = $3::int4,
			next_run_at = CASE WHEN EXISTS (
				SELECT 1 FROM quality_rule r
				WHERE r.target_guid = s.target_guid AND r.target_type = s.target_type
					AND r.state = $4 AND r.deleted = FALSE
			) THEN $5::timestamptz ELSE NULL END,
			updated_at = NOW()
		WHERE s.target_guid = $1 AND s.target_type = $2
		RETURNING `+qualityTargetScheduleColumns,
		targetGUID,
		int16(targetType),
		int32(interval/time.Second),
		storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
		now.Add(interval),
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.NotFound, "target %q has no quality schedule", targetGUID)
		}
		return nil, errors.Wrap(err, "failed to update the quality target schedule")
	}
	return message, nil
}

// SyncQualityTargetSchedule reconciles one target's next run with its current
// enabled rules: a target whose last enabled rule was paused or archived gets a
// NULL next_run_at, and a target whose first rule is enabled again is scheduled
// one interval from now instead of firing immediately. A missing schedule row is
// not an error here: a target can lose its last rule before the row exists.
func (s *Store) SyncQualityTargetSchedule(ctx context.Context, targetGUID string, targetType storepb.MetaType, now time.Time) error {
	if targetGUID == "" {
		return common.Errorf(common.Invalid, "a target GUID is required")
	}
	if targetType == storepb.MetaType_UNSPECIFIED {
		return common.Errorf(common.Invalid, "a target type is required")
	}

	if _, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_target_schedule s
		SET next_run_at = CASE WHEN EXISTS (
				SELECT 1 FROM quality_rule r
				WHERE r.target_guid = s.target_guid AND r.target_type = s.target_type
					AND r.state = $3 AND r.deleted = FALSE
			) THEN COALESCE(s.next_run_at, $4::timestamptz + MAKE_INTERVAL(secs => s.schedule_interval_seconds))
			ELSE NULL END,
			updated_at = NOW()
		WHERE s.target_guid = $1 AND s.target_type = $2`,
		targetGUID,
		int16(targetType),
		storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
		now,
	); err != nil {
		return errors.Wrap(err, "failed to sync the quality target schedule")
	}
	return nil
}

// AdvanceQualityTargetNextRun stamps when the scheduler next considers the
// target due. Only next_run_at changes: scheduling is bookkeeping, so it must not
// bump the row's updated_at (which the target directory reads as "configured").
func (s *Store) AdvanceQualityTargetNextRun(ctx context.Context, targetGUID string, targetType storepb.MetaType, nextRunAt time.Time) error {
	if targetGUID == "" {
		return common.Errorf(common.Invalid, "a target GUID is required")
	}
	if targetType == storepb.MetaType_UNSPECIFIED {
		return common.Errorf(common.Invalid, "a target type is required")
	}
	if nextRunAt.IsZero() {
		return common.Errorf(common.Invalid, "a next run time is required")
	}

	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_target_schedule
		SET next_run_at = $3
		WHERE target_guid = $1 AND target_type = $2`,
		targetGUID, int16(targetType), nextRunAt)
	if err != nil {
		return errors.Wrap(err, "failed to advance the quality target next run")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected for the quality target next run update")
	}
	if affected == 0 {
		return common.Errorf(common.NotFound, "target %q has no quality schedule", targetGUID)
	}
	return nil
}

// DueQualityTarget is one scheduled target that has come due: its cadence, the
// time it came due, and every enabled rule the batch must execute. The rules are
// carried with the target because target-level scheduling runs them together.
type DueQualityTarget struct {
	TargetGUID       string
	TargetType       storepb.MetaType
	ScheduleInterval time.Duration
	NextRunAt        time.Time
	Rules            []*QualityRuleMessage
}

// dueQualityTargetRow is one target's due aggregate before its rules are read:
// its cadence, when it came due and how many enabled rules it will execute.
type dueQualityTargetRow struct {
	guid      string
	metaType  storepb.MetaType
	interval  time.Duration
	firstDue  time.Time
	firstID   int64
	ruleCount int32
}

// selectDueTargets takes whole targets in first-due order until the rule budget
// is met. The target that crosses the budget is included whole, so the result may
// exceed the budget by at most that target's enabled-rule count; a target's
// checks never straddle two scheduler ticks because of the budget.
func selectDueTargets(targets []dueQualityTargetRow, budget int) []dueQualityTargetRow {
	if budget <= 0 || len(targets) == 0 {
		return nil
	}
	selected := make([]dueQualityTargetRow, 0, len(targets))
	rules := 0
	for _, target := range targets {
		selected = append(selected, target)
		rules += int(target.ruleCount)
		if rules >= budget {
			break
		}
	}
	return selected
}

// FindDueQualityTargets returns the scheduled targets whose next_run_at has
// arrived, oldest due first, together with every enabled rule of each target.
// limit is a work budget in rules: whole targets are taken until the budget is
// met, so a target's checks are never cut by the budget.
func (s *Store) FindDueQualityTargets(ctx context.Context, now time.Time, limit int) ([]*DueQualityTarget, error) {
	if limit <= 0 {
		return nil, common.Errorf(common.Invalid, "a positive due-rule limit is required")
	}

	targetRows, err := s.GetDB().QueryContext(ctx, `
		SELECT s.target_guid, s.target_type, s.schedule_interval_seconds, s.next_run_at,
			(MIN(r.id))::BIGINT, (COUNT(r.id))::INT
		FROM quality_target_schedule s
		JOIN quality_rule r
			ON r.target_guid = s.target_guid AND r.target_type = s.target_type
			AND r.state = $1 AND r.deleted = FALSE
		WHERE s.next_run_at IS NOT NULL AND s.next_run_at <= $2
		GROUP BY s.target_guid, s.target_type, s.schedule_interval_seconds, s.next_run_at
		ORDER BY s.next_run_at ASC, s.target_guid ASC`,
		storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED, now)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query due quality target schedules")
	}
	defer targetRows.Close()

	rows := make([]dueQualityTargetRow, 0)
	for targetRows.Next() {
		var target dueQualityTargetRow
		var (
			targetType      int16
			intervalSeconds int32
		)
		if err := targetRows.Scan(&target.guid, &targetType, &intervalSeconds, &target.firstDue, &target.firstID, &target.ruleCount); err != nil {
			return nil, errors.Wrap(err, "failed to scan a due quality target schedule")
		}
		target.metaType = storepb.MetaType(targetType)
		target.interval = time.Duration(intervalSeconds) * time.Second
		rows = append(rows, target)
	}
	if err := targetRows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for due quality target schedules")
	}

	selected := selectDueTargets(rows, limit)
	if len(selected) == 0 {
		return []*DueQualityTarget{}, nil
	}
	guids := make([]string, 0, len(selected))
	objectTypes := make([]int16, 0, len(selected))
	for _, target := range selected {
		guids = append(guids, target.guid)
		objectTypes = append(objectTypes, int16(target.metaType))
	}

	ruleRows, err := s.GetDB().QueryContext(ctx, `
		SELECT `+qualityRuleColumns+`
		FROM quality_rule r
		JOIN unnest($1::text[], $2::int2[]) AS t(guid, object_type)
			ON r.target_guid = t.guid AND r.target_type = t.object_type
		WHERE r.state = $3 AND r.deleted = FALSE
		ORDER BY r.target_guid ASC, r.target_type ASC, r.id ASC`,
		pq.Array(guids), pq.Array(objectTypes),
		storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query the due targets' quality rules")
	}
	defer ruleRows.Close()

	byTarget := make(map[targetKey][]*QualityRuleMessage, len(selected))
	for ruleRows.Next() {
		msg, err := scanQualityRule(ruleRows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a due target's quality rule")
		}
		key := targetKey{guid: msg.TargetGUID, metaType: msg.TargetType}
		byTarget[key] = append(byTarget[key], msg)
	}
	if err := ruleRows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for due targets' quality rules")
	}

	result := make([]*DueQualityTarget, 0, len(selected))
	for _, target := range selected {
		key := targetKey{guid: target.guid, metaType: target.metaType}
		result = append(result, &DueQualityTarget{
			TargetGUID:       target.guid,
			TargetType:       target.metaType,
			ScheduleInterval: target.interval,
			NextRunAt:        target.firstDue,
			Rules:            byTarget[key],
		})
	}
	return result, nil
}

// targetKey identifies one target: the GUID alone is not unique across object
// types.
type targetKey struct {
	guid     string
	metaType storepb.MetaType
}
