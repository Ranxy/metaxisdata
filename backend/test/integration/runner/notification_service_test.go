//go:build integration

package runner

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// A sync's outcome has to outlive the request that asked for it: the database sync
// writes the caller a message they can still read after the response is gone, and
// the message names the database, because that is the only thing distinguishing one
// such message from the next.
func TestSyncWritesTheCallerAMessageRealServerIntegration(t *testing.T) {
	env, ctx, instanceID, _, databaseName := setupMySQLServiceDatabase(t)
	notifications := v1connect.NewNotificationServiceClient(&http.Client{Timeout: 10 * time.Second}, env.BaseURL)
	token := env.AdminToken()

	before, err := notifications.GetUnreadNotificationCount(ctx, withToken(token, &v1pb.GetUnreadNotificationCountRequest{
		Parent: "workspaces/-",
	}))
	require.NoError(t, err)

	env.SyncDatabase(ctx, t, databaseName)

	var message *v1pb.Notification
	require.Eventually(t, func() bool {
		response, err := notifications.ListNotifications(ctx, withToken(token, &v1pb.ListNotificationsRequest{
			Parent:   "workspaces/-",
			PageSize: 10,
		}))
		if err != nil {
			return false
		}
		for _, candidate := range response.Msg.GetNotifications() {
			if candidate.GetSchemaSync().GetInstance() == common.FormatInstance(instanceID) {
				message = candidate
				return true
			}
		}
		return false
	}, 30*time.Second, 200*time.Millisecond, "the sync's message never arrived: %s", env.ServerLogs())

	require.Equal(t, v1pb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC, message.GetType())
	require.Equal(t, v1pb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO, message.GetSeverity())
	require.Equal(t, v1pb.SyncTrigger_SYNC_TRIGGER_MANUAL, message.GetSchemaSync().GetTrigger())
	require.Equal(t, int32(1), message.GetSchemaSync().GetSucceededCount())
	require.Nil(t, message.GetReadTime(), "a new message is unread")
	require.Equal(t, common.FormatInstance(instanceID), message.GetSchemaSync().GetInstance())
	require.Len(t, message.GetSchemaSync().GetDatabases(), 1)
	require.Equal(t, databaseName, message.GetSchemaSync().GetDatabases()[0].GetDatabase())
	require.Equal(t, v1pb.SyncDatabaseState_SYNC_DATABASE_STATE_SUCCEEDED, message.GetSchemaSync().GetDatabases()[0].GetState())

	after, err := notifications.GetUnreadNotificationCount(ctx, withToken(token, &v1pb.GetUnreadNotificationCountRequest{
		Parent: "workspaces/-",
	}))
	require.NoError(t, err)
	require.GreaterOrEqual(t, after.Msg.GetUnreadCount(), before.Msg.GetUnreadCount()+1)

	_, err = notifications.BatchMarkNotificationsRead(ctx, withToken(token, &v1pb.BatchMarkNotificationsReadRequest{
		Parent: "workspaces/-",
		Names:  []string{message.GetName()},
	}))
	require.NoError(t, err)

	marked, err := notifications.ListNotifications(ctx, withToken(token, &v1pb.ListNotificationsRequest{
		Parent:     "workspaces/-",
		PageSize:   10,
		UnreadOnly: true,
	}))
	require.NoError(t, err)
	for _, candidate := range marked.Msg.GetNotifications() {
		require.NotEqual(t, message.GetName(), candidate.GetName(), "a read message is not unread any more")
	}

	_, err = notifications.DeleteNotification(ctx, withToken(token, &v1pb.DeleteNotificationRequest{Name: message.GetName()}))
	require.NoError(t, err)
	// Deleting it again is a not-found: the row is gone, and the caller is not told
	// whether it ever existed under a different owner.
	_, err = notifications.DeleteNotification(ctx, withToken(token, &v1pb.DeleteNotificationRequest{Name: message.GetName()}))
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

// Opening the inbox clears the badge without reading anything: the unread count stays
// where it was, the message keeps its unread state, and only the unseen count — the
// messages written since that moment — goes to zero. A message written afterwards is new
// again, which is what makes the badge a "since last opened" indicator rather than a
// second read state.
func TestOpeningTheInboxClearsTheBadgeOnlyRealServerIntegration(t *testing.T) {
	env, ctx, _, _, databaseName := setupMySQLServiceDatabase(t)
	notifications := v1connect.NewNotificationServiceClient(&http.Client{Timeout: 10 * time.Second}, env.BaseURL)
	token := env.AdminToken()

	env.SyncDatabase(ctx, t, databaseName)

	// The setup syncs the database too, so wait for the count the inbox will be opened
	// with rather than for the write the call above made.
	var before *v1pb.GetUnreadNotificationCountResponse
	require.Eventually(t, func() bool {
		response, err := notifications.GetUnreadNotificationCount(ctx, withToken(token, &v1pb.GetUnreadNotificationCountRequest{
			Parent: "workspaces/-",
		}))
		if err != nil || response.Msg.GetUnreadCount() == 0 {
			return false
		}
		before = response.Msg
		return true
	}, 30*time.Second, 200*time.Millisecond, "the inbox never received a message: %s", env.ServerLogs())
	require.GreaterOrEqual(t, before.GetUnseenCount(), int32(1),
		"a message written before the inbox was ever opened is unseen")

	_, err := notifications.MarkNotificationsSeen(ctx, withToken(token, &v1pb.MarkNotificationsSeenRequest{
		Parent: "workspaces/-",
	}))
	require.NoError(t, err)

	after, err := notifications.GetUnreadNotificationCount(ctx, withToken(token, &v1pb.GetUnreadNotificationCountRequest{
		Parent: "workspaces/-",
	}))
	require.NoError(t, err)
	require.Zero(t, after.Msg.GetUnseenCount(), "opening the inbox clears the badge")
	require.Equal(t, before.GetUnreadCount(), after.Msg.GetUnreadCount(), "opening the inbox reads nothing")
	// The badge counts a subset of the unread messages, and one statement answers both, so
	// the two numbers can never contradict each other on the wire.
	require.LessOrEqual(t, after.Msg.GetUnseenCount(), after.Msg.GetUnreadCount())
	require.LessOrEqual(t, before.GetUnseenCount(), before.GetUnreadCount())

	unread, err := notifications.ListNotifications(ctx, withToken(token, &v1pb.ListNotificationsRequest{
		Parent:     "workspaces/-",
		PageSize:   100,
		UnreadOnly: true,
	}))
	require.NoError(t, err)
	require.NotEmpty(t, unread.Msg.GetNotifications(), "the messages are still unread after the badge was cleared")
	for _, candidate := range unread.Msg.GetNotifications() {
		require.Nil(t, candidate.GetReadTime())
	}

	// Marking seen twice is harmless: the second call only moves the watermark, and the
	// count stays where the first one left it.
	_, err = notifications.MarkNotificationsSeen(ctx, withToken(token, &v1pb.MarkNotificationsSeenRequest{
		Parent: "workspaces/-",
	}))
	require.NoError(t, err)
	repeated, err := notifications.GetUnreadNotificationCount(ctx, withToken(token, &v1pb.GetUnreadNotificationCountRequest{
		Parent: "workspaces/-",
	}))
	require.NoError(t, err)
	require.Zero(t, repeated.Msg.GetUnseenCount())
	require.Equal(t, before.GetUnreadCount(), repeated.Msg.GetUnreadCount())

	// A message written after that moment is unseen again.
	env.SyncDatabase(ctx, t, databaseName)
	require.Eventually(t, func() bool {
		response, err := notifications.GetUnreadNotificationCount(ctx, withToken(token, &v1pb.GetUnreadNotificationCountRequest{
			Parent: "workspaces/-",
		}))
		return err == nil && response.Msg.GetUnseenCount() >= 1
	}, 30*time.Second, 200*time.Millisecond, "a message written after the inbox was opened is not seen as new: %s", env.ServerLogs())
}

// The inbox is one principal's, which is the one thing the workspace-scoped
// authorization model does not cover. A member of the same workspace sees their own
// empty inbox, not the administrator's messages.
func TestNotificationInboxIsPersonalRealServerIntegration(t *testing.T) {
	env, ctx, _, _, databaseName := setupMySQLServiceDatabase(t)
	httpClient := &http.Client{Timeout: 10 * time.Second}
	notifications := v1connect.NewNotificationServiceClient(httpClient, env.BaseURL)
	users := v1connect.NewUserServiceClient(httpClient, env.BaseURL)
	adminToken := env.AdminToken()

	// Somebody has to have a message for the isolation to mean anything.
	env.SyncDatabase(ctx, t, databaseName)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	email := fmt.Sprintf("notify-%s@example.com", suffix)
	const password = "Integration-Password-1!"
	_, err := users.CreateUser(ctx, withToken(adminToken, &v1pb.CreateUserRequest{
		User: &v1pb.User{
			Email:    email,
			Title:    "Notification integration member",
			Password: password,
			UserType: v1pb.UserType_END_USER,
		},
	}))
	require.NoError(t, err)

	memberToken, err := env.LoginAs(ctx, email, password)
	require.NoError(t, err)

	member, err := notifications.ListNotifications(ctx, withToken(memberToken, &v1pb.ListNotificationsRequest{
		Parent:   "workspaces/-",
		PageSize: 50,
	}))
	require.NoError(t, err)
	require.Empty(t, member.Msg.GetNotifications(), "a member must not read another principal's inbox")

	count, err := notifications.GetUnreadNotificationCount(ctx, withToken(memberToken, &v1pb.GetUnreadNotificationCountRequest{
		Parent: "workspaces/-",
	}))
	require.NoError(t, err)
	require.Zero(t, count.Msg.GetUnreadCount())

	// The administrator's own inbox still has the sync's message: the empty list
	// above is isolation, not a broken write.
	admin, err := notifications.ListNotifications(ctx, withToken(adminToken, &v1pb.ListNotificationsRequest{
		Parent:   "workspaces/-",
		PageSize: 50,
	}))
	require.NoError(t, err)
	require.NotEmpty(t, admin.Msg.GetNotifications())
}

// Every ingestion request this server refuses is an administrator's business, and one
// misbehaving producer must not be able to fill their inbox: two identical refusals
// inside the suppression window leave exactly one message, addressed to the
// workspace administrators rather than to the ingestion key's owner.
func TestIngestionRefusalNotifiesTheAdministratorsOnceRealServerIntegration(t *testing.T) {
	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 10 * time.Second}

	key, keyMessage, err := env.Store.CreateOpenLineageAPIKey(ctx, "integration-notify-refusal", "integration-test", "")
	require.NoError(t, err)

	// An event the limits reject: the run has no runId, so nothing in the request can
	// be parsed. Sent twice, the second refusal is inside the same window.
	body := `[{"eventType":"START","run":{},"job":{"namespace":"integration-notify-ns","name":"job"}}]`
	post := func() int {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.BaseURL+"/api/v1/lineage/batch", strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		return resp.StatusCode
	}
	require.Equal(t, http.StatusBadRequest, post(), "an unparseable batch is refused")
	require.Equal(t, http.StatusBadRequest, post())

	notifications := v1connect.NewNotificationServiceClient(httpClient, env.BaseURL)
	var refused []*v1pb.Notification
	require.Eventually(t, func() bool {
		response, err := notifications.ListNotifications(ctx, withToken(env.AdminToken(), &v1pb.ListNotificationsRequest{
			Parent:   "workspaces/-",
			PageSize: 100,
		}))
		if err != nil {
			return false
		}
		refused = refused[:0]
		for _, candidate := range response.Msg.GetNotifications() {
			// The key's masked identifier is what makes this the test's own message and
			// not another scenario's refusal.
			if candidate.GetOpenlineage().GetKind() == v1pb.OpenLineageFailureKind_OPENLINEAGE_FAILURE_KIND_INVALID_EVENT &&
				candidate.GetOpenlineage().GetApiKey() == keyMessage.MaskedKey {
				refused = append(refused, candidate)
			}
		}
		return len(refused) > 0
	}, 30*time.Second, 200*time.Millisecond, "the refusal never reached an administrator: %s", env.ServerLogs())

	// The second refusal was already answered by the time this runs, so one row is the
	// settled state, not a race.
	require.Len(t, refused, 1, "a repeated refusal inside the window must not add a message")
	require.Equal(t, v1pb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING, refused[0].GetSeverity())
	require.Equal(t, keyMessage.MaskedKey, refused[0].GetOpenlineage().GetApiKey())

	// The administrator is the recipient: the message is in their inbox, which is
	// theirs alone, and nobody asked for this ingestion.
	require.NotEmpty(t, refused[0].GetName())
}

// The stream is what replaces polling: a message written while the caller is connected
// reaches them without a reload, and the event carries the count the inbox would answer
// with, so the badge and the list can both be updated from it.
func TestSubscriptionPushesTheCallersMessageRealServerIntegration(t *testing.T) {
	env, ctx, instanceID, _, databaseName := setupMySQLServiceDatabase(t)
	// A stream outlives the request that opened it, so the client that carries it must not
	// have a whole-request deadline: the test bounds each wait itself. A 10 second timeout
	// here ends the stream mid-test, which is what a silent, never-ending subscription
	// looks like from this side.
	notifications := v1connect.NewNotificationServiceClient(&http.Client{}, env.BaseURL)
	token := env.AdminToken()

	// Subscribed before the sync, so what arrives was written while the connection was
	// open rather than read back from the inbox. The fixture already synced this database
	// once, and a manual sync carries no suppression key, so its older message must not
	// be what the stream delivers.
	events, stream, closeStream := notificationEvents(ctx, t, notifications, token)
	defer closeStream()
	env.SyncDatabase(ctx, t, databaseName)

	// The fixture's instance-wide sync has an asynchronous tail of its own, so the stream
	// can carry that message first — it lists only the databases that failed. Wait for the
	// one that names this database, which is what a per-database sync reports.
	var event *v1pb.NotificationEvent
	deadline := time.After(30 * time.Second)
	for event == nil {
		select {
		case candidate, open := <-events:
			if !open {
				t.Fatalf("the stream ended before the sync's message arrived: %s", env.ServerLogs())
			}
			if namesTheSyncedDatabase(candidate, databaseName) {
				event = candidate
			}
		case <-deadline:
			t.Fatalf("the sync's message never reached the connected caller: %s", env.ServerLogs())
		}
	}

	// A reverse proxy that buffers the response body would hold every event in its
	// buffer, and this per-response switch is nginx's way out. The client is the only
	// place it can be observed.
	require.Equal(t, "no", stream.ResponseHeader().Get("X-Accel-Buffering"))

	message := event.GetNotification()
	require.Equal(t, v1pb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC, message.GetType())
	require.Equal(t, v1pb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO, message.GetSeverity())
	require.Equal(t, common.FormatInstance(instanceID), message.GetSchemaSync().GetInstance())
	require.Equal(t, databaseName, message.GetSchemaSync().GetDatabases()[0].GetDatabase())
	require.Nil(t, message.GetReadTime(), "a streamed message is unread")

	// A count the store answered travels with the event; one it could not answer is
	// absent, which is how the client tells "nothing is unread" from "ask me again".
	require.NotNil(t, event.UnreadCount, "the event carries the count of the inbox it was written to")
	require.GreaterOrEqual(t, event.GetUnreadCount(), int32(1))

	// The stream and the list are the same message, named the same way.
	listed, err := notifications.ListNotifications(ctx, withToken(token, &v1pb.ListNotificationsRequest{
		Parent:   "workspaces/-",
		PageSize: 100,
	}))
	require.NoError(t, err)
	names := make([]string, 0, len(listed.Msg.GetNotifications()))
	for _, candidate := range listed.Msg.GetNotifications() {
		names = append(names, candidate.GetName())
	}
	require.Contains(t, names, message.GetName(), "the streamed message is one the inbox lists")
}

// The subscription is the authenticated caller's own inbox: a member of the same
// workspace connected at the same time is not told about the administrator's message.
func TestSubscriptionIsScopedToTheCallerRealServerIntegration(t *testing.T) {
	env, ctx, _, _, databaseName := setupMySQLServiceDatabase(t)
	httpClient := &http.Client{Timeout: 10 * time.Second}
	// The unary calls get a deadline; the streams must not, for the reason above.
	users := v1connect.NewUserServiceClient(httpClient, env.BaseURL)
	notifications := v1connect.NewNotificationServiceClient(&http.Client{}, env.BaseURL)
	adminToken := env.AdminToken()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	email := fmt.Sprintf("stream-%s@example.com", suffix)
	const password = "Integration-Password-1!"
	_, err := users.CreateUser(ctx, withToken(adminToken, &v1pb.CreateUserRequest{
		User: &v1pb.User{
			Email:    email,
			Title:    "Notification stream integration member",
			Password: password,
			UserType: v1pb.UserType_END_USER,
		},
	}))
	require.NoError(t, err)
	memberToken, err := env.LoginAs(ctx, email, password)
	require.NoError(t, err)

	adminEvents, _, closeAdmin := notificationEvents(ctx, t, notifications, adminToken)
	defer closeAdmin()
	memberEvents, _, closeMember := notificationEvents(ctx, t, notifications, memberToken)
	defer closeMember()

	env.SyncDatabase(ctx, t, databaseName)

	// Wait for the first message the administrator's own inbox received — the sync's, or
	// the fixture's instance-wide one. A fan-out that ignored the recipient would have
	// delivered a copy to the member at the same moment, so the member's stream is then
	// watched for long enough that the copy would have arrived, and it must have stayed
	// open rather than merely been quiet for an instant.
	select {
	case <-adminEvents:
	case <-time.After(30 * time.Second):
		t.Fatalf("no message reached the administrator's stream: %s", env.ServerLogs())
	}
	select {
	case event, open := <-memberEvents:
		if open {
			t.Fatalf("a member received another principal's message: %s", event.GetNotification().GetName())
		}
		t.Fatal("the member's stream ended instead of staying open")
	case <-time.After(2 * time.Second):
	}

	count, err := notifications.GetUnreadNotificationCount(ctx, withToken(memberToken, &v1pb.GetUnreadNotificationCountRequest{
		Parent: "workspaces/-",
	}))
	require.NoError(t, err)
	require.Zero(t, count.Msg.GetUnreadCount(), "the member's own inbox is still empty")
}

// namesTheSyncedDatabase reports whether one streamed event is the per-database sync's
// message: it names exactly one database, and names this one.
func namesTheSyncedDatabase(event *v1pb.NotificationEvent, database string) bool {
	databases := event.GetNotification().GetSchemaSync().GetDatabases()
	return len(databases) == 1 && databases[0].GetDatabase() == database
}

// A subscription is authenticated once, at the handshake, and that is what the client's
// reconnect relies on: an access token that expired while a tab sat idle, or a session
// that was logged out elsewhere, must be refused before any subscription exists — there is
// nothing left to tear down, and the reconnect is what re-authenticates the caller.
//
// The same credentials are accepted while they are valid, so each refusal below is the
// state of the credential rather than a stream that never worked. That control is a unary
// call: connect-go's server-streaming client returns as soon as the request is sent, so a
// stream's handshake result only surfaces on the first receive — which is what `refused`
// reads, and why it is only used where a refusal is expected (a live subscription says
// nothing until an event or the heartbeat).
func TestSubscribeNotificationsRefusesAnExpiredOrRevokedSessionRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 10 * time.Second}
	notifications := v1connect.NewNotificationServiceClient(&http.Client{}, env.BaseURL)
	authClient := v1connect.NewAuthServiceClient(httpClient, env.BaseURL)
	userClient := v1connect.NewUserServiceClient(httpClient, env.BaseURL)
	adminToken := env.AdminToken()

	refused := func(token string) error {
		// A refusal answers at once; a stream that should have been refused but was not says
		// nothing until the heartbeat, so the probe is bounded rather than left to hang the
		// test — a timeout then fails the assertion below instead of stalling.
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		stream, err := notifications.SubscribeNotifications(probeCtx, withToken(token, &v1pb.SubscribeNotificationsRequest{
			Parent: "workspaces/-",
		}))
		if err != nil {
			return err
		}
		defer func() { _ = stream.Close() }()
		stream.Receive()
		return stream.Err()
	}
	accepted := func(token string) error {
		_, err := userClient.GetCurrentUser(ctx, withToken(token, &emptypb.Empty{}))
		return err
	}

	// A session of its own, so revoking it cannot disturb the shared admin token.
	const password = "Integration-pass-1!"
	email := fmt.Sprintf("stream-session-%d@example.com", time.Now().UnixNano())
	created, err := userClient.CreateUser(ctx, withToken(adminToken, &v1pb.CreateUserRequest{
		User: &v1pb.User{
			Email:    email,
			Title:    "Notification stream session",
			Password: password,
			UserType: v1pb.UserType_END_USER,
		},
	}))
	require.NoError(t, err)
	login, err := authClient.Login(ctx, connect.NewRequest(&v1pb.LoginRequest{Email: email, Password: password}))
	require.NoError(t, err)
	token := login.Msg.GetToken()
	require.NoError(t, accepted(token), "the session must work before it is logged out")

	_, err = authClient.Logout(ctx, withToken(token, &v1pb.LogoutRequest{}))
	require.NoError(t, err)
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(accepted(token)),
		"the session really is revoked")
	streamErr := refused(token)
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(streamErr),
		"a logged-out session must not be able to open the stream: %v", streamErr)

	// The deployment's own key signs a token that is valid and one that expired an hour
	// ago: the only difference between them is the expiry, so the second refusal is the
	// expiry check and not a claim the server would have rejected anyway.
	secret, err := env.Store.GetSetting(ctx, storepb.SettingName_AUTH_SECRET)
	require.NoError(t, err)
	userID, err := common.GetUserID(created.Msg.GetName())
	require.NoError(t, err)
	// The harness starts the server without the release tag, so it runs the dev profile.
	mint := func(duration time.Duration) string {
		t.Helper()
		signed, err := auth.GenerateAccessToken(created.Msg.GetName(), userID, common.ReleaseModeDev, secret.Value, duration)
		require.NoError(t, err)
		return signed
	}
	require.NoError(t, accepted(mint(time.Hour)), "a token with a later expiry must be accepted")
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(accepted(mint(-time.Hour))),
		"an expired token is refused")
	streamErr = refused(mint(-time.Hour))
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(streamErr),
		"an expired token must not be able to open the stream: %v", streamErr)
}

// notificationEvents opens the caller's stream and forwards the messages it carries.
// Keepalives exist so the connection is not idle and carry nothing, so they are skipped.
// Receive blocks, so it runs on its own goroutine. The channel is closed when the stream
// ends, which is how a test tells a quiet stream from a dead one; the returned function
// closes the stream, ends the goroutine even if it is mid-delivery, and is what every
// caller defers.
func notificationEvents(ctx context.Context, t *testing.T, client v1connect.NotificationServiceClient, token string) (<-chan *v1pb.NotificationEvent, *connect.ServerStreamForClient[v1pb.SubscribeNotificationsResponse], func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(ctx)
	stream, err := client.SubscribeNotifications(ctx, withToken(token, &v1pb.SubscribeNotificationsRequest{
		Parent: "workspaces/-",
	}))
	if err != nil {
		cancel()
		require.NoError(t, err)
	}

	events := make(chan *v1pb.NotificationEvent, 8)
	go func() {
		defer close(events)
		for stream.Receive() {
			event, ok := stream.Msg().GetEvent().(*v1pb.SubscribeNotificationsResponse_Notification)
			if !ok {
				continue
			}
			select {
			case events <- event.Notification:
			case <-ctx.Done():
				return
			}
		}
	}()
	return events, stream, func() {
		cancel()
		_ = stream.Close()
	}
}
