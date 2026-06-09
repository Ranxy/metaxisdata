package llm

import (
	"context"

	"github.com/Ranxy/metaxisdata/backend/store"
)

// NewDBDebugLogger returns a DebugLogger that persists request/response to
// the llm_debug_log table. Only call this when runtime debug is enabled.
func NewDBDebugLogger(st *store.Store, provider, model string) DebugLogger {
	return func(reqBody, respBody string) {
		// Fire-and-forget — don't block the LLM stream on debug writes.
		go func() {
			if err := st.InsertLLMDebugLog(context.Background(), provider, model, reqBody, respBody); err != nil {
				_ = err
			}
		}()
	}
}
