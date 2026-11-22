package audit

import (
	"context"
	"sync"

	"google.golang.org/protobuf/types/known/structpb"
)

// detailContextKey carries the per-request Detail a handler may enrich.
type detailContextKey struct{}

// Detail carries extra audit-only fields a handler learns while serving a
// request whose request and response messages cannot hold them. The motivating
// case is a hard delete: the audit row records the call by name, and the
// deleted row itself is gone, so a handler records its snapshot here before
// deleting it. The audit interceptor merges the fields into the stored request
// payload under "auditDetail".
//
// The recorder is mutex-guarded because the audit interceptor and the handler
// are different goroutines in the streaming path and a future parallel handler
// may record while the row is being written.
type Detail struct {
	mu     sync.Mutex
	fields map[string]any
}

// NewDetailRecorder returns an empty recorder.
func NewDetailRecorder() *Detail {
	return &Detail{fields: map[string]any{}}
}

// WithDetailRecorder installs a recorder the handler can write through the
// request context. The audit interceptor owns the recorder and reads it after
// the handler returns.
func WithDetailRecorder(ctx context.Context) context.Context {
	return context.WithValue(ctx, detailContextKey{}, NewDetailRecorder())
}

// RecordDetail adds one audit-only field to the request's detail payload. It is
// a no-op when the request is not being audited or the caller did not install a
// recorder, so a handler never has to branch on audit configuration.
func RecordDetail(ctx context.Context, key string, value any) {
	detail, ok := ctx.Value(detailContextKey{}).(*Detail)
	if !ok || detail == nil || key == "" {
		return
	}
	detail.mu.Lock()
	defer detail.mu.Unlock()
	if detail.fields == nil {
		detail.fields = map[string]any{}
	}
	detail.fields[key] = value
}

// AttachDetail merges the context's recorder into one audit payload. A request
// with no recorded detail, or a context without a recorder, comes back unchanged.
func AttachDetail(ctx context.Context, payload *structpb.Struct) *structpb.Struct {
	detail, ok := ctx.Value(detailContextKey{}).(*Detail)
	if !ok || detail == nil {
		return payload
	}
	detail.mu.Lock()
	defer detail.mu.Unlock()
	if len(detail.fields) == 0 {
		return payload
	}
	extra, err := structpb.NewStruct(map[string]any{"auditDetail": detail.fields})
	if err != nil {
		// The fields are JSON-shaped by contract; a value the encoder refuses
		// must not cost the audit row, so it is dropped like an unencodable body.
		return payload
	}
	if payload == nil {
		return extra
	}
	if payload.Fields == nil {
		payload.Fields = map[string]*structpb.Value{}
	}
	for name, value := range extra.GetFields() {
		payload.Fields[name] = value
	}
	return payload
}
