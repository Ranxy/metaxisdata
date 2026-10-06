package audit

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// The OAuth2 authorization-code flow carries its credentials under both
// camelCase protojson names and snake_case form names. Either spelling must be
// redacted before an audit row is persisted.
func TestSensitiveAuditFieldsAreRedacted(t *testing.T) {
	t.Parallel()

	for _, field := range []string{
		"codeVerifier", "code_verifier",
		"clientSecret", "client_secret",
		"authorizationCode", "authorizationcode", "authorization_code",
		"deviceCode", "password",
	} {
		raw := map[string]any{
			"name":   "instances/i1",
			field:    "credential-value",
			"nested": map[string]any{field: "credential-value"},
		}
		SanitizeAuditValue(raw)

		require.True(t, IsSensitiveAuditField(field), "expected %q to be sensitive", field)
		require.Equal(t, RedactedValue, raw[field], "expected %q to be redacted", field)
		nested, ok := raw["nested"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, RedactedValue, nested[field], "expected nested %q to be redacted", field)
		require.Equal(t, "instances/i1", raw["name"])
	}
}

// H6: an anonymous audited request must not be able to write its body into the
// permanent ledger. Every string is cut to MaxAuditFieldBytes, wherever it sits,
// and the map the resource/actor columns are derived from is cut too.
func TestMarshalAuditMessageBoundsLongFields(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", MaxAuditFieldBytes*4)
	message, err := structpb.NewStruct(map[string]any{
		"title":  long,
		"nested": map[string]any{"title": long},
		"list":   []any{long},
	})
	require.NoError(t, err)

	structured, raw := MarshalAuditMessage(message)
	require.NotNil(t, structured)

	bounded := strings.Repeat("a", MaxAuditFieldBytes) + auditTruncationSuffix
	require.Equal(t, bounded, structured.GetFields()["title"].GetStringValue())
	require.Equal(t, bounded, structured.GetFields()["nested"].GetStructValue().GetFields()["title"].GetStringValue())
	require.Equal(t, bounded, structured.GetFields()["list"].GetListValue().GetValues()[0].GetStringValue())

	// ResolveResource/ResolveActor copy out of this map, so it is bounded too.
	require.Equal(t, bounded, raw["title"])
	nested, ok := raw["nested"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, bounded, nested["title"])
}

// Truncating each field is not enough: a message with many bounded strings still
// serializes over the cap, and the row is replaced by a marker that keeps the
// method, actor and status the interceptor records around it.
func TestMarshalAuditMessageDropsAnOversizedPayload(t *testing.T) {
	t.Parallel()

	fields := make(map[string]any, 64)
	for i := range 64 {
		fields[fmt.Sprintf("field%d", i)] = strings.Repeat("a", MaxAuditFieldBytes)
	}
	message, err := structpb.NewStruct(fields)
	require.NoError(t, err)

	structured, raw := MarshalAuditMessage(message)
	require.NotNil(t, structured)
	require.Contains(t, structured.GetFields()["truncated"].GetStringValue(), "exceeded")
	require.NotContains(t, structured.GetFields(), "field0")
	// The raw map keeps every bounded field: it is what resource and actor are
	// resolved from, and each of them is a single field.
	require.Contains(t, raw, "field0")
}

// BoundAuditStruct is the MCP server's entry point into the same cap: tool
// arguments are attacker-shaped JSON, and a tool call may carry a megabyte of
// SQL into a ledger that is never pruned.
func TestBoundAuditStructBoundsMCPArguments(t *testing.T) {
	t.Parallel()

	require.Nil(t, BoundAuditStruct(nil))

	long := strings.Repeat("s", MaxAuditFieldBytes*2)
	payload, err := structpb.NewStruct(map[string]any{"arguments": map[string]any{"sql": long}})
	require.NoError(t, err)
	bounded := BoundAuditStruct(payload)
	require.Equal(t, strings.Repeat("s", MaxAuditFieldBytes)+auditTruncationSuffix,
		bounded.GetFields()["arguments"].GetStructValue().GetFields()["sql"].GetStringValue())

	// A multi-byte argument must be cut on a rune boundary: half a rune is
	// invalid UTF-8 and would have made BoundAuditStruct hand back the raw,
	// unbounded payload.
	multiByte, err := structpb.NewStruct(map[string]any{"arguments": map[string]any{"sql": strings.Repeat("名", 3000)}})
	require.NoError(t, err)
	sql := BoundAuditStruct(multiByte).GetFields()["arguments"].GetStructValue().GetFields()["sql"].GetStringValue()
	require.True(t, utf8.ValidString(sql))
	require.LessOrEqual(t, len(sql), MaxAuditFieldBytes+len(auditTruncationSuffix))

	arguments := make(map[string]any, 64)
	for i := range 64 {
		arguments[fmt.Sprintf("field%d", i)] = strings.Repeat("s", MaxAuditFieldBytes)
	}
	oversized, err := structpb.NewStruct(map[string]any{"arguments": arguments})
	require.NoError(t, err)
	require.Contains(t, BoundAuditStruct(oversized).GetFields()["truncated"].GetStringValue(), "exceeded")
}

// H6/M17: the User-Agent is client-chosen and had no bound of its own, so it was
// the same ledger bomb through a different door.
func TestBuildRequestMetadataTruncatesUserAgent(t *testing.T) {
	t.Parallel()

	header := http.Header{}
	header.Set("User-Agent", strings.Repeat("u", MaxUserAgentBytes*8))
	metadata := BuildRequestMetadata(header, "203.0.113.7:1234", nil)
	require.Equal(t, "203.0.113.7", metadata.GetIp())
	require.Equal(t, strings.Repeat("u", MaxUserAgentBytes)+auditTruncationSuffix, metadata.GetUserAgent())

	// The grpc-gateway forwards its own header name when User-Agent is absent.
	gatewayHeader := http.Header{}
	gatewayHeader.Set("grpcgateway-user-agent", strings.Repeat("g", MaxUserAgentBytes*8))
	metadata = BuildRequestMetadata(gatewayHeader, "203.0.113.7:1234", nil)
	require.Equal(t, strings.Repeat("g", MaxUserAgentBytes)+auditTruncationSuffix, metadata.GetUserAgent())

	// A short agent is recorded verbatim.
	shortHeader := http.Header{}
	shortHeader.Set("User-Agent", "curl/8.5.0")
	require.Equal(t, "curl/8.5.0", BuildRequestMetadata(shortHeader, "203.0.113.7:1234", nil).GetUserAgent())

	// The forwarded address is client text until M1 is fixed, so a row stays
	// bounded even when a trusted proxy is what lets it through.
	forwarded := http.Header{}
	forwarded.Set("X-Forwarded-For", strings.Repeat("1", MaxAuditFieldBytes*2))
	metadata = BuildRequestMetadata(forwarded, "10.0.0.1:1234", []string{"10.0.0.1"})
	require.Equal(t, strings.Repeat("1", MaxAuditFieldBytes)+auditTruncationSuffix, metadata.GetIp())
}

// A status message is built by a handler out of the request (an address, a
// name), so it is bounded like every other string in the row.
func TestBuildAuditStatusBoundsTheMessage(t *testing.T) {
	t.Parallel()

	require.Equal(t, "ok", BuildAuditStatus(nil).GetMessage())

	long := strings.Repeat("e", MaxAuditFieldBytes*2)
	bounded := strings.Repeat("e", MaxAuditFieldBytes) + auditTruncationSuffix

	connectStatus := BuildAuditStatus(connect.NewError(connect.CodeInvalidArgument, errors.New(long)))
	require.Equal(t, int32(connect.CodeInvalidArgument), connectStatus.GetCode())
	require.Equal(t, bounded, connectStatus.GetMessage())
	require.Equal(t, bounded, BuildAuditStatus(errors.New(long)).GetMessage())
}

// A cut must land between runes. Half a rune is invalid UTF-8, which structpb and
// protojson reject outright, so a byte-wise cut would drop the whole row — an
// anonymous caller could then erase its own audit record with a CJK field or a
// crafted User-Agent header, no body needed.
func TestBoundAuditValuesStayValidUTF8(t *testing.T) {
	t.Parallel()

	// 3000 three-byte runes: MaxAuditFieldBytes lands mid-rune.
	multiByte := strings.Repeat("名", 3000)
	message, err := structpb.NewStruct(map[string]any{"title": multiByte})
	require.NoError(t, err)

	structured, raw := MarshalAuditMessage(message)
	title := structured.GetFields()["title"].GetStringValue()
	require.True(t, utf8.ValidString(title))
	require.LessOrEqual(t, len(title), MaxAuditFieldBytes+len(auditTruncationSuffix))
	require.True(t, strings.HasSuffix(title, auditTruncationSuffix))
	require.True(t, utf8.ValidString(raw["title"].(string)))

	header := http.Header{}
	header.Set("User-Agent", strings.Repeat("名", 100))
	metadata := BuildRequestMetadata(header, "203.0.113.7:1234", nil)
	require.True(t, utf8.ValidString(metadata.GetUserAgent()))
	require.LessOrEqual(t, len(metadata.GetUserAgent()), MaxUserAgentBytes+len(auditTruncationSuffix))

	status := BuildAuditStatus(connect.NewError(connect.CodeInvalidArgument, errors.New(multiByte)))
	require.True(t, utf8.ValidString(status.GetMessage()))

	// The row itself is what CreateAuditLog writes, so it has to serialize.
	encoded, err := protojson.Marshal(&storepb.AuditLog{Request: structured, RequestMetadata: metadata, Status: status})
	require.NoError(t, err)
	require.NotEmpty(t, encoded)
}

// A message the encoder cannot render still leaves a row: the binary protocol
// accepts string fields that are not UTF-8 at all, and a dropped row is worse
// than a row whose detail became a marker.
func TestMarshalAuditMessageMarksAnUnencodablePayload(t *testing.T) {
	t.Parallel()

	message := &v1pb.CreateUserRequest{User: &v1pb.User{Title: string([]byte{0xff, 0xfe, 0xfd})}}
	structured, raw := MarshalAuditMessage(message)
	require.NotNil(t, structured)
	require.Nil(t, raw)
	require.Contains(t, structured.GetFields()["truncated"].GetStringValue(), "could not be encoded")
}
