package v1

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestShouldSkipAuditHonorsValidateOnly(t *testing.T) {
	t.Parallel()

	require.False(t, shouldSkipAudit(nil))
	require.False(t, shouldSkipAudit(&v1pb.CreateInstanceRequest{}))
	require.True(t, shouldSkipAudit(&v1pb.CreateInstanceRequest{ValidateOnly: true}))
	// Messages without the field must never be skipped.
	require.False(t, shouldSkipAudit(&v1pb.UpdateInstanceRequest{}))
	require.False(t, shouldSkipAudit(&v1pb.GetInstanceRequest{}))
}

func TestResolveParentPrefersTheRequestThenTheResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		request       map[string]any
		response      map[string]any
		defaultParent string
		want          string
	}{
		{"request parent", map[string]any{"parent": "instances/i1"}, nil, "workspaces/ws", "instances/i1"},
		{"response parent", nil, map[string]any{"parent": "instances/i1"}, "workspaces/ws", "instances/i1"},
		{"request wins over response", map[string]any{"parent": "instances/i1"}, map[string]any{"parent": "instances/i2"}, "workspaces/ws", "instances/i1"},
		{"default", nil, nil, "workspaces/ws", "workspaces/ws"},
		{"non-string parent falls back", map[string]any{"parent": map[string]any{}}, nil, "workspaces/ws", "workspaces/ws"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, resolveParent(tt.defaultParent, tt.request, tt.response))
		})
	}
}

func TestResolveResourcePrefersTheResponseName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		request  map[string]any
		response map[string]any
		want     string
	}{
		{"response name", map[string]any{"name": "instances/i1"}, map[string]any{"name": "instances/i2"}, "instances/i2"},
		{"response user name", nil, map[string]any{"user": map[string]any{"name": "users/1"}}, "users/1"},
		{"request name", map[string]any{"name": "instances/i1"}, nil, "instances/i1"},
		{"request user name", map[string]any{"user": map[string]any{"name": "users/2"}}, nil, "users/2"},
		{"response user email", nil, map[string]any{"user": map[string]any{"email": "a@example.com"}}, "a@example.com"},
		{"request email", map[string]any{"email": "b@example.com"}, nil, "b@example.com"},
		{"nothing", nil, nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, resolveResource(tt.request, tt.response))
		})
	}
}

func TestResolveActorPrefersTheAuthenticatedUser(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", resolveActor(context.Background(), nil, nil))
	require.Equal(t, "users/7", resolveActor(context.Background(), map[string]any{
		"user": map[string]any{"name": "users/7"},
	}, nil))
	require.Equal(t, "c@example.com", resolveActor(context.Background(), map[string]any{
		"email": "c@example.com",
	}, nil))

	// The authenticated user must win over request/response fields, which a
	// caller controls.
	ctx := context.WithValue(context.Background(), common.UserContextKey, &store.UserMessage{ID: 42})
	require.Equal(t, common.FormatUserUID(42), resolveActor(ctx, map[string]any{
		"user": map[string]any{"name": "users/999"},
	}, map[string]any{"name": "users/998"}))
}

func TestMapSeveritySeparatesClientAndServerErrors(t *testing.T) {
	t.Parallel()

	require.Equal(t, storepb.AuditLogSeverity_INFO, mapSeverity(nil))
	for _, code := range []connect.Code{
		connect.CodeUnauthenticated,
		connect.CodePermissionDenied,
		connect.CodeInvalidArgument,
		connect.CodeNotFound,
		connect.CodeAlreadyExists,
	} {
		require.Equalf(t, storepb.AuditLogSeverity_WARNING, mapSeverity(connect.NewError(code, errors.New("nope"))), "code %v", code)
	}
	for _, code := range []connect.Code{
		connect.CodeInternal,
		connect.CodeUnknown,
		connect.CodeUnavailable,
		connect.CodeDeadlineExceeded,
		connect.CodeCanceled,
	} {
		require.Equalf(t, storepb.AuditLogSeverity_ERROR, mapSeverity(connect.NewError(code, errors.New("boom"))), "code %v", code)
	}
	require.Equal(t, storepb.AuditLogSeverity_ERROR, mapSeverity(errors.New("plain error")))
}

func TestBuildAuditStatus(t *testing.T) {
	t.Parallel()

	ok := buildAuditStatus(nil)
	require.Equal(t, "ok", ok.GetMessage())
	require.Equal(t, int32(0), ok.GetCode())

	connectStatus := buildAuditStatus(connect.NewError(connect.CodeNotFound, errors.New("instance not found")))
	require.Equal(t, int32(connect.CodeNotFound), connectStatus.GetCode())
	require.Equal(t, "instance not found", connectStatus.GetMessage())

	plainStatus := buildAuditStatus(errors.New("plain failure"))
	require.Equal(t, int32(connect.CodeUnknown), plainStatus.GetCode())
	require.Equal(t, "plain failure", plainStatus.GetMessage())
}

func TestBuildRequestMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		headers        map[string]string
		peerAddr       string
		trustedProxies []string
		wantIP         string
		wantAgent      string
	}{
		{
			name: "forwarded for is ignored from an untrusted peer", headers: map[string]string{"X-Forwarded-For": " 203.0.113.7 , 10.0.0.1"},
			peerAddr: "10.1.2.3:54321", wantIP: "10.1.2.3",
		},
		{
			name: "forwarded for is believed from a trusted peer", headers: map[string]string{"X-Forwarded-For": " 203.0.113.7 , 10.0.0.1"},
			peerAddr: "10.1.2.3:54321", trustedProxies: []string{"10.1.2.3"}, wantIP: "203.0.113.7",
		},
		{
			name: "gateway forwarded for from a trusted peer", headers: map[string]string{"grpcgateway-x-forwarded-for": "203.0.113.9"},
			peerAddr: "10.1.2.3:54321", trustedProxies: []string{"10.1.2.0/24"}, wantIP: "203.0.113.9",
		},
		{
			name: "trusted exact ip without a port", headers: map[string]string{"X-Forwarded-For": "203.0.113.7"},
			peerAddr: "10.1.2.3", trustedProxies: []string{"10.1.2.3"}, wantIP: "203.0.113.7",
		},
		{name: "peer address host", peerAddr: "10.1.2.3:54321", wantIP: "10.1.2.3"},
		{name: "peer address without port", peerAddr: "10.1.2.3", wantIP: "10.1.2.3"},
		{name: "user agent", headers: map[string]string{"User-Agent": "curl/8"}, wantAgent: "curl/8"},
		{name: "gateway user agent", headers: map[string]string{"grpcgateway-user-agent": "grpc-go/1"}, wantAgent: "grpc-go/1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			metadata := buildRequestMetadata(headerFromMap(tt.headers), tt.peerAddr, tt.trustedProxies)
			require.Equal(t, tt.wantIP, metadata.GetIp())
			require.Equal(t, tt.wantAgent, metadata.GetUserAgent())
		})
	}
}

// A caller must not be able to pick its own audit IP by sending a forwarding
// header from an address that is not a configured proxy.
func TestIsTrustedProxy(t *testing.T) {
	t.Parallel()

	require.True(t, isTrustedProxy("10.0.0.1", []string{"10.0.0.1"}))
	require.True(t, isTrustedProxy("10.0.0.7", []string{"10.0.0.0/24"}))
	require.False(t, isTrustedProxy("10.0.1.7", []string{"10.0.0.0/24"}))
	require.False(t, isTrustedProxy("10.0.0.1", nil))
	require.False(t, isTrustedProxy("not-an-ip", []string{"10.0.0.0/24"}))
	require.False(t, isTrustedProxy("10.0.0.1", []string{"", "10.0.0.0/33"}))
}

// Sanitizing a stored payload is what keeps pre-fix audit rows from handing
// plaintext credentials back to a reader.
func TestSanitizeAuditStructRedactsHistoricalPayloads(t *testing.T) {
	t.Parallel()

	payload, err := structpb.NewStruct(map[string]any{
		"key":      "ol_plaintext",
		"password": "hunter2",
		"nested":   map[string]any{"sslKey": "private", "title": "keep me"},
	})
	require.NoError(t, err)

	sanitized := sanitizeAuditStruct(payload)
	raw := sanitized.AsMap()
	require.Equal(t, redactedValue, raw["key"])
	require.Equal(t, redactedValue, raw["password"])
	nested, ok := raw["nested"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, redactedValue, nested["sslKey"])
	require.Equal(t, "keep me", nested["title"])

	require.Nil(t, sanitizeAuditStruct(nil))
}

// headerFromMap builds a real http.Header: Set canonicalizes the keys the same
// way the HTTP server does, which matters for the grpc-gateway fallbacks.
func headerFromMap(values map[string]string) http.Header {
	header := http.Header{}
	for key, value := range values {
		header.Set(key, value)
	}
	return header
}

func TestGetNestedString(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"parent": "instances/i1",
		"user":   map[string]any{"name": "users/1", "count": 3},
		"list":   []any{"a"},
	}

	require.Equal(t, "", getNestedString(nil, "parent"))
	require.Equal(t, "instances/i1", getNestedString(raw, "parent"))
	require.Equal(t, "users/1", getNestedString(raw, "user", "name"))
	require.Equal(t, "", getNestedString(raw, "user", "count"))
	require.Equal(t, "", getNestedString(raw, "user", "missing"))
	require.Equal(t, "", getNestedString(raw, "list", "0"))
	require.Equal(t, "", getNestedString(map[string]any{"user": "not-an-object"}, "user", "name"))
}
