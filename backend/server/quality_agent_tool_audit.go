package server

import (
	"context"
	"encoding/json"
	"log/slog"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
	"github.com/Ranxy/metaxisdata/backend/component/audit"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// agentToolAuditSink records the built-in agent's write/proposal tool calls in
// the same permanent ledger the RPC path writes. An in-process tool call does not
// pass the Connect audit interceptor, so the pipeline reports it here instead.
type agentToolAuditSink struct {
	stores *store.Store
}

// RecordToolCall writes one best-effort audit row.
func (s agentToolAuditSink) RecordToolCall(ctx context.Context, entry tool.AuditEntry) {
	if s.stores == nil || entry.Principal == nil {
		return
	}
	auditCtx, cancel := audit.Context(ctx)
	defer cancel()

	status := &storepb.AuditLogStatus{Message: "ok"}
	severity := storepb.AuditLogSeverity_INFO
	if entry.Failure != nil {
		status.Message = audit.BoundAuditString(entry.Failure.Message)
		severity = storepb.AuditLogSeverity_ERROR
		switch entry.Failure.Code {
		case tool.CodeActionDenied, tool.CodePermissionDenied, tool.CodeUnauthenticated:
			status.Code = int32(connect.CodePermissionDenied)
			severity = storepb.AuditLogSeverity_WARNING
		default:
			status.Code = int32(connect.CodeInternal)
		}
	}
	row := &storepb.AuditLog{
		Parent:    common.FormatWorkspace(entry.Workspace),
		Method:    "agent/tools/call:" + entry.Tool,
		User:      common.FormatUserUID(entry.Principal.ID),
		Severity:  severity,
		Status:    status,
		LatencyMs: entry.Duration.Milliseconds(),
	}
	if payload := auditArgumentsOf(entry.Arguments); payload != nil {
		row.Request = payload
	}
	if _, err := s.stores.CreateAuditLog(auditCtx, row); err != nil {
		slog.Error("failed to persist an agent tool audit log", "error", err, "tool", entry.Tool)
	}
}

// auditArgumentsOf renders a tool call's arguments as a bounded, sanitized audit
// payload. An argument set that is not a JSON object is recorded by marker only.
func auditArgumentsOf(raw json.RawMessage) *structpb.Struct {
	if len(raw) == 0 {
		return nil
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	payload, err := structpb.NewStruct(value)
	if err != nil {
		return nil
	}
	return audit.BoundAuditStruct(audit.SanitizeAuditStruct(payload))
}
