package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The claim query's shape is the invariant: the partial index it reads, the
// SKIP LOCKED claim, the attempts increment and the lease.
func TestBuildClaimNotificationDeliveriesQuery(t *testing.T) {
	t.Parallel()

	query := buildClaimNotificationDeliveriesQuery()
	for _, fragment := range []string{
		"WITH due AS (",
		"WHERE state = ANY($1::int2[]) AND next_attempt_at <= NOW()",
		"ORDER BY next_attempt_at, id",
		"FOR UPDATE SKIP LOCKED",
		"attempts = d.attempts + 1",
		"next_attempt_at = NOW() + ($4::int4 * INTERVAL '1 second')",
		"RETURNING id, event_id, channel_id",
	} {
		require.Contains(t, query, fragment)
	}
	require.NotContains(t, query, "event_key")
}

// A placeholder compared against zero would force a sequential scan of the whole
// outbox, so absent filters must not appear in the statement at all.
func TestBuildListNotificationDeliveriesQuery(t *testing.T) {
	t.Parallel()

	query, args := buildListNotificationDeliveriesQuery(NotificationDeliveryFilter{}, 50, 0)
	require.NotContains(t, query, "WHERE")
	require.Contains(t, query, "ORDER BY created_at DESC, id DESC")
	require.Contains(t, query, "LIMIT $1 OFFSET $2")
	require.Equal(t, []any{50, 0}, args)

	query, args = buildListNotificationDeliveriesQuery(NotificationDeliveryFilter{
		ChannelID: 7,
		State:     storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_DEAD,
	}, 25, 50)
	require.Contains(t, query, "WHERE channel_id = $1 AND state = $2")
	require.Contains(t, query, "LIMIT $3 OFFSET $4")
	require.Equal(t, []any{int64(7), int16(4), 25, 50}, args)

	query, args = buildListNotificationDeliveriesQuery(NotificationDeliveryFilter{
		State: storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_PENDING,
	}, 10, 0)
	require.Contains(t, query, "WHERE state = $1")
	require.Equal(t, []any{int16(1), 10, 0}, args)
}

// A quiet-hours deferral is not an attempt: the statement must return the row to
// PENDING, give the claim's increment back and schedule the window's end.
func TestDeferNotificationDeliveryQuery(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"UPDATE notification_delivery",
		"SET state = $2",
		"attempts = GREATEST(attempts - 1, 0)",
		"next_attempt_at = $3",
		"WHERE id = $1",
	} {
		require.Contains(t, deferNotificationDeliveryQuery, fragment)
	}
}
