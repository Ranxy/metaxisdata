package dataquality

import (
	"fmt"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

const (
	// defaultSampleLimit is how many failed rows one result keeps when the
	// caller does not name a limit. It matches the workspace default
	// (failure_sample_limit reads 0 as 20).
	defaultSampleLimit = 20
	// maxSampleLimit bounds one sample query. A larger request is clamped rather
	// than refused: the caller's intent is "as many as allowed".
	maxSampleLimit = 100
	// maxSampleValueBytes bounds one stored value of a failed row. A wide text
	// column must not bloat the result row, and the proto stores strings.
	maxSampleValueBytes = 256
	// maxExecutedSQLBytes bounds the SQL text recorded on a result (8 KiB, the
	// same bound the rule definition itself has).
	maxExecutedSQLBytes = 8 * 1024
)

// ExecOptions bounds one rule execution. The zero value is the safe default:
// no extra limit, default sample behaviour when sampling is asked for.
type ExecOptions struct {
	// QueryTimeout bounds each query. 0 means the caller's context is the only
	// deadline.
	QueryTimeout time.Duration
	// MaxRows is not used by the aggregate path: an aggregate observation is one
	// row by definition, and the executor always asks for exactly one. It stays
	// in the struct so a caller can share one options value with profiling code,
	// which does bound its rows.
	MaxRows int
	// MaxBytes bounds the accumulated value payload a query may return. The
	// sample query uses it too, so a wide table cannot blow up memory. 0 means
	// unlimited.
	MaxBytes int
	// SampleLimit is how many failed rows to keep. 0 reads as the default (20)
	// and values above the maximum are clamped to it.
	SampleLimit int
	// StoreSamples asks for the failed-row sample. When false no second query is
	// issued, which is what the workspace setting controls.
	StoreSamples bool
}

// Execution is everything one execution measured: the observation the evaluator
// judges, the optional failed-row sample, and the SQL that produced it.
type Execution struct {
	Observation Observation
	// Samples is the failed-row sample, or nil when none was requested, none was
	// possible for this rule type, or the sample query failed.
	Samples *storepb.QualityFailureSamples
	// ExecutedSQL is the aggregate statement, truncated for storage.
	ExecutedSQL string
	// Sampled reports that the observation was made over a sample of the rows
	// rather than the whole filtered table. Only a CEL rule can be sampled today:
	// it reads a bounded number of rows and evaluates the predicate in process.
	Sampled bool
	// Truncated reports that the aggregate or the sample was cut off by a limit.
	Truncated bool
}

// normalizeSampleLimit maps a requested limit to the value one query uses: the
// default for "unset", the maximum for "as many as allowed".
func normalizeSampleLimit(limit int) int {
	if limit <= 0 {
		return defaultSampleLimit
	}
	if limit > maxSampleLimit {
		return maxSampleLimit
	}
	return limit
}

// truncateExecutedSQL cuts the recorded SQL to its storage bound on a rune
// boundary, so the stored text is always valid UTF-8.
func truncateExecutedSQL(sql string) string {
	return common.TruncateUTF8Bytes(sql, maxExecutedSQLBytes)
}

// SamplesFromRows turns query rows into the stored failed-row sample. It trusts
// the caller's limit only up to the maximum, keeps at most that many rows, and
// truncates every value, because the proto stores display strings. Every cell of
// a row that was read records whether it was SQL NULL, so a NULL stays
// distinguishable from an empty string — the two cases a null_percent and a
// string_length failure sample are made of.
func SamplesFromRows(columns []string, rows [][]any, limit int) *storepb.QualityFailureSamples {
	if limit <= 0 || len(columns) == 0 || len(rows) == 0 {
		return nil
	}
	if limit > maxSampleLimit {
		limit = maxSampleLimit
	}
	samples := &storepb.QualityFailureSamples{}
	for _, row := range rows {
		if len(samples.Samples) == limit {
			break
		}
		sample := &storepb.FailureSample{Columns: make([]*storepb.ColumnValue, 0, len(columns))}
		for index, column := range columns {
			cell := &storepb.ColumnValue{Column: column}
			if index < len(row) {
				if row[index] == nil {
					cell.IsNull = proto.Bool(true)
				} else {
					cell.Value = common.TruncateUTF8Bytes(sampleValueString(row[index]), maxSampleValueBytes)
					cell.IsNull = proto.Bool(false)
				}
			}
			sample.Columns = append(sample.Columns, cell)
		}
		samples.Samples = append(samples.Samples, sample)
	}
	if len(samples.Samples) == 0 {
		return nil
	}
	return samples
}

// sampleValueString renders one non-NULL normalized query value as the display
// string the sample stores. The normalization contract is int64, float64, bool,
// string or time.Time, so every other type is a last-resort rendering rather than
// a value this build promises to keep. The caller stores SQL NULL separately.
func sampleValueString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	case time.Time:
		return typed.UTC().Format(time.RFC3339)
	case []byte:
		return string(typed)
	default:
		return fmt.Sprintf("%v", typed)
	}
}
