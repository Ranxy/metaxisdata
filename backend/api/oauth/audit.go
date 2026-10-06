package oauth

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	clog "github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/audit"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// auditRecorder is what an endpoint tells the audit wrapper about a request. The
// wrapper runs after the handler — including when the handler failed early — so
// the two communicate through the request context rather than a return value.
type auditRecorder struct {
	actor  string
	detail map[string]any
}

type auditRecorderKey struct{}

func auditRecorderOf(r *http.Request) *auditRecorder {
	recorder, ok := r.Context().Value(auditRecorderKey{}).(*auditRecorder)
	if !ok {
		return nil
	}
	return recorder
}

// recordAuditActor names the user a protocol event belongs to.
func recordAuditActor(r *http.Request, actor string) {
	if recorder := auditRecorderOf(r); recorder != nil {
		recorder.actor = actor
	}
}

// recordAuditDetail attaches what the endpoint learned about the request. It
// holds no credential: the authorization code and the access token are never put
// here, and the ledger redacts sensitive field names on top of that.
func recordAuditDetail(r *http.Request, detail map[string]any) {
	if recorder := auditRecorderOf(r); recorder != nil {
		recorder.detail = detail
	}
}

// statusWriter remembers the status code so the audit row can be written once,
// after the handler is done.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(payload)
}

// Audited wraps a protocol endpoint so that every request leaves a row in the
// ledger. These routes are plain HTTP and sit outside the ConnectRPC interceptor
// chain, which is where the rest of the API gets its audit trail; a client
// registering itself, a token being issued and a failure on either are events the
// ledger exists for. The authorization decision itself is recorded by the consent
// RPC's own interceptor.
//
// A ledger that cannot be written is logged, never surfaced: an audit failure
// must not fail a request the client is entitled to make.
func (s *Server) Audited(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &auditRecorder{}
		writer := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(writer, r.WithContext(context.WithValue(r.Context(), auditRecorderKey{}, recorder)))
		// A 404 on these routes means the surface is switched off: nothing was
		// registered, issued or decided, and a ledger that is never pruned must not
		// grow a row per probe of a deployment that has the feature disabled.
		if writer.status != http.StatusNotFound {
			s.writeAudit(r, writer.status, recorder, started)
		}
	})
}

func (s *Server) writeAudit(r *http.Request, status int, recorder *auditRecorder, started time.Time) {
	if s.config.Stores == nil {
		return
	}
	failure := audit.ErrorForHTTPStatus(status)

	auditCtx, cancel := audit.Context(r.Context())
	defer cancel()
	workspaceID, err := s.config.Stores.GetWorkspaceID(auditCtx)
	if err != nil {
		slog.Error("failed to resolve the workspace for an OAuth audit log", clog.WithError(err))
		return
	}
	entry := &storepb.AuditLog{
		Parent:          common.FormatWorkspace(workspaceID),
		Method:          r.URL.Path,
		User:            audit.BoundAuditString(recorder.actor),
		Severity:        audit.MapSeverity(failure),
		Status:          audit.BuildAuditStatus(failure),
		LatencyMs:       time.Since(started).Milliseconds(),
		RequestMetadata: audit.BuildRequestMetadata(r.Header, r.RemoteAddr, s.config.TrustedProxies),
	}
	if len(recorder.detail) > 0 {
		if payload, err := structpb.NewStruct(recorder.detail); err == nil {
			entry.Request = audit.BoundAuditStruct(audit.SanitizeAuditStruct(payload))
		}
	}
	if _, err := s.config.Stores.CreateAuditLog(auditCtx, entry); err != nil {
		slog.Error("failed to persist an OAuth audit log", clog.WithError(err))
	}
}
