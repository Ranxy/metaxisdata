package dataquality

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

const (
	// maxSegmentColumns bounds one segment check's grouping columns. It matches the
	// grouped row-count rule: five is the widest key a review table can show.
	maxSegmentColumns = 5
	// maxSegmentCount bounds the segment set one execution records. A larger set is
	// truncated to the largest segments and the execution says so; a truncated
	// observation is never judged, because a segment beyond the cap would look
	// missing.
	maxSegmentCount = 500
	// maxSegmentValueBytes bounds one stored segment key value. The digest below
	// carries the identity of a longer value.
	maxSegmentValueBytes = 128
	// segmentValueDigestBytes is how many SHA-256 bytes one segment value's digest
	// keeps. Eight bytes (16 hex characters) make an accidental collision between
	// a check's segments negligible while keeping the evidence compact.
	segmentValueDigestBytes = 8
	// segmentRowCountColumn is the result column carrying a segment's row count.
	segmentRowCountColumn = "segment_rows"
	// defaultSegmentBaselineRuns is how many previous executions form the baseline
	// when the config does not say.
	defaultSegmentBaselineRuns = 3
	// maxSegmentBaselineRuns bounds the baseline window.
	maxSegmentBaselineRuns = 10
	// defaultSegmentMinOccurrences is how many of the baseline runs must have seen a
	// segment before it counts as expected, when the config does not say. It is
	// clamped to the runs actually available.
	defaultSegmentMinOccurrences = 2
)

// SegmentHistoryRun is one previous execution's segment observation, as the
// caller read it from the store.
type SegmentHistoryRun struct {
	Segments  *storepb.SegmentObservation
	StartedAt time.Time
}

// SegmentExpectation is one segment the baseline expects to keep existing.
type SegmentExpectation struct {
	Key          []string
	IsNull       []bool
	Masked       []bool
	ValueDigests []string
	RowCount     int64
	LastSeen     time.Time
	Occurrences  int
}

// SegmentBaseline is the expected segment set derived from a segment check's
// recent executions: the segments that appeared often enough to count, and the
// full set of keys any run saw so a new segment can be told apart.
type SegmentBaseline struct {
	Expected []SegmentExpectation
	Known    map[string]struct{}
	Runs     int
}

// SegmentCheckBounds resolves a segment check's baseline window and occurrence
// threshold, applying the defaults. It is exported so the runner can size the
// history read with the same numbers validation accepts.
func SegmentCheckBounds(config *storepb.QualityRuleConfig) (baselineRuns, minOccurrences int, err error) {
	segment := config.GetSegmentCheck()
	if segment == nil {
		return 0, 0, invalidConfig("the segment-check config is missing")
	}
	baselineRuns = int(segment.GetBaselineRuns())
	if baselineRuns == 0 {
		baselineRuns = defaultSegmentBaselineRuns
	}
	if baselineRuns < 1 || baselineRuns > maxSegmentBaselineRuns {
		return 0, 0, invalidConfig("baseline_runs must be between 1 and %d, got %d", maxSegmentBaselineRuns, baselineRuns)
	}
	minOccurrences = int(segment.GetMinOccurrences())
	if minOccurrences == 0 {
		minOccurrences = min(defaultSegmentMinOccurrences, baselineRuns)
	}
	if minOccurrences < 1 || minOccurrences > baselineRuns {
		return 0, 0, invalidConfig("min_occurrences must be between 1 and baseline_runs (%d), got %d", baselineRuns, minOccurrences)
	}
	return baselineRuns, minOccurrences, nil
}

// BuildSegmentBaseline derives the expected segment set from a check's recent
// executions, newest first. Truncated observations are skipped: their observed set
// is incomplete, so a segment missing from one of them would be misread as "never
// seen" (and a real absence as "new"). A segment is expected when at least
// min(min_occurrences, len(usable runs)) runs observed it; the current execution
// fails for each expected segment it no longer has.
func BuildSegmentBaseline(runs []SegmentHistoryRun, baselineRuns, minOccurrences int) *SegmentBaseline {
	if baselineRuns <= 0 || len(runs) == 0 {
		return nil
	}
	baseline := &SegmentBaseline{Known: make(map[string]struct{})}
	byKey := make(map[string]SegmentExpectation)
	order := make([]string, 0)
	for _, run := range runs {
		if run.Segments == nil || run.Segments.GetTruncated() {
			continue
		}
		baseline.Runs++
		for _, record := range run.Segments.GetObserved() {
			if record == nil {
				continue
			}
			key := segmentKey(record.GetValues(), record.GetIsNull(), record.GetValueDigests())
			baseline.Known[key] = struct{}{}
			expectation, ok := byKey[key]
			if !ok {
				// Runs arrive newest first, so the first observation is the latest
				// one and its row count is the last known size.
				expectation = SegmentExpectation{
					Key:          append([]string(nil), record.GetValues()...),
					IsNull:       append([]bool(nil), record.GetIsNull()...),
					Masked:       append([]bool(nil), record.GetMasked()...),
					ValueDigests: append([]string(nil), record.GetValueDigests()...),
					RowCount:     record.GetRowCount(),
					LastSeen:     run.StartedAt,
				}
				order = append(order, key)
			}
			expectation.Occurrences++
			byKey[key] = expectation
		}
		if baseline.Runs == baselineRuns {
			break
		}
	}
	if baseline.Runs == 0 {
		return nil
	}
	required := minOccurrences
	if required > baseline.Runs {
		required = baseline.Runs
	}
	for _, key := range order {
		expectation := byKey[key]
		if expectation.Occurrences >= required {
			baseline.Expected = append(baseline.Expected, expectation)
		}
	}
	return baseline
}

// segmentKey canonicalizes one segment's key values so two runs compare equal
// regardless of value content. It prefers the value digest, because the display
// value is truncated and two long values sharing the truncation prefix would
// otherwise collapse into one segment. Length prefixes keep "a|b" and "a", "|b"
// apart.
func segmentKey(values []string, isNull []bool, digests []string) string {
	var key []byte
	for index, value := range values {
		component := value
		if index < len(digests) && digests[index] != "" {
			component = digests[index]
		}
		key = strconv.AppendInt(key, int64(len(component)), 10)
		key = append(key, ':')
		if index < len(isNull) && isNull[index] {
			key = append(key, '1')
		} else {
			key = append(key, '0')
		}
		key = append(key, component...)
		key = append(key, ';')
	}
	return string(key)
}

// segmentValueDigest fingerprints one full segment value. Values are stored
// truncated for display; the digest keeps two values with the same prefix
// distinct across executions.
func segmentValueDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:segmentValueDigestBytes])
}

// compileSegmentCheck reads the distinct segments of the grouping columns and
// their row counts, largest first. The statement asks for one row more than the
// evidence cap so the executor can tell a complete observation from a truncated
// one without a second query.
func compileSegmentCheck(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetSegmentCheck()
	columns, err := segmentCheckColumns(input.Target, config.GetColumns())
	if err != nil {
		return CompiledQuery{}, err
	}
	if _, _, err := SegmentCheckBounds(input.Config); err != nil {
		return CompiledQuery{}, err
	}
	if err := compileValidateCountThresholds(input.Config, config.FailThreshold, config.WarnThreshold); err != nil {
		return CompiledQuery{}, err
	}
	relation, where, filterArgs, err := scanRelation(input)
	if err != nil {
		return CompiledQuery{}, err
	}
	groupList := qualifyColumns(input.Dialect, columns)
	order := " ORDER BY " + segmentRowCountColumn + " DESC, " + groupList
	query := "SELECT " + groupList + ", COUNT(*) AS " + segmentRowCountColumn +
		" FROM " + relation + where + " GROUP BY " + groupList + order +
		" " + input.Dialect.LimitClause(maxSegmentCount+1)
	resultColumns := make([]string, 0, len(columns)+1)
	resultColumns = append(resultColumns, columns...)
	resultColumns = append(resultColumns, segmentRowCountColumn)
	return CompiledQuery{
		SQL:     query,
		Args:    filterArgs,
		Kind:    QuerySample,
		Columns: resultColumns,
	}, nil
}

// validateSegmentCheckConfig checks a segment check: 1-5 distinct grouping
// columns the engine can group, a legal baseline window, and count thresholds.
func validateSegmentCheckConfig(target ResolvedTarget, config *storepb.QualityRuleConfig) error {
	segment := config.GetSegmentCheck()
	if segment == nil {
		return invalidConfig("the segment-check config is missing")
	}
	if _, err := segmentCheckColumns(target, segment.GetColumns()); err != nil {
		return err
	}
	if _, _, err := SegmentCheckBounds(config); err != nil {
		return err
	}
	return validateCountThresholds(config, segment.FailThreshold, segment.WarnThreshold)
}

// evaluateSegmentCheck judges the disappeared-segment count. A NULL observation is
// a run with no usable baseline (the first execution) or a truncated segment set,
// which on_empty decides: the check does not pretend a partial comparison passed.
func evaluateSegmentCheck(config *storepb.QualityRuleConfig, obs Observation, onEmpty storepb.QualityEmptyBehavior) ResultState {
	segment := config.GetSegmentCheck()
	if segment == nil {
		return ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_ERROR}
	}
	failValue := 0.0
	if segment.FailThreshold != nil {
		failValue = *segment.FailThreshold
	}
	thresholds := &storepb.QualityThresholds{
		Comparison: storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN,
		OnEmpty:    onEmpty,
		FailValue:  float64Ptr(failValue),
		WarnValue:  segment.WarnThreshold,
	}
	if obs.ObservedNull {
		return ResultState{
			Status:     emptyStatus(onEmpty, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED),
			Thresholds: thresholds,
		}
	}
	// A baseline that expected no segments judged nothing: both units follow
	// on_empty, the same way a count rule with no rows does. Deciding this per
	// unit would give the same data two different verdicts.
	if obs.TotalRows == 0 && obs.FailedRows == 0 {
		return ResultState{Status: emptyStatus(onEmpty, storepb.QualityStatus_QUALITY_STATUS_PASS), Thresholds: thresholds}
	}
	observed := float64(obs.FailedRows)
	if thresholdMetric(config) == storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT {
		observed = float64(obs.FailedRows) / float64(obs.TotalRows) * 100
	}
	return applyThresholds(observed, thresholds)
}

// executeSegmentCheckRule runs a segment check: it reads the current segment set,
// compares it with the baseline the caller derived from previous executions, and
// keeps the observation as evidence for the following runs.
func executeSegmentCheckRule(ctx context.Context, queryer db.ReadOnlyQueryer, _ *Resolution, rule RuleSnapshot, opts ExecOptions, compiled CompiledQuery, m *metrics.Metrics) (*Execution, error) {
	if compiled.Kind != QuerySample {
		return nil, badData("the compiler returned query kind %d for a segment check", compiled.Kind)
	}
	if len(compiled.Columns) < 2 {
		return nil, badData("a segment check needs at least one grouping column and the row count")
	}
	// One row more than the cap: only by asking for the extra row can the
	// executor tell a complete observation from a truncated one.
	result, err := queryer.QueryReadOnly(ctx, compiled.SQL, compiled.Args, db.QueryOptions{
		MaxRows:  maxSegmentCount + 1,
		MaxBytes: opts.MaxBytes,
		Timeout:  opts.QueryTimeout,
		Purpose:  metrics.QueryPurposeRuleSample,
		Metrics:  m,
	})
	if err != nil {
		return nil, err
	}
	if err := requireSampleColumns(result.Columns, compiled.Columns); err != nil {
		return nil, err
	}
	segmentColumns := compiled.Columns[:len(compiled.Columns)-1]
	observed, truncated, err := segmentRecordsFromRows(segmentColumns, result.Rows, personalSampleColumns(rule, opts))
	if truncated || result.Truncated {
		truncated = true
	}
	if err != nil {
		return nil, err
	}
	observation := Observation{ObservedKind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT}
	evidence := &storepb.SegmentObservation{Observed: observed, Truncated: truncated}
	baseline := opts.Segments
	switch {
	case baseline == nil || baseline.Runs == 0 || truncated:
		// No baseline yet (the first execution), or a partial comparison: record
		// the observed set, judge nothing.
		observation.ObservedNull = true
		observation.Truncated = truncated
	default:
		observedKeys := make(map[string]struct{}, len(observed))
		for _, record := range observed {
			observedKeys[segmentKey(record.GetValues(), record.GetIsNull(), record.GetValueDigests())] = struct{}{}
		}
		missing := make([]*storepb.SegmentRecord, 0)
		for _, expectation := range baseline.Expected {
			if _, ok := observedKeys[segmentKey(expectation.Key, expectation.IsNull, expectation.ValueDigests)]; ok {
				continue
			}
			missing = append(missing, &storepb.SegmentRecord{
				Values:       expectation.Key,
				IsNull:       expectation.IsNull,
				Masked:       expectation.Masked,
				ValueDigests: expectation.ValueDigests,
				RowCount:     expectation.RowCount,
				LastSeen:     timestamppb.New(expectation.LastSeen),
			})
		}
		evidence.Disappeared = missing
		evidence.BaselineRuns = int32(baseline.Runs)
		evidence.ExpectedCount = int64(len(baseline.Expected))
		evidence.NewSegments = newSegmentRecords(observed, baseline.Known)
		observation.Observed = float64(len(missing))
		observation.FailedRows = int64(len(missing))
		observation.TotalRows = int64(len(baseline.Expected))
	}
	return &Execution{
		Observation: observation,
		Evidence:    &storepb.QualityExecutionEvidence{Segments: evidence},
		ExecutedSQL: truncateExecutedSQL(compiled.SQL),
		Truncated:   truncated,
	}, nil
}

// segmentRecordsFromRows turns the grouped rows into evidence records, keeping at
// most the cap. A row beyond the cap means the observation is truncated; the rows
// are already largest-first, so the cap keeps the segments that matter most.
func segmentRecordsFromRows(columns []string, rows [][]any, masked map[string]struct{}) ([]*storepb.SegmentRecord, bool, error) {
	truncated := len(rows) > maxSegmentCount
	limit := min(len(rows), maxSegmentCount)
	records := make([]*storepb.SegmentRecord, 0, limit)
	for _, row := range rows[:limit] {
		if len(row) != len(columns)+1 {
			return nil, false, badData("a segment row returned %d columns, expect %d", len(row), len(columns)+1)
		}
		record := &storepb.SegmentRecord{
			Values:       make([]string, 0, len(columns)),
			IsNull:       make([]bool, 0, len(columns)),
			Masked:       make([]bool, 0, len(columns)),
			ValueDigests: make([]string, 0, len(columns)),
		}
		for index, name := range columns {
			cell := row[index]
			if cell == nil {
				record.Values = append(record.Values, "")
				record.IsNull = append(record.IsNull, true)
				record.Masked = append(record.Masked, false)
				record.ValueDigests = append(record.ValueDigests, "")
				continue
			}
			// The digest is taken over the display form the evidence stores, so a
			// personal value never contributes a fingerprint of its raw text.
			value := sampleValueString(cell)
			maskedValue := false
			if _, ok := masked[name]; ok {
				value = MaskPersonalValue(value)
				maskedValue = true
			}
			record.ValueDigests = append(record.ValueDigests, segmentValueDigest(value))
			record.Values = append(record.Values, common.TruncateUTF8Bytes(value, maxSegmentValueBytes))
			record.IsNull = append(record.IsNull, false)
			record.Masked = append(record.Masked, maskedValue)
		}
		count, ok := row[len(columns)].(int64)
		if !ok {
			// A COUNT(*) read from a driver that normalizes to another integer type
			// still has exactly one value; require the normalization contract.
			return nil, false, badData("a segment row count is %T, expect int64", row[len(columns)])
		}
		record.RowCount = count
		records = append(records, record)
	}
	return records, truncated, nil
}

// newSegmentRecords returns the observed segments the baseline never saw. They are
// evidence only: a new segment does not fail the check.
func newSegmentRecords(observed []*storepb.SegmentRecord, known map[string]struct{}) []*storepb.SegmentRecord {
	records := make([]*storepb.SegmentRecord, 0)
	for _, record := range observed {
		if _, ok := known[segmentKey(record.GetValues(), record.GetIsNull(), record.GetValueDigests())]; ok {
			continue
		}
		records = append(records, record)
	}
	return records
}
