package qualityrunner

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// referenceCloseTimeout bounds the best-effort close of a reference session. The
// close runs detached from the execution context so a batch deadline or a
// shutdown cannot leave the session open and the lease held.
const referenceCloseTimeout = 10 * time.Second

// errReferenceConnectionUnavailable reports that the reference instance's
// connection budget was busy when a reconciliation needed its own session. It is
// a transient capacity condition, not a verdict on the check: the execution is
// SKIPPED and runs again on its next schedule.
var errReferenceConnectionUnavailable = errors.New("the reference instance had no free connection slot")

// resolveReferenceTargetFunc resolves a cross-table rule's reference relation to
// its metadata. NewRunner binds it to the store; a test replaces it to exercise the
// reference wiring without a database.
type resolveReferenceTargetFunc func(ctx context.Context, guid string, metaType storepb.MetaType) (*dataquality.Resolution, error)

// prepareReferenceExecution resolves a reconciliation's reference relation and
// builds the executor option that opens its own read-only session. The relation is
// a second registry target, so it resolves with the same store read as the rule's
// own target. A relation that no longer resolves is a stored definition that cannot
// execute, so the error carries Invalid: the caller records ERROR(UNSUPPORTED),
// the same class as a target that lost a named column.
//
// The option always carries an opener; the executor calls it only when the two
// sides cannot share the target's connection, so a same-container reference costs
// no second session.
func (r *Runner) prepareReferenceExecution(ctx context.Context, snapshot *dataquality.RuleSnapshot, admission batchAdmission) (*dataquality.ReferenceExecution, error) {
	reference := snapshot.Config.GetCrossTable().GetReference()
	resolved, err := r.resolveReferenceTarget(ctx, reference.GetMetaGuid(), reference.GetMetaType())
	if err != nil {
		// A reference the registry no longer has is a stored definition that cannot
		// execute: ERROR(UNSUPPORTED), like a target that lost a named column. A
		// transient store failure is not the definition's fault, so it keeps its own
		// classification (ERROR(EXECUTION)) instead of being reported as unsupported.
		if common.ErrorCode(err) == common.NotFound {
			return nil, common.Errorf(common.Invalid, "%s", err)
		}
		return nil, err
	}
	return &dataquality.ReferenceExecution{
		Resolution: resolved,
		Open: func(ctx context.Context) (db.ReadOnlyQueryer, func(), error) {
			return r.openReferenceSession(ctx, resolved, admission)
		},
	}, nil
}

// openReferenceSession opens the reference side's own read-only session under a
// non-blocking lease. By the time a reconciliation needs a second session the
// batch already holds its target instance's lease, so a blocking wait here could
// deadlock two batches that each hold what the other wants; a busy reference
// instance therefore comes back as errReferenceConnectionUnavailable and the
// execution is skipped instead of failed. The lease is taken only when the session
// is really needed, never at admission.
func (r *Runner) openReferenceSession(ctx context.Context, reference *dataquality.Resolution, admission batchAdmission) (db.ReadOnlyQueryer, func(), error) {
	release, ok := r.stateCfg.TryAcquireInstanceConnection(state.InstanceConnectionRequest{
		InstanceID:         reference.Instance.ResourceID,
		MaximumConnections: instanceConnectionLimit(reference.Instance),
		Priority:           admission.priority,
	})
	if !ok {
		return nil, nil, errReferenceConnectionUnavailable
	}
	// The lease is released on every path that does not hand it to the caller: the
	// driver factory is not expected to panic, but a panic there must not leak the
	// slot. The lease function is a sync.Once, so this safety net and
	// releaseReferenceSession cannot free two slots.
	handedOff := false
	defer func() {
		if !handedOff {
			release()
		}
	}()
	driver, err := r.dbFactory.GetReadOnlyDatabaseDriver(ctx, reference.Instance, reference.Database)
	if err != nil {
		return nil, nil, err
	}
	queryer, ok := driver.(db.ReadOnlyQueryer)
	if !ok {
		releaseReferenceSession(ctx, driver, release)
		return nil, nil, common.Errorf(common.Invalid, "this database engine does not support read-only query execution")
	}
	handedOff = true
	var once sync.Once
	return queryer, func() {
		once.Do(func() { releaseReferenceSession(ctx, driver, release) })
	}, nil
}

// releaseReferenceSession closes a reference driver and returns its lease. It runs
// on a detached context because the executor releases the session from a defer that
// may fire after the batch deadline or a shutdown cancelled the execution context.
// The once-guard in the returned release makes this the single close.
func releaseReferenceSession(ctx context.Context, driver db.Driver, release func()) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), referenceCloseTimeout)
	defer cancel()
	if err := driver.Close(closeCtx); err != nil {
		slog.Warn("failed to close a quality rule's reference database session", log.WithError(err))
	}
	release()
}
