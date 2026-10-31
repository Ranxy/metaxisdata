package audit

import (
	"context"
	"maps"
	"sync"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// StreamRecorder collects what an audited server-streaming call received and
// what its handler reported about the result. The audit interceptor installs one
// on the handler context and wraps the connection, then writes the collected
// values into the audit row once the stream finishes.
type StreamRecorder struct {
	mu      sync.Mutex
	request proto.Message
	result  map[string]any
}

// streamRecorderContextKey is the context key under which the interceptor
// installs its recorder. It is unexported so only this package can install one.
type streamRecorderContextKey struct{}

// NewStreamRecorder returns an empty recorder.
func NewStreamRecorder() *StreamRecorder {
	return &StreamRecorder{}
}

// WithStreamRecorder returns a context carrying recorder. The audit interceptor
// calls it before invoking a streaming handler, so the handler can attach a
// result summary with RecordStreamResult.
func WithStreamRecorder(ctx context.Context, recorder *StreamRecorder) context.Context {
	if recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, streamRecorderContextKey{}, recorder)
}

// RecordRequest keeps the first request message received on the stream. A value
// that is not a proto.Message, and every later message, is ignored: a
// server-streaming call has exactly one request, and a bidirectional one still
// has the message that starts it.
func (r *StreamRecorder) RecordRequest(message any) {
	protoMessage, ok := message.(proto.Message)
	if !ok || protoMessage == nil {
		return
	}
	cloned := proto.Clone(protoMessage)
	if cloned == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.request == nil {
		r.request = cloned
	}
}

// Request returns the recorded request message, or nil when the stream received
// none.
func (r *StreamRecorder) Request() proto.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.request
}

// RecordStreamResult attaches a result summary to the audited stream. The audit
// interceptor installs the recorder on the context; a handler called outside an
// audited stream (a unit test, a direct call) is a no-op. Repeated calls merge,
// with later keys winning.
func RecordStreamResult(ctx context.Context, result map[string]any) {
	if len(result) == 0 {
		return
	}
	recorder, ok := recorderFromContext(ctx)
	if !ok {
		return
	}
	recorder.recordResult(result)
}

// recorderFromContext returns the recorder the audit interceptor installed, if
// any.
func recorderFromContext(ctx context.Context) (*StreamRecorder, bool) {
	if ctx == nil {
		return nil, false
	}
	recorder, ok := ctx.Value(streamRecorderContextKey{}).(*StreamRecorder)
	if !ok || recorder == nil {
		return nil, false
	}
	return recorder, true
}

func (r *StreamRecorder) recordResult(result map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.result == nil {
		r.result = make(map[string]any, len(result))
	}
	maps.Copy(r.result, result)
}

// Result returns a copy of the recorded result summary, or nil when the handler
// reported none.
func (r *StreamRecorder) Result() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.result) == 0 {
		return nil
	}
	result := make(map[string]any, len(r.result))
	maps.Copy(result, r.result)
	return result
}

// MarshalAuditResult renders a result summary a streaming handler built itself
// for the ledger. It applies the same redaction, field bounds and whole-payload
// bound as a proto response; a nil result yields a nil payload. The raw map is
// returned alongside the payload so the caller can derive the row's resource and
// actor from it, exactly as MarshalAuditMessage allows.
func MarshalAuditResult(result map[string]any) (*structpb.Struct, map[string]any) {
	if len(result) == 0 {
		return nil, nil
	}
	raw := make(map[string]any, len(result))
	maps.Copy(raw, result)
	SanitizeAuditValue(raw)
	boundAuditValue(raw)
	structured, err := structpb.NewStruct(raw)
	if err != nil {
		return auditUnencodableMarker(), nil
	}
	return capAuditPayload(structured), raw
}
