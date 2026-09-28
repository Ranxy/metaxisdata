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
		err := next(ctx, conn)
		auditCtx, cancel := audit.Context(ctx)
		defer cancel()
		workspaceID, workspaceErr := in.store.GetWorkspaceID(auditCtx)
		if workspaceErr == nil {
			auditLog := &storepb.AuditLog{
				Parent:          common.FormatWorkspace(workspaceID),
				Method:          conn.Spec().Procedure,
				User:            audit.ResolveActor(ctx, nil, nil),
				Severity:        audit.MapSeverity(err),
				Status:          audit.BuildAuditStatus(err),
				LatencyMs:       time.Since(startTime).Milliseconds(),
				RequestMetadata: audit.BuildRequestMetadata(conn.RequestHeader(), "", in.trustedProxies),
			}
			if _, createErr := in.store.CreateAuditLog(auditCtx, auditLog); createErr != nil {
				slog.Error("failed to persist stream audit log", "method", conn.Spec().Procedure, clog.WithError(createErr))
			}
		}
		return err
	}
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
	requestStruct, requestMap, marshalErr := audit.MarshalAuditMessage(requestMessage)
	if marshalErr != nil {
		return marshalErr
	}

	var responseMessage proto.Message
	if !isNilConnectValue(resp) {
		responseMessage, ok = resp.Any().(proto.Message)
		if !ok {
			responseMessage = nil
		}
	}
	responseStruct, responseMap, marshalErr := audit.MarshalAuditMessage(responseMessage)
	if marshalErr != nil {
		return marshalErr
	}

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
