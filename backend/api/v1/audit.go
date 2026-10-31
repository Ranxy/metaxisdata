package v1

import (
	"context"
	"log/slog"
	"reflect"
	"time"

	"connectrpc.com/connect"
	pkgerrors "github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/Ranxy/metaxisdata/backend/common"
	clog "github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/audit"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type AuditInterceptor struct {
	store *store.Store
	// trustedProxies are the peer IPs/CIDRs whose forwarding headers may be
	// believed. Empty means the connection address is the client address.
	trustedProxies []string
}

func NewAuditInterceptor(store *store.Store, trustedProxies []string) *AuditInterceptor {
	return &AuditInterceptor{store: store, trustedProxies: trustedProxies}
}

func (in *AuditInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		authCtx, ok := common.GetAuthContextFromContext(ctx)
		if !ok || !authCtx.Audit {
			return next(ctx, req)
		}

		requestMessage, ok := req.Any().(proto.Message)
		if !ok {
			return next(ctx, req)
		}
		if shouldSkipAudit(requestMessage) {
			return next(ctx, req)
		}

		startTime := time.Now()
		resp, err := next(ctx, req)
		if auditErr := in.createAuditLog(ctx, req, resp, err, startTime); auditErr != nil {
			slog.Error("failed to persist audit log", "method", req.Spec().Procedure, clog.WithError(auditErr))
		}
		return resp, err
	}
}

func (*AuditInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		return next(ctx, spec)
	}
}

func (in *AuditInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		authCtx, ok := common.GetAuthContextFromContext(ctx)
		if !ok || !authCtx.Audit {
			return next(ctx, conn)
		}

		startTime := time.Now()
		// The request arrives through the wrapped connection, and the handler
		// reports its outcome through the context, so the row written below can
		// carry what a unary row carries. The parent stays the resolved
		// workspace: a request's own parent may be "workspaces/-", which the
		// audit search would then fail to scope.
		recorder := audit.NewStreamRecorder()
		streamCtx := audit.WithStreamRecorder(ctx, recorder)
		err := next(streamCtx, &recordingStreamingHandlerConn{StreamingHandlerConn: conn, recorder: recorder})
		auditCtx, cancel := audit.Context(ctx)
		defer cancel()
		workspaceID, workspaceErr := in.store.GetWorkspaceID(auditCtx)
		if workspaceErr == nil {
			requestStruct, requestMap := audit.MarshalAuditMessage(recorder.Request())
			responseStruct, responseMap := audit.MarshalAuditResult(recorder.Result())
			auditLog := &storepb.AuditLog{
				Parent:          common.FormatWorkspace(workspaceID),
				Method:          conn.Spec().Procedure,
				Resource:        audit.ResolveResource(requestMap, responseMap),
				User:            audit.ResolveActor(ctx, requestMap, responseMap),
				Severity:        audit.MapSeverity(err),
				Request:         requestStruct,
				Response:        responseStruct,
				Status:          audit.BuildAuditStatus(err),
				LatencyMs:       time.Since(startTime).Milliseconds(),
				RequestMetadata: streamingRequestMetadata(conn, in.trustedProxies),
			}
			if auditLog.Resource == "" {
				auditLog.Resource = auditLog.User
			}
			if _, createErr := in.store.CreateAuditLog(auditCtx, auditLog); createErr != nil {
				slog.Error("failed to persist stream audit log", "method", conn.Spec().Procedure, clog.WithError(createErr))
			}
		}
		return err
	}
}

// recordingStreamingHandlerConn captures the request message an audited stream
// receives. connect receives a server-streaming request through the handler
// connection before the service method runs, so wrapping Receive is the one
// place that sees what was asked for without changing every handler.
type recordingStreamingHandlerConn struct {
	connect.StreamingHandlerConn
	recorder *audit.StreamRecorder
}

func (c *recordingStreamingHandlerConn) Receive(message any) error {
	if err := c.StreamingHandlerConn.Receive(message); err != nil {
		return err
	}
	c.recorder.RecordRequest(message)
	return nil
}

// streamingRequestMetadata is the request metadata of a streaming call. It is a
// function of its own so a test can pin the peer address: this branch used to
// pass "" and the column address then stayed empty on every audited stream, which
// would have given a future streaming method rows with no origin at all.
func streamingRequestMetadata(conn connect.StreamingHandlerConn, trustedProxies []string) *storepb.AuditRequestMetadata {
	return audit.BuildRequestMetadata(conn.RequestHeader(), conn.Peer().Addr, trustedProxies)
}

func (in *AuditInterceptor) createAuditLog(ctx context.Context, req connect.AnyRequest, resp connect.AnyResponse, err error, startTime time.Time) error {
	auditCtx, cancel := audit.Context(ctx)
	defer cancel()

	workspaceID, workspaceErr := in.store.GetWorkspaceID(auditCtx)
	if workspaceErr != nil {
		return pkgerrors.Wrap(workspaceErr, "failed to get workspace id for audit log")
	}

	requestMessage, ok := req.Any().(proto.Message)
	if !ok {
		return pkgerrors.New("failed to cast request to proto.Message")
	}
	requestStruct, requestMap := audit.MarshalAuditMessage(requestMessage)

	var responseMessage proto.Message
	if !isNilConnectValue(resp) {
		responseMessage, ok = resp.Any().(proto.Message)
		if !ok {
			responseMessage = nil
		}
	}
	responseStruct, responseMap := audit.MarshalAuditMessage(responseMessage)

	auditLog := &storepb.AuditLog{
		Parent:          audit.ResolveParent(common.FormatWorkspace(workspaceID), requestMap, responseMap),
		Method:          req.Spec().Procedure,
		Resource:        audit.ResolveResource(requestMap, responseMap),
		User:            audit.ResolveActor(ctx, requestMap, responseMap),
		Severity:        audit.MapSeverity(err),
		Request:         requestStruct,
		Response:        responseStruct,
		Status:          audit.BuildAuditStatus(err),
		LatencyMs:       time.Since(startTime).Milliseconds(),
		RequestMetadata: audit.BuildRequestMetadata(req.Header(), req.Peer().Addr, in.trustedProxies),
	}

	if auditLog.Resource == "" {
		auditLog.Resource = auditLog.User
	}

	_, createErr := in.store.CreateAuditLog(auditCtx, auditLog)
	return createErr
}

func shouldSkipAudit(message proto.Message) bool {
	if message == nil {
		return false
	}
	field := message.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name("validate_only"))
	if field == nil {
		return false
	}
	return message.ProtoReflect().Get(field).Bool()
}

func isNilConnectValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
