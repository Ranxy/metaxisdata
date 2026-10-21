// Package dataquality implements the semantics of a data quality check: what a
// rule observes, the read-only SQL it compiles to, and whether the observed value
// passes. It depends on the store for the metadata it needs but on nothing above
// it — no HTTP, no transactions, no notifications, no LLM calls — so every
// judgment here is testable without a database.
package dataquality

import (
	"time"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// EngineVersion identifies the compile and judgment semantics this build
// implements. It is recorded on every result; bump it whenever a change can alter
// a verdict, so a stored result can be explained by the engine that produced it.
// Rules re-run on their own schedule, so no backfill is needed.
const EngineVersion = "dq.v1"

// Target is what a rule observes: the identity of one relation in the metadata
// registry plus the path snapshot taken when the rule was saved.
type Target struct {
	// GUID is the registry GUID of the relation, e.g.
	// instances/ins1;app;public;orders.
	GUID string
	// MetaType is the registry object type the GUID was registered under.
	MetaType storepb.MetaType
	// Path is display metadata; it is not the identity.
	Path *storepb.QualityTargetPath
}

// RuleSnapshot is the immutable definition a result is judged with. The runner
// loads it from the rule's revision, not from the mutable head, so editing a rule
// never changes what a historical result means.
type RuleSnapshot struct {
	ID        int64
	Revision  int32
	Type      storepb.QualityRuleType
	Dimension storepb.QualityDimension
	Severity  storepb.QualitySeverity
	Target    Target
	Config    *storepb.QualityRuleConfig
}

// Observation is what one execution measured, before any threshold is applied.
type Observation struct {
	// Observed is the measured value; ObservedKind says in what unit.
	Observed     float64
	ObservedKind storepb.QualityObservedKind
	// FailedRows and TotalRows are the numerator and denominator the observation
	// was computed from, kept so a result can show "3 / 1200 (0.25%)".
	FailedRows int64
	TotalRows  int64
	// ObservedNull reports that the measured value is SQL NULL: an aggregate over
	// an empty (or all-NULL) column, or one side of a cross-table reconciliation.
	// It is not zero; the evaluator follows on_empty for it.
	ObservedNull bool
	// MaxTimestamp is the newest value a freshness rule saw; nil when it saw none.
	MaxTimestamp *time.Time
	// Baseline is the rule's last successful observation, for row_count_change.
	// Nil means there is no recent baseline to compare against.
	Baseline *float64
	// Sampled reports that the observation was made over a sample of the rows.
	Sampled bool
	// SampleRows is how many rows the sample read.
	SampleRows int64
	// Truncated reports that the result or sample was cut off by a limit.
	Truncated bool
}

// QueryKind is the shape of a compiled statement, which tells the executor how to
// interpret its rows.
type QueryKind int

const (
	// QueryAggregate is a one-row aggregate whose columns the evaluator reads by
	// name (failed_rows / total_rows / max_ts / observed).
	QueryAggregate QueryKind = iota + 1
	// QueryFailedRows returns the failed rows themselves; the executor counts them.
	QueryFailedRows
	// QuerySample returns a bounded sample of rows for in-process evaluation.
	QuerySample
	// QueryMetadata produces no SQL: the observation is computed from the
	// metadata registry itself (schema_drift), so the executor must not touch the
	// target database and the SQL preview has nothing to show.
	QueryMetadata
)

// CompiledQuery is one read-only statement plus the arguments bound to it.
type CompiledQuery struct {
	SQL  string
	Args []any
	Kind QueryKind
	// Columns are the result columns the evaluator expects, in order. A missing
	// one is BAD_DATA rather than a silent zero.
	Columns []string
}

// statusRank orders the six result states by how much attention they need. It is
// deliberately not the enum's numeric order: NOT_EVALUATED (6) ranks below WARN.
var statusRank = map[storepb.QualityStatus]int{
	storepb.QualityStatus_QUALITY_STATUS_PASS:          0,
	storepb.QualityStatus_QUALITY_STATUS_SKIPPED:       1,
	storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED: 2,
	storepb.QualityStatus_QUALITY_STATUS_WARN:          3,
	storepb.QualityStatus_QUALITY_STATUS_FAIL:          4,
	storepb.QualityStatus_QUALITY_STATUS_ERROR:         5,
}

// Rollup returns the worst state among the given results. A run without results
// is NOT_EVALUATED: nothing was judged, which is not a pass.
func Rollup(statuses []storepb.QualityStatus) storepb.QualityStatus {
	if len(statuses) == 0 {
		return storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED
	}
	worst := storepb.QualityStatus_QUALITY_STATUS_PASS
	worstRank := statusRank[worst]
	for _, status := range statuses {
		rank, ok := statusRank[status]
		if !ok {
			// UNSPECIFIED is not a verdict; treat it as nothing judged.
			continue
		}
		if rank > worstRank {
			worst = status
			worstRank = rank
		}
	}
	return worst
}

// RollupCounts returns the run-level state from its six-state tallies, so a run
// whose results are older than the retention window still reports its rollup.
func RollupCounts(counts *storepb.QualityRunCounts) storepb.QualityStatus {
	if counts == nil || counts.GetTotal() == 0 {
		return storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED
	}
	switch {
	case counts.GetError() > 0:
		return storepb.QualityStatus_QUALITY_STATUS_ERROR
	case counts.GetFail() > 0:
		return storepb.QualityStatus_QUALITY_STATUS_FAIL
	case counts.GetWarn() > 0:
		return storepb.QualityStatus_QUALITY_STATUS_WARN
	case counts.GetNotEvaluated() > 0:
		return storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED
	case counts.GetSkipped() == counts.GetTotal():
		return storepb.QualityStatus_QUALITY_STATUS_SKIPPED
	default:
		return storepb.QualityStatus_QUALITY_STATUS_PASS
	}
}

// CountResults tallies six states into the message a run row stores, and returns
// the rollup the run's outcome column holds.
func CountResults(statuses []storepb.QualityStatus) (*storepb.QualityRunCounts, storepb.QualityStatus) {
	counts := &storepb.QualityRunCounts{Total: int32(len(statuses))}
	var pass, warn, fail, errs, skipped, notEvaluated int32
	for _, status := range statuses {
		switch status {
		case storepb.QualityStatus_QUALITY_STATUS_PASS:
			pass++
		case storepb.QualityStatus_QUALITY_STATUS_WARN:
			warn++
		case storepb.QualityStatus_QUALITY_STATUS_FAIL:
			fail++
		case storepb.QualityStatus_QUALITY_STATUS_ERROR:
			errs++
		case storepb.QualityStatus_QUALITY_STATUS_SKIPPED:
			skipped++
		case storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED:
			notEvaluated++
		case storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED:
			// Not a verdict, but the rule was attempted: keep it inside total and
			// out of PASS, so the counts still add up.
			notEvaluated++
		default:
			// A value outside the enum is not a verdict either.
			notEvaluated++
		}
	}
	counts.Pass = pass
	counts.Warn = warn
	counts.Fail = fail
	counts.Error = errs
	counts.Skipped = skipped
	counts.NotEvaluated = notEvaluated
	return counts, RollupCounts(counts)
}

// Error codes are the strings a result stores in error_code. They are stable
// because the frontend maps them to localized messages.
const (
	ErrCodeConnection    = "CONNECTION"
	ErrCodeAuth          = "AUTH"
	ErrCodeTimeout       = "TIMEOUT"
	ErrCodePermission    = "PERMISSION"
	ErrCodeUnsupported   = "UNSUPPORTED"
	ErrCodeSQLSyntax     = "SQL_SYNTAX"
	ErrCodeBadData       = "BAD_DATA"
	ErrCodeLimitExceeded = "LIMIT_EXCEEDED"
	ErrCodeOrphaned      = "ORPHANED"
	// ErrCodeQuota is legacy: capacity contention is deferred before a run starts,
	// so the runner no longer writes it. It stays because stored results may carry
	// it and the frontend localizes the code.
	ErrCodeQuota      = "QUOTA"
	ErrCodeNoReadOnly = "NO_READ_ONLY_SOURCE"
)

// ResultState is the verdict of one rule execution: the status plus the
// threshold snapshot that produced it and the observation it was judged on.
type ResultState struct {
	Status     storepb.QualityStatus
	Thresholds *storepb.QualityThresholds
}

// GetStatus returns the verdict.
func (r ResultState) GetStatus() storepb.QualityStatus { return r.Status }

// GetThresholds returns the threshold snapshot the verdict was judged on.
func (r ResultState) GetThresholds() *storepb.QualityThresholds { return r.Thresholds }
