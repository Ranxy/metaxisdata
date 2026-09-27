package lineagevalidation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// probeRevalidator counts passes and can hold one open, so a test can trigger a
// pass while another is running.
type probeRevalidator struct {
	mu      sync.Mutex
	calls   int
	block   bool
	err     error
	started chan struct{}
	release chan struct{}
}

func newProbeRevalidator() *probeRevalidator {
	return &probeRevalidator{
		started: make(chan struct{}, 8),
		release: make(chan struct{}),
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

// start runs the runner with a settle delay short enough for a test.
func start(t *testing.T, revalidator Revalidator) (*Runner, context.CancelFunc, *sync.WaitGroup) {
	t.Helper()

	runner := NewRunner(revalidator)
	runner.settle = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go runner.Run(ctx, wg)
	return runner, cancel, wg
}

func waitForPass(t *testing.T, probe *probeRevalidator) {
	t.Helper()
	select {
	case <-probe.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a revalidation pass")
	}
}

// A sync round queues every database of every instance, so a burst of triggers
// must cost one pass: the pending edge set is the same for all of them.
func TestTriggerCoalescesOneSyncRound(t *testing.T) {
	t.Parallel()

	probe := newProbeRevalidator()
	runner, cancel, wg := start(t, probe)
	defer func() {
		cancel()
		wg.Wait()
	}()

	for range 5 {
		runner.Trigger()
	}
	waitForPass(t, probe)

	// Well past the settle delay: a coalesced burst must not produce a second pass.
	time.Sleep(10 * runner.settle)
	require.Equal(t, 1, probe.count())
}

// A sync that commits while a pass is running changes what that pass was reading,
// so it must get a pass of its own.
func TestTriggerDuringAPassRunsOneMore(t *testing.T) {
	t.Parallel()

	probe := newProbeRevalidator()
	probe.setBlocked(true)
	runner, cancel, wg := start(t, probe)
	defer func() {
		cancel()
		wg.Wait()
	}()

	runner.Trigger()
	waitForPass(t, probe)

	runner.Trigger()
	probe.setBlocked(false)
	close(probe.release)

	waitForPass(t, probe)
	require.Equal(t, 2, probe.count())
}

// The syncer calls Trigger unconditionally when it was wired with a runner; a
// server without one must not panic or spin.
func TestTriggerWithoutARevalidatorIsIgnored(t *testing.T) {
	t.Parallel()

	runner := NewRunner(nil)
	require.NotPanics(t, runner.Trigger)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.Run(ctx, wg)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return when the context was cancelled")
	}
	wg.Wait()
}

// A failed pass must not wedge the runner: the next sync retries the same edges.
func TestFailedPassKeepsTheRunnerRunning(t *testing.T) {
	t.Parallel()

	probe := newProbeRevalidator()
	probe.err = errors.New("store is unavailable")
	runner, cancel, wg := start(t, probe)
	defer func() {
		cancel()
		wg.Wait()
	}()

	runner.Trigger()
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
	_, cancel, wg := start(t, probe)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return when the context was cancelled")
	}
}
