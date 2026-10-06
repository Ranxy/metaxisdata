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

	// A forwarded entry that is not an address is ignored rather than recorded,
	// so the trusted proxy's own address is what the row carries.
	forwarded := http.Header{}
	forwarded.Set("X-Forwarded-For", strings.Repeat("1", MaxIPBytes*2))
	metadata = BuildRequestMetadata(forwarded, "10.0.0.1:1234", []string{"10.0.0.1"})
	require.Equal(t, "10.0.0.1", metadata.GetIp())

	// A peer address that is not an address either is still bounded, so a row
	// stays bounded whatever the transport hands over.
	metadata = BuildRequestMetadata(http.Header{}, strings.Repeat("x", MaxIPBytes*2), nil)
	require.Equal(t, strings.Repeat("x", MaxIPBytes)+auditTruncationSuffix, metadata.GetIp())
}

// M1: X-Forwarded-For is appended to by every hop, so the left end is whatever
// the caller sent and the right end is what the nearest trusted proxy observed.
// Reading it from the right is what keeps a caller from choosing its own audit IP
// and resetting its rate-limit bucket.
func TestClientAddressReadsTheForwardedChainFromTheRight(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		peer           string
		trustedProxies []string
		forwarded      string
		gatewayPeer    string
		want           string
	}{
		{
			name: "an untrusted peer is never taken from the header",
			peer: "203.0.113.5:4040",
			// The caller is not a proxy, so the header is not evidence at all.
			forwarded: "1.2.3.4",
			want:      "203.0.113.5",
		},
		{
			name:           "a forged leftmost entry is ignored",
			peer:           "10.0.0.1:5000",
			trustedProxies: []string{"10.0.0.1"},
			forwarded:      "1.2.3.4, 198.51.100.7",
			want:           "198.51.100.7",
		},
		{
			name:           "the chain is walked until the first untrusted hop",
			peer:           "10.0.0.1:5000",
			trustedProxies: []string{"10.0.0.0/8"},
			forwarded:      "1.2.3.4, 10.0.0.2, 10.0.0.1",
			want:           "1.2.3.4",
		},
		{
			name:           "a fully trusted chain falls back to its leftmost entry",
			peer:           "10.0.0.1:5000",
			trustedProxies: []string{"10.0.0.0/8"},
			forwarded:      "10.0.0.9, 10.0.0.2",
			want:           "10.0.0.9",
		},
		{
			name:           "an IPv6 chain entry is normalized",
			peer:           "10.0.0.1:5000",
			trustedProxies: []string{"10.0.0.1"},
			forwarded:      "[2001:db8::1]:4321, 10.0.0.1",
			want:           "2001:db8::1",
		},
		{
			name:           "non-address entries are skipped, not recorded",
			peer:           "10.0.0.1:5000",
			trustedProxies: []string{"10.0.0.1"},
			forwarded:      "garbage, 198.51.100.7",
			want:           "198.51.100.7",
		},
		{
			name:        "the REST gateway hop resolves to the outer peer",
			peer:        "127.0.0.1:5555",
			forwarded:   "9.9.9.9, 203.0.113.7",
			gatewayPeer: "203.0.113.7",
			want:        "203.0.113.7",
		},
		{
			// Both sides come from the same outer RemoteAddr, but each is parsed
			// separately, so the comparison is on the normalized form.
			name:        "a non-canonical gateway stamp still matches",
			peer:        "127.0.0.1:5555",
			forwarded:   "2001:0db8:0000::1",
			gatewayPeer: "2001:db8::1",
			want:        "2001:db8::1",
		},
		{
			name:        "the REST gateway hop then follows the trusted chain",
			peer:        "127.0.0.1:5555",
			forwarded:   "9.9.9.9, 198.51.100.7, 10.0.0.1",
			gatewayPeer: "10.0.0.1",
			trustedProxies: []string{
				"10.0.0.1",
			},
			want: "198.51.100.7",
		},
		{
			// A caller that sends the marker itself cannot make it match the
			// entry the gateway appends, so it is ignored.
			name:        "a stamped peer that does not match the forwarded tail is ignored",
			peer:        "127.0.0.1:5555",
			forwarded:   "9.9.9.9, 203.0.113.7",
			gatewayPeer: "1.2.3.4",
			want:        "127.0.0.1",
		},
		{
			name:      "a loopback peer without the marker stays loopback",
			peer:      "127.0.0.1:5555",
			forwarded: "1.2.3.4",
			want:      "127.0.0.1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			header := http.Header{}
			if tc.forwarded != "" {
				header.Set("X-Forwarded-For", tc.forwarded)
			}
			if tc.gatewayPeer != "" {
				header.Set(GatewayPeerHeader, tc.gatewayPeer)
			}
			require.Equal(t, tc.want, ClientAddress(header, tc.peer, tc.trustedProxies))
		})
	}
}

// The gateway middleware overwrites rather than appends, so a caller cannot
// smuggle its own value into the marker the resolver reads.
func TestStampGatewayPeerOverwritesACallerSuppliedValue(t *testing.T) {
	t.Parallel()

	header := http.Header{}
	header.Set(gatewayPeerForwardHeader, "1.2.3.4")
	StampGatewayPeer(header, "203.0.113.7:4040")
	require.Equal(t, "203.0.113.7", header.Get(gatewayPeerForwardHeader))

	// A missing peer leaves no marker behind, so nothing is believed.
	empty := http.Header{}
	empty.Set(gatewayPeerForwardHeader, "1.2.3.4")
	StampGatewayPeer(empty, "")
	require.Empty(t, empty.Get(gatewayPeerForwardHeader))
}

// grpc-gateway turns each header line into metadata and joins the entries it
// received with the peer it appended; several lines keep their order.
func TestClientAddressJoinsSeveralForwardedHeaderLines(t *testing.T) {
	t.Parallel()

	header := http.Header{}
	header.Add("X-Forwarded-For", "1.2.3.4, 198.51.100.7")
	header.Add("X-Forwarded-For", "10.0.0.1")
	require.Equal(t, "198.51.100.7", ClientAddress(header, "10.0.0.1:5000", []string{"10.0.0.1"}))
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
