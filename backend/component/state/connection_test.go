package state

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// testInstanceID is the one instance every governor test exercises.
const testInstanceID = "i1"

func connectionRequest(limit int, priority ConnectionPriority) InstanceConnectionRequest {
	return InstanceConnectionRequest{
		InstanceID:         testInstanceID,
		MaximumConnections: limit,
		Priority:           priority,
	}
}

// holderCount and waiterCount are test views into the governor; the production
// API deliberately exposes neither.
func (g *connectionGovernor) holderCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.holders[testInstanceID]
}

func (g *connectionGovernor) waiterCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.waiters[testInstanceID])
}

func waitForWaiters(t *testing.T, g *connectionGovernor, want int) {
	t.Helper()
	require.Eventually(t, func() bool { return g.waiterCount() == want }, time.Second, time.Millisecond)
}

type grantResult struct {
	label   string
	release func()
	err     error
}

// acquireAsync starts one blocked acquisition. Its result arrives once the
// governor hands out a slot.
func acquireAsync(ctx context.Context, stateCfg *State, request InstanceConnectionRequest, label string) <-chan grantResult {
	result := make(chan grantResult, 1)
	go func() {
		release, err := stateCfg.AcquireInstanceConnection(ctx, request)
		result <- grantResult{label: label, release: release, err: err}
	}()
	return result
}

func receiveGrant(t *testing.T, result <-chan grantResult) grantResult {
	t.Helper()
	select {
	case grant := <-result:
		return grant
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a connection slot")
		return grantResult{}
	}
}

// Every driver-opening path shares one budget: the test connection and the
// schema syncs must see the same outstanding count.
func TestAcquireInstanceConnectionSharesThePerInstanceLimit(t *testing.T) {
	t.Parallel()

	stateCfg, err := New(nil)
	require.NoError(t, err)

	releaseSync, ok := stateCfg.TryAcquireInstanceConnection(connectionRequest(2, ConnectionPriorityBackground))
	require.True(t, ok)
	releaseTest, ok := stateCfg.TryAcquireInstanceConnection(connectionRequest(2, ConnectionPriorityInteractive))
	require.True(t, ok)

	_, ok = stateCfg.TryAcquireInstanceConnection(connectionRequest(2, ConnectionPriorityBackground))
	require.False(t, ok, "the third connection exceeds the limit")

	releaseSync()
	releaseThird, ok := stateCfg.TryAcquireInstanceConnection(connectionRequest(2, ConnectionPriorityBackground))
	require.True(t, ok)

	releaseTest()
	releaseThird()
	_, ok = stateCfg.TryAcquireInstanceConnection(connectionRequest(0, ConnectionPriorityBackground))
	require.True(t, ok, "a limit of zero means unlimited")
}

// The saturation that used to surface as a QUOTA error is now a wait: the caller
// is woken with the slot the other holder releases.
func TestAcquireInstanceConnectionWaitsForASlot(t *testing.T) {
	t.Parallel()

	stateCfg, err := New(nil)
	require.NoError(t, err)
	hold, ok := stateCfg.TryAcquireInstanceConnection(connectionRequest(1, ConnectionPriorityBackground))
	require.True(t, ok)

	waiter := acquireAsync(context.Background(), stateCfg, connectionRequest(1, ConnectionPriorityBackground), "background")
	waitForWaiters(t, stateCfg.instanceConnections, 1)

	select {
	case grant := <-waiter:
		t.Fatalf("the waiter was admitted while the budget was exhausted: %+v", grant)
	case <-time.After(20 * time.Millisecond):
	}

	hold()
	grant := receiveGrant(t, waiter)
	require.NoError(t, grant.err)
	require.Equal(t, 1, stateCfg.instanceConnections.holderCount())
	grant.release()
	require.Equal(t, 0, stateCfg.instanceConnections.holderCount())
}

// A caller that cannot wait forever gets a distinguishable, transient error and
// leaves the queue and the counter as it found them.
func TestAcquireInstanceConnectionHonorsItsWaitBudget(t *testing.T) {
	t.Parallel()

	stateCfg, err := New(nil)
	require.NoError(t, err)
	hold, ok := stateCfg.TryAcquireInstanceConnection(connectionRequest(1, ConnectionPriorityBackground))
	require.True(t, ok)
	defer hold()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	release, err := stateCfg.AcquireInstanceConnection(ctx, connectionRequest(1, ConnectionPriorityBackground))
	require.ErrorIs(t, err, ErrInstanceConnectionLimit)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, release)
	require.Equal(t, 1, stateCfg.instanceConnections.holderCount())
	require.Equal(t, 0, stateCfg.instanceConnections.waiterCount())
}

// A cancelled waiter must leave the queue; otherwise it would be granted a slot
// nobody holds.
func TestCancelledWaiterLeavesTheQueue(t *testing.T) {
	t.Parallel()

	stateCfg, err := New(nil)
	require.NoError(t, err)
	hold, ok := stateCfg.TryAcquireInstanceConnection(connectionRequest(1, ConnectionPriorityBackground))
	require.True(t, ok)

	ctx, cancel := context.WithCancel(context.Background())
	waiter := acquireAsync(ctx, stateCfg, connectionRequest(1, ConnectionPriorityBackground), "background")
	waitForWaiters(t, stateCfg.instanceConnections, 1)
	cancel()
	grant := receiveGrant(t, waiter)
	require.ErrorIs(t, grant.err, ErrInstanceConnectionLimit)
	require.ErrorIs(t, grant.err, context.Canceled)
	require.Nil(t, grant.release)
	require.Equal(t, 0, stateCfg.instanceConnections.waiterCount())

	hold()
	release, ok := stateCfg.TryAcquireInstanceConnection(connectionRequest(1, ConnectionPriorityBackground))
	require.True(t, ok, "the cancelled waiter must not hold a slot")
	release()
}

// A released slot goes to the highest-priority waiter, and waiters of one class
// keep their arrival order.
func TestWaitersAreServedByPriorityThenArrival(t *testing.T) {
	t.Parallel()

	stateCfg, err := New(nil)
	require.NoError(t, err)
	hold, ok := stateCfg.TryAcquireInstanceConnection(connectionRequest(1, ConnectionPriorityBackground))
	require.True(t, ok)

	backgroundFirst := acquireAsync(context.Background(), stateCfg, connectionRequest(1, ConnectionPriorityBackground), "background-1")
	waitForWaiters(t, stateCfg.instanceConnections, 1)
	backgroundSecond := acquireAsync(context.Background(), stateCfg, connectionRequest(1, ConnectionPriorityBackground), "background-2")
	waitForWaiters(t, stateCfg.instanceConnections, 2)
	manual := acquireAsync(context.Background(), stateCfg, connectionRequest(1, ConnectionPriorityManual), "manual")
	waitForWaiters(t, stateCfg.instanceConnections, 3)

	hold()
	first := receiveGrant(t, manual)
	require.Equal(t, "manual", first.label, "a user-triggered run must beat the background waiters")
	first.release()

	second := receiveGrant(t, backgroundFirst)
	require.Equal(t, "background-1", second.label, "background waiters keep their arrival order")
	second.release()

	third := receiveGrant(t, backgroundSecond)
	require.Equal(t, "background-2", third.label)
	third.release()
	require.Equal(t, 0, stateCfg.instanceConnections.holderCount())
}

// A caller that can retry later must not jump a queued waiter: the schema
// syncer relies on this to leave the next slot to a run that is already waiting.
func TestTryAcquireDoesNotJumpAQueuedWaiter(t *testing.T) {
	t.Parallel()

	stateCfg, err := New(nil)
	require.NoError(t, err)
	governor := stateCfg.instanceConnections
	governor.mu.Lock()
	governor.waiters[testInstanceID] = append(governor.waiters[testInstanceID], &connectionWaiter{
		priority: ConnectionPriorityManual,
		limit:    2,
		ready:    make(chan struct{}),
	})
	governor.mu.Unlock()

	_, ok := stateCfg.TryAcquireInstanceConnection(connectionRequest(2, ConnectionPriorityBackground))
	require.False(t, ok, "a free slot belongs to the queued waiter")
}

func TestReleaseIsIdempotent(t *testing.T) {
	t.Parallel()

	stateCfg, err := New(nil)
	require.NoError(t, err)
	releaseFirst, ok := stateCfg.TryAcquireInstanceConnection(connectionRequest(1, ConnectionPriorityBackground))
	require.True(t, ok)
	releaseSecond, ok := stateCfg.TryAcquireInstanceConnection(connectionRequest(0, ConnectionPriorityBackground))
	require.True(t, ok)

	releaseFirst()
	releaseFirst()
	require.Equal(t, 1, stateCfg.instanceConnections.holderCount(), "a double release must not free another holder's slot")

	releaseSecond()
	releaseSecond()
	require.Equal(t, 0, stateCfg.instanceConnections.holderCount())
}

func TestUnlimitedBudgetNeverQueues(t *testing.T) {
	t.Parallel()

	stateCfg, err := New(nil)
	require.NoError(t, err)
	releases := make([]func(), 0, 5)
	for range 5 {
		release, ok := stateCfg.TryAcquireInstanceConnection(connectionRequest(0, ConnectionPriorityBackground))
		require.True(t, ok)
		releases = append(releases, release)
	}
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	require.Equal(t, 5, stateCfg.instanceConnections.holderCount(), "an unlimited instance is still counted")
	require.Equal(t, 0, stateCfg.instanceConnections.waiterCount())
}

func TestAcquireWithACancelledContextDoesNotConsumeASlot(t *testing.T) {
	t.Parallel()

	stateCfg, err := New(nil)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	release, err := stateCfg.AcquireInstanceConnection(ctx, connectionRequest(1, ConnectionPriorityBackground))
	require.ErrorIs(t, err, ErrInstanceConnectionLimit)
	require.Nil(t, release)
	require.Equal(t, 0, stateCfg.instanceConnections.holderCount())
}

// A waiter that is cancelled at the same moment it is granted must return the slot
// instead of leaking it. The race is timing dependent, so this runs it many times
// and asserts the budget is fully available again after every round.
func TestCancelledWaiterNeverLeaksItsSlot(t *testing.T) {
	t.Parallel()

	stateCfg, err := New(nil)
	require.NoError(t, err)
	for range 200 {
		hold, ok := stateCfg.TryAcquireInstanceConnection(connectionRequest(1, ConnectionPriorityBackground))
		require.True(t, ok)
		ctx, cancel := context.WithCancel(context.Background())
		waiter := acquireAsync(ctx, stateCfg, connectionRequest(1, ConnectionPriorityBackground), "background")
		waitForWaiters(t, stateCfg.instanceConnections, 1)
		cancel()
		hold()

		grant := receiveGrant(t, waiter)
		if grant.err == nil {
			// Granted just as the context was cancelled; give it back like a caller
			// that observed the cancellation would.
			grant.release()
		} else {
			require.ErrorIs(t, grant.err, ErrInstanceConnectionLimit)
		}

		release, ok := stateCfg.TryAcquireInstanceConnection(connectionRequest(1, ConnectionPriorityBackground))
		require.True(t, ok, "the cancelled waiter leaked a slot")
		release()
	}
}
