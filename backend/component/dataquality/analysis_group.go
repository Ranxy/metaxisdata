package dataquality

import (
	"context"
	"errors"
	"sync"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// analysisGroup coalesces identical in-flight LLM analyses inside one process.
//
// Two generation passes can ask the same question at the same time — a double
// click, two reviewers, a workbench submit racing an editor shortcut. The queue
// already converges on one proposal per identity, but without coalescing both
// passes still pay for the same model call. The group runs the call once per
// identical prompt and shares its text, usage and diagnostic; every caller then
// reconciles the same answer through the normal identity-keyed upsert, so the
// queue outcome is unchanged while the spend is not duplicated.
//
// The deployment is single-replica (see .agents/docs/data-quality.md), so an
// in-process group is sufficient. A leader that loses its context does not poison
// a live follower: the follower retries and becomes the leader.
type analysisGroup struct {
	mu    sync.Mutex
	calls map[string]*analysisCall
}

// errAnalysisPanicked is what followers of a panicked shared call receive. The
// leader's panic is re-raised to its caller, so the runner's per-job recover still
// sees it.
var errAnalysisPanicked = errors.New("the shared LLM analysis panicked")

// analysisCall is one in-flight shared analysis.
type analysisCall struct {
	done       chan struct{}
	text       string
	usage      *llm.Usage
	diagnostic *storepb.QualityLLMDiagnostic
	err        error
	// ctxErr records the leader's context state, so a follower can tell "the answer
	// is an error" from "the leader went away and this caller is still alive".
	ctxErr error
	// waiters counts the callers that joined this in-flight call. It is guarded by
	// the group mutex and exists for tests to observe coalescing deterministically.
	waiters int
}

func newAnalysisGroup() *analysisGroup {
	return &analysisGroup{calls: make(map[string]*analysisCall)}
}

// Do runs one analysis per key while it is in flight and shares the result. fn is
// called with the leader's context. A follower whose own context ends returns that
// context's error. A follower still alive when the leader's context ended retries,
// which makes it the new leader.
func (g *analysisGroup) Do(ctx context.Context, key string, fn func(context.Context) (string, *llm.Usage, *storepb.QualityLLMDiagnostic, error)) (string, *llm.Usage, *storepb.QualityLLMDiagnostic, error) {
	for {
		if err := ctx.Err(); err != nil {
			return "", nil, nil, err
		}
		g.mu.Lock()
		if call, ok := g.calls[key]; ok {
			call.waiters++
			g.mu.Unlock()
			select {
			case <-call.done:
				if call.err != nil && call.ctxErr != nil && ctx.Err() == nil {
					// The leader was canceled or timed out, this caller was not: ask again.
					continue
				}
				return call.text, call.usage, call.diagnostic, call.err
			case <-ctx.Done():
				return "", nil, nil, ctx.Err()
			}
		}
		call := &analysisCall{done: make(chan struct{})}
		g.calls[key] = call
		g.mu.Unlock()

		// A panic in the shared call must not leave the entry behind: every later
		// caller of the same prompt would block on a channel nobody closes. Publish a
		// failure to the waiting callers, drop the entry, and re-panic so the
		// surrounding runner/job isolation still handles the panic.
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					call.err = errAnalysisPanicked
					call.ctxErr = nil
					close(call.done)
					g.mu.Lock()
					delete(g.calls, key)
					g.mu.Unlock()
					panic(recovered)
				}
			}()
			call.text, call.usage, call.diagnostic, call.err = fn(ctx)
			call.ctxErr = ctx.Err()
		}()
		if call.err == nil && call.ctxErr != nil {
			call.err = call.ctxErr
		}
		// Publish the result before leaving the map, so a caller that joined the
		// completed call still reads it; followers already hold the pointer.
		close(call.done)
		g.mu.Lock()
		delete(g.calls, key)
		g.mu.Unlock()
		return call.text, call.usage, call.diagnostic, call.err
	}
}
