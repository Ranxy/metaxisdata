package state

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ConnectionPriority orders the requests waiting for one instance's connection
// budget. A released slot goes to the highest-priority waiter first and to the
// oldest waiter within a priority, so work a person is waiting on never queues
// behind a full round of background database syncs.
type ConnectionPriority int

const (
	// ConnectionPriorityBackground is periodic work: the schema syncer and the
	// scheduled data-quality runs.
	ConnectionPriorityBackground ConnectionPriority = iota
	// ConnectionPriorityManual is work a user asked for but does not wait on
	// synchronously, such as a manual data-quality run.
	ConnectionPriorityManual
	// ConnectionPriorityInteractive is work a person is waiting on, such as the
	// instance's test-connection action. It is served before every other class.
	ConnectionPriorityInteractive
)

// InstanceConnectionRequest is one request for a lease on an instance's
// connection budget. MaximumConnections <= 0 means the instance sets no ceiling:
// the request is admitted at once, but it is still counted so diagnostics see
// the real number of sessions.
type InstanceConnectionRequest struct {
	InstanceID         string
	MaximumConnections int
	Priority           ConnectionPriority
}

// ErrInstanceConnectionLimit reports that no connection slot became free before
// the caller's wait budget ran out. It is a transient capacity condition, not a
// verdict on the work that needed the connection.
var ErrInstanceConnectionLimit = errors.New("instance connection limit reached")

// connectionGovernor hands out the per-instance connection budget. It is a fair
// queue rather than a counter: a request that cannot be admitted waits on its
// context and is woken with a slot, so contention becomes backpressure at the
// call site instead of an error that the caller has to interpret.
//
// A lease represents one session on the target database: its holder opens a
// driver, runs its statements sequentially and closes it, so the budget bounds
// concurrently open sessions rather than the driver pools' internal connection
// count.
type connectionGovernor struct {
	mu      sync.Mutex
	holders map[string]int
	waiters map[string][]*connectionWaiter
	// sequence orders waiters within a priority so a caller cannot be overtaken
	// by later arrivals of the same class.
	sequence uint64
}

// connectionWaiter is one queued request. granted is set under the governor's
// lock together with the holder count and before ready is closed, so a caller
// that loses the race against its own context can hand the slot back instead of
// leaking it.
type connectionWaiter struct {
	priority ConnectionPriority
	sequence uint64
	limit    int
	granted  bool
	ready    chan struct{}
}

func newConnectionGovernor() *connectionGovernor {
	return &connectionGovernor{
		holders: make(map[string]int),
		waiters: make(map[string][]*connectionWaiter),
	}
}

// AcquireInstanceConnection blocks until one of the instance's outstanding
// connection slots is free, ctx is done, or the ceiling admits the request
// immediately. The returned release function is idempotent; callers must call it
// once they are done with the connection.
//
// A limit of zero or less means "no ceiling" and never queues.
func (s *State) AcquireInstanceConnection(ctx context.Context, request InstanceConnectionRequest) (func(), error) {
	return s.instanceConnections.acquire(ctx, request)
}

// TryAcquireInstanceConnection admits the request only when it can have a slot
// immediately. A queued waiter owns the next free slot, so a try-acquire never
// jumps the queue: callers that can retry later (the schema syncer keeps its
// database queued for the next tick) must not starve the callers that wait.
func (s *State) TryAcquireInstanceConnection(request InstanceConnectionRequest) (func(), bool) {
	return s.instanceConnections.tryAcquire(request)
}

func (g *connectionGovernor) acquire(ctx context.Context, request InstanceConnectionRequest) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, limitError(request.InstanceID, err)
	}

	g.mu.Lock()
	if len(g.waiters[request.InstanceID]) == 0 && g.admissibleLocked(request) {
		g.holders[request.InstanceID]++
		g.mu.Unlock()
		return g.lease(request.InstanceID), nil
	}
	waiter := &connectionWaiter{
		priority: request.Priority,
		sequence: g.sequence,
		limit:    request.MaximumConnections,
		ready:    make(chan struct{}),
	}
	g.sequence++
	g.waiters[request.InstanceID] = append(g.waiters[request.InstanceID], waiter)
	g.mu.Unlock()

	select {
	case <-waiter.ready:
		return g.lease(request.InstanceID), nil
	case <-ctx.Done():
		g.mu.Lock()
		granted := waiter.granted
		if !granted {
			g.removeWaiterLocked(request.InstanceID, waiter)
		}
		g.mu.Unlock()
		if granted {
			// The slot was granted while the caller was giving up; return it to the
			// queue rather than leaking it.
			g.release(request.InstanceID)
		}
		return nil, limitError(request.InstanceID, ctx.Err())
	}
}

func (g *connectionGovernor) tryAcquire(request InstanceConnectionRequest) (func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.waiters[request.InstanceID]) > 0 || !g.admissibleLocked(request) {
		return nil, false
	}
	g.holders[request.InstanceID]++
	return g.lease(request.InstanceID), true
}

func (g *connectionGovernor) admissibleLocked(request InstanceConnectionRequest) bool {
	return request.MaximumConnections <= 0 || g.holders[request.InstanceID] < request.MaximumConnections
}

// lease returns the idempotent release function for one admitted slot.
func (g *connectionGovernor) lease(instanceID string) func() {
	var once sync.Once
	return func() {
		once.Do(func() { g.release(instanceID) })
	}
}

func (g *connectionGovernor) release(instanceID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.holders[instanceID] <= 1 {
		delete(g.holders, instanceID)
	} else {
		g.holders[instanceID]--
	}
	g.dispatchLocked(instanceID)
}

// dispatchLocked hands a freed slot to the queued waiters, highest priority
// first and oldest first within a priority. Every caller of one instance passes
// the same ceiling, so the head waiter's limit is the one that decides whether
// another slot is available.
func (g *connectionGovernor) dispatchLocked(instanceID string) {
	waiters := g.waiters[instanceID]
	for len(waiters) > 0 {
		next := bestWaiterIndex(waiters)
		waiter := waiters[next]
		if waiter.limit > 0 && g.holders[instanceID] >= waiter.limit {
			break
		}
		waiters = append(waiters[:next], waiters[next+1:]...)
		g.holders[instanceID]++
		waiter.granted = true
		close(waiter.ready)
	}
	if len(waiters) == 0 {
		delete(g.waiters, instanceID)
		return
	}
	g.waiters[instanceID] = waiters
}

func bestWaiterIndex(waiters []*connectionWaiter) int {
	best := 0
	for i := 1; i < len(waiters); i++ {
		higherPriority := waiters[i].priority > waiters[best].priority
		samePriorityOlder := waiters[i].priority == waiters[best].priority &&
			waiters[i].sequence < waiters[best].sequence
		if higherPriority || samePriorityOlder {
			best = i
		}
	}
	return best
}

func (g *connectionGovernor) removeWaiterLocked(instanceID string, waiter *connectionWaiter) {
	waiters := g.waiters[instanceID]
	for i, candidate := range waiters {
		if candidate != waiter {
			continue
		}
		waiters = append(waiters[:i], waiters[i+1:]...)
		break
	}
	if len(waiters) == 0 {
		delete(g.waiters, instanceID)
		return
	}
	g.waiters[instanceID] = waiters
}

// limitError names the instance and keeps the context cause, so a caller that
// gives up waiting logs why it stopped waiting as well as what it needed.
func limitError(instanceID string, cause error) error {
	return fmt.Errorf("%w: instance %q waited for a free connection slot: %w", ErrInstanceConnectionLimit, instanceID, cause)
}
