package llm

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// debugLogQueueSize bounds how many debug writes may be queued or in flight.
	debugLogQueueSize = 64
	// debugLogWorkers bounds the goroutines writing the debug log.
	debugLogWorkers = 2
	// debugLogWriteTimeout bounds one insert so a stuck database cannot pin a
	// worker forever.
	debugLogWriteTimeout = 10 * time.Second
)

type debugLogJob struct {
	store    *store.Store
	provider string
	model    string
	reqBody  string
	respBody string
}

// debugLogWriter is a fixed-size queue drained by a fixed number of workers. It is
// a value rather than package-level channel state so the server can stop it before
// the store closes: the previous shape started goroutines on a Background context
// that outlived shutdown and could write to a closed connection pool.
type debugLogWriter struct {
	mu   sync.RWMutex
	jobs chan debugLogJob
	once sync.Once
	wg   sync.WaitGroup
	// closed is set under mu before jobs is closed, so a sender that holds the
	// read lock can never send on a closed channel.
	closed bool
	// write persists one job; production uses writeDebugLog, a test replaces it.
	write func(debugLogJob)
}

func newDebugLogWriter() *debugLogWriter {
	return &debugLogWriter{
		jobs:  make(chan debugLogJob, debugLogQueueSize),
		write: writeDebugLog,
	}
}

// start launches the writers once. A caller that never produced a debug log never
// pays for the goroutines.
func (w *debugLogWriter) start() {
	w.once.Do(func() {
		for range debugLogWorkers {
			w.wg.Go(func() {
				for job := range w.jobs {
					w.write(job)
				}
			})
		}
	})
}

// enqueue hands one job to the writers. A full queue drops the entry with a
// warning rather than spawning another goroutine, and an entry that arrives after
// shutdown is dropped: it is a debug log, not a durable record.
func (w *debugLogWriter) enqueue(job debugLogJob) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		slog.Warn("dropping the LLM debug log entry: the writer is shut down",
			"provider", job.provider, "model", job.model)
		return
	}
	select {
	case w.jobs <- job:
	default:
		slog.Warn("dropping the LLM debug log entry: writer queue is full",
			"provider", job.provider, "model", job.model)
	}
}

// shutdown stops accepting jobs, lets the writers drain the queue, and waits for
// them until ctx ends. A timed-out drain is logged and abandoned: the entry is a
// debug log, and blocking process exit for it is worse than losing it.
func (w *debugLogWriter) shutdown(ctx context.Context) error {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.jobs)
	}
	w.mu.Unlock()

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		slog.Warn("the LLM debug log writers did not drain before shutdown", "error", ctx.Err())
		return ctx.Err()
	}
}

// debugLoggers is the process's debug-log writer. It is lazy: nothing starts until
// the first debug logger is built.
var debugLoggers = newDebugLogWriter()

// ShutdownDebugLogWorkers stops the process's LLM debug-log writers and waits for
// the queued entries to be written, bounded by ctx. The server calls it before
// closing the store, so a queued debug write cannot outlive its connection pool.
func ShutdownDebugLogWorkers(ctx context.Context) error {
	return debugLoggers.shutdown(ctx)
}

func writeDebugLog(job debugLogJob) {
	ctx, cancel := context.WithTimeout(context.Background(), debugLogWriteTimeout)
	defer cancel()
	if err := job.store.InsertLLMDebugLog(ctx, job.provider, job.model, job.reqBody, job.respBody); err != nil {
		slog.Warn("failed to write the LLM debug log", "error", err)
	}
}

// NewDBDebugLogger returns a DebugLogger that persists request/response to
// the llm_debug_log table. Only call this when runtime debug is enabled. Writes
// are handed to a bounded worker pool; when the queue is full the entry is
// dropped with a warning rather than spawning another goroutine.
func NewDBDebugLogger(st *store.Store, provider, model string) DebugLogger {
	debugLoggers.start()
	return func(reqBody, respBody string) {
		debugLoggers.enqueue(debugLogJob{store: st, provider: provider, model: model, reqBody: reqBody, respBody: respBody})
	}
}
