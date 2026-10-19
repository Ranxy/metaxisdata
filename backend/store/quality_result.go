package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualityRuleResultColumns is the projection every quality_rule_result query
// returns, in the order scanQualityRuleResult reads it.
const qualityRuleResultColumns = "id, run_id, rule_id, rule_revision, rule_engine_version, target_guid, target_type, status, observed_value, observed_kind, thresholds, failed_rows, total_rows, sampled, sample_rows, truncated, sample, executed_sql, error_code, error_message, duration_ms, started_at, finished_at"

// QualityRuleResultMessage is one rule's outcome in one run. Rows are
// insert-only: nothing in this file updates a stored result. A rule edit changes
// the rule's current revision, not the results already recorded, so a result
// always names the rule_revision and engine version that produced it.
type QualityRuleResultMessage struct {
	ID                int64
	RunID             int64
	RuleID            int64
	RuleRevision      int32
	RuleEngineVersion string
	TargetGUID        string
	TargetType        storepb.MetaType
	Status            storepb.QualityStatus
	// ObservedValue is nil when the check could not produce a value (ERROR or
	// NOT_EVALUATED); zero is a legitimate observed value.
	ObservedValue *float64
	ObservedKind  storepb.QualityObservedKind
	Thresholds    *storepb.QualityThresholds
	FailedRows    int64
	TotalRows     int64
	Sampled       bool
	SampleRows    int64
	Truncated     bool
	Sample        *storepb.QualityFailureSamples
	ExecutedSQL   string
	ErrorCode     string
	ErrorMessage  string
	DurationMS    int32
	StartedAt     time.Time
	FinishedAt    time.Time
}

// FindQualityRuleResultMessage is the query filter for rule results. Filters
// compose with AND; OrderAsc selects ascending started_at for callers that read
// a rule's history forwards.
type FindQualityRuleResultMessage struct {
	RunID      *int64
	RuleID     *int64
	Statuses   []storepb.QualityStatus
	TargetGUID *string
	// StartAt and EndAt bound started_at as the half-open interval
	// [StartAt, EndAt), so a boundary result is never returned twice.
	StartAt  *time.Time
	EndAt    *time.Time
	Limit    *int
	Offset   *int
	OrderAsc bool
}

// buildQualityRuleResultWhere renders the WHERE predicate and positional
// arguments for a result filter. It is a pure function so the predicate is
// unit-testable without a database; ordering and paging stay in the callers.
func buildQualityRuleResultWhere(find *FindQualityRuleResultMessage) (string, []any) {
	if find == nil {
		find = &FindQualityRuleResultMessage{}
	}
	conditions := []string{}
	args := []any{}
	placeholder := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	if find.RunID != nil {
		conditions = append(conditions, "run_id = "+placeholder(*find.RunID))
	}
	if find.RuleID != nil {
		conditions = append(conditions, "rule_id = "+placeholder(*find.RuleID))
	}
	if len(find.Statuses) > 0 {
		// The column is INT2 while lib/pq sends int4 arrays for []int32, so the
		// cast is explicit and lets PostgreSQL narrow the array.
		statuses := make([]int32, len(find.Statuses))
		for i, status := range find.Statuses {
			statuses[i] = int32(status)
		}
		conditions = append(conditions, "status = ANY("+placeholder(pq.Array(statuses))+"::int[])")
	}
	if find.TargetGUID != nil {
		conditions = append(conditions, "target_guid = "+placeholder(*find.TargetGUID))
	}
	if find.StartAt != nil {
		conditions = append(conditions, "started_at >= "+placeholder(*find.StartAt))
	}
	if find.EndAt != nil {
		conditions = append(conditions, "started_at < "+placeholder(*find.EndAt))
	}

	where := strings.Join(conditions, " AND ")
	if where == "" {
		where = "TRUE"
	}
	return where, args
}

// qualityRuleResultOrderClause renders the ORDER BY of a result list. The id
// breaks ties on started_at so a page boundary is deterministic; ascending order
// is for reading a rule's history forwards.
func qualityRuleResultOrderClause(orderAsc bool) string {
	if orderAsc {
		return " ORDER BY started_at ASC, id ASC"
	}
	return " ORDER BY started_at DESC, id DESC"
}

// marshalQualityThresholds renders the thresholds JSONB snapshot. A nil message
// stores "{}", matching the column default, so the column is never NULL.
func marshalQualityThresholds(thresholds *storepb.QualityThresholds) ([]byte, error) {
	if thresholds == nil {
		thresholds = &storepb.QualityThresholds{}
	}
	payload, err := protojson.Marshal(thresholds)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal quality result thresholds")
	}
	return payload, nil
}

// marshalQualityFailureSamples renders the sample JSONB column. A nil message
// stores "{}", matching the column default.
func marshalQualityFailureSamples(sample *storepb.QualityFailureSamples) ([]byte, error) {
	if sample == nil {
		sample = &storepb.QualityFailureSamples{}
	}
	payload, err := protojson.Marshal(sample)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal quality failure samples")
	}
	return payload, nil
}

// scanQualityRuleResult reads one result row. observed_value is nullable, while
// the thresholds and sample JSONB snapshots are unmarshalled with the protojson
// wrapper so a row written by an older engine version stays readable.
func scanQualityRuleResult(scanner interface{ Scan(...any) error }) (*QualityRuleResultMessage, error) {
	var (
		msg           QualityRuleResultMessage
		observedValue sql.NullFloat64
		thresholdsRaw []byte
		sampleRaw     []byte
	)
	err := scanner.Scan(
		&msg.ID,
		&msg.RunID,
		&msg.RuleID,
		&msg.RuleRevision,
		&msg.RuleEngineVersion,
		&msg.TargetGUID,
		&msg.TargetType,
		&msg.Status,
		&observedValue,
		&msg.ObservedKind,
		&thresholdsRaw,
		&msg.FailedRows,
		&msg.TotalRows,
		&msg.Sampled,
		&msg.SampleRows,
		&msg.Truncated,
		&sampleRaw,
		&msg.ExecutedSQL,
		&msg.ErrorCode,
		&msg.ErrorMessage,
		&msg.DurationMS,
		&msg.StartedAt,
		&msg.FinishedAt,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to scan quality rule result")
	}

	if observedValue.Valid {
		msg.ObservedValue = &observedValue.Float64
	}
	msg.Thresholds = &storepb.QualityThresholds{}
	if len(thresholdsRaw) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(thresholdsRaw, msg.Thresholds); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal quality result thresholds")
		}
	}
	msg.Sample = &storepb.QualityFailureSamples{}
	if len(sampleRaw) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(sampleRaw, msg.Sample); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal quality failure samples")
		}
	}
	return &msg, nil
}

// qualityRuleResultInsertStatement appends results in one round trip. The INSERT
// column list and the UNNEST argument order are the same contract: argument N
// fills column N. The casts are explicit because lib/pq sends []int32 as int4
// and a bare parameter would leave PostgreSQL to guess the array type; the int2
// columns (target_type, status, observed_kind) are narrowed here on purpose.
const qualityRuleResultInsertStatement = `
	INSERT INTO quality_rule_result (
		run_id, rule_id, rule_revision, rule_engine_version, target_guid, target_type,
		status, observed_value, observed_kind, thresholds, failed_rows, total_rows,
		sampled, sample_rows, truncated, sample, executed_sql, error_code,
		error_message, duration_ms, started_at, finished_at
	)
	SELECT * FROM UNNEST(
		$1::bigint[], $2::bigint[], $3::int[], $4::text[], $5::text[], $6::int2[],
		$7::int2[], $8::double precision[], $9::int2[], $10::jsonb[], $11::bigint[],
		$12::bigint[], $13::boolean[], $14::bigint[], $15::boolean[], $16::jsonb[],
		$17::text[], $18::text[], $19::text[], $20::int[], $21::timestamptz[],
		$22::timestamptz[]
	)
`

// BatchCreateQualityRuleResults appends one run's per-rule results. There is no
// update path: a result is a fact about an execution, so a retry inserts a new
// row (or a retried run is refused at the run level) rather than rewriting the
// old one. An empty batch is a no-op and never touches the database.
func (s *Store) BatchCreateQualityRuleResults(ctx context.Context, results []*QualityRuleResultMessage) error {
	if len(results) == 0 {
		return nil
	}

	runIDs := make([]int64, len(results))
	ruleIDs := make([]int64, len(results))
	ruleRevisions := make([]int32, len(results))
	ruleEngineVersions := make([]string, len(results))
	targetGUIDs := make([]string, len(results))
	targetTypes := make([]int32, len(results))
	statuses := make([]int32, len(results))
	observedValues := make([]sql.NullFloat64, len(results))
	observedKinds := make([]int32, len(results))
	thresholds := make([]string, len(results))
	failedRows := make([]int64, len(results))
	totalRows := make([]int64, len(results))
	sampled := make([]bool, len(results))
	sampleRows := make([]int64, len(results))
	truncated := make([]bool, len(results))
	samples := make([]string, len(results))
	executedSQLs := make([]string, len(results))
	errorCodes := make([]string, len(results))
	errorMessages := make([]string, len(results))
	durationsMS := make([]int32, len(results))
	startedAts := make([]time.Time, len(results))
	finishedAts := make([]time.Time, len(results))

	for i, result := range results {
		if result == nil {
			return common.Errorf(common.Invalid, "quality rule result %d is nil", i)
		}
		runIDs[i] = result.RunID
		ruleIDs[i] = result.RuleID
		ruleRevisions[i] = result.RuleRevision
		ruleEngineVersions[i] = result.RuleEngineVersion
		targetGUIDs[i] = result.TargetGUID
		targetTypes[i] = int32(result.TargetType)
		statuses[i] = int32(result.Status)
		if result.ObservedValue != nil {
			observedValues[i] = sql.NullFloat64{Float64: *result.ObservedValue, Valid: true}
		}
		observedKinds[i] = int32(result.ObservedKind)
		thresholdsRaw, err := marshalQualityThresholds(result.Thresholds)
		if err != nil {
			return err
		}
		thresholds[i] = string(thresholdsRaw)
		failedRows[i] = result.FailedRows
		totalRows[i] = result.TotalRows
		sampled[i] = result.Sampled
		sampleRows[i] = result.SampleRows
		truncated[i] = result.Truncated
		sampleRaw, err := marshalQualityFailureSamples(result.Sample)
		if err != nil {
			return err
		}
		samples[i] = string(sampleRaw)
		executedSQLs[i] = result.ExecutedSQL
		errorCodes[i] = result.ErrorCode
		errorMessages[i] = result.ErrorMessage
		durationsMS[i] = result.DurationMS
		startedAts[i] = result.StartedAt
		finishedAts[i] = result.FinishedAt
	}

	_, err := s.GetDB().ExecContext(ctx, qualityRuleResultInsertStatement,
		pq.Array(runIDs),
		pq.Array(ruleIDs),
		pq.Array(ruleRevisions),
		pq.Array(ruleEngineVersions),
		pq.Array(targetGUIDs),
		pq.Array(targetTypes),
		pq.Array(statuses),
		pq.Array(observedValues),
		pq.Array(observedKinds),
		pq.Array(thresholds),
		pq.Array(failedRows),
		pq.Array(totalRows),
		pq.Array(sampled),
		pq.Array(sampleRows),
		pq.Array(truncated),
		pq.Array(samples),
		pq.Array(executedSQLs),
		pq.Array(errorCodes),
		pq.Array(errorMessages),
		pq.Array(durationsMS),
		pq.Array(startedAts),
		pq.Array(finishedAts),
	)
	if err != nil {
		return errors.Wrap(err, "failed to create quality rule results")
	}
	return nil
}

// scanQualityRuleResults drains a result set into messages. Every list in this
// file reads the same projection, so they share one scan loop.
func scanQualityRuleResults(rows *sql.Rows) ([]*QualityRuleResultMessage, error) {
	defer rows.Close()

	var results []*QualityRuleResultMessage
	for rows.Next() {
		result, err := scanQualityRuleResult(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate quality rule results")
	}
	return results, nil
}

// ListQualityRuleResults lists results matching the filter. Descending
// started_at is the default because the caller that shows a run's results reads
// the newest first.
func (s *Store) ListQualityRuleResults(ctx context.Context, find *FindQualityRuleResultMessage) ([]*QualityRuleResultMessage, error) {
	where, args := buildQualityRuleResultWhere(find)
	orderAsc := find != nil && find.OrderAsc
	query := `SELECT ` + qualityRuleResultColumns + ` FROM quality_rule_result WHERE ` + where + qualityRuleResultOrderClause(orderAsc)

	if find != nil && find.Limit != nil {
		args = append(args, max(*find.Limit, 0))
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if find != nil && find.Offset != nil {
		args = append(args, max(*find.Offset, 0))
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list quality rule results")
	}
	return scanQualityRuleResults(rows)
}

// qualityRuleResultByIDStatement reads one result by its row id. A result's
// public resource name carries this id rather than a resource_id, because the
// table is insert-only and has no public identity column of its own.
const qualityRuleResultByIDStatement = `
	SELECT ` + qualityRuleResultColumns + `
	FROM quality_rule_result
	WHERE id = $1
`

// GetQualityRuleResult returns one result by row id, or (nil, nil) when no such
// result exists. The id is validated before any query so a malformed name fails
// without touching the database.
func (s *Store) GetQualityRuleResult(ctx context.Context, id int64) (*QualityRuleResultMessage, error) {
	if id <= 0 {
		return nil, common.Errorf(common.Invalid, "quality rule result id is required")
	}
	result, err := scanQualityRuleResult(s.GetDB().QueryRowContext(ctx, qualityRuleResultByIDStatement, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to get the quality rule result")
	}
	return result, nil
}

// ListRecentQualityRuleResults returns the rule's newest limit results. The
// row_count_change rule type reads its baseline through this method.
func (s *Store) ListRecentQualityRuleResults(ctx context.Context, ruleID int64, limit int) ([]*QualityRuleResultMessage, error) {
	if ruleID <= 0 {
		return nil, common.Errorf(common.Invalid, "quality rule id is required")
	}
	if limit <= 0 {
		return nil, common.Errorf(common.Invalid, "quality result limit must be positive")
	}

	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT `+qualityRuleResultColumns+`
		FROM quality_rule_result
		WHERE rule_id = $1
		ORDER BY started_at DESC, id DESC
		LIMIT $2
	`, ruleID, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list recent quality rule results")
	}
	return scanQualityRuleResults(rows)
}

// qualityRuleResultLatestStatement selects the newest result of each rule in one
// statement. DISTINCT ON (rule_id) with the matching ORDER BY prefix is what
// keeps this one index scan per rule instead of one query per rule.
const qualityRuleResultLatestStatement = `
	SELECT DISTINCT ON (rule_id) ` + qualityRuleResultColumns + `
	FROM quality_rule_result
	WHERE rule_id = ANY($1::bigint[])
	ORDER BY rule_id, started_at DESC, id DESC
`

// ListLatestQualityRuleResults returns each rule's newest result, keyed by rule
// id. Rules that have no result are absent from the map. An empty input is a
// no-op and never touches the database.
func (s *Store) ListLatestQualityRuleResults(ctx context.Context, ruleIDs []int64) (map[int64]*QualityRuleResultMessage, error) {
	latest := make(map[int64]*QualityRuleResultMessage, len(ruleIDs))
	if len(ruleIDs) == 0 {
		return latest, nil
	}

	rows, err := s.GetDB().QueryContext(ctx, qualityRuleResultLatestStatement, pq.Array(ruleIDs))
	if err != nil {
		return nil, errors.Wrap(err, "failed to list latest quality rule results")
	}
	results, err := scanQualityRuleResults(rows)
	if err != nil {
		return nil, err
	}
	for _, result := range results {
		latest[result.RuleID] = result
	}
	return latest, nil
}
