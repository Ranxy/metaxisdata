package lineageanalyzer

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// errAnalysisStale tells drainAndAnalyze that the result was computed from text
// that has already been replaced, so the object is queued again instead of
// being retried as a failure.
var errAnalysisStale = errors.New("analysis result is stale")

// errAnalysisGone tells analyzeObject that the object disappeared before its
// result could be written, so the result is dropped instead of retried.
var errAnalysisGone = errors.New("analysis object is gone")

// drainRecordsTimeout reports whether the drain owns the timeout outcome for
// this error. An interrupted analysis is recorded by analyzeObject; the drain
// must not count the same attempt as a timeout.
func drainRecordsTimeout(err error) bool {
	return !errors.Is(err, errAnalysisInterrupted)
}

// errAnalysisInterrupted marks an analysis cut short by its deadline or by the
// runner's shutdown. analyzeObject records that outcome itself; the sentinel is
// what lets the drain tell this branch apart from every other deadline hit, so
// the same attempt is not also counted as a timeout.
var errAnalysisInterrupted = errors.New("analysis is interrupted")

// finishAnalysisWrite records the outcome of a terminal branch once its state
// write returns. A successful write records the branch result; a failed one is
// classified like any other failed analysis, so a vanished object, a stale result
// or a store error is not reported as the branch result.
func (a *Analyzer) finishAnalysisWrite(ctx context.Context, metaType storepb.MetaType, engine storepb.Engine, result string, writeErr error) error {
	if writeErr != nil {
		return a.finishFailedAnalysis(ctx, metaType, engine, writeErr)
	}
	a.metrics.ObserveLineageAnalysis(metaType, engine, result)
	return nil
}

// finishFailedAnalysis records why an analysis did not reach a result and returns
// the error the caller must return. A vanished object drops its result silently.
// A stale result and a deadline/shutdown are recorded by the drain, which owns
// those outcomes (and re-queues the object); everything else is a store error,
// recorded here because the drain does not count generic failures.
func (a *Analyzer) finishFailedAnalysis(ctx context.Context, metaType storepb.MetaType, engine storepb.Engine, err error) error {
	if result := analysisFailureResult(ctx, err); result != "" {
		a.metrics.ObserveLineageAnalysis(metaType, engine, result)
	}
	if errors.Is(err, errAnalysisGone) {
		return nil
	}
	return err
}

// analysisFailureResult maps a failed analysis to the outcome recorded for it. It
// returns "" when the drain records the outcome instead: a stale result or a
// timeout is retried there. A vanished object is always "gone", even when the
// context expired meanwhile.
func analysisFailureResult(ctx context.Context, err error) string {
	switch {
	case errors.Is(err, errAnalysisGone):
		return metrics.AnalysisObjectGone
	case errors.Is(err, errAnalysisStale), analysisTimedOut(ctx, err):
		return ""
	default:
		return metrics.AnalysisStoreError
	}
}

// analysisEdgeCounts splits the edges an analysis submits to the replace into
// column- and table-level counts. A table-level edge names neither a source nor a
// target column. The counts are an upper bound on what the replace inserts: it
// deletes the old set and re-inserts with ON CONFLICT DO NOTHING, which drops
// duplicates.
func analysisEdgeCounts(lineages []*store.ColumnLineage) (column, table int) {
	for _, l := range lineages {
		if l.SourceColumn == "" && l.TargetColumn == "" {
			table++
		}
	}
	return len(lineages) - table, table
}

// storeError surfaces an error before any result exists. It records the message
// on the object's existing analysis-state row without touching its hash: a
// transient failure must not make a successful result look unanalyzed, and the
// next attempt still compares against the last stored result.
func storeError(ctx context.Context, s *store.Store, metaGUID string, metaType storepb.MetaType, cause error, msg string) error {
	var full string
	if cause != nil {
		full = fmt.Sprintf("%s: %v", msg, cause)
	} else {
		full = msg
	}
	_ = s.RecordAnalysisError(ctx, metaGUID, metaType, full)
	if cause != nil {
		return errors.Wrap(cause, msg)
	}
	return errors.New(msg)
}

// analysisWriteError classifies a failed result write: a vanished object drops the
// result, a changed one asks for a re-analysis of the new text, and anything else
// is a transient store error that the retry map handles. analyzeObject turns
// errAnalysisGone back into a nil return, so the object is not retried.
func analysisWriteError(ctx context.Context, s *store.Store, metaGUID string, metaType storepb.MetaType, cause error, msg string) error {
	switch {
	case errors.Is(cause, store.ErrAnalysisGone):
		slog.Debug("Lineage analysis dropped: the object disappeared during analysis",
			slog.String("guid", metaGUID), slog.String("type", metaType.String()))
		return errAnalysisGone
	case errors.Is(cause, store.ErrAnalysisStale):
		return errAnalysisStale
	default:
		return storeError(ctx, s, metaGUID, metaType, cause, msg)
	}
}

// markAnalyzed records a successful analysis or a deliberate skip (an engine
// without an analyzer) with the current metahash and the analyzer version. A
// non-empty errorMessage documents why analysis was skipped while still
// recording the hash, so the object is not retried until its metadata changes
// or the analyzer rules do. It leaves the stored edges alone: a skip that is not
// about this object's SQL has nothing to say about them.
func markAnalyzed(ctx context.Context, s *store.Store, metaGUID string, metaType storepb.MetaType, metaHash []byte, errorMessage string) error {
	var message *string
	if errorMessage != "" {
		message = &errorMessage
	}
	if err := s.MarkAnalyzedIfCurrent(ctx, metaGUID, metaType, metaHash, lineage.AnalyzerVersion, message); err != nil {
		return analysisWriteError(ctx, s, metaGUID, metaType, err, "failed to record analysis state")
	}
	return nil
}

// emptyDefinitionClearsLineage reports whether an empty stored definition means the
// object's SQL is gone rather than unavailable. A manual or reported statement's text
// can be cleared, and the edges of the SQL it no longer has are wrong. A view or
// materialized view cannot exist without a defining query, so an empty definition there
// is a read failure (for example a MySQL account without SHOW VIEW), and keeping the
// last correct edges is right until the definition is readable again.
func emptyDefinitionClearsLineage(metaType storepb.MetaType) bool {
	switch metaType {
	case storepb.MetaType_MANUAL_SQL, storepb.MetaType_APPLICATION_SQL:
		return true
	default:
		return false
	}
}

// analysisFailureMessage builds the version row's error_message for a
// deterministic analysis failure. A nil cause means there is no SQL to analyze
// at all: the object's definition is empty, which is recorded as a skip rather
// than a parse failure but still clears the previous definition's edges. The
// message is never empty: it is the only explanation the graph offers for a
// graph that was just cleared.
func analysisFailureMessage(cause error) string {
	if cause == nil {
		return "lineage analysis skipped: the object has no SQL definition"
	}
	return fmt.Sprintf("lineage analysis failed: %v", cause)
}

// markAnalysisFailed records a deterministic analysis failure — SQL the analyzer
// could not parse, or an object left with no SQL definition. The current hash is
// stored with the error message so the hourly scan stops re-queueing an unchanged
// definition (a metadata or analyzer change re-queues it), and any lineage a
// previous definition left behind is dropped because it describes SQL the object
// no longer has. Clearing the edges and recording the failure commit together. A
// gap the analysis reported instead of a failure takes the partial path above and
// keeps its edges.
func markAnalysisFailed(ctx context.Context, s *store.Store, metaGUID string, metaType storepb.MetaType, metaHash []byte, cause error) error {
	err := s.MarkAnalysisFailedIfCurrent(ctx, metaGUID, metaType, metaHash, lineage.AnalyzerVersion,
		analysisFailureMessage(cause))
	if err != nil {
		return analysisWriteError(ctx, s, metaGUID, metaType, err, "failed to record analysis failure")
	}
	return nil
}
