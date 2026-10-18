package llm

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Shutdown drains what is already queued: the writer is a bounded queue, and the
// queued entries are the ones a crash would lose.
func TestDebugLogWriterDrainsQueuedEntriesBeforeStopping(t *testing.T) {
	t.Parallel()

	writer := newDebugLogWriter()
	var written atomic.Int32
	writer.write = func(debugLogJob) { written.Add(1) }
	writer.start()

	for range 3 {
		writer.enqueue(debugLogJob{provider: "p", model: "m"})
	}
	require.NoError(t, writer.shutdown(context.Background()))
	require.Equal(t, int32(3), written.Load())
}

// A write that arrives after shutdown is dropped instead of panicking on a closed
// channel; the entry is a debug log, not a durable record.
func TestDebugLogWriterDropsAfterShutdown(t *testing.T) {
	t.Parallel()

	writer := newDebugLogWriter()
	var written atomic.Int32
	writer.write = func(debugLogJob) { written.Add(1) }
	writer.start()
	require.NoError(t, writer.shutdown(context.Background()))

	writer.enqueue(debugLogJob{provider: "p", model: "m"})
	require.Zero(t, written.Load())
}

// Shutdown is bounded: a writer that cannot drain must not block process exit.
func TestDebugLogWriterShutdownHonorsTheContext(t *testing.T) {
	t.Parallel()

	writer := newDebugLogWriter()
	release := make(chan struct{})
	writer.write = func(debugLogJob) { <-release }
	writer.start()
	writer.enqueue(debugLogJob{provider: "p", model: "m"})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, writer.shutdown(ctx), context.DeadlineExceeded)
	close(release)
}
