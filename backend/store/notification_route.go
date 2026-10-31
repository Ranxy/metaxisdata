package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// notificationRouteResourceIDLength is the length of a route's resource id, in
// the same alphabet and length as the other non-recycled resources.
const notificationRouteResourceIDLength = 12

// notificationRouteColumns is the column list every notification_route read
// returns, in the order scanNotificationRoute consumes it.
const notificationRouteColumns = `id, resource_id, payload, created_at, updated_at`

// NotificationRouteMessage is one owner/team routing rule as the store holds it.
type NotificationRouteMessage struct {
	ID         int64
	ResourceID string
	Payload    *storepb.NotificationRoute
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// CreateNotificationRouteMessage is the write shape of a new route.
type CreateNotificationRouteMessage struct {
	Payload *storepb.NotificationRoute
}

// UpdateNotificationRouteMessage is the write shape of a route update.
type UpdateNotificationRouteMessage struct {
	ResourceID string
	Payload    *storepb.NotificationRoute
}

// routeScanner is the row shape scanned from either a *sql.Row or *sql.Rows.
type routeScanner interface {
	Scan(dest ...any) error
}

func scanNotificationRoute(row routeScanner) (*NotificationRouteMessage, error) {
	var (
		message NotificationRouteMessage
		payload []byte
	)
	if err := row.Scan(&message.ID, &message.ResourceID, &payload, &message.CreatedAt, &message.UpdatedAt); err != nil {
		return nil, err
	}
	route := &storepb.NotificationRoute{}
	if err := common.ProtojsonUnmarshaler.Unmarshal(payload, route); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal the notification route")
	}
	message.Payload = route
	return &message, nil
}

// CreateNotificationRoute inserts one route and returns it as stored.
func (s *Store) CreateNotificationRoute(ctx context.Context, create *CreateNotificationRouteMessage) (*NotificationRouteMessage, error) {
	if create == nil || create.Payload == nil {
		return nil, common.Errorf(common.Invalid, "a notification route is required")
	}
	raw, err := protojson.Marshal(create.Payload)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal the notification route")
	}
	resourceID, err := common.RandomResourceID(notificationRouteResourceIDLength)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate a notification route resource id")
	}
	message, err := scanNotificationRoute(s.GetDB().QueryRowContext(ctx, `
		INSERT INTO notification_route (resource_id, payload)
		VALUES ($1, $2)
		RETURNING `+notificationRouteColumns,
		resourceID, raw))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the notification route")
	}
	return message, nil
}

// ListNotificationRoutes returns every configured route.
func (s *Store) ListNotificationRoutes(ctx context.Context) ([]*NotificationRouteMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT `+notificationRouteColumns+`
		FROM notification_route
		ORDER BY id`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the notification routes")
	}
	defer rows.Close()

	var messages []*NotificationRouteMessage
	for rows.Next() {
		message, err := scanNotificationRoute(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a notification route")
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the notification routes")
	}
	return messages, nil
}

// GetNotificationRoute reads one route by its resource id. A nil message means
// no such route.
func (s *Store) GetNotificationRoute(ctx context.Context, resourceID string) (*NotificationRouteMessage, error) {
	if resourceID == "" {
		return nil, common.Errorf(common.Invalid, "a notification route resource id is required")
	}
	message, err := scanNotificationRoute(s.GetDB().QueryRowContext(ctx, `
		SELECT `+notificationRouteColumns+`
		FROM notification_route
		WHERE resource_id = $1`, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the notification route")
	}
	return message, nil
}

// UpdateNotificationRoute rewrites one route's payload. A nil message means no
// such route.
func (s *Store) UpdateNotificationRoute(ctx context.Context, update *UpdateNotificationRouteMessage) (*NotificationRouteMessage, error) {
	if update == nil || update.Payload == nil {
		return nil, common.Errorf(common.Invalid, "a notification route is required")
	}
	if update.ResourceID == "" {
		return nil, common.Errorf(common.Invalid, "a notification route resource id is required")
	}
	raw, err := protojson.Marshal(update.Payload)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal the notification route")
	}
	message, err := scanNotificationRoute(s.GetDB().QueryRowContext(ctx, `
		UPDATE notification_route
		SET payload = $2, updated_at = NOW()
		WHERE resource_id = $1
		RETURNING `+notificationRouteColumns,
		update.ResourceID, raw))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to update the notification route")
	}
	return message, nil
}

// DeleteNotificationRoute removes one route.
func (s *Store) DeleteNotificationRoute(ctx context.Context, resourceID string) error {
	if resourceID == "" {
		return common.Errorf(common.Invalid, "a notification route resource id is required")
	}
	const query = `DELETE FROM notification_route WHERE resource_id = $1`
	_, err := s.GetDB().ExecContext(ctx, query, resourceID)
	if err != nil {
		return errors.Wrap(err, "failed to delete the notification route")
	}
	return nil
}
