package schemasync

import (
	"context"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// fakeNotifier captures what a sync operation reports, so the aggregation can be
// exercised without a store or a database.
type fakeNotifier struct {
	sent  []*storepb.Notification
	admin []*storepb.Notification
	err   error
}

func (f *fakeNotifier) Send(_ context.Context, message *storepb.Notification) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, message)
	return nil
}

func (f *fakeNotifier) SendToWorkspaceAdmins(_ context.Context, message *storepb.Notification) error {
	if f.err != nil {
		return f.err
	}
	f.admin = append(f.admin, message)
	return nil
}

// testOperation registers an operation over databases and takes them back out of the
// checker's queue: the checker dequeues a database before syncing it, so what is left
// in the operation's pending set is what the tests drive by hand.
func testOperation(syncer *Syncer, initiatorID int, databases ...*store.DatabaseMessage) *SyncOperation {
	instance := &store.InstanceMessage{ResourceID: "inst-1", Metadata: &storepb.Instance{Title: "prod"}}
	operation := syncer.StartOperation(storepb.SyncTrigger_SYNC_TRIGGER_MANUAL, initiatorID, instance)
	syncer.RecordInstanceResult(context.Background(), operation, nil)
	syncer.EnqueueDatabases(context.Background(), operation, databases)
	for _, database := range databases {
		syncer.databaseSyncMap.Delete(database.String())
	}
	return operation
}

// One message per operation, however many databases it covered: a full sync of a
// hundred databases must not be a hundred rows in somebody's inbox.
func TestOperationReportsOneMessageWhenEveryDatabaseSucceeds(t *testing.T) {
	t.Parallel()

	notifier := &fakeNotifier{}
	syncer := &Syncer{notifier: notifier}
	databases := []*store.DatabaseMessage{
		{InstanceID: "inst-1", DatabaseName: "app"},
		{InstanceID: "inst-1", DatabaseName: "audit"},
	}
	operation := testOperation(syncer, 7, databases...)

	syncer.completeDatabase(context.Background(), databases[0], nil)
	require.Empty(t, notifier.sent, "the operation is still waiting for the second database")

	syncer.completeDatabase(context.Background(), databases[1], nil)
	require.Len(t, notifier.sent, 1)

	message := notifier.sent[0]
	require.Equal(t, int32(7), message.GetRecipientId())
	require.Equal(t, storepb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC, message.GetType())
	require.Equal(t, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO, message.GetSeverity())
	require.Empty(t, message.GetDedupeKey(), "a message to the initiator is not deduplicated")
	require.Equal(t, "instances/inst-1", message.GetSchemaSync().GetInstance())
	require.Equal(t, "prod", message.GetSchemaSync().GetInstanceTitle())
	require.Equal(t, storepb.SyncTrigger_SYNC_TRIGGER_MANUAL, message.GetSchemaSync().GetTrigger())
	require.Equal(t, int32(2), message.GetSchemaSync().GetSucceededCount())
	require.Zero(t, message.GetSchemaSync().GetFailedCount())
	require.Empty(t, operation.databases)

	// The finished operation is gone: a later result for the same database cannot
	// resurrect it.
	syncer.completeDatabase(context.Background(), databases[0], errors.New("late"))
	require.Len(t, notifier.sent, 1)
}

func TestOperationReportsEachFailingDatabase(t *testing.T) {
	t.Parallel()

	notifier := &fakeNotifier{}
	syncer := &Syncer{notifier: notifier}
	databases := []*store.DatabaseMessage{
		{InstanceID: "inst-1", DatabaseName: "app"},
		{InstanceID: "inst-1", DatabaseName: "audit"},
	}
	operation := testOperation(syncer, 7, databases...)

	syncer.completeDatabase(context.Background(), databases[0], nil)
	syncer.completeDatabase(context.Background(), databases[1], errors.New("permission denied"))
	require.Len(t, notifier.sent, 1)

	detail := notifier.sent[0].GetSchemaSync()
	require.Equal(t, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR, notifier.sent[0].GetSeverity())
	require.Equal(t, int32(1), detail.GetSucceededCount())
	require.Equal(t, int32(1), detail.GetFailedCount())
	require.Len(t, detail.GetDatabases(), 1)
	require.Equal(t, "instances/inst-1/databases/audit", detail.GetDatabases()[0].GetDatabase())
	require.Equal(t, storepb.SyncDatabaseState_SYNC_DATABASE_STATE_FAILED, detail.GetDatabases()[0].GetState())
	require.Equal(t, "permission denied", detail.GetDatabases()[0].GetError())
	require.Equal(t, 1, operation.failed)
}

// A database can complete before the caller has finished registering the
// operation; reporting then would describe an operation that had not started.
func TestOperationWaitsForTheInstanceStep(t *testing.T) {
	t.Parallel()

	notifier := &fakeNotifier{}
	syncer := &Syncer{notifier: notifier}
	instance := &store.InstanceMessage{ResourceID: "inst-1"}
	database := &store.DatabaseMessage{InstanceID: "inst-1", DatabaseName: "app"}

	operation := syncer.StartOperation(storepb.SyncTrigger_SYNC_TRIGGER_MANUAL, 7, instance)
	syncer.EnqueueDatabases(context.Background(), operation, []*store.DatabaseMessage{database})
	syncer.completeDatabase(context.Background(), database, nil)
	require.Empty(t, notifier.sent)

	syncer.RecordInstanceResult(context.Background(), operation, nil)
	require.Len(t, notifier.sent, 1)
	require.Equal(t, int32(1), notifier.sent[0].GetSchemaSync().GetSucceededCount())
}

// An instance the sync could not read is reported on its own, without waiting for
// databases that were never queued.
func TestOperationReportsAFailedInstanceStep(t *testing.T) {
	t.Parallel()

	notifier := &fakeNotifier{}
	syncer := &Syncer{notifier: notifier}
	instance := &store.InstanceMessage{ResourceID: "inst-1", Metadata: &storepb.Instance{Title: "prod"}}

	operation := syncer.StartOperation(storepb.SyncTrigger_SYNC_TRIGGER_MANUAL, 7, instance)
	syncer.RecordInstanceResult(context.Background(), operation, errors.New("connection refused"))
	syncer.FinishOperation(context.Background(), operation)

	require.Len(t, notifier.sent, 1)
	require.Equal(t, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR, notifier.sent[0].GetSeverity())
	require.Equal(t, "connection refused", notifier.sent[0].GetSchemaSync().GetInstanceError())
	require.Equal(t, "prod", notifier.sent[0].GetSchemaSync().GetInstanceTitle())
}

// A database the checker will never report on — its instance disappeared while its
// sync was queued — still reaches the inbox, as an unfinished entry at the deadline.
func TestOperationReportsUnfinishedDatabasesAtTheDeadline(t *testing.T) {
	t.Parallel()

	notifier := &fakeNotifier{}
	syncer := &Syncer{notifier: notifier}
	databases := []*store.DatabaseMessage{
		{InstanceID: "inst-1", DatabaseName: "app"},
		{InstanceID: "inst-1", DatabaseName: "audit"},
	}
	operation := testOperation(syncer, 7, databases...)
	syncer.completeDatabase(context.Background(), databases[0], nil)
	require.Empty(t, notifier.sent)

	operation.mu.Lock()
	operation.deadline = time.Now().Add(-time.Minute)
	operation.mu.Unlock()
	syncer.expireOperations(context.Background(), time.Now())

	require.Len(t, notifier.sent, 1)
	detail := notifier.sent[0].GetSchemaSync()
	require.Equal(t, int32(1), detail.GetSucceededCount())
	require.Equal(t, int32(1), detail.GetUnfinishedCount())
	require.Len(t, detail.GetDatabases(), 1)
	require.Equal(t, storepb.SyncDatabaseState_SYNC_DATABASE_STATE_UNFINISHED, detail.GetDatabases()[0].GetState())
	require.Empty(t, detail.GetDatabases()[0].GetError())

	// The deadline does not fire twice for the same operation.
	syncer.expireOperations(context.Background(), time.Now())
	require.Len(t, notifier.sent, 1)
}

// A database still waiting for its turn is progress, not a stall: the deadline is
// there to catch a result that will never come, not to cut off a slow one.
func TestOperationDeadlineRenewsWhileADatabaseIsStillQueued(t *testing.T) {
	t.Parallel()

	notifier := &fakeNotifier{}
	syncer := &Syncer{notifier: notifier}
	instance := &store.InstanceMessage{ResourceID: "inst-1", Metadata: &storepb.Instance{Title: "prod"}}
	database := &store.DatabaseMessage{InstanceID: "inst-1", DatabaseName: "app"}

	operation := syncer.StartOperation(storepb.SyncTrigger_SYNC_TRIGGER_MANUAL, 7, instance)
	syncer.RecordInstanceResult(context.Background(), operation, nil)
	syncer.EnqueueDatabases(context.Background(), operation, []*store.DatabaseMessage{database})

	operation.mu.Lock()
	operation.deadline = time.Now().Add(-time.Minute)
	operation.mu.Unlock()
	syncer.expireOperations(context.Background(), time.Now())

	require.Empty(t, notifier.sent, "a queued database is progress, not a stall")
	operation.mu.Lock()
	renewed := operation.deadline.After(time.Now())
	operation.mu.Unlock()
	require.True(t, renewed, "the window is renewed while there is work in progress")

	// The queue drops it without a result — the instance disappeared, say, and no
	// completion ever arrives: the window now closes and the database is reported.
	syncer.databaseSyncMap.Delete(database.String())
	operation.mu.Lock()
	operation.deadline = time.Now().Add(-time.Minute)
	operation.mu.Unlock()
	syncer.expireOperations(context.Background(), time.Now())

	require.Len(t, notifier.sent, 1)
	require.Equal(t, int32(1), notifier.sent[0].GetSchemaSync().GetUnfinishedCount())
}

// The API and the periodic scan can queue the same database, and the checker
// serializes them into one sync. Both operations asked for its outcome, so both
// have to hear it.
func TestDatabaseResultReachesEveryWaitingOperation(t *testing.T) {
	t.Parallel()

	notifier := &fakeNotifier{}
	syncer := &Syncer{notifier: notifier}
	database := &store.DatabaseMessage{InstanceID: "inst-1", DatabaseName: "app"}
	first := testOperation(syncer, 7, database)
	second := testOperation(syncer, 8, database)

	syncer.completeDatabase(context.Background(), database, nil)

	require.Len(t, notifier.sent, 2)
	recipients := []int32{notifier.sent[0].GetRecipientId(), notifier.sent[1].GetRecipientId()}
	require.ElementsMatch(t, []int32{7, 8}, recipients)
	require.True(t, first.done)
	require.True(t, second.done)
	require.Equal(t, 1, first.succeeded)
	require.Equal(t, 1, second.succeeded)
}

// Nobody asked for a scheduled sync, so a failure after every retry goes to the
// administrators, deduplicated by the window rather than by process memory.
func TestBackgroundDatabaseFailureNotifiesTheAdministrators(t *testing.T) {
	t.Parallel()

	notifier := &fakeNotifier{}
	syncer := &Syncer{notifier: notifier}
	instance := &store.InstanceMessage{ResourceID: "inst-1", Metadata: &storepb.Instance{Title: "prod"}}
	database := &store.DatabaseMessage{InstanceID: "inst-1", DatabaseName: "app"}

	syncer.notifyBackgroundDatabaseFailure(context.Background(), instance, database, errors.New("permission denied"))

	require.Empty(t, notifier.sent, "a background failure has no initiator")
	require.Len(t, notifier.admin, 1)
	message := notifier.admin[0]
	require.Zero(t, message.GetRecipientId(), "the recipient is stamped per administrator by the sender")
	require.NotEmpty(t, message.GetDedupeKey())
	require.Contains(t, message.GetDedupeKey(), "schema-sync.database:instances/inst-1/databases/app:")
	require.Equal(t, storepb.SyncTrigger_SYNC_TRIGGER_BACKGROUND, message.GetSchemaSync().GetTrigger())
	require.Equal(t, "prod", message.GetSchemaSync().GetInstanceTitle())
	require.Equal(t, int32(1), message.GetSchemaSync().GetFailedCount())
}

func TestBackgroundInstanceFailureNotifiesTheAdministrators(t *testing.T) {
	t.Parallel()

	notifier := &fakeNotifier{}
	syncer := &Syncer{notifier: notifier}
	instance := &store.InstanceMessage{ResourceID: "inst-1", Metadata: &storepb.Instance{Title: "prod"}}

	syncer.notifyBackgroundInstanceFailure(context.Background(), instance, errors.New("connection refused"))

	require.Len(t, notifier.admin, 1)
	message := notifier.admin[0]
	require.Contains(t, message.GetDedupeKey(), "schema-sync.instance:inst-1:")
	require.Equal(t, "connection refused", message.GetSchemaSync().GetInstanceError())
}

// A runner without a notifier — the unit tests, and a build that never wires one —
// must not panic on the reporting path.
func TestOperationWithoutANotifierIsSilent(t *testing.T) {
	t.Parallel()

	syncer := &Syncer{}
	database := &store.DatabaseMessage{InstanceID: "inst-1", DatabaseName: "app"}
	operation := testOperation(syncer, 7, database)
	syncer.completeDatabase(context.Background(), database, nil)
	require.Empty(t, syncer.operations)
	require.True(t, operation.done)
}
