package lineagevalidation

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// probeRevalidator counts passes, can hold one open so a test can trigger a pass
// while another is running, reports when a pass starts and finishes, can report a
// full batch so a test can model a backlog, and can run a hook after each batch so
// a test can end the pass between batches.
type probeRevalidator struct {
	mu       sync.Mutex
	calls    int
	block    bool
	err      error
	hasMore  int
	started  chan struct{}
	finished chan struct{}
	release  chan struct{}

	revalidated  int
	stillUnknown int
	onBatch      func()
}

func newProbeRevalidator() *probeRevalidator {
	return &probeRevalidator{
		revalidated: 1,
		started:     make(chan struct{}, 8),
		finished:    make(chan struct{}, 8),
		release:     make(chan struct{}),
	}
}

func (p *probeRevalidator) RevalidateContradictedColumnClaims(ctx context.Context, _ int) (int, int, bool, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	block := p.block
	err := p.err
	hasMore := call <= p.hasMore
	revalidated, stillUnknown := p.revalidated, p.stillUnknown
	onBatch := p.onBatch
	p.mu.Unlock()

	select {
	case p.started <- struct{}{}:
	default:
	}
	defer func() {
		select {
		case p.finished <- struct{}{}:
		default:
		}
	}()

	if block {
		select {
		case <-p.release:
		case <-ctx.Done():
			return 0, 0, false, ctx.Err()
		}
	}
	if onBatch != nil {
		onBatch()
	}
	if err != nil {
		return 0, 0, false, err
	}
	return revalidated, stillUnknown, hasMore, nil
}

func (p *probeRevalidator) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *probeRevalidator) setBlocked(blocked bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.block = blocked
}

// start runs the runner with a settle delay short enough for a test. The returned
// stop cancels the runner and waits for Run to return.
func start(t *testing.T, revalidator Revalidator) (*Runner, func()) {
	t.Helper()

	runner := NewRunner(revalidator)
	runner.settle = 20 * time.Millisecond
	return runner, startRunner(t, runner)
}

// startRunner runs an already configured runner; the caller may have replaced its
// settle timer to control the settle window.
func startRunner(t *testing.T, runner *Runner) func() {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.Run(ctx)
	}()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Run did not return when the context was cancelled")
		}
	}
}

func waitForPass(t *testing.T, probe *probeRevalidator) {
	t.Helper()
	select {
	case <-probe.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a revalidation pass")
	}
}

func waitForPassToFinish(t *testing.T, probe *probeRevalidator) {
	t.Helper()
	select {
	case <-probe.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a revalidation pass to finish")
	}
}

// A restart must not leave the edges ingested while the process was down waiting
// for a sync that changes something: Run revalidates once on its own.
func TestRunStartsWithAPass(t *testing.T) {
	t.Parallel()

	probe := newProbeRevalidator()
	_, stop := start(t, probe)
	defer stop()

	waitForPass(t, probe)
	require.Equal(t, 1, probe.count())
}

// A sync round queues every database of every instance, so a burst of triggers
// must cost one pass: the pending edge set is the same for all of them. The burst
// arrives while the startup pass is held open, so every signal is delivered
// before the runner can consume any of them.
func TestTriggerCoalescesOneSyncRound(t *testing.T) {
	t.Parallel()

	probe := newProbeRevalidator()
	probe.setBlocked(true)
	runner, stop := start(t, probe)
	defer stop()

	// The startup pass is the one held open.
	waitForPass(t, probe)

	for range 5 {
		runner.Trigger()
	}
	probe.setBlocked(false)
	close(probe.release)

	waitForPass(t, probe)
	// Well past the settle delay: the coalesced burst must not produce a third pass.
	time.Sleep(10 * runner.settle)
	require.Equal(t, 2, probe.count())
}

// A sync that commits while a pass is running changes what that pass was reading,
// so it must get a pass of its own.
func TestTriggerDuringAPassRunsOneMore(t *testing.T) {
	t.Parallel()

	probe := newProbeRevalidator()
	probe.setBlocked(true)
	runner, stop := start(t, probe)
	defer stop()

	// The startup pass is the one held open.
	waitForPass(t, probe)

	runner.Trigger()
	probe.setBlocked(false)
	close(probe.release)

	waitForPass(t, probe)
	require.Equal(t, 2, probe.count())
}

// A sync that commits during the settle window names the same pending edges as the
// pass about to run, and that pass reads metadata after the commit, so the signal
// must be folded in rather than cost a second settle and pass. The settle window is
// held open by the injected timer, so the second trigger is deterministically
// delivered inside it.
func TestTriggerDuringSettleIsFolded(t *testing.T) {
	t.Parallel()

	probe := newProbeRevalidator()
	settleEntered := make(chan struct{}, 1)
	settleGate := make(chan time.Time, 1)
	runner := NewRunner(probe)
	runner.settle = 10 * time.Millisecond
	runner.settleTimer = func(time.Duration) <-chan time.Time {
		settleEntered <- struct{}{}
		return settleGate
	}
	stop := startRunner(t, runner)
	defer stop()

	enterSettle := func() {
		t.Helper()
		select {
		case <-settleEntered:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for the settle window to open")
		}
	}
	endSettle := func() { settleGate <- time.Now() }

	// The startup pass runs after the first settle window; release it.
	enterSettle()
	endSettle()
	waitForPass(t, probe)
	waitForPassToFinish(t, probe)

	// A sync commits after the pass: it is consumed and the runner settles again.
	runner.Trigger()
	enterSettle()
	// A second sync commits during that settle and must be folded into the pass
	// about to run.
	runner.Trigger()
	endSettle()

	waitForPass(t, probe)
	waitForPassToFinish(t, probe)
	// The folded signal must not open another settle window: were it still queued,
	// the runner would consume it and wait out a second settle here.
	select {
	case <-settleEntered:
		t.Fatal("a trigger that arrived during settle must be folded into the pass that ran, not schedule another settle")
	case <-time.After(20 * runner.settle):
	}
	require.Equal(t, 2, probe.count())
}

// The syncer calls Trigger unconditionally when it was wired with a runner; a
// server without one must not panic or spin.
func TestTriggerWithoutARevalidatorIsIgnored(t *testing.T) {
	t.Parallel()

	runner := NewRunner(nil)
	require.NotPanics(t, runner.Trigger)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.Run(ctx)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return when the context was cancelled")
	}
}

// A failed pass must not wedge the runner: the next sync retries the same edges.
func TestFailedPassKeepsTheRunnerRunning(t *testing.T) {
	t.Parallel()

	probe := newProbeRevalidator()
	probe.err = errors.New("store is unavailable")
	runner, stop := start(t, probe)
	defer stop()

	// The startup pass fails first.
	waitForPass(t, probe)

	probe.mu.Lock()
	probe.err = nil
	probe.mu.Unlock()

	runner.Trigger()
	waitForPass(t, probe)
	require.Equal(t, 2, probe.count())
}

func TestRunReturnsOnContextCancel(t *testing.T) {
	t.Parallel()

	probe := newProbeRevalidator()
	_, stop := start(t, probe)
	stop()
}

// One pass drains a backlog larger than the implementation's batch, and the counts
// it reports are the sum over the batches rather than the last one alone.
func TestBacklogIsDrainedInOnePass(t *testing.T) {
	t.Parallel()

	probe := newProbeRevalidator()
	probe.revalidated = 2
	probe.stillUnknown = 3
	probe.hasMore = 1

	revalidated, stillUnknown := NewRunner(probe).runOnce(context.Background())

	require.Equal(t, 2, probe.count(), "a full batch earns a second call")
	require.Equal(t, 4, revalidated)
	require.Equal(t, 6, stillUnknown)
}

// The drain stops when the pass context ends; the batches it already ran stay
// counted and the next trigger continues where this pass stopped.
func TestBacklogDrainStopsWhenTheContextEnds(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	probe := newProbeRevalidator()
	probe.hasMore = 10
	probe.onBatch = cancel

	revalidated, stillUnknown := NewRunner(probe).runOnce(ctx)

	require.Equal(t, 1, probe.count(), "the drain must stop asking once its context ends")
	require.Equal(t, 1, revalidated, "the batch that ran is still counted")
	require.Equal(t, 0, stillUnknown)
}

// The pass deadline bounds the drain even when every batch reports more pending
// edges, so one pass can never run unbounded.
func TestBacklogDrainStopsAtThePassDeadline(t *testing.T) {
	t.Parallel()

	probe := newProbeRevalidator()
	probe.hasMore = math.MaxInt
	// One batch per 5ms keeps the loop from spinning while the deadline passes, and
	// keeps the deadline well inside the batch budget so this proves the deadline.
	probe.onBatch = func() { time.Sleep(5 * time.Millisecond) }
	runner := NewRunner(probe)
	runner.passTimeout = 20 * time.Millisecond

	started := time.Now()
	revalidated, _ := runner.runOnce(context.Background())

	require.Less(t, time.Since(started), 5*time.Second, "the pass must end at its deadline")
	require.Positive(t, probe.count(), "the drain ran at least one batch")
	require.Less(t, probe.count(), maxRevalidationBatches, "the deadline, not the batch budget, ended this pass")
	require.Equal(t, probe.count(), revalidated)
}

// The batch budget bounds a pass even when the backlog never shrinks: a still
// unknown relation stays eligible, so an unbudgeted drain would rewrite the same
// edges until the deadline.
func TestBacklogDrainIsBoundedPerPass(t *testing.T) {
	t.Parallel()

	probe := newProbeRevalidator()
	probe.hasMore = math.MaxInt
	probe.revalidated = 2
	runner := NewRunner(probe)
	runner.passTimeout = time.Minute

	revalidated, _ := runner.runOnce(context.Background())

	require.Equal(t, maxRevalidationBatches, probe.count())
	require.Equal(t, 2*maxRevalidationBatches, revalidated)
}
