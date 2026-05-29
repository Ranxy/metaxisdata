package v1

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"reflect"
	"strings"
	"time"

	"connectrpc.com/connect"
	pkgerrors "github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	clog "github.com/Ranxy/metaxisdata/backend/common/log"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const redactedValue = "[REDACTED]"

type AuditInterceptor struct {
	store *store.Store
}

func NewAuditInterceptor(store *store.Store) *AuditInterceptor {
	return &AuditInterceptor{store: store}
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
		workspaceID, workspaceErr := in.store.GetWorkspaceID(ctx)
		if workspaceErr == nil {
			auditLog := &storepb.AuditLog{
				Parent:          common.FormatWorkspace(workspaceID),
				Method:          conn.Spec().Procedure,
				User:            resolveActor(ctx, nil, nil),
				Severity:        mapSeverity(err),
				Status:          buildAuditStatus(err),
				LatencyMs:       time.Since(startTime).Milliseconds(),
				RequestMetadata: buildRequestMetadata(conn.RequestHeader(), ""),
			}
			if _, createErr := in.store.CreateAuditLog(ctx, auditLog); createErr != nil {
				slog.Error("failed to persist stream audit log", "method", conn.Spec().Procedure, clog.WithError(createErr))
			}
		}
		return err
	}
}

func (in *AuditInterceptor) createAuditLog(ctx context.Context, req connect.AnyRequest, resp connect.AnyResponse, err error, startTime time.Time) error {
	workspaceID, workspaceErr := in.store.GetWorkspaceID(ctx)
	if workspaceErr != nil {
		return pkgerrors.Wrap(workspaceErr, "failed to get workspace id for audit log")
	}

	requestMessage, ok := req.Any().(proto.Message)
	if !ok {
		return pkgerrors.New("failed to cast request to proto.Message")
	}
	requestStruct, requestMap, marshalErr := marshalAuditMessage(requestMessage)
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
	responseStruct, responseMap, marshalErr := marshalAuditMessage(responseMessage)
	if marshalErr != nil {
		return marshalErr
	}

	auditLog := &storepb.AuditLog{
		Parent:          resolveParent(common.FormatWorkspace(workspaceID), requestMap, responseMap),
		Method:          req.Spec().Procedure,
		Resource:        resolveResource(requestMap, responseMap),
		User:            resolveActor(ctx, requestMap, responseMap),
		Severity:        mapSeverity(err),
		Request:         requestStruct,
		Response:        responseStruct,
		Status:          buildAuditStatus(err),
		LatencyMs:       time.Since(startTime).Milliseconds(),
		RequestMetadata: buildRequestMetadata(req.Header(), req.Peer().Addr),
	}

	if serviceData := getServiceData(ctx); serviceData != nil {
		auditLog.ServiceData = serviceData
	}
	if auditLog.Resource == "" {
		auditLog.Resource = auditLog.User
	}

	_, createErr := in.store.CreateAuditLog(ctx, auditLog)
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

func marshalAuditMessage(message proto.Message) (*structpb.Struct, map[string]any, error) {
	if message == nil {
		return nil, nil, nil
	}

	payload, err := protojson.Marshal(message)
	if err != nil {
		return nil, nil, pkgerrors.Wrap(err, "failed to marshal audit message")
	}

	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, nil, pkgerrors.Wrap(err, "failed to unmarshal audit message json")
	}
	sanitizeAuditValue(raw)
	structured, err := structpb.NewStruct(raw)
	if err != nil {
		return nil, nil, pkgerrors.Wrap(err, "failed to convert audit message to struct")
	}
	return structured, raw, nil
}

func sanitizeAuditValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, childValue := range typed {
			if isSensitiveAuditField(key) {
				typed[key] = redactedValue
				continue
			}
			sanitizeAuditValue(childValue)
		}
	case []any:
		for i := range typed {
			sanitizeAuditValue(typed[i])
		}
	default:
	}
}

func isSensitiveAuditField(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	if normalized == "code" || normalized == "authorization" || normalized == "cookie" || normalized == "idpcontext" {
		return true
	}
	for _, marker := range []string{"password", "token", "secret", "credential", "servicekey", "apikey", "api_key", "accesskey", "privatekey", "private_key"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func resolveParent(defaultParent string, requestMap, responseMap map[string]any) string {
	for _, candidate := range []string{
		getNestedString(requestMap, "parent"),
		getNestedString(responseMap, "parent"),
	} {
		if candidate != "" {
			return candidate
		}
	}
	return defaultParent
}

func resolveResource(requestMap, responseMap map[string]any) string {
	for _, candidate := range []string{
		getNestedString(responseMap, "name"),
		getNestedString(responseMap, "user", "name"),
		getNestedString(requestMap, "name"),
		getNestedString(requestMap, "user", "name"),
		getNestedString(responseMap, "user", "email"),
		getNestedString(requestMap, "email"),
	} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func resolveActor(ctx context.Context, requestMap, responseMap map[string]any) string {
	if user, ok := GetUserFromContext(ctx); ok && user != nil {
		return common.FormatUserUID(user.ID)
	}
	for _, candidate := range []string{
		getNestedString(responseMap, "user", "name"),
		getNestedString(requestMap, "user", "name"),
		getNestedString(requestMap, "email"),
		getNestedString(responseMap, "user", "email"),
	} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func getNestedString(raw map[string]any, keys ...string) string {
	if raw == nil {
		return ""
	}
	current := any(raw)
	for _, key := range keys {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = object[key]
		if !ok {
			return ""
		}
	}
	value, ok := current.(string)
	if !ok {
		return ""
	}
	return value
}

func mapSeverity(err error) storepb.AuditSeverity {
	if err == nil {
		return storepb.AuditSeverity_INFO
	}
	connectErr, ok := errors.AsType[*connect.Error](err)
	if !ok {
		return storepb.AuditSeverity_ERROR
	}
	switch connectErr.Code() {
	case connect.CodeUnauthenticated, connect.CodePermissionDenied, connect.CodeInvalidArgument, connect.CodeNotFound, connect.CodeAlreadyExists:
		return storepb.AuditSeverity_WARNING
	default:
		return storepb.AuditSeverity_ERROR
	}
}

func buildAuditStatus(err error) *storepb.AuditStatus {
	if err == nil {
		return &storepb.AuditStatus{Message: "ok"}
	}
	connectErr, ok := errors.AsType[*connect.Error](err)
	if !ok {
		return &storepb.AuditStatus{Code: int32(connect.CodeUnknown), Message: err.Error()}
	}
	return &storepb.AuditStatus{Code: int32(connectErr.Code()), Message: connectErr.Message()}
}

func buildRequestMetadata(header http.Header, peerAddr string) *storepb.RequestMetadata {
	ip := strings.TrimSpace(strings.Split(header.Get("X-Forwarded-For"), ",")[0])
	if ip == "" {
		ip = strings.TrimSpace(strings.Split(header.Get("grpcgateway-x-forwarded-for"), ",")[0])
	}
	if ip == "" && peerAddr != "" {
		host, _, err := net.SplitHostPort(peerAddr)
		if err == nil {
			ip = host
		} else {
			ip = peerAddr
		}
	}

	userAgent := header.Get("User-Agent")
	if userAgent == "" {
		userAgent = header.Get("grpcgateway-user-agent")
	}

	return &storepb.RequestMetadata{Ip: ip, UserAgent: userAgent}
}

func getServiceData(ctx context.Context) *structpb.Struct {
	serviceData, ok := ctx.Value(common.ServiceDataKey).(map[string]any)
	if !ok || len(serviceData) == 0 {
		return nil
	}
	structured, err := structpb.NewStruct(serviceData)
	if err != nil {
		return nil
	}
	return structured
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
