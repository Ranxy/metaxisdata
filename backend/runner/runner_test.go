package runner

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testRunner struct {
	ran bool
}

func (*testRunner) Name() string { return "test" }

func (r *testRunner) Run(context.Context) { r.ran = true }

type panickingRunner struct{}

func (*panickingRunner) Name() string { return "panicking" }

func (*panickingRunner) Run(context.Context) { panic("boom") }

// Go is the server's single entry point for a background loop: it must call Run
// and release the wait group count.
func TestGoRunsTheRunnerAndReleasesTheWaitGroup(t *testing.T) {
	t.Parallel()

	var wg sync.WaitGroup
	r := &testRunner{}
	Go(context.Background(), &wg, r, nil)
	wg.Wait()
	require.True(t, r.ran)
}

// A bug in one background loop must be logged, not propagated into the process:
// the other runners and the web server keep running.
func TestGoRecoversAPanickingRunner(t *testing.T) {
	t.Parallel()

	var wg sync.WaitGroup
	require.NotPanics(t, func() {
		Go(context.Background(), &wg, &panickingRunner{}, nil)
		wg.Wait()
	})
}

// captureHandler records the log lines the lifecycle emits. A test replaces the
// process logger with it, so such a test must not be parallel.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (*captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, record slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, record.Clone())
	return nil
}

func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }

func (h *captureHandler) WithGroup(_ string) slog.Handler { return h }

// find returns the recorded line with msg and the runner attribute, failing the
// test when the lifecycle line is missing.
func (h *captureHandler) find(t *testing.T, msg, runnerName string) slog.Record {
	t.Helper()

	h.mu.Lock()
	defer h.mu.Unlock()
	for _, record := range h.records {
		if record.Message != msg {
			continue
		}
		for _, attr := range recordAttrs(record) {
			if attr.Key == "runner" && attr.Value.String() == runnerName {
				return record
			}
		}
	}
	t.Fatalf("no %q log line for runner %q", msg, runnerName)
	return slog.Record{}
}

func recordAttrs(record slog.Record) []slog.Attr {
	attrs := make([]slog.Attr, 0, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})
	return attrs
}

func attrValue(t *testing.T, record slog.Record, key string) slog.Value {
	t.Helper()

	for _, attr := range recordAttrs(record) {
		if attr.Key == key {
			return attr.Value
		}
	}
	t.Fatalf("log line %q has no %q attribute", record.Message, key)
	return slog.Value{}
}

// Go owns the runner's lifecycle: one line when the loop starts and one when it
// stops, both naming the runner and the second carrying how long it ran.
func TestGoLogsRunnerLifecycle(t *testing.T) {
	// Not parallel: it replaces the process logger.
	previous := slog.Default()
	handler := &captureHandler{}
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(previous) })

	var wg sync.WaitGroup
	Go(context.Background(), &wg, &testRunner{}, nil)
	wg.Wait()

	started := handler.find(t, "background runner started", "test")
	require.Equal(t, "test", attrValue(t, started, "runner").String())

	stopped := handler.find(t, "background runner stopped", "test")
	require.Equal(t, "test", attrValue(t, stopped, "runner").String())
	require.GreaterOrEqual(t, attrValue(t, stopped, "duration").Duration(), time.Duration(0))
}

// A panicking runner keeps the panic boundary and still gets its stop line: the
// panic is logged with its stack and the process keeps running.
func TestGoLogsAPanickingRunnerStopping(t *testing.T) {
	// Not parallel: it replaces the process logger.
	previous := slog.Default()
	handler := &captureHandler{}
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(previous) })

	var wg sync.WaitGroup
	require.NotPanics(t, func() {
		Go(context.Background(), &wg, &panickingRunner{}, nil)
		wg.Wait()
	})

	handler.find(t, "background runner started", "panicking")
	handler.find(t, "background runner stopped", "panicking")
	handler.find(t, "background runner panicked", "panicking")
}

// Jitter keeps a periodic runner off the exact interval so restarted runners do
// not stay aligned, and never returns less than the requested interval.
func TestJitterStaysWithinTenPercent(t *testing.T) {
	t.Parallel()

	interval := time.Hour
	for range 1000 {
		jittered := Jitter(interval)
		require.GreaterOrEqual(t, jittered, interval)
		require.Less(t, jittered, interval+interval/10)
	}
	// An interval too small to have a 10% spread is returned unchanged.
	require.Equal(t, time.Nanosecond, Jitter(time.Nanosecond))
}
