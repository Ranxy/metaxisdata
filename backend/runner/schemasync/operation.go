package schemasync

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/notification"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// operationTimeout bounds how long an operation waits for its databases. A full
// sync of a large instance runs at MaximumOutstanding concurrency and retries a
// failed database twice, so this is deliberately far above one sync's 15-minute
// deadline: it is the backstop that turns "somebody dropped this database from
// the queue" into a message instead of a leak.
const operationTimeout = 2 * time.Hour

// Notifier delivers the messages a sync operation produces. *notification.Service
// is the production implementation; the interface keeps the runner testable
// without a database.
type Notifier interface {
	Send(ctx context.Context, message *storepb.Notification) error
	SendToWorkspaceAdmins(ctx context.Context, message *storepb.Notification) error
}

// SyncOperation is one user-visible sync operation: the instance metadata step
// plus every database that step queued.
//
// The API registers it, the checker reports each database's outcome, and exactly
// one caller finishes it and writes the message. It is process state, like the
// queue it tracks: a restart loses the operations in flight, and so does it lose
// the syncs themselves.
type SyncOperation struct {
	mu sync.Mutex

	initiatorID   int
	trigger       storepb.SyncTrigger
	instanceID    string
	instanceTitle string
	deadline      time.Time

	// instanceRecorded and sealed are the two edges of "the caller is done
	// telling us things": the instance step has reported, and no database will be
	// added. Only an operation past both of them may finish, or one whose
	// databases complete faster than the caller finishes talking would be
	// reported before it started.
	instanceRecorded bool
	sealed           bool
	done             bool

	instanceErr string
	// pending holds the databases this operation is still waiting for, keyed the
	// way the checker's queue is.
	pending    map[string]*store.DatabaseMessage
	succeeded  int
	failed     int
	unfinished int
	// databases are the entries the message names, which for an instance-wide
	// operation are the databases that failed or never reported.
	databases []*storepb.SyncDatabaseResult
}

// StartOperation registers one sync operation for the caller.
func (s *Syncer) StartOperation(trigger storepb.SyncTrigger, initiatorID int, instance *store.InstanceMessage) *SyncOperation {
	operation := &SyncOperation{
		initiatorID:   initiatorID,
		trigger:       trigger,
		instanceID:    instance.ResourceID,
		instanceTitle: instance.Metadata.GetTitle(),
		deadline:      time.Now().Add(operationTimeout),
		pending:       map[string]*store.DatabaseMessage{},
	}

	s.operationMu.Lock()
	s.operations = append(s.operations, operation)
	s.operationMu.Unlock()
	return operation
}

// RecordInstanceResult records the instance metadata step's outcome.
func (s *Syncer) RecordInstanceResult(ctx context.Context, operation *SyncOperation, err error) {
	if operation == nil {
		return
	}

	operation.mu.Lock()
	if err != nil {
		operation.instanceErr = err.Error()
	}
	operation.instanceRecorded = true
	operation.mu.Unlock()
	s.maybeFinishOperation(ctx, operation)
}

// EnqueueDatabases queues the databases this operation covers and seals it: no
// database may be added afterwards, so an operation whose databases all report
// finishes here. An empty list is how a caller says "the instance step was all
// there was".
func (s *Syncer) EnqueueDatabases(ctx context.Context, operation *SyncOperation, databases []*store.DatabaseMessage) {
	if operation == nil {
		return
	}

	for _, database := range databases {
		// A database that cannot be synced is not registered either, or the
		// operation would wait for a result that can never come.
		if database == nil || database.Deleted {
			continue
		}
		// The database is registered before it is queued, so the checker can never
		// report a result the operation has not heard of.
		operation.mu.Lock()
		operation.pending[database.String()] = database
		operation.mu.Unlock()
		s.enqueueDatabase(database, operation)
	}

	operation.mu.Lock()
	operation.sealed = true
	operation.mu.Unlock()
	s.maybeFinishOperation(ctx, operation)
}

// FinishOperation seals an operation that has no databases to wait for, because
// the instance step was the whole of it.
func (s *Syncer) FinishOperation(ctx context.Context, operation *SyncOperation) {
	s.EnqueueDatabases(ctx, operation, nil)
}

// completeDatabase reports one database's outcome to every operation waiting for
// it. A database can belong to more than one operation — the API and the periodic
// scan can both queue it, and the checker serializes them into a single sync — so
// the result is fanned out rather than claimed by the first operation.
func (s *Syncer) completeDatabase(ctx context.Context, database *store.DatabaseMessage, err error) {
	if database == nil {
		return
	}

	key := database.String()
	s.operationMu.Lock()
	var waiting []*SyncOperation
	for _, operation := range s.operations {
		operation.mu.Lock()
		_, pending := operation.pending[key]
		if pending {
			delete(operation.pending, key)
			if err != nil {
				operation.failed++
				operation.databases = append(operation.databases, &storepb.SyncDatabaseResult{
					Database: common.FormatDatabase(database.InstanceID, database.DatabaseName),
					State:    storepb.SyncDatabaseState_SYNC_DATABASE_STATE_FAILED,
					Error:    err.Error(),
				})
			} else {
				operation.succeeded++
			}
			waiting = append(waiting, operation)
		}
		operation.mu.Unlock()
	}
	s.operationMu.Unlock()

	for _, operation := range waiting {
		s.maybeFinishOperation(ctx, operation)
	}
}

// expireOperations finishes the operations whose deadline has passed, recording
// the databases that never reported. An operation that is waiting on a database
// the checker will never see again — the instance was deleted while its sync was
// queued — reaches the inbox this way instead of waiting forever.
//
// An operation that is merely slow is not expired: the deadline is a window, and a
// database still queued or being synced renews it. Without that, a full sync of a
// large estate — a thousand databases at a hundred concurrent, each with its own
// fifteen-minute deadline — could outlive the first window and be reported as
// unfinished while it is in fact still running.
func (s *Syncer) expireOperations(ctx context.Context, now time.Time) {
	s.operationMu.Lock()
	var expired []*SyncOperation
	for _, operation := range s.operations {
		operation.mu.Lock()
		if operation.done || now.Before(operation.deadline) {
			operation.mu.Unlock()
			continue
		}
		if s.hasWorkInProgressLocked(operation) {
			operation.deadline = now.Add(operationTimeout)
			operation.mu.Unlock()
			continue
		}
		for _, database := range operation.pending {
			operation.unfinished++
			operation.databases = append(operation.databases, &storepb.SyncDatabaseResult{
				Database: common.FormatDatabase(database.InstanceID, database.DatabaseName),
				State:    storepb.SyncDatabaseState_SYNC_DATABASE_STATE_UNFINISHED,
			})
		}
		operation.pending = map[string]*store.DatabaseMessage{}
		// The operation never got to finish its own bookkeeping; the deadline is
		// what makes it reportable anyway.
		operation.instanceRecorded = true
		operation.sealed = true
		expired = append(expired, operation)
		operation.mu.Unlock()
	}
	s.operationMu.Unlock()

	for _, operation := range expired {
		s.maybeFinishOperation(ctx, operation)
	}
}

// hasWorkInProgressLocked reports whether any database the operation waits for is
// still queued or being synced. The caller must hold operation.mu.
//
// A queued database is one the checker has not started yet — it may be waiting out a
// retry's backoff — and an in-flight one has been dequeued but not reported, so it is
// in neither the queue nor the finished set. Both mean the result is still coming.
func (s *Syncer) hasWorkInProgressLocked(operation *SyncOperation) bool {
	for key := range operation.pending {
		if _, queued := s.databaseSyncMap.Load(key); queued {
			return true
		}
		if s.databaseSyncGate.owns(key) {
			return true
		}
	}
	return false
}

// maybeFinishOperation finishes the operation for the one caller that finds it
// ready, and delivers the message it earned.
func (s *Syncer) maybeFinishOperation(ctx context.Context, operation *SyncOperation) {
	operation.mu.Lock()
	ready := !operation.done && operation.sealed && operation.instanceRecorded && len(operation.pending) == 0
	if ready {
		operation.done = true
	}
	operation.mu.Unlock()
	if !ready {
		return
	}
	s.deliverOperation(ctx, operation)
}

// deliverOperation writes the operation's message to the user who asked for the
// sync, and drops it from the live set.
func (s *Syncer) deliverOperation(ctx context.Context, operation *SyncOperation) {
	operation.mu.Lock()
	detail := &storepb.SchemaSyncDetail{
		Instance:        common.FormatInstance(operation.instanceID),
		InstanceTitle:   operation.instanceTitle,
		Trigger:         operation.trigger,
		InstanceError:   operation.instanceErr,
		Databases:       operation.databases,
		SucceededCount:  int32(operation.succeeded),
		FailedCount:     int32(operation.failed),
		UnfinishedCount: int32(operation.unfinished),
	}
	initiatorID := operation.initiatorID
	operation.mu.Unlock()

	s.operationMu.Lock()
	s.operations = slices.DeleteFunc(s.operations, func(candidate *SyncOperation) bool {
		return candidate == operation
	})
	s.operationMu.Unlock()

	if s.notifier == nil || initiatorID <= 0 {
		return
	}

	// One message per operation, and its severity is the worst thing in it: an
	// instance the user asked for that did not finish is not an "info".
	severity := storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO
	if detail.GetInstanceError() != "" || detail.GetFailedCount() > 0 || detail.GetUnfinishedCount() > 0 {
		severity = storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR
	}
	if err := s.notifier.Send(ctx, notification.SchemaSyncMessage(initiatorID, severity, detail)); err != nil {
		notification.LogFailure(err,
			slog.String("instance", operation.instanceID),
			slog.Int("initiator", initiatorID))
	}
}

// SyncDatabaseForUser syncs one database and reports the outcome to the user who
// asked for it. It returns the sync error so the caller can still answer the
// request with it; the message is the durable record of the outcome, which is
// what the caller has nothing else to leave behind.
func (s *Syncer) SyncDatabaseForUser(ctx context.Context, database *store.DatabaseMessage, initiatorID int) error {
	err := s.SyncDatabaseSchema(ctx, database)
	if s.notifier == nil || initiatorID <= 0 {
		return err
	}

	detail := &storepb.SchemaSyncDetail{
		Instance:      common.FormatInstance(database.InstanceID),
		InstanceTitle: s.instanceTitle(ctx, database.InstanceID),
		Trigger:       storepb.SyncTrigger_SYNC_TRIGGER_MANUAL,
	}
	severity := storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO
	// The message names the database either way: a user who synced one database has
	// nothing else to tell the one that finished from the next one.
	result := &storepb.SyncDatabaseResult{
		Database: common.FormatDatabase(database.InstanceID, database.DatabaseName),
		State:    storepb.SyncDatabaseState_SYNC_DATABASE_STATE_SUCCEEDED,
	}
	if err != nil {
		severity = storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR
		detail.FailedCount = 1
		result.State = storepb.SyncDatabaseState_SYNC_DATABASE_STATE_FAILED
		result.Error = err.Error()
	} else {
		detail.SucceededCount = 1
	}
	detail.Databases = []*storepb.SyncDatabaseResult{result}

	if sendErr := s.notifier.Send(ctx, notification.SchemaSyncMessage(initiatorID, severity, detail)); sendErr != nil {
		notification.LogFailure(sendErr,
			slog.String("database", database.String()),
			slog.Int("initiator", initiatorID))
	}
	return err
}

// notifyBackgroundDatabaseFailure tells the workspace administrators that a
// database has failed every retry. Nobody asked for this sync, so there is no
// initiator; the message carries an hour-bucketed dedupe key so a target that
// stays broken reports once per window rather than once per backoff cycle.
func (s *Syncer) notifyBackgroundDatabaseFailure(ctx context.Context, instance *store.InstanceMessage, database *store.DatabaseMessage, cause error) {
	if s.notifier == nil {
		return
	}

	detail := &storepb.SchemaSyncDetail{
		Instance:      common.FormatInstance(database.InstanceID),
		InstanceTitle: instance.Metadata.GetTitle(),
		Trigger:       storepb.SyncTrigger_SYNC_TRIGGER_BACKGROUND,
		FailedCount:   1,
		Databases: []*storepb.SyncDatabaseResult{{
			Database: common.FormatDatabase(database.InstanceID, database.DatabaseName),
			State:    storepb.SyncDatabaseState_SYNC_DATABASE_STATE_FAILED,
			Error:    cause.Error(),
		}},
	}
	message := notification.SchemaSyncMessage(0, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR, detail)
	message.DedupeKey = notification.DedupeKey("schema-sync.database", database.String(), time.Now(), notification.BackgroundFailureWindow)
	if err := s.notifier.SendToWorkspaceAdmins(ctx, message); err != nil {
		notification.LogFailure(err, slog.String("database", database.String()))
	}
}

// notifyBackgroundInstanceFailure reports an instance the periodic scan could not
// sync at all. The instance's last sync time does not advance on failure, so the
// scan meets the same failure every fifteen minutes; the hour bucket is what
// keeps that from being fifteen messages an hour.
func (s *Syncer) notifyBackgroundInstanceFailure(ctx context.Context, instance *store.InstanceMessage, cause error) {
	if s.notifier == nil {
		return
	}

	detail := &storepb.SchemaSyncDetail{
		Instance:      common.FormatInstance(instance.ResourceID),
		InstanceTitle: instance.Metadata.GetTitle(),
		Trigger:       storepb.SyncTrigger_SYNC_TRIGGER_BACKGROUND,
		InstanceError: cause.Error(),
	}
	message := notification.SchemaSyncMessage(0, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR, detail)
	message.DedupeKey = notification.DedupeKey("schema-sync.instance", instance.ResourceID, time.Now(), notification.BackgroundFailureWindow)
	if err := s.notifier.SendToWorkspaceAdmins(ctx, message); err != nil {
		notification.LogFailure(err, slog.String("instance", instance.ResourceID))
	}
}

// instanceTitle resolves an instance's display title, best effort: a message
// that cannot name the instance still names its resource.
func (s *Syncer) instanceTitle(ctx context.Context, instanceID string) string {
	instance, err := s.store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &instanceID})
	if err != nil || instance == nil {
		return ""
	}
	return instance.Metadata.GetTitle()
}
