package qualityrunner

import (
	"context"
	"log/slog"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// instanceForAdmission loads the instance a batch targets so the batch can be
// admitted against its connection ceiling. A nil instance means the row is gone:
// the executions still run because target resolution records ORPHANED for their
// rules without opening a connection. A read error is transient and defers the
// batch, because executing it could open a connection outside the budget.
func (r *Runner) instanceForAdmission(ctx context.Context, resourceID string) (*store.InstanceMessage, error) {
	if resourceID == "" {
		// A rule whose target GUID could not be attributed to an instance cannot
		// open a connection either; its execution records SKIPPED(ORPHANED).
		return nil, nil
	}
	return r.stores.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &resourceID})
}

// admitBatch reserves the instance's connection budget for one batch and returns
// its lease plus the admission later rules are checked against. A batch holds one
// lease for its whole query phase: its rules execute serially against the same
// instance and need at most one session at a time, and taking the lease up front
// is what keeps a mid-batch admission from failing. A nil instance needs none: its
// rules are ORPHANED before a driver is opened.
func (r *Runner) admitBatch(ctx context.Context, instance *store.InstanceMessage, priority state.ConnectionPriority, wait time.Duration) (func(), batchAdmission, bool) {
	// A gone or metadata-less instance is ORPHANED by target resolution before a
	// driver is opened, so it needs no lease.
	if instance == nil || instance.Deleted || instance.Metadata == nil {
		return func() {}, batchAdmission{priority: priority}, true
	}
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	release, err := r.stateCfg.AcquireInstanceConnection(waitCtx, state.InstanceConnectionRequest{
		InstanceID:         instance.ResourceID,
		MaximumConnections: instanceConnectionLimit(instance),
		Priority:           priority,
	})
	if err != nil {
		return nil, batchAdmission{}, false
	}
	return release, batchAdmission{instanceResourceID: instance.ResourceID, priority: priority}, true
}

// ruleConnectionLease returns the budget covering one rule's driver. The batch's
// own lease covers the normal case. A rule whose newest revision targets another
// instance than the batch was admitted for needs a lease on that instance, and a
// batch that needed no lease at all (its instance row was gone) must lease the
// instance the rule really resolves to before opening a driver.
//
// The fallback tries once instead of waiting: it runs while the batch already
// holds another instance's lease, and a blocking wait between two concurrent
// batches could each hold what the other wants until both deadlines. A rule that
// cannot be covered is SKIPPED and runs on its next schedule.
func (r *Runner) ruleConnectionLease(instance *store.InstanceMessage, admission batchAdmission) (func(), bool) {
	if instance == nil || instance.Deleted || instance.Metadata == nil {
		// Target resolution reports ORPHANED before a driver is opened.
		return func() {}, true
	}
	if admission.instanceResourceID != "" && admission.instanceResourceID == instance.ResourceID {
		return func() {}, true
	}
	return r.stateCfg.TryAcquireInstanceConnection(state.InstanceConnectionRequest{
		InstanceID:         instance.ResourceID,
		MaximumConnections: instanceConnectionLimit(instance),
		Priority:           admission.priority,
	})
}

// instanceConnectionLimit is the instance's configured connection ceiling, with
// the deployment default when the instance does not set one. A non-positive
// setting means "use the default", not "unlimited"; the schema syncer and the
// connection test read it the same way.
func instanceConnectionLimit(instance *store.InstanceMessage) int {
	maximumConnections := int(instance.Metadata.GetMaximumConnections())
	if maximumConnections <= 0 {
		maximumConnections = common.DefaultInstanceMaximumConnections
	}
	return maximumConnections
}

// abandonUnadmittedBatch closes a manual batch that never got the instance's
// connection budget. It executed nothing, so ABANDONED with no outcome is the
// honest terminal state; leaving it PENDING would defer it to the restart sweep
// and show executions that apparently never end.
func (r *Runner) abandonUnadmittedBatch(ctx context.Context, batchID int64, selected int, reason string) {
	// The runner's context is often already cancelled here (a shutdown ended the
	// wait), but the rows must still reach a terminal state: PENDING executions
	// with no queue entry would otherwise linger until a later startup sweep.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), abandonWriteTimeout)
	defer cancel()
	abandoned, err := r.stores.AbandonQualityExecutions(writeCtx, batchID, time.Now().UTC())
	if err != nil {
		slog.Warn("failed to abandon a manual quality batch that never got a connection slot",
			slog.Int64("batch", batchID),
			log.WithError(err))
		// The rows are left to the startup sweep; count the planned work as
		// abandoned so the metric still reflects what the request lost.
		for range selected {
			r.metrics.CountQualityExecutionAbandoned(reason)
		}
		return
	}
	for range abandoned {
		r.metrics.CountQualityExecutionAbandoned(reason)
	}
	if err := r.stores.FinishQualityRun(writeCtx, batchID, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, &storepb.QualityStatusCounts{Total: int32(selected)}, time.Now().UTC()); err != nil {
		slog.Warn("failed to close a manual quality batch that never got a connection slot",
			slog.Int64("batch", batchID),
			log.WithError(err))
	}
}

// abandonEmptyBatch closes an internal batch no execution row was planned for.
func (r *Runner) abandonEmptyBatch(ctx context.Context, batchID int64) {
	if err := r.stores.FinishQualityRun(ctx, batchID, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, nil, time.Now().UTC()); err != nil {
		slog.Warn("failed to close an internal quality batch with no planned executions",
			slog.Int64("batch", batchID),
			log.WithError(err))
	}
}

// abandonBatch abandons a batch's remaining executions and then the batch. It is
// used whenever the batch cannot finish cleanly (a verdict write failed, a task
// was skipped, the batch deadline expired, or the batch could not be marked
// RUNNING): no execution may be left PENDING/RUNNING and the batch must not
// report a partial rollup.
func (r *Runner) abandonBatch(ctx context.Context, st batchExecutionStore, batchID int64, planned int, reason string) {
	// The batch context is usually alive here, but a cancelled one must not stop
	// the terminal writes.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), abandonWriteTimeout)
	defer cancel()
	abandoned, err := st.AbandonQualityExecutions(writeCtx, batchID, time.Now().UTC())
	if err != nil {
		// The rows are left to the startup sweep, but the batch itself must still
		// reach a terminal state: an ABANDONED batch with orphaned rows is more
		// honest than a batch that stays RUNNING forever.
		slog.Error("failed to abandon the remaining executions of a quality batch",
			slog.Int64("batch", batchID),
			log.WithError(err))
	}
	for range abandoned {
		r.metrics.CountQualityExecutionAbandoned(reason)
	}
	counts := &storepb.QualityStatusCounts{Total: int32(planned)}
	if err := st.FinishQualityRun(writeCtx, batchID, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, counts, time.Now().UTC()); err != nil {
		slog.Error("failed to abandon a quality batch that could not finish cleanly",
			slog.Int64("batch", batchID),
			log.WithError(err))
	}
}
