package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The claim query's shape is the invariant: the partial index it reads, the
// SKIP LOCKED claim, the attempts increment, the lease, and a RETURNING list
// qualified with the UPDATE target. The qualification is load-bearing: the FROM
// also holds the due CTE, whose id column would otherwise make the RETURNING
// list ambiguous and fail the whole claim (SQLSTATE 42702).
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
		"RETURNING d.id, d.event_id, d.channel_id",
	} {
		require.Contains(t, query, fragment)
	}
	require.NotContains(t, query, "event_key")

	// The qualified list is the same columns in the same order as the shared
	// projection: scanNotificationDelivery consumes them positionally.
	require.Equal(t,
		strings.Join(strings.Fields(qualifyColumnList(notificationDeliveryColumns, "d.")), " "),
		strings.Join(strings.Fields(notificationDeliveryColumnsForClaim), " "))
}

// qualifyColumnList prefixes every column of a comma-separated projection, so a
// statement can name the UPDATE target's columns without restating the list.
func qualifyColumnList(columns, prefix string) string {
	parts := strings.Split(columns, ",")
	qualified := make([]string, 0, len(parts))
	for _, part := range parts {
		qualified = append(qualified, prefix+strings.TrimSpace(part))
	}
	return strings.Join(qualified, ", ")
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
		"quiet_deferred = TRUE",
		"WHERE id = $1 AND state = $4",
	} {
		require.Contains(t, deferNotificationDeliveryQuery, fragment)
	}
}

// The terminal write compares the state in a CASE to stamp sent_at. Both operands
// must carry an explicit int4 cast: without it PostgreSQL infers two types for $2
// (assignment target and equality operand) and rejects the write with 42P08,
// which leaves delivered rows SENDING and lets the dispatcher re-post them.
func TestFinishNotificationDeliveryQueryCastsTheComparedState(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"SET state = $2",
		"sent_at = CASE WHEN $2::int4 = $5::int4 THEN NOW() ELSE NULL END",
		"quiet_deferred = FALSE",
		"WHERE id = $1",
	} {
		require.Contains(t, finishNotificationDeliveryQuery, fragment)
	}
}
