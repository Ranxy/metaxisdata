package lineagevalidation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// probeRevalidator counts passes, can hold one open so a test can trigger a pass
// while another is running, and reports when a pass starts and finishes.
type probeRevalidator struct {
	mu       sync.Mutex
	calls    int
	block    bool
	err      error
	started  chan struct{}
	finished chan struct{}
	release  chan struct{}
}

func newProbeRevalidator() *probeRevalidator {
	return &probeRevalidator{
		started:  make(chan struct{}, 8),
		finished: make(chan struct{}, 8),
		release:  make(chan struct{}),
	}
}

func (p *probeRevalidator) RevalidateContradictedColumnClaims(ctx context.Context, _ int) (int, int, error) {
	p.mu.Lock()
	p.calls++
	block := p.block
	err := p.err
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
			return 0, 0, ctx.Err()
		}
	}
	if err != nil {
		return 0, 0, err
	}
	return 1, 0, nil
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
