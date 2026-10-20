package dataquality

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

// Executor runs one compiled rule against a read-only queryer and turns its rows
// into an Observation. It owns no state and no connection: the caller opens the
// read-only driver, hands it in, and classifies whatever error comes back with
// ClassifyError.
type Executor struct{}

// NewExecutor returns an executor.
func NewExecutor() *Executor {
	return &Executor{}
}

// Execute compiles the rule, runs its aggregate statement through the read-only
// queryer, and interprets the result columns into an Observation. A CEL rule is
// the exception: its statement returns a sample of rows and the predicate runs in
// this process. It optionally runs one more bounded query for the failed-row
// sample.
//
// The rule's config must already be valid: validation is the save/dry-run path's
// job, so a config that fails validation here is a defect rather than a user
// error. A failed aggregate returns an error (classified by ClassifyError); a
// failed sample does not, because the observation is the verdict and the sample is
// context — with one exception: a cancelled context is returned, because the
// caller asked to stop.
func (*Executor) Execute(ctx context.Context, queryer db.ReadOnlyQueryer, resolution *Resolution, rule RuleSnapshot, opts ExecOptions) (*Execution, error) {
	if resolution == nil {
		return nil, common.Errorf(common.Internal, "a resolved target is required to execute a rule")
	}
	if rule.Config == nil {
		return nil, invalidConfig("the rule config is required")
	}
	if err := validateExecutionMetric(rule.Type, rule.Config); err != nil {
		return nil, err
	}
	// A metadata rule reads the registry, not the target database: it needs no
	// queryer and no SQL. This branch runs before the queryer requirement so a
	// caller without a read-only data source can still execute it.
	if IsMetadataRule(rule.Type) {
		return executeMetadataRule(resolution, rule, opts)
	}
	if queryer == nil {
		return nil, common.Errorf(common.Internal, "a read-only queryer is required to execute a rule")
	}

	dialect, err := DialectFor(resolution.Target.Engine)
	if err != nil {
		// Validation rejects an engine this build cannot compile for, so reaching
		// here means the stored rule and this build disagree. Invalid is the code
		// the runner maps to SKIPPED(UNSUPPORTED) rather than ERROR.
		return nil, common.Errorf(common.Invalid, "%s", err)
	}
	filter, err := ParseFilter(rule.Config.GetFilter())
	if err != nil {
		return nil, err
	}
	input := CompileInput{
		Dialect: dialect,
		Target:  resolution.Target,
		Type:    rule.Type,
		Config:  rule.Config,
		Filter:  filter,
	}
	compiled, err := Compile(input)
	if err != nil {
		return nil, err
	}
	if rule.Type == storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION {
		// A CEL rule reads a bounded sample of rows and evaluates the predicate in
		// this process; the query is not an aggregate and its rows are the input.
		return executeCELRule(ctx, queryer, resolution, rule, opts, compiled)
	}
	if compiled.Kind != QueryAggregate {
		return nil, badData("the compiler returned query kind %d for a rule that is observed by an aggregate", compiled.Kind)
	}

	// An aggregate observation is exactly one row; asking for one lets the
	// scanner stop instead of materializing a second row.
	result, err := queryer.QueryReadOnly(ctx, compiled.SQL, compiled.Args, db.QueryOptions{
		MaxRows:  1,
		MaxBytes: opts.MaxBytes,
		Timeout:  opts.QueryTimeout,
	})
	if err != nil {
		return nil, err
	}
	observation, err := observationFromAggregate(rule.Type, compiled, result)
	if err != nil {
		return nil, err
	}
	execution := &Execution{
		Observation: observation,
		ExecutedSQL: truncateExecutedSQL(compiled.SQL),
		Truncated:   result.Truncated,
	}
	execution.Observation.Truncated = result.Truncated

	if opts.StoreSamples {
		if err := addSamples(ctx, queryer, input, execution, opts); err != nil {
			return nil, err
		}
	}
	return execution, nil
}

// addSamples fetches the failed rows and attaches them to the execution. A rule
// with no failed rows, a rule type whose failure is a measurement, and a failed
// sample query all leave Samples nil: the sample is additional context, never the
// verdict.
func addSamples(ctx context.Context, queryer db.ReadOnlyQueryer, input CompileInput, execution *Execution, opts ExecOptions) error {
	sampleLimit := normalizeSampleLimit(opts.SampleLimit)
	sample, err := CompileFailureRows(input, sampleLimit)
	if err != nil {
		// A rule type that cannot produce a sample is a compile-time defect, not a
		// transient failure, and CompileFailureRows returns an empty statement for
		// the types that legitimately have none.
		return err
	}
	if sample.SQL == "" || execution.Observation.FailedRows <= 0 {
		return nil
	}
	sampleResult, err := queryer.QueryReadOnly(ctx, sample.SQL, sample.Args, db.QueryOptions{
		MaxRows:  sampleLimit,
		MaxBytes: opts.MaxBytes,
		Timeout:  opts.QueryTimeout,
	})
	if err != nil {
		// A failed sample is context, not the verdict: the observation stands. The
		// exception is cancellation, which is the caller asking to stop.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
	} else {
		execution.Samples = SamplesFromRows(sampleResult.Columns, sampleResult.Rows, sampleLimit)
		if execution.Samples != nil {
			execution.Observation.SampleRows = int64(len(execution.Samples.GetSamples()))
		}
		execution.Truncated = execution.Truncated || sampleResult.Truncated
		execution.Observation.Truncated = execution.Truncated
	}
	return nil
}

// executeCELRule runs one CEL predicate over a bounded sample of the target's
// rows. The sample query returns only the columns the expression reads, the
// predicate runs in this process, and the observation is the count of rows that
// did not satisfy it.
//
// The sample is all the rule ever sees, so the result records whether it was the
// whole filtered table (fewer rows than the cap) or a sample (the cap was filled),
// and how many rows were read.
func executeCELRule(ctx context.Context, queryer db.ReadOnlyQueryer, resolution *Resolution, rule RuleSnapshot, opts ExecOptions, compiled CompiledQuery) (*Execution, error) {
	config := rule.Config.GetCelExpression()
	if config == nil {
		return nil, invalidConfig("the CEL expression config is missing")
	}
	columns, err := celColumns(resolution.Target, config.GetColumns())
	if err != nil {
		return nil, err
	}
	sampleRows, err := celSampleRows(config.GetSampleRows())
	if err != nil {
		return nil, err
	}
	program, err := CompileCEL(columns, config.GetExpression())
	if err != nil {
		return nil, err
	}
	if compiled.Kind != QuerySample {
		return nil, badData("the compiler returned query kind %d for a CEL rule, expect a sample query", compiled.Kind)
	}

	// MaxRows is the cap itself, not a page size: only by asking for exactly
	// sample_rows can the executor tell a capped sample from a complete scan.
	result, err := queryer.QueryReadOnly(ctx, compiled.SQL, compiled.Args, db.QueryOptions{
		MaxRows:  sampleRows,
		MaxBytes: opts.MaxBytes,
		Timeout:  opts.QueryTimeout,
	})
	if err != nil {
		return nil, err
	}
	if err := requireSampleColumns(result.Columns, compiled.Columns); err != nil {
		return nil, err
	}

	sampleLimit := 0
	if opts.StoreSamples {
		sampleLimit = normalizeSampleLimit(opts.SampleLimit)
	}
	violations, evaluated, failingRows, err := evaluateCELRows(program, columns, result.Rows, sampleLimit)
	if err != nil {
		return nil, err
	}

	rowsRead := int64(len(result.Rows))
	sampled := rowsRead == int64(sampleRows)
	observation := Observation{
		Observed:     float64(violations),
		ObservedKind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
		FailedRows:   violations,
		TotalRows:    evaluated,
		Sampled:      sampled,
		SampleRows:   rowsRead,
		Truncated:    result.Truncated,
	}
	execution := &Execution{
		Observation: observation,
		ExecutedSQL: truncateExecutedSQL(compiled.SQL),
		Sampled:     sampled,
		Truncated:   result.Truncated,
	}
	if sampleLimit > 0 && len(failingRows) > 0 {
		// The failed-row sample is the rows the predicate rejected, already in
		// memory: no second statement is needed for a CEL rule.
		execution.Samples = SamplesFromRows(compiled.Columns, failingRows, sampleLimit)
	}
	return execution, nil
}

// requireSampleColumns checks that the sample returned the columns the compiled
// statement selected, in order. Reading them by position is only safe when the
// two lists agree.
func requireSampleColumns(columns []string, expected []string) error {
	if len(columns) != len(expected) {
		return badData("the sample returned %d columns, expect %d", len(columns), len(expected))
	}
	for index, name := range expected {
		if !strings.EqualFold(strings.TrimSpace(columns[index]), name) {
			return badData("the sample column %d is %q, expect %q", index, columns[index], name)
		}
	}
	return nil
}

// validateExecutionMetric rejects a percentage threshold on a rule whose
// observation has no total to divide by. Validation refuses the combination when
// the rule is saved; this is the run-time backstop for a stored rule that this
// build judges differently.
func validateExecutionMetric(ruleType storepb.QualityRuleType, config *storepb.QualityRuleConfig) error {
	if thresholdMetric(config) != storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT {
		return nil
	}
	if ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT {
		// The drift snapshot is a fixed list of expected columns, not a row set; a
		// percentage of it is not a meaningful threshold.
		return invalidConfig("schema_drift measures a count of drift items; threshold_metric PERCENT is not supported")
	}
	if ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE ||
		ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE {
		// These thresholds bound an absolute value (an interval edge or a tolerated
		// difference), not a fraction of rows.
		return invalidConfig("%s thresholds are absolute values; threshold_metric PERCENT is not supported", ruleType)
	}
	switch ruleType {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION:
		// These observations carry a denominator, so the evaluator can compute the
		// percentage. For a CEL rule it is the number of sampled rows the expression
		// evaluated. null_percent ignores the shared metric (its own config decides
		// count vs percentage) but still has a total, so it is accepted here rather
		// than refused for a field it does not read.
		return nil
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED:
		return invalidConfig("rule type %s has no total rows for a percentage threshold", ruleType)
	default:
		return invalidConfig("rule type %s has no total rows for a percentage threshold", ruleType)
	}
}

// observationFromAggregate interprets the aggregate row. The compiled statement
// declares the columns it returns in CompiledQuery.Columns; a result that does not
// contain them, a value of the wrong type, or a row count other than one is
// BAD_DATA — never a silent zero, because a zero reads as a passing check.
func observationFromAggregate(ruleType storepb.QualityRuleType, compiled CompiledQuery, result *db.QueryResult) (Observation, error) {
	if result == nil {
		return Observation{}, badData("the aggregate returned no result")
	}
	if len(result.Rows) != 1 {
		return Observation{}, badData("the aggregate returned %d rows, expect 1", len(result.Rows))
	}
	indexes, err := resultColumnIndexes(result.Columns, compiled.Columns)
	if err != nil {
		return Observation{}, err
	}

	switch ruleType {
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE:
		total, err := aggregateInt(result, indexes, "total_rows")
		if err != nil {
			return Observation{}, err
		}
		// row_count_change compares this count with the rule's last successful
		// observation. The baseline is the runner's to supply; the executor only
		// measures, so a change rule's observation is the count itself.
		return Observation{
			Observed:     float64(total),
			ObservedKind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
			TotalRows:    total,
		}, nil
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT:
		return countObservation(result, indexes)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION:
		// The grouped statement returns failed_rows, groups and total_rows; the
		// total is the whole filtered table, so a percentage threshold has the
		// same denominator as every other count rule. "groups" is informational
		// (how many distinct duplicated keys there are) and has no observation
		// field, so it is validated as part of the contract and not read.
		return countObservation(result, indexes)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN:
		return countObservation(result, indexes)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK:
		// The grouped statement names its columns total_groups / failed_groups; the
		// denominator is the group count, so a percentage threshold divides by it.
		total, err := aggregateInt(result, indexes, "total_groups")
		if err != nil {
			return Observation{}, err
		}
		failed, err := aggregateInt(result, indexes, "failed_groups")
		if err != nil {
			return Observation{}, err
		}
		if failed < 0 {
			return Observation{}, badData("failed_groups is negative (%d)", failed)
		}
		return Observation{
			Observed:     float64(failed),
			ObservedKind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
			FailedRows:   failed,
			TotalRows:    total,
		}, nil
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE:
		total, err := aggregateInt(result, indexes, "total_rows")
		if err != nil {
			return Observation{}, err
		}
		// An aggregate over no rows (or only NULLs) is SQL NULL, which is missing
		// data: the evaluator follows on_empty rather than judging it as zero.
		observed, present, err := aggregateFloat(result, indexes, "observed")
		if err != nil {
			return Observation{}, err
		}
		if !present {
			return Observation{
				ObservedKind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_NUMBER,
				ObservedNull: true,
				TotalRows:    total,
			}, nil
		}
		return Observation{
			Observed:     observed,
			ObservedKind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_NUMBER,
			TotalRows:    total,
		}, nil
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE:
		left, leftPresent, err := aggregateFloat(result, indexes, "left_value")
		if err != nil {
			return Observation{}, err
		}
		right, rightPresent, err := aggregateFloat(result, indexes, "right_value")
		if err != nil {
			return Observation{}, err
		}
		if !leftPresent || !rightPresent {
			return Observation{
				ObservedKind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_NUMBER,
				ObservedNull: true,
			}, nil
		}
		// The observed value is the absolute difference; the tolerance is the GT
		// threshold the evaluator applies to it.
		return Observation{
			Observed:     math.Abs(left - right),
			ObservedKind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_NUMBER,
		}, nil
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS:
		return freshnessObservation(result, indexes)
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL:
		// The user's statement defines a failing row itself, so there is no
		// denominator: failed_rows is the observation and total_rows stays zero.
		failed, err := aggregateInt(result, indexes, "failed_rows")
		if err != nil {
			return Observation{}, err
		}
		return Observation{
			Observed:     float64(failed),
			ObservedKind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
			FailedRows:   failed,
		}, nil
	case storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED:
		return Observation{}, badData("rule type %s has no aggregate observation", ruleType)
	default:
		return Observation{}, badData("rule type %s has no aggregate observation", ruleType)
	}
}

// countObservation interprets the shared two-column count shape: the filtered row
// total and the rows the failure condition selected. Both columns are required
// even when the threshold is a COUNT, because the result always reports "3 / 1200"
// to the reviewer.
func countObservation(result *db.QueryResult, indexes map[string]int) (Observation, error) {
	total, err := aggregateInt(result, indexes, "total_rows")
	if err != nil {
		return Observation{}, err
	}
	failed, err := aggregateInt(result, indexes, "failed_rows")
	if err != nil {
		return Observation{}, err
	}
	// SUM(CASE …) is NULL when the filtered set is empty, which means "no failed
	// rows", not "missing data".
	if failed < 0 {
		return Observation{}, badData("failed_rows is negative (%d)", failed)
	}
	return Observation{
		Observed:     float64(failed),
		ObservedKind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
		FailedRows:   failed,
		TotalRows:    total,
	}, nil
}

// freshnessObservation interprets the freshness aggregate: the row count, the
// newest timestamp and the lag the database computed from its own clock. A NULL
// timestamp means the column held no value; that is missing data, which the
// evaluator decides through on_empty, so the lag is left at zero.
func freshnessObservation(result *db.QueryResult, indexes map[string]int) (Observation, error) {
	total, err := aggregateInt(result, indexes, "total_rows")
	if err != nil {
		return Observation{}, err
	}
	maxTimestamp, err := aggregateTimestamp(result, indexes, "max_ts")
	if err != nil {
		return Observation{}, err
	}
	if maxTimestamp == nil {
		return Observation{
			ObservedKind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_SECONDS,
			TotalRows:    total,
		}, nil
	}
	lag, present, err := aggregateFloat(result, indexes, "lag_seconds")
	if err != nil {
		return Observation{}, err
	}
	if !present {
		// The database returned a timestamp but no lag: the two columns disagree
		// about the same row, so the result cannot be judged.
		return Observation{}, badData("lag_seconds is NULL while max_ts is set")
	}
	return Observation{
		Observed:     lag,
		ObservedKind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_SECONDS,
		TotalRows:    total,
		MaxTimestamp: maxTimestamp,
	}, nil
}

// resultColumnIndexes maps the expected column names to their positions in the
// result. Every column the compiler declared must be present: reading a missing
// column as zero would turn "the schema changed" into "the check passed".
func resultColumnIndexes(columns []string, expected []string) (map[string]int, error) {
	if len(expected) == 0 {
		return nil, badData("the compiled statement declares no result columns")
	}
	indexes := make(map[string]int, len(columns))
	for index, name := range columns {
		if _, exists := indexes[name]; !exists {
			indexes[name] = index
		}
	}
	for _, name := range expected {
		if _, ok := indexes[name]; !ok {
			return nil, badData("the query result has no %q column", name)
		}
	}
	return indexes, nil
}

// aggregateValue returns the raw value of one required column of the single
// result row.
func aggregateValue(result *db.QueryResult, indexes map[string]int, column string) (any, error) {
	index, ok := indexes[column]
	if !ok {
		return nil, badData("the query result has no %q column", column)
	}
	row := result.Rows[0]
	if index >= len(row) {
		return nil, badData("the result row has no value for %q", column)
	}
	return row[index], nil
}

// aggregateInt reads one integer column. An integer may arrive as int64, as a
// float64 (PostgreSQL's numeric SUM and MySQL's DECIMAL both normalize to
// float64), or as a numeric string from a driver that reports no type. A
// non-integral number is a contract violation, not a rounded count.
func aggregateInt(result *db.QueryResult, indexes map[string]int, column string) (int64, error) {
	value, err := aggregateValue(result, indexes, column)
	if err != nil {
		return 0, err
	}
	switch typed := value.(type) {
	case nil:
		// An aggregate over an empty set is NULL; for a count that is zero.
		return 0, nil
	case int64:
		return typed, nil
	case float64:
		if typed != math.Trunc(typed) {
			return 0, badData("column %q is not an integer (%v)", column, typed)
		}
		return int64(typed), nil
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil {
			return parsed, nil
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil || parsed != math.Trunc(parsed) {
			return 0, badData("column %q is not an integer (%q)", column, typed)
		}
		return int64(parsed), nil
	default:
		return 0, badData("column %q has an unexpected %T value", column, value)
	}
}

// aggregateFloat reads one optional numeric column and reports whether it was
// present. A NULL is reported as absent rather than as zero: zero is a meaningful
// lag.
func aggregateFloat(result *db.QueryResult, indexes map[string]int, column string) (float64, bool, error) {
	value, err := aggregateValue(result, indexes, column)
	if err != nil {
		return 0, false, err
	}
	switch typed := value.(type) {
	case nil:
		return 0, false, nil
	case float64:
		return typed, true, nil
	case int64:
		return float64(typed), true, nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false, badData("column %q is not a number (%q)", column, typed)
		}
		return parsed, true, nil
	default:
		return 0, false, badData("column %q has an unexpected %T value", column, value)
	}
}

// aggregateTimestamp reads one optional timestamp column. The scanner decodes a
// date/time database type into time.Time; a driver that reports the type loosely
// leaves a string, which is parsed here rather than read as zero.
func aggregateTimestamp(result *db.QueryResult, indexes map[string]int, column string) (*time.Time, error) {
	value, err := aggregateValue(result, indexes, column)
	if err != nil {
		return nil, err
	}
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case time.Time:
		measured := typed
		return &measured, nil
	case string:
		return parseAggregateTimestamp(typed, column)
	case []byte:
		return parseAggregateTimestamp(string(typed), column)
	default:
		return nil, badData("column %q has an unexpected %T value", column, value)
	}
}

// aggregateTimestampLayouts are the spellings a loosely typed timestamp can
// arrive in. RFC 3339 is what a normalized driver returns; the others are the
// engines' display forms.
var aggregateTimestampLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// parseAggregateTimestamp parses one string timestamp, rejecting an unparseable
// value rather than treating it as the zero time.
func parseAggregateTimestamp(value, column string) (*time.Time, error) {
	trimmed := strings.TrimSpace(value)
	for _, layout := range aggregateTimestampLayouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			measured := parsed
			return &measured, nil
		}
	}
	return nil, badData("column %q is not a timestamp (%q)", column, value)
}

// schemaDriftItem is one difference between the live column list and a drift
// rule's snapshot, plus the reason a reviewer sees in the sample.
type schemaDriftItem struct {
	Column string
	Reason string
}

// executeMetadataRule observes a rule that reads the metadata registry instead of
// the target database. schema_drift compares the live column list with the saved
// snapshot; it runs no query, needs no read-only data source and records no SQL.
func executeMetadataRule(resolution *Resolution, rule RuleSnapshot, opts ExecOptions) (*Execution, error) {
	config := rule.Config.GetSchemaDrift()
	if config == nil {
		return nil, invalidConfig("the schema-drift config is missing")
	}
	items := compareSchemaDrift(resolution.Target.Columns, config)
	observation := Observation{
		Observed:     float64(len(items)),
		ObservedKind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
		FailedRows:   int64(len(items)),
		TotalRows:    int64(len(config.GetExpectedColumns())),
	}
	execution := &Execution{Observation: observation, ExecutedSQL: ""}
	if !opts.StoreSamples {
		return execution, nil
	}
	limit := normalizeSampleLimit(opts.SampleLimit)
	execution.Samples = schemaDriftSamples(items, limit)
	if execution.Samples != nil {
		execution.Observation.SampleRows = int64(len(execution.Samples.GetSamples()))
	}
	if len(items) > limit {
		// The observation is complete; only the sample was capped, and the result
		// says so rather than implying the list was exhaustive.
		execution.Truncated = true
		execution.Observation.Truncated = true
	}
	return execution, nil
}

// compareSchemaDrift compares the registry's current columns with the saved
// snapshot. The items come in a stable order: missing and changed-type columns in
// snapshot order, then out-of-order shared columns in snapshot order, then extra
// live columns in live order. Duplicate snapshot entries are ignored here because
// validation refuses them at save time.
func compareSchemaDrift(current []ColumnInfo, config *storepb.SchemaDriftConfig) []schemaDriftItem {
	live := make(map[string]ColumnInfo, len(current))
	for _, column := range current {
		live[column.Name] = column
	}
	expected := config.GetExpectedColumns()
	expectedNames := make(map[string]struct{}, len(expected))
	var items []schemaDriftItem
	for _, want := range expected {
		if want == nil {
			continue
		}
		name := strings.TrimSpace(want.GetName())
		if _, ok := expectedNames[name]; ok {
			continue
		}
		expectedNames[name] = struct{}{}
		actual, ok := live[name]
		if !ok {
			items = append(items, schemaDriftItem{Column: name, Reason: "missing"})
			continue
		}
		if schemaDriftChecksTypes(config) && !strings.EqualFold(strings.TrimSpace(want.GetType()), strings.TrimSpace(actual.Type)) {
			items = append(items, schemaDriftItem{
				Column: name,
				Reason: fmt.Sprintf("type: %s -> %s", strings.TrimSpace(want.GetType()), strings.TrimSpace(actual.Type)),
			})
		}
	}
	if config.GetCheckOrder() {
		items = append(items, schemaDriftOrderItems(expected, current)...)
	}
	if !config.GetAllowExtraColumns() {
		for _, column := range current {
			if _, ok := expectedNames[column.Name]; ok {
				continue
			}
			items = append(items, schemaDriftItem{Column: column.Name, Reason: "extra"})
		}
	}
	return items
}

// schemaDriftOrderItems reports the shared columns whose relative order changed.
// Only columns present in both lists participate: a missing or extra column is
// already its own drift item, and counting it again as an order change would
// double-report one problem.
func schemaDriftOrderItems(expected []*storepb.SchemaDriftColumn, current []ColumnInfo) []schemaDriftItem {
	liveNames := make(map[string]struct{}, len(current))
	for _, column := range current {
		liveNames[column.Name] = struct{}{}
	}
	shared := make(map[string]struct{}, len(expected))
	expectedOrder := make([]string, 0, len(expected))
	for _, want := range expected {
		if want == nil {
			continue
		}
		name := strings.TrimSpace(want.GetName())
		if _, ok := liveNames[name]; !ok {
			continue
		}
		if _, ok := shared[name]; ok {
			continue
		}
		shared[name] = struct{}{}
		expectedOrder = append(expectedOrder, name)
	}
	currentOrder := make([]string, 0, len(expectedOrder))
	for _, column := range current {
		if _, ok := shared[column.Name]; ok {
			currentOrder = append(currentOrder, column.Name)
		}
	}
	var items []schemaDriftItem
	for index, name := range expectedOrder {
		if index < len(currentOrder) && currentOrder[index] == name {
			continue
		}
		items = append(items, schemaDriftItem{Column: name, Reason: "order"})
	}
	return items
}

// schemaDriftSamples renders drift items in the failure-sample shape: one sample
// row per item, naming the affected column and the reason.
func schemaDriftSamples(items []schemaDriftItem, limit int) *storepb.QualityFailureSamples {
	if len(items) == 0 || limit <= 0 {
		return nil
	}
	if limit > maxSampleLimit {
		limit = maxSampleLimit
	}
	samples := &storepb.QualityFailureSamples{}
	for _, item := range items {
		if len(samples.Samples) == limit {
			break
		}
		samples.Samples = append(samples.Samples, &storepb.FailureSample{Columns: []*storepb.ColumnValue{{
			Column: common.TruncateUTF8Bytes(item.Column, maxSampleValueBytes),
			Value:  common.TruncateUTF8Bytes(item.Reason, maxSampleValueBytes),
		}}})
	}
	if len(samples.Samples) == 0 {
		return nil
	}
	return samples
}

// schemaDriftChecksTypes reports whether a type change counts as drift. Unset
// means yes: a schema-drift rule protects the column contract, and a type change
// is the change most worth catching, so the author turns it off explicitly.
func schemaDriftChecksTypes(config *storepb.SchemaDriftConfig) bool {
	if config.CheckTypes == nil {
		return true
	}
	return *config.CheckTypes
}
