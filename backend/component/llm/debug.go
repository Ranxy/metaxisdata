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

// debugLogQueue is a fixed-size queue drained by a fixed number of workers.
// The previous implementation started one unbounded goroutine per LLM call with
// no concurrency limit and discarded errors.
var debugLogQueue = make(chan debugLogJob, debugLogQueueSize)

var startDebugLogWorkers = sync.OnceFunc(func() {
	for i := 0; i < debugLogWorkers; i++ {
		go func() {
			for job := range debugLogQueue {
				writeDebugLog(job)
			}
		}()
	}
})

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
	startDebugLogWorkers()
	return func(reqBody, respBody string) {
		select {
		case debugLogQueue <- debugLogJob{store: st, provider: provider, model: model, reqBody: reqBody, respBody: respBody}:
		default:
			slog.Warn("dropping the LLM debug log entry: writer queue is full",
				"provider", provider, "model", model)
		}
	}
}
