package notification

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestHubFansOutToTheRecipientsSubscriptions(t *testing.T) {
	t.Parallel()

	hub := newHub()
	first, err := hub.subscribe(7)
	require.NoError(t, err)
	second, err := hub.subscribe(7)
	require.NoError(t, err)
	other, err := hub.subscribe(8)
	require.NoError(t, err)

	hub.publish(7, Event{Notification: &storepb.Notification{Id: 1}, UnreadCount: 2, UnreadCountKnown: true})

	require.Equal(t, int64(1), (<-first.ch).Notification.GetId())
	require.Equal(t, int64(1), (<-second.ch).Notification.GetId())
	select {
	case event := <-other.ch:
		t.Fatalf("an inbox received another recipient's message: %v", event.Notification)
	default:
	}
}

// A subscription that has stopped reading must not hold the writer — a sync runner
// or an ingestion handler — open. It is ended instead, and its client reconnects and
// refreshes the inbox, where the message already is.
func TestHubDropsASubscriptionThatCannotKeepUp(t *testing.T) {
	t.Parallel()

	hub := newHub()
	sub, err := hub.subscribe(7)
	require.NoError(t, err)

	// One more message than the buffer holds, published from its own goroutine so a
	// blocking publish fails the test instead of hanging it.
	published := make(chan struct{})
	go func() {
		defer close(published)
		for range subscriptionBuffer + 1 {
			hub.publish(7, Event{})
		}
	}()
	select {
	case <-published:
	case <-time.After(2 * time.Second):
		t.Fatal("publish blocked on a subscription that had stopped reading")
	}

	for range subscriptionBuffer {
		_, ok := <-sub.ch
		require.True(t, ok, "the events that fit must still be delivered")
	}
	_, ok := <-sub.ch
	require.False(t, ok, "a subscription that fell behind is ended")
}

func TestHubRefusesPastTheBoundPerRecipient(t *testing.T) {
	t.Parallel()

	hub := newHub()
	subs := make([]*subscription, 0, MaxSubscriptionsPerRecipient)
	for range MaxSubscriptionsPerRecipient {
		sub, err := hub.subscribe(7)
		require.NoError(t, err)
		subs = append(subs, sub)
	}

	_, err := hub.subscribe(7)
	require.ErrorContains(t, err, "live notification streams", "one account must not open streams without bound")

	// Another recipient is unaffected, and releasing one makes room again.
	_, err = hub.subscribe(8)
	require.NoError(t, err)
	hub.unsubscribe(7, subs[0])
	_, err = hub.subscribe(7)
	require.NoError(t, err)
}

func TestHubCloseEndsEverySubscription(t *testing.T) {
	t.Parallel()

	hub := newHub()
	first, err := hub.subscribe(7)
	require.NoError(t, err)
	second, err := hub.subscribe(8)
	require.NoError(t, err)

	hub.close()
	hub.close() // idempotent: the server may drain more than once
	_, ok := <-first.ch
	require.False(t, ok)
	_, ok = <-second.ch
	require.False(t, ok)

	// A connection that arrives during shutdown ends at once rather than waiting for
	// a message that will never come.
	late, err := hub.subscribe(7)
	require.NoError(t, err)
	_, ok = <-late.ch
	require.False(t, ok)

	// Nothing after close may panic: no send on a closed channel, no double close.
	hub.publish(7, Event{})
	hub.unsubscribe(7, first)
	hub.unsubscribe(7, late)
}

func TestHubUnsubscribeEndsTheSubscriptionOnce(t *testing.T) {
	t.Parallel()

	hub := newHub()
	sub, err := hub.subscribe(7)
	require.NoError(t, err)

	hub.unsubscribe(7, sub)
	_, ok := <-sub.ch
	require.False(t, ok)
	hub.unsubscribe(7, sub)
	hub.publish(7, Event{})
}
