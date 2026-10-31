package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// notificationChannelResourceIDLength is the length of a channel's resource id,
// in the same alphabet and length as the other non-recycled resources.
const notificationChannelResourceIDLength = 12

// notificationChannelColumns is the column list every notification_channel read
// returns, in the order scanNotificationChannel consumes it.
const notificationChannelColumns = `id, resource_id, payload, (secret <> '') AS has_secret, created_at, updated_at`

const (
	notificationChannelSecretQuery = `SELECT secret FROM notification_channel WHERE id = $1`
	deleteNotificationChannelQuery = `DELETE FROM notification_channel WHERE resource_id = $1`
)

// NotificationChannelMessage is one outbound channel as the store holds it. The
// credential is not part of the message: it is only reachable through
// GetNotificationChannelSecret, so no read path can leak it into an API response
// by accident.
type NotificationChannelMessage struct {
	ID         int64
	ResourceID string
	Payload    *storepb.NotificationChannel
	// HasSecret reports whether a credential is stored, without reading it.
	HasSecret bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CreateNotificationChannelMessage is the write shape of a new channel.
type CreateNotificationChannelMessage struct {
	Payload *storepb.NotificationChannel
	// Secret is write-only and encrypted before storage; nil stores no credential.
	Secret *storepb.NotificationChannelSecret
}

// UpdateNotificationChannelMessage is the write shape of a channel update.
type UpdateNotificationChannelMessage struct {
	ResourceID string
	Payload    *storepb.NotificationChannel
	// Secret replaces the stored credential when present and keeps it when nil.
	Secret *storepb.NotificationChannelSecret
}

// channelScanner is the row shape scanned from either a *sql.Row or *sql.Rows.
type channelScanner interface {
	Scan(dest ...any) error
}

func scanNotificationChannel(row channelScanner) (*NotificationChannelMessage, error) {
	var (
		message NotificationChannelMessage
		payload []byte
	)
	if err := row.Scan(&message.ID, &message.ResourceID, &payload, &message.HasSecret, &message.CreatedAt, &message.UpdatedAt); err != nil {
		return nil, err
	}
	channel := &storepb.NotificationChannel{}
	if err := common.ProtojsonUnmarshaler.Unmarshal(payload, channel); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal the notification channel")
	}
	message.Payload = channel
	return &message, nil
}

// CreateNotificationChannel inserts one channel and returns it as stored.
func (s *Store) CreateNotificationChannel(ctx context.Context, create *CreateNotificationChannelMessage) (*NotificationChannelMessage, error) {
	if create == nil || create.Payload == nil {
		return nil, common.Errorf(common.Invalid, "a notification channel is required")
	}
	raw, err := protojson.Marshal(create.Payload)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal the notification channel")
	}
	secret, err := s.encryptNotificationChannelSecret(create.Secret)
	if err != nil {
		return nil, err
	}

	resourceID, err := common.RandomResourceID(notificationChannelResourceIDLength)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate a notification channel resource id")
	}

	message, err := scanNotificationChannel(s.GetDB().QueryRowContext(ctx, `
		INSERT INTO notification_channel (resource_id, payload, secret)
		VALUES ($1, $2, $3)
		RETURNING `+notificationChannelColumns,
		resourceID, raw, secret))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the notification channel")
	}
	return message, nil
}

// ListNotificationChannels returns every configured channel, enabled or not.
// The list is unbounded on purpose: the number of destinations an administrator
// configures is small, and the enqueue path needs all of them anyway.
func (s *Store) ListNotificationChannels(ctx context.Context) ([]*NotificationChannelMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT `+notificationChannelColumns+`
		FROM notification_channel
		ORDER BY id`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the notification channels")
	}
	defer rows.Close()

	var messages []*NotificationChannelMessage
	for rows.Next() {
		message, err := scanNotificationChannel(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a notification channel")
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the notification channels")
	}
	return messages, nil
}

// GetNotificationChannel reads one channel by its resource id. A nil message
// means no such channel.
func (s *Store) GetNotificationChannel(ctx context.Context, resourceID string) (*NotificationChannelMessage, error) {
	if resourceID == "" {
		return nil, common.Errorf(common.Invalid, "a notification channel resource id is required")
	}
	message, err := scanNotificationChannel(s.GetDB().QueryRowContext(ctx, `
		SELECT `+notificationChannelColumns+`
		FROM notification_channel
		WHERE resource_id = $1`, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the notification channel")
	}
	return message, nil
}

// UpdateNotificationChannel rewrites one channel's payload and, when present,
// its credential. A nil message means no such channel.
func (s *Store) UpdateNotificationChannel(ctx context.Context, update *UpdateNotificationChannelMessage) (*NotificationChannelMessage, error) {
	if update == nil || update.Payload == nil {
		return nil, common.Errorf(common.Invalid, "a notification channel is required")
	}
	if update.ResourceID == "" {
		return nil, common.Errorf(common.Invalid, "a notification channel resource id is required")
	}
	raw, err := protojson.Marshal(update.Payload)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal the notification channel")
	}
	secret, err := s.encryptNotificationChannelSecret(update.Secret)
	if err != nil {
		return nil, err
	}

	// A nil secret keeps the stored ciphertext: the update statement only touches
	// the column when a new credential was supplied.
	message, err := scanNotificationChannel(s.GetDB().QueryRowContext(ctx, `
		UPDATE notification_channel
		SET payload = $2,
		    secret = CASE WHEN $3 = '' THEN secret ELSE $3 END,
		    updated_at = NOW()
		WHERE resource_id = $1
		RETURNING `+notificationChannelColumns,
		update.ResourceID, raw, secret))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to update the notification channel")
	}
	return message, nil
}

// DeleteNotificationChannel removes one channel. Its deliveries go with it.
func (s *Store) DeleteNotificationChannel(ctx context.Context, resourceID string) error {
	if resourceID == "" {
		return common.Errorf(common.Invalid, "a notification channel resource id is required")
	}
	if _, err := s.GetDB().ExecContext(ctx, deleteNotificationChannelQuery, resourceID); err != nil {
		return errors.Wrap(err, "failed to delete the notification channel")
	}
	return nil
}

// GetNotificationChannelSecret decrypts the credential of one channel. It is
// only called by the dispatcher and the test-send path, never by a read RPC.
func (s *Store) GetNotificationChannelSecret(ctx context.Context, channelID int64) (*storepb.NotificationChannelSecret, error) {
	if channelID <= 0 {
		return nil, common.Errorf(common.Invalid, "a notification channel id is required")
	}
	var encrypted string
	if err := s.GetDB().QueryRowContext(ctx, notificationChannelSecretQuery, channelID).Scan(&encrypted); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the notification channel secret")
	}
	if encrypted == "" {
		return nil, nil
	}
	cipher, err := s.credentialCipher()
	if err != nil {
		return nil, err
	}
	plaintext, err := cipher.Decrypt(encrypted)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decrypt the notification channel secret")
	}
	secret, err := unmarshalNotificationChannelSecret(plaintext)
	if err != nil {
		return nil, err
	}
	return secret, nil
}

// unmarshalNotificationChannelSecret decodes the sealed credential. PR-1 stored
// it under webhookUrl/webhookSigningSecret before the fields were renamed; that
// build was never released, but reading the old keys once keeps a development
// channel usable instead of silently reporting an empty endpoint while
// secret_set stays true.
func unmarshalNotificationChannelSecret(plaintext string) (*storepb.NotificationChannelSecret, error) {
	secret := &storepb.NotificationChannelSecret{}
	if err := common.ProtojsonUnmarshaler.Unmarshal([]byte(plaintext), secret); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal the notification channel secret")
	}
	if secret.GetEndpoint() != "" || secret.GetSigningSecret() != "" {
		return secret, nil
	}
	var legacy struct {
		WebhookURL           string `json:"webhookUrl"`
		WebhookSigningSecret string `json:"webhookSigningSecret"`
	}
	if err := json.Unmarshal([]byte(plaintext), &legacy); err == nil {
		if legacy.WebhookURL != "" {
			secret.Endpoint = legacy.WebhookURL
		}
		if legacy.WebhookSigningSecret != "" {
			secret.SigningSecret = legacy.WebhookSigningSecret
		}
	}
	return secret, nil
}

// encryptNotificationChannelSecret seals the credential. An empty secret is
// stored as the empty string so the update path can tell "keep" from "replace".
func (s *Store) encryptNotificationChannelSecret(secret *storepb.NotificationChannelSecret) (string, error) {
	if secret == nil || (secret.GetEndpoint() == "" && secret.GetSigningSecret() == "") {
		return "", nil
	}
	raw, err := protojson.Marshal(secret)
	if err != nil {
		return "", errors.Wrap(err, "failed to marshal the notification channel secret")
	}
	cipher, err := s.credentialCipher()
	if err != nil {
		return "", err
	}
	encrypted, err := cipher.Encrypt(string(raw))
	if err != nil {
		return "", errors.Wrap(err, "failed to encrypt the notification channel secret")
	}
	return encrypted, nil
}
