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
// rule, so a paused rule never schedules its target. When the row already exists,
// a NULL next_run_at is filled with now plus the row's own interval, not the
// caller's: a target with a customized cadence keeps that cadence when it is
// reactivated by a new check.
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
			) THEN $5::timestamptz + MAKE_INTERVAL(secs => $3::int4) ELSE NULL END
		ON CONFLICT (target_guid, target_type) DO UPDATE
		SET next_run_at = COALESCE(
				quality_target_schedule.next_run_at,
				CASE WHEN EXISTS (
					SELECT 1 FROM quality_rule r
					WHERE r.target_guid = quality_target_schedule.target_guid
						AND r.target_type = quality_target_schedule.target_type
						AND r.state = $4 AND r.deleted = FALSE
				) THEN $5::timestamptz + MAKE_INTERVAL(secs => quality_target_schedule.schedule_interval_seconds)
				ELSE NULL END
			),
			updated_at = NOW()
		RETURNING `+qualityTargetScheduleColumns,
		targetGUID,
		int16(targetType),
		int32(interval/time.Second),
		storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
		now,
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

// CountPendingQualityTargetRules returns how many enabled rules of the target
// still need an attempt in its current due cycle. A rule is pending until it has
// a completed scheduled execution since the cycle started or its abandoned
// attempts reached maxAttempts. Zero means the cycle is settled and the caller may
// advance the target's schedule.
func (s *Store) CountPendingQualityTargetRules(ctx context.Context, targetGUID string, targetType storepb.MetaType, maxAttempts int) (int, error) {
	if targetGUID == "" {
		return 0, common.Errorf(common.Invalid, "a target GUID is required")
	}
	if targetType == storepb.MetaType_UNSPECIFIED {
		return 0, common.Errorf(common.Invalid, "a target type is required")
	}
	if maxAttempts <= 0 {
		return 0, common.Errorf(common.Invalid, "a positive scheduled-attempt limit is required")
	}

	var pending int
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT (COUNT(*))::INT
		FROM quality_rule r
		JOIN quality_target_schedule s
			ON s.target_guid = r.target_guid AND s.target_type = r.target_type
		LEFT JOIN LATERAL (
			SELECT (COUNT(*))::INT AS count,
				BOOL_OR(e.state = $5) AS completed
			FROM quality_execution e
			WHERE e.rule_id = r.id
				AND e.target_guid = r.target_guid
				AND e.target_type = r.target_type
				AND e.trigger = $6
				AND e.started_at >= s.next_run_at
		) AS attempts ON TRUE
		WHERE s.target_guid = $1 AND s.target_type = $2
			AND s.next_run_at IS NOT NULL
			AND r.state = $3 AND r.deleted = FALSE
			AND NOT COALESCE(attempts.completed, FALSE)
			AND COALESCE(attempts.count, 0) < $4`,
		targetGUID,
		int16(targetType),
		storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
		maxAttempts,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED,
		storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED,
	).Scan(&pending)
	if err != nil {
		return 0, errors.Wrap(err, "failed to count the target's pending quality rules")
	}
	return pending, nil
}

// AdvanceDueQualityTargetNextRun advances a schedule only while it is still due.
// It is the runner's cycle-completion write: if a reader edited the interval to a
// future slot while the batch was executing, the update is a no-op instead of
// overwriting that edit. The target must still have an enabled rule, so an
// all-paused target is not given a new slot.
func (s *Store) AdvanceDueQualityTargetNextRun(ctx context.Context, targetGUID string, targetType storepb.MetaType, nextRunAt, now time.Time) (bool, error) {
	if targetGUID == "" {
		return false, common.Errorf(common.Invalid, "a target GUID is required")
	}
	if targetType == storepb.MetaType_UNSPECIFIED {
		return false, common.Errorf(common.Invalid, "a target type is required")
	}
	if nextRunAt.IsZero() || now.IsZero() {
		return false, common.Errorf(common.Invalid, "a next run time and now are required")
	}

	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_target_schedule s
		SET next_run_at = $3
		WHERE s.target_guid = $1 AND s.target_type = $2
			AND s.next_run_at IS NOT NULL AND s.next_run_at <= $4
			AND EXISTS (
				SELECT 1 FROM quality_rule r
				WHERE r.target_guid = s.target_guid AND r.target_type = s.target_type
					AND r.state = $5 AND r.deleted = FALSE
			)`,
		targetGUID, int16(targetType), nextRunAt, now,
		storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED)
	if err != nil {
		return false, errors.Wrap(err, "failed to advance the due quality target next run")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to get rows affected for the due quality target next run update")
	}
	return affected > 0, nil
}

// DueQualityRule is one rule the current target cycle still has to attempt, with
// the attempt number the planner must record on its execution row. A rule is
// pending until its current cycle has a completed attempted execution or its
// retry budget is exhausted.
type DueQualityRule struct {
	Rule    *QualityRuleMessage
	Attempt int32
}

// DueQualityTarget is one scheduled target that has come due: its cadence, the
// time it came due, and the enabled rules the current cycle still has to attempt.
// The rules are carried with the target because target-level scheduling runs them
// together.
type DueQualityTarget struct {
	TargetGUID       string
	TargetType       storepb.MetaType
	TargetPath       *storepb.QualityTargetPath
	ScheduleInterval time.Duration
	NextRunAt        time.Time
	Rules            []*DueQualityRule
}

// dueQualityTargetRow is one target's due aggregate before its rules are read:
// its cadence, when it came due and how many enabled rules it will execute.
type dueQualityTargetRow struct {
	guid      string
	metaType  storepb.MetaType
	interval  time.Duration
	firstDue  time.Time
	ruleCount int32
}

// selectDueTargets takes whole targets in first-due order until either the rule
// budget or the target budget is met. Both budgets exist because a target now
// becomes its own batch: the rule budget bounds work, the target budget bounds how
// many batches one tick creates. The target that crosses a budget is included
// whole, so a target's checks are never cut by a budget.
func selectDueTargets(targets []dueQualityTargetRow, ruleBudget, targetBudget int) []dueQualityTargetRow {
	if ruleBudget <= 0 || targetBudget <= 0 || len(targets) == 0 {
		return nil
	}
	selected := make([]dueQualityTargetRow, 0, len(targets))
	rules := 0
	for _, target := range targets {
		selected = append(selected, target)
		rules += int(target.ruleCount)
		if rules >= ruleBudget || len(selected) >= targetBudget {
			break
		}
	}
	return selected
}

// dueQualityTargetsStatement selects the targets one tick may execute. The LIMIT
// is what bounds the tick's read work after a backlog: the Go-side budget then
// trims this prefix further, but it never has to aggregate every due target.
const dueQualityTargetsStatement = `
	SELECT s.target_guid, s.target_type, s.schedule_interval_seconds, s.next_run_at,
		(COUNT(r.id))::INT
	FROM quality_target_schedule s
	JOIN quality_rule r
		ON r.target_guid = s.target_guid AND r.target_type = s.target_type
		AND r.state = $1 AND r.deleted = FALSE
	WHERE s.next_run_at IS NOT NULL AND s.next_run_at <= $2
	GROUP BY s.target_guid, s.target_type, s.schedule_interval_seconds, s.next_run_at
	ORDER BY s.next_run_at ASC, s.target_guid ASC
	LIMIT $3`

// dueQualityTargetRulesStatement reads the pending rules of the selected targets.
// It re-checks s.next_run_at <= $7 so a target the user postponed between the two
// statements is not executed against its stale due snapshot.
var dueQualityTargetRulesStatement = `
	SELECT ` + qualifyQualityRuleColumns("r") + `, COALESCE(attempts.count, 0)::INT
	FROM quality_rule r
	JOIN unnest($1::text[], $2::int2[]) AS t(guid, object_type)
		ON r.target_guid = t.guid AND r.target_type = t.object_type
	JOIN quality_target_schedule s
		ON s.target_guid = r.target_guid AND s.target_type = r.target_type
	LEFT JOIN LATERAL (
		SELECT (COUNT(*))::INT AS count,
			BOOL_OR(e.state = $4) AS completed
		FROM quality_execution e
		WHERE e.rule_id = r.id
			AND e.target_guid = r.target_guid
			AND e.target_type = r.target_type
			AND e.trigger = $5
			AND e.started_at >= s.next_run_at
	) AS attempts ON TRUE
	WHERE r.state = $3 AND r.deleted = FALSE
		AND s.next_run_at <= $7
		AND NOT COALESCE(attempts.completed, FALSE)
		AND COALESCE(attempts.count, 0) < $6
	ORDER BY r.target_guid ASC, r.target_type ASC, r.id ASC`

// FindDueQualityTargets returns the scheduled targets whose next_run_at has
// arrived, oldest due first. Each target carries the enabled rules the current
// cycle still has to attempt: a rule with a completed execution since the cycle
// started is done, and a rule whose abandoned attempts reached maxAttempts is
// exhausted. A target can therefore come back with no rules; that means its cycle
// is settled and the caller only has to advance its schedule.
//
// ruleLimit and targetLimit are work budgets: the due-target query returns at most
// targetLimit rows and whole targets are then taken until either budget is met, so
// a target's checks are never cut by a budget.
func (s *Store) FindDueQualityTargets(ctx context.Context, now time.Time, ruleLimit, targetLimit, maxAttempts int) ([]*DueQualityTarget, error) {
	if ruleLimit <= 0 {
		return nil, common.Errorf(common.Invalid, "a positive due-rule limit is required")
	}
	if targetLimit <= 0 {
		return nil, common.Errorf(common.Invalid, "a positive due-target limit is required")
	}
	if maxAttempts <= 0 {
		return nil, common.Errorf(common.Invalid, "a positive scheduled-attempt limit is required")
	}

	targetRows, err := s.GetDB().QueryContext(ctx, dueQualityTargetsStatement,
		storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED, now, targetLimit)
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
		if err := targetRows.Scan(&target.guid, &targetType, &intervalSeconds, &target.firstDue, &target.ruleCount); err != nil {
			return nil, errors.Wrap(err, "failed to scan a due quality target schedule")
		}
		target.metaType = storepb.MetaType(targetType)
		target.interval = time.Duration(intervalSeconds) * time.Second
		rows = append(rows, target)
	}
	if err := targetRows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for due quality target schedules")
	}

	selected := selectDueTargets(rows, ruleLimit, targetLimit)
	if len(selected) == 0 {
		return []*DueQualityTarget{}, nil
	}
	guids := make([]string, 0, len(selected))
	objectTypes := make([]int16, 0, len(selected))
	for _, target := range selected {
		guids = append(guids, target.guid)
		objectTypes = append(objectTypes, int16(target.metaType))
	}

	ruleRows, err := s.GetDB().QueryContext(ctx, dueQualityTargetRulesStatement,
		pq.Array(guids), pq.Array(objectTypes),
		storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED,
		storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED,
		maxAttempts, now)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query the due targets' quality rules")
	}
	defer ruleRows.Close()

	byTarget := make(map[targetKey][]*DueQualityRule, len(selected))
	for ruleRows.Next() {
		var attempt int32
		msg, err := scanQualityRule(dueQualityRuleRow{rows: ruleRows, attempt: &attempt})
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a due target's quality rule")
		}
		key := targetKey{guid: msg.TargetGUID, metaType: msg.TargetType}
		byTarget[key] = append(byTarget[key], &DueQualityRule{Rule: msg, Attempt: attempt + 1})
	}
	if err := ruleRows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for due targets' quality rules")
	}

	result := make([]*DueQualityTarget, 0, len(selected))
	for _, target := range selected {
		key := targetKey{guid: target.guid, metaType: target.metaType}
		path := &storepb.QualityTargetPath{}
		if rules := byTarget[key]; len(rules) > 0 && rules[0].Rule.TargetPath != nil {
			path = rules[0].Rule.TargetPath
		}
		result = append(result, &DueQualityTarget{
			TargetGUID:       target.guid,
			TargetType:       target.metaType,
			TargetPath:       path,
			ScheduleInterval: target.interval,
			NextRunAt:        target.firstDue,
			Rules:            byTarget[key],
		})
	}
	return result, nil
}

// dueQualityRuleRow adapts *sql.Rows for scanQualityRule while the due query also
// returns how many scheduled attempts the rule already used in this cycle.
type dueQualityRuleRow struct {
	rows    *sql.Rows
	attempt *int32
}

func (r dueQualityRuleRow) Scan(dest ...any) error {
	return r.rows.Scan(append(dest, r.attempt)...)
}

// targetKey identifies one target: the GUID alone is not unique across object
// types.
type targetKey struct {
	guid     string
	metaType storepb.MetaType
}
