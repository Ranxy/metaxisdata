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
	"unicode/utf8"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const RedactedValue = "[REDACTED]"

// MaxAuditFieldBytes bounds one string inside an audit payload. Audited methods
// include ones reachable without a credential, and the ledger is never pruned,
// so no single field may carry a request body. The value matches the MCP
// server's per-argument bound, which used to be the only place this cap existed.
const MaxAuditFieldBytes = 8 << 10

// MaxAuditPayloadBytes bounds one serialized audit payload (request or
// response). Truncating fields is not enough on its own: a repeated field or a
// nested object can carry an unbounded number of bounded strings, and a List
// response would otherwise copy the ledger into the ledger. A payload over the
// cap is replaced by a marker instead of being stored.
const MaxAuditPayloadBytes = 256 << 10

// MaxUserAgentBytes bounds the recorded User-Agent. The header is chosen by the
// client and is only limited by the HTTP server's header limit, so an anonymous
// request could otherwise append a megabyte to a permanent row.
const MaxUserAgentBytes = 256

// MaxIPBytes bounds the recorded address. A real one is at most 45 bytes; the
// value still comes from request text on a misconfigured deployment, and it is
// echoed into the ledger and used as a rate-limit key.
const MaxIPBytes = 64

// auditTruncationSuffix marks a value the field bound cut short.
const auditTruncationSuffix = "...(truncated)"

// AuditWriteTimeout bounds an audit write that runs detached from the request,
// so a hung database cannot pile up audit goroutines.
const AuditWriteTimeout = 10 * time.Second

// Context detaches an audit write from the request: a client disconnect
// must not cancel the record of what that client did.
func Context(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), AuditWriteTimeout)
}

// MarshalAuditMessage renders a message for the ledger. It always returns a
// payload and never an error: an audit row must survive a message the encoder
// cannot render — the binary protocol accepts string fields that are not UTF-8
// — because a dropped row is worse than a row whose detail was replaced by a
// marker. A nil message yields a nil payload.
func MarshalAuditMessage(message proto.Message) (*structpb.Struct, map[string]any) {
	if message == nil {
		return nil, nil
	}

	payload, err := protojson.Marshal(message)
	if err != nil {
		return auditUnencodableMarker(), nil
	}

	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return auditUnencodableMarker(), nil
	}
	SanitizeAuditValue(raw)
	// Bound the fields before anything is derived from them: resource, actor
	// and parent are copied out of this map into columns of their own.
	boundAuditValue(raw)
	structured, err := structpb.NewStruct(raw)
	if err != nil {
		return auditUnencodableMarker(), nil
	}
	return capAuditPayload(structured), raw
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

// BoundAuditStruct applies the field and whole-payload bounds to a payload built
// outside MarshalAuditMessage — the MCP server renders tool arguments itself.
// Callers that also need redaction call SanitizeAuditStruct first. A payload that
// cannot be rebuilt is dropped for a marker rather than stored unbounded.
func BoundAuditStruct(payload *structpb.Struct) *structpb.Struct {
	if payload == nil {
		return nil
	}
	raw := payload.AsMap()
	boundAuditValue(raw)
	bounded, err := structpb.NewStruct(raw)
	if err != nil {
		return auditUnencodableMarker()
	}
	return capAuditPayload(bounded)
}

// capAuditPayload replaces a payload it cannot measure or that serializes over
// MaxAuditPayloadBytes with a marker. The caller keeps the method, actor,
// resource and status, so the row still says what happened; only the oversized
// detail is dropped.
func capAuditPayload(payload *structpb.Struct) *structpb.Struct {
	if payload == nil {
		return nil
	}
	serialized, err := protojson.Marshal(payload)
	if err != nil {
		return auditUnencodableMarker()
	}
	if len(serialized) <= MaxAuditPayloadBytes {
		return payload
	}
	return auditOversizedMarker()
}

func auditOversizedMarker() *structpb.Struct {
	return auditMarker(fmt.Sprintf("payload exceeded %d bytes and was dropped", MaxAuditPayloadBytes))
}

func auditUnencodableMarker() *structpb.Struct {
	return auditMarker("payload could not be encoded and was dropped")
}

func auditMarker(message string) *structpb.Struct {
	return &structpb.Struct{Fields: map[string]*structpb.Value{
		"truncated": structpb.NewStringValue(message),
	}}
}

// boundAuditValue shortens every over-long string in place, wherever it sits in
// the payload.
func boundAuditValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, childValue := range typed {
			if text, ok := childValue.(string); ok {
				typed[key] = truncateAuditString(text, MaxAuditFieldBytes)
				continue
			}
			boundAuditValue(childValue)
		}
	case []any:
		for i := range typed {
			if text, ok := typed[i].(string); ok {
				typed[i] = truncateAuditString(text, MaxAuditFieldBytes)
				continue
			}
			boundAuditValue(typed[i])
		}
	default:
	}
}

// BoundAuditString bounds one free-standing string a caller writes into a row
// outside a payload of its own, such as a status message.
func BoundAuditString(value string) string {
	return truncateAuditString(value, MaxAuditFieldBytes)
}

func truncateAuditString(value string, maxBytes int) string {
	// A header may carry bytes that are not UTF-8 at all; a truncated rune is
	// just as bad. Either would make protojson reject the whole row, so the
	// string is repaired before it is cut.
	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, "\uFFFD")
	}
	if len(value) <= maxBytes {
		return value
	}
	return common.TruncateUTF8Bytes(value, maxBytes) + auditTruncationSuffix
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
		return &storepb.AuditLogStatus{Code: int32(connect.CodeUnknown), Message: BoundAuditString(err.Error())}
	}
	return &storepb.AuditLogStatus{Code: int32(connectErr.Code()), Message: BoundAuditString(connectErr.Message())}
}

// GatewayPeerHeader is the metadata key the REST gateway carries the outer
// request's peer address on. The gateway reaches the Connect handler over a
// loopback self-connection, so without it every REST request would be recorded
// as 127.0.0.1. The /v1/* middleware stamps it (see StampGatewayPeer); the
// value is only believed when it also matches the last X-Forwarded-For entry,
// which is what grpc-gateway itself appends.
const GatewayPeerHeader = "Metaxisdata-Client-Peer"

// gatewayPeerForwardHeader is how the header travels from the /v1/* middleware
// to the Connect handler: grpc-gateway forwards "Grpc-Metadata-*" headers as
// gRPC metadata, stripping the prefix.
const gatewayPeerForwardHeader = "Grpc-Metadata-" + GatewayPeerHeader

// StampGatewayPeer records the outer peer address for a request the REST gateway
// is about to forward over its loopback self-connection. It overwrites any
// caller-supplied copy, so the only value that reaches the Connect handler is
// the one this process put there.
func StampGatewayPeer(header http.Header, peerAddr string) {
	header.Del(gatewayPeerForwardHeader)
	if peer := HostFromAddr(peerAddr); peer != "" {
		header.Set(gatewayPeerForwardHeader, peer)
	}
}

// ClientAddress resolves the address a request should be attributed to. The
// forwarding chain is believed only when the connection itself comes from a
// configured trusted proxy; otherwise any client could pick its own audit IP or
// reset its rate-limit bucket with X-Forwarded-For.
//
// The chain is read from the right: each hop appends the address it saw, so the
// rightmost entry is what the nearest trusted proxy observed and the leftmost is
// the least trustworthy. Walking right to left and stopping at the first address
// that is not a configured proxy therefore yields the client, and a value an
// attacker prepended to the header is never reached.
func ClientAddress(header http.Header, peerAddr string, trustedProxies []string) string {
	peerHost := HostFromAddr(peerAddr)
	if peerHost == "" {
		return ""
	}
	// The REST gateway proxies through a loopback self-connection, so the peer
	// alone says nothing about the caller. grpc-gateway appends the outer
	// request's RemoteAddr to X-Forwarded-For, and the gateway middleware records
	// that same address in GatewayPeerHeader; the two only agree when the
	// request really came through the gateway, because the gateway appends the
	// last entry itself and the middleware overwrites the header.
	if ip := net.ParseIP(peerHost); ip != nil && ip.IsLoopback() {
		if outer := HostFromAddr(header.Get(GatewayPeerHeader)); outer != "" && outer == lastForwarded(header) {
			peerHost = outer
		}
	}

	ip := peerHost
	if IsTrustedProxy(peerHost, trustedProxies) {
		if forwarded := forwardedClient(header, trustedProxies); forwarded != "" {
			ip = forwarded
		}
	}
	return ip
}

// forwardedClient walks the forwarding chain from the closest hop outward and
// returns the first address that is not a configured proxy. When every hop is a
// proxy the chain is fully trusted and the furthest upstream address, the first
// one seen, is the best attribution available.
func forwardedClient(header http.Header, trustedProxies []string) string {
	hosts := forwardedHosts(header)
	for i := len(hosts) - 1; i >= 0; i-- {
		if !IsTrustedProxy(hosts[i], trustedProxies) {
			return hosts[i]
		}
	}
	if len(hosts) > 0 {
		return hosts[0]
	}
	return ""
}

// lastForwarded returns the last address in the chain: the entry the nearest hop
// appended.
func lastForwarded(header http.Header) string {
	hosts := forwardedHosts(header)
	if len(hosts) == 0 {
		return ""
	}
	return hosts[len(hosts)-1]
}

// forwardedHosts is every valid address in every X-Forwarded-For header line, in
// order. grpc-gateway joins the entries it received and its own appended peer
// into one line; a client that sends several lines keeps their order.
func forwardedHosts(header http.Header) []string {
	var hosts []string
	for _, value := range header.Values("X-Forwarded-For") {
		for entry := range strings.SplitSeq(value, ",") {
			if host, ok := normalizeIP(entry); ok {
				hosts = append(hosts, host)
			}
		}
	}
	return hosts
}

// normalizeIP parses an address as it appears in a forwarding chain: a bare IP,
// an IP:port pair, or a bracketed IPv6 address. Anything else is not an address
// the server can attribute a request to, and is skipped rather than recorded as
// caller-chosen text.
func normalizeIP(entry string) (string, bool) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return "", false
	}
	if ip := net.ParseIP(entry); ip != nil {
		return ip.String(), true
	}
	if host, _, err := net.SplitHostPort(entry); err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return ip.String(), true
		}
		return "", false
	}
	if strings.HasPrefix(entry, "[") && strings.HasSuffix(entry, "]") {
		if ip := net.ParseIP(entry[1 : len(entry)-1]); ip != nil {
			return ip.String(), true
		}
	}
	return "", false
}

// BuildRequestMetadata records where a request came from. Forwarding headers are
// only believed when the connection itself comes from a configured trusted
// proxy: otherwise any client could pick its own audit IP by sending
// X-Forwarded-For.
func BuildRequestMetadata(header http.Header, peerAddr string, trustedProxies []string) *storepb.AuditRequestMetadata {
	userAgent := header.Get("User-Agent")
	if userAgent == "" {
		userAgent = header.Get("grpcgateway-user-agent")
	}

	return &storepb.AuditRequestMetadata{
		Ip:        truncateAuditString(ClientAddress(header, peerAddr, trustedProxies), MaxIPBytes),
		UserAgent: truncateAuditString(userAgent, MaxUserAgentBytes),
	}
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
