package notification

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// fakeStore is the narrow store this package reads, so the recipient resolution
// and the suppression can be exercised without a database.
type fakeStore struct {
	workspaceID string
	policy      *storepb.IamPolicy
	groups      map[string]*store.GroupMessage
	users       map[int]*store.UserMessage

	created  []*storepb.Notification
	failNext error
}

func (f *fakeStore) CreateNotification(_ context.Context, notification *storepb.Notification) (*storepb.Notification, error) {
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return nil, err
	}
	f.created = append(f.created, notification)
	return notification, nil
}

func (f *fakeStore) GetWorkspaceID(context.Context) (string, error) {
	return f.workspaceID, nil
}

func (f *fakeStore) GetWorkspaceIamPolicy(context.Context) (*store.IamPolicyMessage, error) {
	return &store.IamPolicyMessage{Policy: f.policy}, nil
}

func (f *fakeStore) GetGroup(_ context.Context, email string) (*store.GroupMessage, error) {
	group, ok := f.groups[email]
	if !ok {
		return nil, errors.Errorf("group %q not found", email)
	}
	return group, nil
}

func (f *fakeStore) GetUserByID(_ context.Context, id int) (*store.UserMessage, error) {
	user, ok := f.users[id]
	if !ok {
		return nil, errors.Errorf("user %d not found", id)
	}
	return user, nil
}

func newOpenLineageMessage() *storepb.Notification {
	return OpenLineageMessage(storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING, &storepb.OpenLineageDetail{
		Kind:      storepb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_INVALID_EVENT,
		Namespace: "ns-1",
	})
}

func TestDedupeKeyBucketsWithinTheWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	require.Equal(t,
		DedupeKey("schema-sync.instance", "instances/inst1", now, time.Hour),
		DedupeKey("schema-sync.instance", "instances/inst1", now.Add(11*time.Minute), time.Hour),
	)
	require.NotEqual(t,
		DedupeKey("schema-sync.instance", "instances/inst1", now, time.Hour),
		DedupeKey("schema-sync.instance", "instances/inst1", now.Add(2*time.Hour), time.Hour),
	)
	require.NotEqual(t,
		DedupeKey("schema-sync.instance", "instances/inst1", now, time.Hour),
		DedupeKey("schema-sync.database", "instances/inst1", now, time.Hour),
	)
	// No window means no suppression, which is how a message addressed to the
	// user who asked for the work is sent.
	require.Empty(t, DedupeKey("schema-sync.instance", "instances/inst1", now, 0))
}

func TestSendDeliversAndSuppressesRepeats(t *testing.T) {
	t.Parallel()

	fake := &fakeStore{workspaceID: "ws-1"}
	service := newServiceWithStore(fake)
	now := time.Now()
	key := DedupeKey("openlineage", "key/7:INVALID_EVENT", now, BackgroundFailureWindow)

	first := newOpenLineageMessage()
	first.RecipientId = 42
	first.DedupeKey = key
	require.NoError(t, service.Send(context.Background(), first))

	// The same occurrence reporting again inside the window writes nothing.
	repeat := newOpenLineageMessage()
	repeat.RecipientId = 42
	repeat.DedupeKey = key
	require.NoError(t, service.Send(context.Background(), repeat))
	require.Len(t, fake.created, 1)

	// The next window is a new key and therefore a new message.
	next := newOpenLineageMessage()
	next.RecipientId = 42
	next.DedupeKey = DedupeKey("openlineage", "key/7:INVALID_EVENT", now.Add(2*time.Hour), BackgroundFailureWindow)
	require.NoError(t, service.Send(context.Background(), next))
	require.Len(t, fake.created, 2)

	// A message to the initiator carries no key and is never suppressed.
	personal := SchemaSyncMessage(42, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO, &storepb.SchemaSyncDetail{})
	require.NoError(t, service.Send(context.Background(), personal))
	require.NoError(t, service.Send(context.Background(), personal))
	require.Len(t, fake.created, 4)

	for _, created := range fake.created {
		require.Equal(t, "workspaces/ws-1", created.GetParent())
	}
}

// A failed write must not consume the window, or one hiccup would silence the
// condition for an hour.
func TestSendRetriesAfterAFailedWrite(t *testing.T) {
	t.Parallel()

	fake := &fakeStore{workspaceID: "ws-1", failNext: errors.New("boom")}
	service := newServiceWithStore(fake)
	now := time.Now()
	key := DedupeKey("openlineage", "key/7:INVALID_EVENT", now, BackgroundFailureWindow)

	message := newOpenLineageMessage()
	message.RecipientId = 42
	message.DedupeKey = key
	require.Error(t, service.Send(context.Background(), message))

	require.NoError(t, service.Send(context.Background(), message))
	require.Len(t, fake.created, 1)
}

func TestSendToWorkspaceAdminsExpandsGroupsAndSkipsTheBot(t *testing.T) {
	t.Parallel()

	fake := &fakeStore{
		workspaceID: "ws-1",
		policy: &storepb.IamPolicy{
			Bindings: []*storepb.Binding{
				// The baseline binding every workspace carries; nobody holds the
				// admin role through it.
				{
					Role:    common.FormatRole(common.WorkspaceMember),
					Members: []string{common.AllUsers},
				},
				{
					Role:    common.FormatRole(common.WorkspaceAdmin),
					Members: []string{"users/2", "groups/ops@example.com"},
				},
			},
		},
		groups: map[string]*store.GroupMessage{
			"ops@example.com": {
				Email: "ops@example.com",
				Payload: &storepb.GroupPayload{Members: []*storepb.GroupMember{
					// 2 is already an explicit member of the binding, 1 is the
					// system bot, and 3 signs in like anybody else.
					{Member: "users/2"},
					{Member: "users/1"},
					{Member: "users/3"},
				}},
			},
		},
		users: map[int]*store.UserMessage{
			1: {ID: 1, Name: "SYSTEM"},
			2: {ID: 2, Name: "alice"},
			3: {ID: 3, Name: "bob"},
		},
	}
	service := newServiceWithStore(fake)

	message := newOpenLineageMessage()
	message.DedupeKey = DedupeKey("openlineage", "ns-1:INVALID_EVENT", time.Now(), BackgroundFailureWindow)
	require.NoError(t, service.SendToWorkspaceAdmins(context.Background(), message))

	require.Len(t, fake.created, 2)
	require.Equal(t, int32(2), fake.created[0].GetRecipientId())
	require.Equal(t, int32(3), fake.created[1].GetRecipientId())
	for _, created := range fake.created {
		require.Equal(t, "workspaces/ws-1", created.GetParent())
		require.Equal(t, storepb.NotificationType_NOTIFICATION_TYPE_OPENLINEAGE, created.GetType())
		require.Empty(t, created.GetId())
	}

	// The second occurrence inside the window reaches nobody.
	require.NoError(t, service.SendToWorkspaceAdmins(context.Background(), message))
	require.Len(t, fake.created, 2)
}

// A deployment whose only administrator is the allUsers baseline has nobody to
// tell, and that is not an error: the caller's own log line remains the record.
func TestSendToWorkspaceAdminsWithoutAnAdministrator(t *testing.T) {
	t.Parallel()

	fake := &fakeStore{
		workspaceID: "ws-1",
		policy: &storepb.IamPolicy{
			Bindings: []*storepb.Binding{
				{Role: common.FormatRole(common.WorkspaceMember), Members: []string{common.AllUsers}},
			},
		},
	}
	service := newServiceWithStore(fake)

	require.NoError(t, service.SendToWorkspaceAdmins(context.Background(), newOpenLineageMessage()))
	require.Empty(t, fake.created)
}

// One runaway error must not decide how large a forever-kept row is, and one
// failing database must not turn a message into a copy of the estate.
func TestMessagesBoundWhatTheyStore(t *testing.T) {
	t.Parallel()

	details := &storepb.SchemaSyncDetail{
		Instance: "instances/inst1",
		Databases: []*storepb.SyncDatabaseResult{{
			Database: "instances/inst1/databases/app",
			Error:    strings.Repeat("x", MaxErrorBytes+64),
		}},
	}
	failures := make([]*storepb.SyncDatabaseResult, 0, MaxFailureEntries+5)
	for i := range MaxFailureEntries + 5 {
		failures = append(failures, &storepb.SyncDatabaseResult{
			Database: fmt.Sprintf("instances/inst1/databases/db_%03d", i),
			Error:    "boom",
		})
	}
	details.Databases = append(details.Databases, failures...)
	details.FailedCount = int32(len(details.Databases))

	message := SchemaSyncMessage(7, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR, details)
	require.Len(t, message.GetSchemaSync().GetDatabases(), MaxFailureEntries)
	require.Equal(t, int32(len(details.Databases)), message.GetSchemaSync().GetFailedCount(), "the counts still describe the whole operation")
	require.Len(t, message.GetSchemaSync().GetDatabases()[0].GetError(), MaxErrorBytes)
	// The caller's detail is left alone: the bound is the message's, not a
	// mutation of what the sender built.
	require.Len(t, details.Databases, MaxFailureEntries+6)
	require.Len(t, details.Databases[0].GetError(), MaxErrorBytes+64)

	// A truncation that splits a multi-byte rune would store an invalid string.
	runes := SchemaSyncMessage(7, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR, &storepb.SchemaSyncDetail{
		Databases: []*storepb.SyncDatabaseResult{{
			Database: "instances/inst1/databases/app",
			Error:    strings.Repeat("数", MaxErrorBytes),
		}},
	})
	require.LessOrEqual(t, len(runes.GetSchemaSync().GetDatabases()[0].GetError()), MaxErrorBytes)
	require.True(t, utf8.ValidString(runes.GetSchemaSync().GetDatabases()[0].GetError()))

	ingestion := OpenLineageMessage(storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR, &storepb.OpenLineageDetail{
		Error: strings.Repeat("e", MaxErrorBytes*2),
	})
	require.Len(t, ingestion.GetOpenlineage().GetError(), MaxErrorBytes)
	require.Nil(t, OpenLineageMessage(storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO, nil).GetOpenlineage())
	require.Nil(t, SchemaSyncMessage(7, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO, nil).GetSchemaSync())
}

// A namespace that matched no instance is a configuration gap an administrator
// has to close, and it repeats for every event the producer sends, so it is
// deduplicated by the day rather than by the hour.
func TestReportUnmatchedNamespaceNotifiesTheAdministrators(t *testing.T) {
	t.Parallel()

	fake := &fakeStore{
		workspaceID: "ws-1",
		policy: &storepb.IamPolicy{Bindings: []*storepb.Binding{{
			Role:    common.FormatRole(common.WorkspaceAdmin),
			Members: []string{"users/2"},
		}}},
		users: map[int]*store.UserMessage{2: {ID: 2, Name: "alice"}},
	}
	service := newServiceWithStore(fake)

	service.ReportUnmatchedNamespace(context.Background(), "mysql://db:3306", "shop.orders")

	require.Len(t, fake.created, 1)
	message := fake.created[0]
	require.Equal(t, int32(2), message.GetRecipientId())
	require.Equal(t, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING, message.GetSeverity())
	require.Equal(t, storepb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_NAMESPACE_UNMAPPED, message.GetOpenlineage().GetKind())
	require.Equal(t, "mysql://db:3306", message.GetOpenlineage().GetNamespace())
	require.Equal(t, "shop.orders", message.GetOpenlineage().GetDataset())
	require.Contains(t, message.GetDedupeKey(), "openlineage.namespace-unmapped:mysql://db:3306:")
}
