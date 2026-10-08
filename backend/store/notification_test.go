package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

type notificationScannerStub struct {
	id          int64
	recipientID int32
	createdAt   time.Time
	readAt      sql.NullTime
	payload     []byte
}

func (s notificationScannerStub) Scan(dest ...any) error {
	*(dest[0].(*int64)) = s.id
	*(dest[1].(*int32)) = s.recipientID
	*(dest[2].(*time.Time)) = s.createdAt
	*(dest[3].(*sql.NullTime)) = s.readAt
	*(dest[4].(*[]byte)) = s.payload
	return nil
}

// The inbox is the one surface the workspace-scoped authorization model does not
// cover: a notification belongs to one principal. Every listing therefore has to
// carry the recipient predicate, and unread_only has to narrow it rather than
// replace it.
func TestNotificationScopeAlwaysScopesToTheRecipient(t *testing.T) {
	t.Parallel()

	where, args := notificationScope(42, false)
	require.Equal(t, "recipient_id = $1", where)
	require.Equal(t, []any{42}, args)

	where, args = notificationScope(42, true)
	require.Equal(t, "recipient_id = $1 AND read_at IS NULL", where)
	require.Equal(t, []any{42}, args)
}

func TestScanNotificationStampsColumnBackedFields(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	readAt := createdAt.Add(5 * time.Minute)
	// The payload deliberately carries a stale create time, recipient and read
	// time: the columns are the truth, so scanning must overwrite them.
	payload := []byte(`{
		"parent": "workspaces/default",
		"type": "NOTIFICATION_TYPE_SCHEMA_SYNC",
		"severity": "NOTIFICATION_SEVERITY_INFO",
		"recipientId": 7,
		"createTime": "2000-01-01T00:00:00Z",
		"readTime": "2000-01-01T00:00:00Z",
		"schemaSync": {"instance": "instances/inst1", "succeededCount": 3}
	}`)

	notification, err := scanNotification(notificationScannerStub{
		id:          11,
		recipientID: 42,
		createdAt:   createdAt,
		readAt:      sql.NullTime{Time: readAt, Valid: true},
		payload:     payload,
	})
	require.NoError(t, err)
	require.Equal(t, int64(11), notification.GetId())
	require.Equal(t, int32(42), notification.GetRecipientId())
	require.Equal(t, timestamppb.New(createdAt).AsTime(), notification.GetCreateTime().AsTime())
	require.Equal(t, timestamppb.New(readAt).AsTime(), notification.GetReadTime().AsTime())
	require.Equal(t, "workspaces/default", notification.GetParent())
	require.Equal(t, storepb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC, notification.GetType())
	require.Equal(t, "instances/inst1", notification.GetSchemaSync().GetInstance())
	require.Equal(t, int32(3), notification.GetSchemaSync().GetSucceededCount())
}

func TestScanNotificationLeavesUnreadWithoutReadTime(t *testing.T) {
	t.Parallel()

	notification, err := scanNotification(notificationScannerStub{
		id:          12,
		recipientID: 42,
		createdAt:   time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC),
		readAt:      sql.NullTime{},
		payload:     []byte(`{"type": "NOTIFICATION_TYPE_OPENLINEAGE"}`),
	})
	require.NoError(t, err)
	require.Nil(t, notification.GetReadTime())
	require.Equal(t, storepb.NotificationType_NOTIFICATION_TYPE_OPENLINEAGE, notification.GetType())
}
