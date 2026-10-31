package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// notificationDeliveryColumns is the column list every notification_delivery
// read returns, in the order scanNotificationDelivery consumes it.
const notificationDeliveryColumns = `id, event_id, channel_id, state,
	attempts, next_attempt_at, last_error, payload, created_at, sent_at, quiet_deferred`

// NotificationDeliveryMessage is one queued outbound delivery.
type NotificationDeliveryMessage struct {
	ID          int64
	EventID     string
	EventKey    string
	ChannelID   int64
	State       storepb.NotificationDeliveryState
	Attempts    int32
	NextAttempt time.Time
	LastError   string
	Payload     *storepb.NotificationDeliveryEvent
	CreatedAt   time.Time
	SentAt      *time.Time
	// QuietDeferred marks a row held by the workspace quiet-hours schedule, so a
	// schedule change can release it without touching retry backoff rows.
	QuietDeferred bool
}

// CreateNotificationDeliveryMessage is one row of an enqueue batch.
type CreateNotificationDeliveryMessage struct {
	EventID     string
	EventKey    string
	ChannelID   int64
	Payload     *storepb.NotificationDeliveryEvent
	NextAttempt time.Time
}

// CreateNotificationDeliveries queues one event for several channels at once.
// The unique (event_id, channel_id) index makes a repeated enqueue a no-op, so
// a retried producer cannot post the same event twice.
func (s *Store) CreateNotificationDeliveries(ctx context.Context, creates []*CreateNotificationDeliveryMessage) error {
	if len(creates) == 0 {
		return nil
	}
	var (
		eventIDs   []string
		channelIDs []int32
		payloads   []string
		attempts   []int32
		nextAt     []time.Time
	)
	state := int16(storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_PENDING)
	states := make([]int16, 0, len(creates))
	for _, create := range creates {
		if create == nil || create.Payload == nil {
			continue
		}
		raw, err := protojson.Marshal(create.Payload)
		if err != nil {
			return errors.Wrap(err, "failed to marshal a notification delivery event")
		}
		eventIDs = append(eventIDs, create.EventID)
		channelIDs = append(channelIDs, int32(create.ChannelID))
		payloads = append(payloads, string(raw))
		attempts = append(attempts, 0)
		nextAt = append(nextAt, create.NextAttempt)
		states = append(states, state)
	}
	if len(eventIDs) == 0 {
		return nil
	}

	// The join to notification_channel drops a row whose channel was deleted after
	// the fan-out read it, instead of failing the whole batch on the foreign key and
	// losing the healthy channels' deliveries with it.
	_, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO notification_delivery (
			event_id, channel_id, state, attempts, next_attempt_at, payload
		)
		SELECT u.event_id, u.channel_id, u.state, u.attempts, u.next_attempt_at, u.payload
		FROM UNNEST(
			$1::text[], $2::int4[], $3::int2[], $4::int4[], $5::timestamptz[], $6::jsonb[]
		) AS u(event_id, channel_id, state, attempts, next_attempt_at, payload)
		JOIN notification_channel c ON c.id = u.channel_id
		ON CONFLICT (event_id, channel_id) DO NOTHING`,
		pq.Array(eventIDs), pq.Array(channelIDs), pq.Array(states),
		pq.Array(attempts), pq.Array(nextAt), pq.Array(payloads))
	if err != nil {
		return errors.Wrap(err, "failed to queue the notification deliveries")
	}
	return nil
}

// deliveryScanner is the row shape scanned from either a *sql.Row or *sql.Rows.
type deliveryScanner interface {
	Scan(dest ...any) error
}

func scanNotificationDelivery(row deliveryScanner) (*NotificationDeliveryMessage, error) {
	var (
		message NotificationDeliveryMessage
		state   int16
		payload []byte
		sentAt  sql.NullTime
	)
	dest := []any{
		&message.ID,
		&message.EventID,
		&message.ChannelID,
		&state,
		&message.Attempts,
		&message.NextAttempt,
		&message.LastError,
		&payload,
		&message.CreatedAt,
		&sentAt,
		&message.QuietDeferred,
	}
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	message.State = storepb.NotificationDeliveryState(state)
	if sentAt.Valid {
		message.SentAt = &sentAt.Time
	}
	event := &storepb.NotificationDeliveryEvent{}
	if err := common.ProtojsonUnmarshaler.Unmarshal(payload, event); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal the notification delivery event")
	}
	message.Payload = event
	return &message, nil
}

// ClaimNotificationDeliveries leases up to limit due deliveries for one attempt.
// A PENDING row is due at next_attempt_at; a SENDING row whose lease deadline
// passed is reclaimed, so a dispatcher that crashed mid-post does not strand the
// delivery. It increments attempts, so the counter reflects attempts started
// rather than attempts finished.
func (s *Store) ClaimNotificationDeliveries(ctx context.Context, limit int, lease time.Duration) ([]*NotificationDeliveryMessage, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.GetDB().QueryContext(ctx, buildClaimNotificationDeliveriesQuery(),
		pq.Array([]int16{
			int16(storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_PENDING),
			int16(storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_SENDING),
		}),
		limit,
		int16(storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_SENDING),
		int32(lease/time.Second),
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to claim the notification deliveries")
	}
	defer rows.Close()

	var messages []*NotificationDeliveryMessage
	for rows.Next() {
		message, err := scanNotificationDelivery(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a notification delivery")
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the notification deliveries")
	}
	return messages, nil
}

// FinishNotificationDelivery records the outcome of one attempt. SENT is
// terminal; a failed attempt goes back to PENDING with its next attempt time or
// becomes DEAD when the caller has exhausted the budget.
func (s *Store) FinishNotificationDelivery(ctx context.Context, id int64, state storepb.NotificationDeliveryState, lastError string, nextAttempt time.Time) error {
	if id <= 0 {
		return common.Errorf(common.Invalid, "a notification delivery id is required")
	}
	const query = `
		UPDATE notification_delivery
		SET state = $2,
		    last_error = $3,
		    next_attempt_at = $4,
		    sent_at = CASE WHEN $2 = $5 THEN NOW() ELSE NULL END,
		    quiet_deferred = FALSE
		WHERE id = $1`
	_, err := s.GetDB().ExecContext(ctx, query,
		id, int16(state), lastError, nextAttempt,
		int16(storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_SENT),
	)
	if err != nil {
		return errors.Wrap(err, "failed to finish the notification delivery")
	}
	return nil
}

// deferNotificationDeliveryQuery is the quiet-hours reschedule. It must put the
// row back to PENDING and give the attempt back, or a deferral would consume the
// delivery's retry budget.
const deferNotificationDeliveryQuery = `
	UPDATE notification_delivery
	SET state = $2,
	    attempts = GREATEST(attempts - 1, 0),
	    next_attempt_at = $3,
	    quiet_deferred = TRUE
	WHERE id = $1 AND state = $4`

// DeferNotificationDelivery reschedules a claimed delivery without charging it an
// attempt: a quiet-hours deferral is scheduling, not a failed post, and the claim
// already incremented the counter. It leaves the row PENDING with the given next
// attempt, so the first tick after the window ends picks it up.
func (s *Store) DeferNotificationDelivery(ctx context.Context, id int64, nextAttempt time.Time) error {
	if id <= 0 {
		return common.Errorf(common.Invalid, "a notification delivery id is required")
	}
	_, err := s.GetDB().ExecContext(ctx, deferNotificationDeliveryQuery,
		id,
		int16(storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_PENDING),
		nextAttempt,
		int16(storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_SENDING),
	)
	if err != nil {
		return errors.Wrap(err, "failed to defer the notification delivery")
	}
	return nil
}

// ReleaseQuietDeferredDeliveries releases every delivery the quiet-hours schedule
// is holding, so a schedule change takes effect immediately instead of at the old
// window's end. It returns how many rows were released.
func (s *Store) ReleaseQuietDeferredDeliveries(ctx context.Context) (int64, error) {
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE notification_delivery
		SET next_attempt_at = NOW(), quiet_deferred = FALSE
		WHERE state = $1 AND quiet_deferred`,
		int16(storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_PENDING),
	)
	if err != nil {
		return 0, errors.Wrap(err, "failed to release the quiet-deferred notification deliveries")
	}
	released, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count the released notification deliveries")
	}
	return released, nil
}

// GetNotificationDelivery reads one delivery by row id. A nil message means no
// such delivery.
func (s *Store) GetNotificationDelivery(ctx context.Context, id int64) (*NotificationDeliveryMessage, error) {
	if id <= 0 {
		return nil, common.Errorf(common.Invalid, "a notification delivery id is required")
	}
	message, err := scanNotificationDelivery(s.GetDB().QueryRowContext(ctx, `
		SELECT `+notificationDeliveryColumns+`
		FROM notification_delivery
		WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the notification delivery")
	}
	return message, nil
}

// NotificationDeliveryFilter narrows a delivery listing. A nil channel or
// UNSPECIFIED state means no filter on that field.
type NotificationDeliveryFilter struct {
	ChannelID int64
	State     storepb.NotificationDeliveryState
	Limit     int
	Offset    int
}

// ListNotificationDeliveries returns deliveries newest first.
func (s *Store) ListNotificationDeliveries(ctx context.Context, filter NotificationDeliveryFilter) ([]*NotificationDeliveryMessage, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	query, args := buildListNotificationDeliveriesQuery(filter, limit, offset)
	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the notification deliveries")
	}
	defer rows.Close()

	var messages []*NotificationDeliveryMessage
	for rows.Next() {
		message, err := scanNotificationDelivery(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a notification delivery")
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the notification deliveries")
	}
	return messages, nil
}

// RequeueNotificationDelivery puts one terminal delivery back in the queue for
// an administrator-initiated retry. A delivery that is not DEAD is returned
// unchanged, so the call is idempotent and cannot duplicate a post.
func (s *Store) RequeueNotificationDelivery(ctx context.Context, id int64) (*NotificationDeliveryMessage, error) {
	if id <= 0 {
		return nil, common.Errorf(common.Invalid, "a notification delivery id is required")
	}
	message, err := scanNotificationDelivery(s.GetDB().QueryRowContext(ctx, `
		UPDATE notification_delivery
		SET state = $2,
		    attempts = 0,
		    next_attempt_at = NOW(),
		    last_error = '',
		    quiet_deferred = FALSE
		WHERE id = $1 AND state = $3
		RETURNING `+notificationDeliveryColumns,
		id,
		int16(storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_PENDING),
		int16(storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_DEAD),
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to requeue the notification delivery")
	}
	return message, nil
}

// PruneNotificationDeliveries deletes terminal deliveries older than the given
// cutoff. Queued deliveries are never pruned.
func (s *Store) PruneNotificationDeliveries(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.GetDB().ExecContext(ctx, `
		DELETE FROM notification_delivery
		WHERE state IN ($1, $2) AND created_at < $3`,
		int16(storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_SENT),
		int16(storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_DEAD),
		before,
	)
	if err != nil {
		return 0, errors.Wrap(err, "failed to prune the notification deliveries")
	}
	pruned, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count the pruned notification deliveries")
	}
	return pruned, nil
}

// buildClaimNotificationDeliveriesQuery returns the claim statement. It is a
// function because its shape is the invariant: the partial index it reads, the
// SKIP LOCKED claim, the attempts increment and the lease all live here.
func buildClaimNotificationDeliveriesQuery() string {
	return `
		WITH due AS (
			SELECT id
			FROM notification_delivery
			WHERE state = ANY($1::int2[]) AND next_attempt_at <= NOW()
			ORDER BY next_attempt_at, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE notification_delivery d
		SET state = $3,
		    attempts = d.attempts + 1,
		    next_attempt_at = NOW() + ($4::int4 * INTERVAL '1 second')
		FROM due
		WHERE d.id = due.id
		RETURNING ` + notificationDeliveryColumns
}

// buildListNotificationDeliveriesQuery returns the listing statement for the
// filters that were actually set. A placeholder that is compared against zero
// would defeat the index and force a sequential scan of the whole outbox.
func buildListNotificationDeliveriesQuery(filter NotificationDeliveryFilter, limit, offset int) (string, []any) {
	var (
		conditions []string
		args       []any
	)
	query := `SELECT ` + notificationDeliveryColumns + ` FROM notification_delivery`
	if filter.ChannelID > 0 {
		args = append(args, filter.ChannelID)
		conditions = append(conditions, "channel_id = $"+strconv.Itoa(len(args)))
	}
	if filter.State != storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_UNSPECIFIED {
		args = append(args, int16(filter.State))
		conditions = append(conditions, "state = $"+strconv.Itoa(len(args)))
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	args = append(args, limit, offset)
	query += " ORDER BY created_at DESC, id DESC LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args))
	return query, args
}

// ReleaseNotificationDelivery returns one leased delivery to the queue without
// consuming an attempt. The dispatcher uses it when a batch cannot be resolved
// for a reason outside the delivery itself (the channel read failed): the
// delivery is not at fault and must not be charged an attempt or killed.
func (s *Store) ReleaseNotificationDelivery(ctx context.Context, id int64) error {
	if id <= 0 {
		return common.Errorf(common.Invalid, "a notification delivery id is required")
	}
	const query = `
		UPDATE notification_delivery
		SET state = $2,
		    attempts = GREATEST(attempts - 1, 0),
		    next_attempt_at = NOW(),
		    quiet_deferred = FALSE
		WHERE id = $1 AND state = $3`
	_, err := s.GetDB().ExecContext(ctx, query,
		id,
		int16(storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_PENDING),
		int16(storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_SENDING),
	)
	if err != nil {
		return errors.Wrap(err, "failed to release the notification delivery")
	}
	return nil
}
