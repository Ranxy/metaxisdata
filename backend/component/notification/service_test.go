package notification

import (
	"context"
	"testing"
	"time"

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
