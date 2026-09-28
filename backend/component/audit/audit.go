// Package audit holds the audit-row primitives shared by the Connect
// interceptors and any other writer of audit records.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	pkgerrors "github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const RedactedValue = "[REDACTED]"

// AuditWriteTimeout bounds an audit write that runs detached from the request,
// so a hung database cannot pile up audit goroutines.
const AuditWriteTimeout = 10 * time.Second

// Context detaches an audit write from the request: a client disconnect
// must not cancel the record of what that client did.
func Context(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), AuditWriteTimeout)
}

func MarshalAuditMessage(message proto.Message) (*structpb.Struct, map[string]any, error) {
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
	SanitizeAuditValue(raw)
	structured, err := structpb.NewStruct(raw)
	if err != nil {
		return nil, nil, pkgerrors.Wrap(err, "failed to convert audit message to struct")
	}
	return structured, raw, nil
}

func SanitizeAuditValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, childValue := range typed {
			if IsSensitiveAuditField(key) {
				typed[key] = RedactedValue
				continue
			}
			SanitizeAuditValue(childValue)
		}
	case []any:
		for i := range typed {
			SanitizeAuditValue(typed[i])
		}
	default:
	}
}

// SanitizeAuditStruct re-runs redaction over a stored payload. Rows written
// before the redaction list was complete may still hold plaintext credentials,
// so the read path must not hand them back.
func SanitizeAuditStruct(payload *structpb.Struct) *structpb.Struct {
	if payload == nil {
		return nil
	}
	raw := payload.AsMap()
	SanitizeAuditValue(raw)
	sanitized, err := structpb.NewStruct(raw)
	if err != nil {
		return payload
	}
	return sanitized
}

func IsSensitiveAuditField(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "code", "authorization", "authorizationcode", "authorization_code", "cookie", "idpcontext",
		// These are bare field names of credential-bearing messages, so a
		// substring match would not catch them.
		"key", "sslkey", "sslcert", "sshprivatekey",
		"passwd", "pwd", "bearer", "jwt", "session",
		// The OAuth2 authorization-code flow fields. The verifier and the
		// client secret are credentials for the token exchange, and the
		// authorization code is single-use but replayable until redeemed.
		"codeverifier", "code_verifier", "clientsecret", "client_secret",
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

func ResolveParent(defaultParent string, requestMap, responseMap map[string]any) string {
	for _, candidate := range []string{
		GetNestedString(requestMap, "parent"),
		GetNestedString(responseMap, "parent"),
	} {
		if candidate != "" {
			return candidate
		}
	}
	return defaultParent
}

func ResolveResource(requestMap, responseMap map[string]any) string {
	for _, candidate := range []string{
		GetNestedString(responseMap, "name"),
		GetNestedString(responseMap, "user", "name"),
		GetNestedString(requestMap, "name"),
		GetNestedString(requestMap, "user", "name"),
		GetNestedString(responseMap, "user", "email"),
		GetNestedString(requestMap, "email"),
	} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func ResolveActor(ctx context.Context, requestMap, responseMap map[string]any) string {
	if user, ok := ctx.Value(common.UserContextKey).(*store.UserMessage); ok && user != nil {
		return common.FormatUserUID(user.ID)
	}
	for _, candidate := range []string{
		GetNestedString(responseMap, "user", "name"),
		GetNestedString(requestMap, "user", "name"),
		GetNestedString(requestMap, "email"),
		GetNestedString(responseMap, "user", "email"),
	} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func GetNestedString(raw map[string]any, keys ...string) string {
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

func MapSeverity(err error) storepb.AuditLogSeverity {
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

func BuildAuditStatus(err error) *storepb.AuditLogStatus {
	if err == nil {
		return &storepb.AuditLogStatus{Message: "ok"}
	}
	connectErr, ok := errors.AsType[*connect.Error](err)
	if !ok {
		return &storepb.AuditLogStatus{Code: int32(connect.CodeUnknown), Message: err.Error()}
	}
	return &storepb.AuditLogStatus{Code: int32(connectErr.Code()), Message: connectErr.Message()}
}

// BuildRequestMetadata records where a request came from. Forwarding headers are
// only believed when the connection itself comes from a configured trusted
// proxy: otherwise any client could pick its own audit IP by sending
// X-Forwarded-For.
func BuildRequestMetadata(header http.Header, peerAddr string, trustedProxies []string) *storepb.AuditRequestMetadata {
	peerHost := HostFromAddr(peerAddr)
	ip := peerHost
	if peerHost != "" && IsTrustedProxy(peerHost, trustedProxies) {
		if forwarded := FirstForwardedFor(header); forwarded != "" {
			ip = forwarded
		}
	}

	userAgent := header.Get("User-Agent")
	if userAgent == "" {
		userAgent = header.Get("grpcgateway-user-agent")
	}

	return &storepb.AuditRequestMetadata{Ip: ip, UserAgent: userAgent}
}

func HostFromAddr(addr string) string {
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

func FirstForwardedFor(header http.Header) string {
	for _, key := range []string{"X-Forwarded-For", "grpcgateway-x-forwarded-for"} {
		if value := header.Get(key); value != "" {
			if first := strings.TrimSpace(strings.Split(value, ",")[0]); first != "" {
				return first
			}
		}
	}
	return ""
}

// IsTrustedProxy matches the peer against an exact IP or a CIDR entry.
func IsTrustedProxy(host string, trustedProxies []string) bool {
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

// ErrorForHTTPStatus maps an ingestion response status onto the error shape
// MapSeverity/BuildAuditStatus understand.
func ErrorForHTTPStatus(status int) error {
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		return nil
	}
	code := connect.CodeUnknown
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		code = connect.CodePermissionDenied
	case status == http.StatusBadRequest, status == http.StatusRequestEntityTooLarge, status == http.StatusTooManyRequests:
		code = connect.CodeInvalidArgument
	case status >= http.StatusInternalServerError:
		code = connect.CodeInternal
	default:
		// Other statuses (404, 409, …) keep the unknown-code default.
	}
	return connect.NewError(code, fmt.Errorf("ingestion request failed with status %d", status))
}
