package notification

import (
	"sync"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Event is one message handed to the live subscriptions of an inbox: the row a
// write produced, and the recipient's unread count right after it.
type Event struct {
	Notification *storepb.Notification
	UnreadCount  int32
	// UnreadCountKnown is false when the count could not be read. The message
	// itself is still worth delivering; the badge is not, because a zero would
	// read as "everything is read".
	UnreadCountKnown bool
}

const (
	// subscriptionBuffer is how far behind one connection may fall. Notifications
	// are low-frequency, so this is a safety valve rather than a working queue.
	subscriptionBuffer = 16
	// MaxSubscriptionsPerRecipient bounds what one account may hold open. One
	// browser tab holds one, and a reconnect overlaps briefly with the stream it
	// replaces. Past the bound the newest connection is refused rather than the
	// oldest one dropped: the caller is told, instead of another tab going quiet.
	MaxSubscriptionsPerRecipient = 8
)

// subscription is one live connection's end of the hub. The hub owns the channel:
// it closes it to end the connection.
type subscription struct {
	ch chan Event
}

// hub fans one written notification out to the live subscriptions of its
// recipient. It is process-local, like the device login state and the sync
// operation aggregator: a message written by another replica is not pushed, and
// the client finds it on its next reconnect or refresh.
//
// A subscription never blocks its publisher. The publisher is a sync runner or an
// ingestion handler, and a client that has stopped reading must not hold either of
// them open; a subscription that cannot keep up is ended instead, which the client
// answers by reconnecting and refreshing. The message is in the inbox regardless.
type hub struct {
	mu     sync.Mutex
	closed bool
	// subs holds the subscriptions of one recipient, keyed by principal id: a
	// notification belongs to one principal, so the fan-out is per inbox.
	subs map[int]map[*subscription]struct{}
}

func newHub() *hub {
	return &hub{subs: map[int]map[*subscription]struct{}{}}
}

// subscribe registers a subscription for one recipient. Past
// MaxSubscriptionsPerRecipient it refuses, and after the hub is closed it returns a
// subscription that has already ended, so a connection opened while the server is
// draining finishes at once instead of waiting for anything.
func (h *hub) subscribe(recipientID int) (*subscription, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		sub := &subscription{ch: make(chan Event)}
		close(sub.ch)
		return sub, nil
	}
	if own := h.subs[recipientID]; len(own) >= MaxSubscriptionsPerRecipient {
		return nil, errors.Errorf("recipient %d already holds %d live notification streams", recipientID, len(own))
	}

	sub := &subscription{ch: make(chan Event, subscriptionBuffer)}
	own := h.subs[recipientID]
	if own == nil {
		own = map[*subscription]struct{}{}
		h.subs[recipientID] = own
	}
	own[sub] = struct{}{}
	return sub, nil
}

// unsubscribe releases a subscription. It is safe to call on a subscription that
// has already ended, and more than once.
func (h *hub) unsubscribe(recipientID int, sub *subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.removeLocked(recipientID, sub)
}

// publish hands one event to every live subscription of one recipient.
func (h *hub) publish(recipientID int, event Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for sub := range h.subs[recipientID] {
		select {
		case sub.ch <- event:
		default:
			// The connection is not keeping up, so it is ended rather than allowed
			// to hold this publisher. Its client reconnects and refreshes the
			// inbox, where the message already is.
			h.removeLocked(recipientID, sub)
		}
	}
}

// close ends every live subscription and refuses new ones. The server drains
// through it: a notification stream is an in-flight request that never ends on its
// own, and the HTTP server's shutdown waits for those.
func (h *hub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return
	}
	h.closed = true
	for recipientID, own := range h.subs {
		for sub := range own {
			close(sub.ch)
		}
		delete(h.subs, recipientID)
	}
}

// removeLocked forgets a subscription and closes its channel. The caller holds the
// hub lock, so no event can be sent on a closed channel afterwards.
func (h *hub) removeLocked(recipientID int, sub *subscription) {
	own, ok := h.subs[recipientID]
	if !ok {
		return
	}
	if _, ok := own[sub]; !ok {
		return
	}
	delete(own, sub)
	if len(own) == 0 {
		delete(h.subs, recipientID)
	}
	close(sub.ch)
}
