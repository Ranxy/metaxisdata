package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// FindNotificationMessage is the message for finding notifications. RecipientID
// is required: a notification belongs to one principal, and every query in this
// file scopes on that id rather than on the workspace.
type FindNotificationMessage struct {
	RecipientID *int
	UnreadOnly  bool
	Limit       *int
	Offset      *int
}

// CreateNotification inserts one notification and returns it with the id and
// create time the row was stamped with. It returns (nil, nil) when the recipient
// already has a notification carrying the same dedupe_key, so a background
// sender can report a condition on every occurrence while the message itself is
// written once per suppression window (and once across replicas, because the
// suppression is the unique index rather than process memory).
func (s *Store) CreateNotification(ctx context.Context, notification *storepb.Notification) (*storepb.Notification, error) {
	if notification == nil {
		return nil, errors.New("notification is required")
	}
	if notification.RecipientId <= 0 {
		return nil, errors.New("notification recipient is required")
	}

	// The column-backed fields are cleared from the copy that becomes the
	// payload: the payload holds only the message proper, so a column and the
	// payload can never disagree about when a message was created or read.
	stored, ok := proto.Clone(notification).(*storepb.Notification)
	if !ok {
		return nil, errors.New("failed to clone notification")
	}
	stored.Id = 0
	stored.CreateTime = nil
	stored.RecipientId = 0
	stored.ReadTime = nil
	stored.DedupeKey = ""

	payload, err := protojson.Marshal(stored)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal notification payload")
	}

	// The server stamps the time, like audit_log does, so a message cannot claim
	// to predate the event it reports.
	createTime := time.Now().UTC()
	var id int64
	var createdAt time.Time
	if err := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO notification (recipient_id, created_at, dedupe_key, payload)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING
		RETURNING id, created_at
	`, notification.RecipientId, createTime, notification.DedupeKey, payload).Scan(&id, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to create notification")
	}

	stored.Id = id
	stored.CreateTime = timestamppb.New(createdAt)
	stored.RecipientId = notification.RecipientId
	stored.DedupeKey = notification.DedupeKey
	return stored, nil
}

// notificationScope returns the predicate and arguments that select one
// recipient's notifications. It exists so that "a notification is only ever
// read by its recipient" is one testable function instead of a clause repeated
// in every query in this file: this is the one table whose rows the
// workspace-scoped authorization model must not hand to everybody.
func notificationScope(recipientID int, unreadOnly bool) (where string, args []any) {
	conditions := []string{"recipient_id = $1"}
	if unreadOnly {
		conditions = append(conditions, "read_at IS NULL")
	}
	return strings.Join(conditions, " AND "), []any{recipientID}
}

// ListNotifications lists one recipient's notifications, newest first.
func (s *Store) ListNotifications(ctx context.Context, find *FindNotificationMessage) ([]*storepb.Notification, error) {
	if find == nil || find.RecipientID == nil {
		return nil, errors.New("notification recipient is required")
	}

	where, args := notificationScope(*find.RecipientID, find.UnreadOnly)
	query := `
		SELECT id, recipient_id, created_at, read_at, payload
		FROM notification
		WHERE ` + where + `
		ORDER BY created_at DESC, id DESC`

	if find.Limit != nil {
		query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, *find.Limit)
	}
	if find.Offset != nil {
		query += fmt.Sprintf(" OFFSET $%d", len(args)+1)
		args = append(args, *find.Offset)
	}

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list notifications")
	}
	defer rows.Close()

	var notifications []*storepb.Notification
	for rows.Next() {
		notification, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		notifications = append(notifications, notification)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate notifications")
	}

	return notifications, nil
}

// CountNotificationCounts counts one recipient's unread notifications and the
// subset of them written after they last opened the inbox — the number the inbox
// page works with and the number the bell's badge shows. Both come from one
// statement, so the subset can never come out larger than the whole: two reads
// would answer with two snapshots, and a message written between them would make
// the badge claim more new messages than there are unread ones.
//
// A recipient who never opened the inbox has no watermark, and every unread
// message is new: -infinity is what "no watermark" compares as.
func (s *Store) CountNotificationCounts(ctx context.Context, recipientID int) (unread int, unseen int, err error) {
	where, args := notificationScope(recipientID, true)
	err = s.GetDB().QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (
				WHERE notification.created_at > COALESCE(principal.notification_seen_at, '-infinity'::timestamptz)
			)
		FROM notification
		JOIN principal ON principal.id = notification.recipient_id
		WHERE `+where,
		args...).Scan(&unread, &unseen)
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to count notifications")
	}
	return unread, unseen, nil
}

// MarkNotificationsSeen records that one recipient has now opened the inbox,
// which clears the badge. The server stamps the time, as it does for read_at, so
// a caller cannot claim to have opened the inbox before a message it should see
// was written. Nothing is marked read here: an unread message stays unread until
// it is clicked.
//
// The comparison the count makes is between this database clock and the
// application clock that stamped created_at, so the two hosts are assumed to agree.
func (s *Store) MarkNotificationsSeen(ctx context.Context, recipientID int) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE principal
		SET notification_seen_at = NOW()
		WHERE id = $1
	`, recipientID)
	if err != nil {
		return errors.Wrap(err, "failed to mark notifications seen")
	}
	return nil
}

// MarkNotificationsRead marks the named notifications of one recipient read and
// returns how many rows changed. Already-read rows are left alone, so the count
// is the number of messages this call actually turned unread → read.
func (s *Store) MarkNotificationsRead(ctx context.Context, recipientID int, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE notification
		SET read_at = NOW()
		WHERE recipient_id = $1 AND id = ANY($2) AND read_at IS NULL
	`, recipientID, pq.Array(ids))
	if err != nil {
		return 0, errors.Wrap(err, "failed to mark notifications read")
	}
	return result.RowsAffected()
}

// MarkAllNotificationsRead marks every unread notification of one recipient
// read and returns how many rows changed.
func (s *Store) MarkAllNotificationsRead(ctx context.Context, recipientID int) (int64, error) {
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE notification
		SET read_at = NOW()
		WHERE recipient_id = $1 AND read_at IS NULL
	`, recipientID)
	if err != nil {
		return 0, errors.Wrap(err, "failed to mark all notifications read")
	}
	return result.RowsAffected()
}

// DeleteNotification deletes one notification of one recipient. The second
// result is false when the recipient has no such row, which includes the case
// where it belongs to somebody else.
func (s *Store) DeleteNotification(ctx context.Context, recipientID int, id int64) (bool, error) {
	result, err := s.GetDB().ExecContext(ctx, `
		DELETE FROM notification
		WHERE recipient_id = $1 AND id = $2
	`, recipientID, id)
	if err != nil {
		return false, errors.Wrap(err, "failed to delete notification")
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to count deleted notifications")
	}
	return deleted > 0, nil
}

func scanNotification(scanner interface {
	Scan(dest ...any) error
}) (*storepb.Notification, error) {
	var (
		id          int64
		recipientID int32
		createdAt   time.Time
		readAt      sql.NullTime
		payload     []byte
	)
	if err := scanner.Scan(&id, &recipientID, &createdAt, &readAt, &payload); err != nil {
		return nil, errors.Wrap(err, "failed to scan notification")
	}

	notification := &storepb.Notification{}
	if err := common.ProtojsonUnmarshaler.Unmarshal(payload, notification); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal notification payload")
	}
	// The column-backed fields are stamped here, so a payload written before a
	// column changed cannot resurrect a stale value.
	notification.Id = id
	notification.RecipientId = recipientID
	notification.CreateTime = timestamppb.New(createdAt)
	notification.ReadTime = nil
	if readAt.Valid {
		notification.ReadTime = timestamppb.New(readAt.Time)
	}
	return notification, nil
}
