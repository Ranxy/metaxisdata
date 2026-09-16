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

// auditWriteTimeout bounds an audit write that runs detached from the request,
// so a hung database cannot pile up audit goroutines.
const auditWriteTimeout = 10 * time.Second

type AuditInterceptor struct {
	store *store.Store
	// trustedProxies are the peer IPs/CIDRs whose forwarding headers may be
	// believed. Empty means the connection address is the client address.
	trustedProxies []string
}

func NewAuditInterceptor(store *store.Store, trustedProxies []string) *AuditInterceptor {
	return &AuditInterceptor{store: store, trustedProxies: trustedProxies}
}

// auditContext detaches an audit write from the request: a client disconnect
// must not cancel the record of what that client did.
func auditContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), auditWriteTimeout)
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
		auditCtx, cancel := auditContext(ctx)
		defer cancel()
		workspaceID, workspaceErr := in.store.GetWorkspaceID(auditCtx)
		if workspaceErr == nil {
			auditLog := &storepb.AuditLog{
				Parent:          common.FormatWorkspace(workspaceID),
				Method:          conn.Spec().Procedure,
				User:            resolveActor(ctx, nil, nil),
				Severity:        mapSeverity(err),
				Status:          buildAuditStatus(err),
				LatencyMs:       time.Since(startTime).Milliseconds(),
				RequestMetadata: buildRequestMetadata(conn.RequestHeader(), "", in.trustedProxies),
			}
			if _, createErr := in.store.CreateAuditLog(auditCtx, auditLog); createErr != nil {
				slog.Error("failed to persist stream audit log", "method", conn.Spec().Procedure, clog.WithError(createErr))
			}
		}
		return err
	}
}

func (in *AuditInterceptor) createAuditLog(ctx context.Context, req connect.AnyRequest, resp connect.AnyResponse, err error, startTime time.Time) error {
	auditCtx, cancel := auditContext(ctx)
	defer cancel()

	workspaceID, workspaceErr := in.store.GetWorkspaceID(auditCtx)
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
		RequestMetadata: buildRequestMetadata(req.Header(), req.Peer().Addr, in.trustedProxies),
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

// sanitizeAuditStruct re-runs redaction over a stored payload. Rows written
// before the redaction list was complete may still hold plaintext credentials,
// so the read path must not hand them back.
func sanitizeAuditStruct(payload *structpb.Struct) *structpb.Struct {
	if payload == nil {
		return nil
	}
	raw := payload.AsMap()
	sanitizeAuditValue(raw)
	sanitized, err := structpb.NewStruct(raw)
	if err != nil {
		return payload
	}
	return sanitized
}

func isSensitiveAuditField(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "code", "authorization", "cookie", "idpcontext",
		// These are bare field names of credential-bearing messages, so a
		// substring match would not catch them.
		"key", "sslkey", "sslcert", "sshprivatekey",
		"passwd", "pwd", "bearer", "jwt", "session",
		// The device login polling secret. CreateDeviceLogin returns it, and it
		// must not end up in an audit record where it could be replayed while
		// the approval is still open.
		"devicecode", "device_code":
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

func mapSeverity(err error) storepb.AuditLogSeverity {
	if err == nil {
		return storepb.AuditLogSeverity_INFO
	}
	connectErr, ok := errors.AsType[*connect.Error](err)
	if !ok {
		return storepb.AuditLogSeverity_ERROR
	}
	switch connectErr.Code() {
	case connect.CodeUnauthenticated, connect.CodePermissionDenied, connect.CodeInvalidArgument, connect.CodeNotFound, connect.CodeAlreadyExists:
		return storepb.AuditLogSeverity_WARNING
	default:
		return storepb.AuditLogSeverity_ERROR
	}
}

func buildAuditStatus(err error) *storepb.AuditLogStatus {
	if err == nil {
		return &storepb.AuditLogStatus{Message: "ok"}
	}
	connectErr, ok := errors.AsType[*connect.Error](err)
	if !ok {
		return &storepb.AuditLogStatus{Code: int32(connect.CodeUnknown), Message: err.Error()}
	}
	return &storepb.AuditLogStatus{Code: int32(connectErr.Code()), Message: connectErr.Message()}
}

// buildRequestMetadata records where a request came from. Forwarding headers are
// only believed when the connection itself comes from a configured trusted
// proxy: otherwise any client could pick its own audit IP by sending
// X-Forwarded-For.
func buildRequestMetadata(header http.Header, peerAddr string, trustedProxies []string) *storepb.AuditRequestMetadata {
	peerHost := hostFromAddr(peerAddr)
	ip := peerHost
	if peerHost != "" && isTrustedProxy(peerHost, trustedProxies) {
		if forwarded := firstForwardedFor(header); forwarded != "" {
			ip = forwarded
		}
	}

	userAgent := header.Get("User-Agent")
	if userAgent == "" {
		userAgent = header.Get("grpcgateway-user-agent")
	}

	return &storepb.AuditRequestMetadata{Ip: ip, UserAgent: userAgent}
}

func hostFromAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func firstForwardedFor(header http.Header) string {
	for _, key := range []string{"X-Forwarded-For", "grpcgateway-x-forwarded-for"} {
		if value := header.Get(key); value != "" {
			if first := strings.TrimSpace(strings.Split(value, ",")[0]); first != "" {
				return first
			}
		}
	}
	return ""
}

// isTrustedProxy matches the peer against an exact IP or a CIDR entry.
func isTrustedProxy(host string, trustedProxies []string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, entry := range trustedProxies {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			if _, network, err := net.ParseCIDR(entry); err == nil && network.Contains(ip) {
				return true
			}
			continue
		}
		if parsed := net.ParseIP(entry); parsed != nil && parsed.Equal(ip) {
			return true
		}
	}
	return false
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
