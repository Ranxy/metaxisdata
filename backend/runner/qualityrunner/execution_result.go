package qualityrunner

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// observeRuleExecution records the per-rule funnel exactly once, right where the
// batch collects each written execution. This loop is the only place that sees
// every verdict an execution attempt produces, so buildExecutionMessage (the pure
// row mapping) must not record it too.
func (r *Runner) observeRuleExecution(execution *ruleExecution) {
	if execution == nil || execution.result == nil {
		return
	}
	ruleType := ruleExecutionType(execution)
	r.metrics.ObserveQualityRuleExecution(ruleType, execution.result.Status, time.Duration(execution.result.DurationMS)*time.Millisecond)
	r.metrics.CountQualityRuleError(ruleType, execution.result.ErrorCode)
}

// ruleExecutionType derives the rule type a metric should label an execution
// with. A rule that failed before its snapshot was read (an inflight skip, a
// retired revision) still names its type on the due scan's head row; a manual
// execution has no head, so the last resort is "unspecified" rather than a wrong
// label.
func ruleExecutionType(execution *ruleExecution) storepb.QualityRuleType {
	if execution == nil {
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED
	}
	if execution.snapshot != nil {
		return execution.snapshot.Type
	}
	if execution.head != nil {
		return execution.head.RuleType
	}
	return storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED
}

// loadRuleSnapshot loads the immutable definition an execution is judged with. A
// rule edited after its execution row was planned is executed with its newest
// revision, and FinishQualityExecution records the revision the verdict really
// judged; the planned row is never rewritten by an edit.
func (r *Runner) loadRuleSnapshot(ctx context.Context, ruleID int64) (*dataquality.RuleSnapshot, *store.QualityRuleMessage, error) {
	limit := 1
	revisions, err := r.stores.ListQualityRuleRevisions(ctx, ruleID, &limit, nil)
	if err != nil {
		return nil, nil, err
	}
	if len(revisions) == 0 {
		return nil, nil, common.Errorf(common.NotFound, "quality rule %d has no revision", ruleID)
	}

	revision := revisions[0]
	// The mutable head carries the owner, severity, mute and resource name the
	// problem ledger and the notifications need. It is read by internal id with no
	// lifecycle filter, so an archived or deleted rule still resolves and retires.
	head := r.resolveRuleHead(ctx, ruleID)
	if head != nil && (head.Deleted || head.State == storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED) {
		return nil, head, errRuleRetired
	}
	return &dataquality.RuleSnapshot{
		ID:        revision.RuleID,
		Revision:  revision.Revision,
		Type:      revision.RuleType,
		Dimension: revision.Dimension,
		Severity:  revision.Severity,
		Target: dataquality.Target{
			GUID:     revision.TargetGUID,
			MetaType: revision.TargetType,
			Path:     revision.TargetPath,
		},
		Config: revision.Config,
	}, head, nil
}

// ruleBaseline returns the rule's last evaluated observation, the denominator
// row_count_change compares the current count against. An unreadable baseline is
// not fatal: the rule then reports NOT_EVALUATED, which is the correct "no
// baseline" verdict rather than a made-up comparison.
func (r *Runner) ruleBaseline(ctx context.Context, ruleID int64, interval time.Duration) *float64 {
	executions, err := r.stores.ListRecentQualityExecutions(ctx, ruleID, 1)
	if err != nil {
		slog.Warn("failed to read a quality rule baseline",
			slog.Int64("rule", ruleID),
			log.WithError(err))
		return nil
	}
	return baselineFromExecutions(executions, time.Now().UTC(), interval)
}

// acquireInflight marks one rule as being executed and reports whether this
// caller owns it. The guard is process-local, like the design's in-flight map: a
// second execution of the same rule records SKIPPED instead of executing it
// twice.
func (r *Runner) acquireInflight(ruleID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.inflight[ruleID]; ok {
		return false
	}
	r.inflight[ruleID] = struct{}{}
	return true
}

// releaseInflight clears one rule's in-flight mark.
func (r *Runner) releaseInflight(ruleID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.inflight, ruleID)
}

// buildExecutionMessage maps one execution outcome onto the row
// FinishQualityExecution writes. The planned PENDING row supplies the identity
// (id, batch, rule, trigger, initiator) and its revision/target snapshot; a
// verdict that read a snapshot overwrites them with what was really judged, and a
// verdict that failed before it could read one keeps the planned values.
func buildExecutionMessage(planned *store.QualityExecutionMessage, outcome *ruleExecutionOutcome) *store.QualityExecutionMessage {
	execution := &store.QualityExecutionMessage{}
	if planned != nil {
		execution.ID = planned.ID
		execution.RunID = planned.RunID
		execution.RuleID = planned.RuleID
		execution.RuleRevision = planned.RuleRevision
		execution.TargetGUID = planned.TargetGUID
		execution.TargetType = planned.TargetType
		execution.TargetPath = planned.TargetPath
		execution.InstanceResourceID = planned.InstanceResourceID
		execution.Trigger = planned.Trigger
		execution.InitiatorID = planned.InitiatorID
	}
	execution.RuleEngineVersion = dataquality.EngineVersion
	execution.State = storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED
	execution.Status = outcome.state.Status
	execution.ObservedKind = outcome.observation.ObservedKind
	execution.Thresholds = outcome.state.Thresholds
	execution.FailedRows = outcome.observation.FailedRows
	execution.TotalRows = outcome.observation.TotalRows
	execution.Sampled = outcome.observation.Sampled
	execution.SampleRows = outcome.observation.SampleRows
	execution.Truncated = outcome.observation.Truncated
	execution.ErrorCode = outcome.errorCode
	execution.ErrorMessage = truncateErrorMessage(outcome.errorMessage)
	execution.DurationMS = durationMS(outcome.startedAt, outcome.finishedAt)
	execution.StartedAt = outcome.startedAt
	execution.FinishedAt = outcome.finishedAt
	if outcome.snapshot != nil {
		execution.RuleRevision = outcome.snapshot.Revision
		execution.TargetGUID = outcome.snapshot.Target.GUID
		execution.TargetType = outcome.snapshot.Target.MetaType
		execution.TargetPath = outcome.snapshot.Target.Path
	}
	if outcome.execution != nil {
		execution.Sample = outcome.execution.Samples
		execution.ExecutedSQL = outcome.execution.ExecutedSQL
		execution.Sampled = outcome.execution.Sampled
		execution.Truncated = outcome.execution.Truncated
	}
	execution.ObservedValue = observedValue(outcome.state.Status, outcome.observation.Observed)
	return execution
}

// failureExecutionMessage records an execution that could not be judged. ERROR and
// SKIPPED verdicts carry no observation, and their thresholds snapshot is nil
// because no verdict was reached. The snapshot is the definition the execution
// actually read, when one was read: a failure after loading it must name that
// revision and target, not the revision the due scan planned minutes earlier.
func failureExecutionMessage(planned *store.QualityExecutionMessage, snapshot *dataquality.RuleSnapshot, startedAt time.Time, status storepb.QualityStatus, code, message string) *store.QualityExecutionMessage {
	return buildExecutionMessage(planned, &ruleExecutionOutcome{
		snapshot:     snapshot,
		startedAt:    startedAt,
		finishedAt:   time.Now().UTC(),
		state:        dataquality.ResultState{Status: status},
		errorCode:    code,
		errorMessage: message,
	})
}

// observedValue decides whether an execution stores a measured value. ERROR,
// SKIPPED and NOT_EVALUATED verdicts have no measurement; storing zero for them
// would be indistinguishable from a real zero and would make a trend chart invent
// a value.
func observedValue(status storepb.QualityStatus, observed float64) *float64 {
	switch status {
	case storepb.QualityStatus_QUALITY_STATUS_PASS,
		storepb.QualityStatus_QUALITY_STATUS_WARN,
		storepb.QualityStatus_QUALITY_STATUS_FAIL:
		value := observed
		return &value
	case storepb.QualityStatus_QUALITY_STATUS_ERROR,
		storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
		storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED:
		return nil
	case storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED:
		return nil
	default:
		return nil
	}
}

// durationMS is how long one rule took, clamped to the INT4 column.
func durationMS(startedAt, finishedAt time.Time) int32 {
	elapsed := finishedAt.Sub(startedAt).Milliseconds()
	if elapsed < 0 {
		return 0
	}
	if elapsed > int64(^uint32(0)>>1) {
		return int32(^uint32(0) >> 1)
	}
	return int32(elapsed)
}

// truncateErrorMessage bounds the stored error text so one broken query cannot put
// an unbounded blob in an execution row. It cuts on a rune boundary because the
// column is UTF-8 text.
func truncateErrorMessage(message string) string {
	return common.TruncateUTF8Bytes(message, maxErrorMessageLength)
}

// errorCodeAndMessage classifies an error that happened before or during the
// read-only query. Every error goes through the engine's classifier: capacity
// contention is never one of them, because a batch is admitted against the
// instance's connection budget before it starts and holds the lease throughout.
func errorCodeAndMessage(err error) (code string, message string) {
	if errors.Is(err, dbfactory.ErrNoReadOnlyDataSource) {
		return dataquality.ErrCodeNoReadOnly, err.Error()
	}
	code, message = dataquality.ClassifyError(err)
	if message == "" {
		message = err.Error()
	}
	return code, message
}
