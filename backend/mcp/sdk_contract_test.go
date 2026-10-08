package mcp_test

// Phase 0 spike for .agents/docs/mcp-server.md.
//
// These tests touch no Backend code: they exercise only the MCP Go SDK, to pin
// the behaviors the server-side MCP design depends on. They are kept as a
// contract test, because every one of them is an assumption the design would
// silently break on if an SDK upgrade changed it:
//
//  1. the SDK supports the 2026-07-28 revision our stateless design needs;
//  2. a tool handler sees the verified bearer identity (req.Extra.TokenInfo)
//     when the handler is wrapped by the SDK's bearer middleware;
//  3. missing/insufficient credentials fail at the transport (401/403 with
//     WWW-Authenticate), not inside the tool;
//  4. one stateless handler serves both protocol models (2026-07-28 without a
//     handshake, 2025-06-18 with one) and does not require a session id;
//  5. what the wire actually carries for structured output, i.e. whether
//     content and structuredContent are both emitted (context cost);
//  6. how audience comparison normalizes a resource identifier.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/stretchr/testify/require"
)

const (
	spikeScope        = "metaxisdata.mcp.read"
	spikeToken        = "spike-token"
	spikeTokenNoScope = "spike-token-no-scope"
	spikeUserID       = "user-42"
	spikeHeader       = "X-Spike"
	spikeHeaderValue  = "spike-header"

	protocolStateless = "2026-07-28"
	protocolLegacy    = "2025-06-18"

	mcpPath = "/mcp"
	prmPath = "/.well-known/oauth-protected-resource"

	whoamiTool    = "whoami"
	rowsTypedTool = "rows_typed"
	rowsRawTool   = "rows_raw"
)

type spikeServer struct {
	endpoint string
	prmURL   string
}

// newSpikeServer starts a stateless, auth-wrapped MCP endpoint over
// httptest, plus its protected-resource metadata document.
func newSpikeServer(t *testing.T) *spikeServer {
	t.Helper()

	mux := http.NewServeMux()
	server := httptest.NewUnstartedServer(mux)
	base := "http://" + server.Listener.Addr().String()
	resource := base + mcpPath
	prmURL := base + prmPath

	mux.Handle(prmPath, auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:               resource,
		AuthorizationServers:   []string{base},
		ScopesSupported:        []string{spikeScope},
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "metaxisdata spike",
	}))

	mcpServer := newTools()
	streamable := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return mcpServer },
		&mcpsdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)
	mux.Handle(mcpPath, auth.RequireBearerToken(verifyToken, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: prmURL,
		Scopes:              []string{spikeScope},
	})(streamable))

	server.Start()
	t.Cleanup(server.Close)
	return &spikeServer{endpoint: base + mcpPath, prmURL: prmURL}
}

func verifyToken(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
	switch token {
	case spikeToken:
		return &auth.TokenInfo{UserID: spikeUserID, Scopes: []string{spikeScope}, Expiration: time.Now().Add(time.Hour)}, nil
	case spikeTokenNoScope:
		return &auth.TokenInfo{UserID: spikeUserID, Scopes: []string{"other.scope"}, Expiration: time.Now().Add(time.Hour)}, nil
	default:
		return nil, auth.ErrInvalidToken
	}
}

type whoamiOut struct {
	UserID          string   `json:"userId"`
	Scopes          []string `json:"scopes,omitempty"`
	HasHeader       bool     `json:"hasHeader"`
	ProtocolVersion string   `json:"protocolVersion,omitempty"`
}

type spikeRow struct {
	GUID     string `json:"guid"`
	Name     string `json:"name"`
	MetaType string `json:"metaType"`
}

type rowsOut struct {
	Rows          []spikeRow `json:"rows"`
	NextPageToken string     `json:"nextPageToken,omitempty"`
}

func sampleRows() []spikeRow {
	rows := make([]spikeRow, 0, 25)
	for i := range 25 {
		rows = append(rows, spikeRow{
			GUID:     "1;shop;;order_" + string(rune('a'+i%26)),
			Name:     "order_" + string(rune('a'+i%26)),
			MetaType: "TABLE",
		})
	}
	return rows
}

func newTools() *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "metaxisdata-spike", Version: "v0"}, nil)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        whoamiTool,
		Description: "report the authenticated caller",
	}, func(_ context.Context, req *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, whoamiOut, error) {
		out := whoamiOut{}
		if extra := req.GetExtra(); extra != nil {
			if extra.TokenInfo != nil {
				out.UserID = extra.TokenInfo.UserID
				out.Scopes = extra.TokenInfo.Scopes
			}
			out.HasHeader = extra.Header.Get(spikeHeader) != ""
		}
		if req.Session != nil {
			if params := req.Session.InitializeParams(); params != nil {
				out.ProtocolVersion = params.ProtocolVersion
			}
		}
		return nil, out, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        rowsTypedTool,
		Description: "return a projected row list through a typed handler",
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, rowsOut, error) {
		return nil, rowsOut{Rows: sampleRows()}, nil
	})

	server.AddTool(&mcpsdk.Tool{
		Name:         rowsRawTool,
		Description:  "return a projected row list through a raw handler",
		InputSchema:  map[string]any{"type": "object"},
		OutputSchema: map[string]any{"type": "object"},
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		payload, err := json.Marshal(rowsOut{Rows: sampleRows()})
		if err != nil {
			return nil, err
		}
		return &mcpsdk.CallToolResult{StructuredContent: json.RawMessage(payload)}, nil
	})

	return server
}

// headerTransport adds the bearer token (and a marker header) to every request,
// which is how a real MCP client presents its OAuth token.
type headerTransport struct {
	base  http.RoundTripper
	token string
}

func (h *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.Header.Set("Authorization", "Bearer "+h.token)
	cloned.Header.Set(spikeHeader, spikeHeaderValue)
	return h.base.RoundTrip(cloned)
}

// captureTransport records the response body of every POST, which is how the
// tests observe the actual wire shape instead of the SDK's decoded view.
type captureTransport struct {
	base http.RoundTripper

	mu   sync.Mutex
	seen []exchange
}

type exchange struct {
	URL      string
	Status   int
	Body     string
	Response http.Header
}

func (c *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := c.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if req.Method != http.MethodPost {
		return resp, nil
	}
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, readErr
	}
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(strings.NewReader(string(body)))

	c.mu.Lock()
	c.seen = append(c.seen, exchange{URL: req.URL.String(), Status: resp.StatusCode, Body: string(body), Response: resp.Header.Clone()})
	c.mu.Unlock()
	return resp, nil
}

func (c *captureTransport) lastResult() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.seen) - 1; i >= 0; i-- {
		if strings.Contains(c.seen[i].Body, `"result"`) {
			return c.seen[i].Body, true
		}
	}
	return "", false
}

func connect(t *testing.T, endpoint, token, protocol string, capture *captureTransport) *mcpsdk.ClientSession {
	t.Helper()

	base := http.RoundTripper(http.DefaultTransport)
	if capture != nil {
		capture.base = base
		base = capture
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "spike-client", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcpsdk.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           &http.Client{Transport: &headerTransport{base: base, token: token}},
		DisableStandaloneSSE: true,
	}, &mcpsdk.ClientSessionOptions{ProtocolVersion: protocol})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callTool(t *testing.T, session *mcpsdk.ClientSession, name string) *mcpsdk.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: map[string]any{}})
	require.NoError(t, err)
	return result
}

func structuredJSON(t *testing.T, result *mcpsdk.CallToolResult) string {
	t.Helper()
	payload, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	return string(payload)
}

// postRaw sends one JSON-RPC message straight to the endpoint, which is how a
// client that the SDK does not drive would talk to it.
func postRaw(t *testing.T, endpoint, token, body string) (int, http.Header, string) {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header.Clone(), string(payload)
}

func TestSDKSupportsTheProtocolRevisionTheDesignNeeds(t *testing.T) {
	t.Parallel()

	versions := mcpsdk.SupportedProtocolVersions()
	require.Contains(t, versions, protocolStateless, "the stateless design depends on this revision")
	t.Logf("supported protocol versions: %v", versions)
}

func TestStatelessServerDeliversTheVerifiedIdentityToTheToolHandler(t *testing.T) {
	t.Parallel()

	s := newSpikeServer(t)
	session := connect(t, s.endpoint, spikeToken, protocolStateless, &captureTransport{})
	result := callTool(t, session, whoamiTool)

	require.False(t, result.IsError)
	payload := structuredJSON(t, result)
	require.Contains(t, payload, spikeUserID, "the tool must see the verified user id")
	require.Contains(t, payload, `"hasHeader":true`, "RequestExtra must carry the HTTP headers")
	require.Contains(t, payload, spikeScope, "the token scopes must be visible to the tool")
	t.Logf("whoami structuredContent: %s", payload)
}

func TestCredentialsAreRejectedAtTheTransportNotInsideTheTool(t *testing.T) {
	t.Parallel()

	s := newSpikeServer(t)
	list := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`

	status, header, _ := postRaw(t, s.endpoint, "", list)
	require.Equal(t, http.StatusUnauthorized, status, "no token must be a 401 before the tool runs")
	challenge := header.Get("WWW-Authenticate")
	require.Contains(t, challenge, "resource_metadata=", "the 401 must point at the protected-resource metadata")

	status, header, _ = postRaw(t, s.endpoint, spikeTokenNoScope, list)
	require.Equal(t, http.StatusForbidden, status, "a token without the required scope must be a 403")
	scoped := header.Get("WWW-Authenticate")
	require.Contains(t, scoped, "scope=", "the 403 must name the scope a step-up authorization would need")
	// Observed on SDK v1.8.0: the 403 carries scope= but not
	// error="insufficient_scope", which RFC 6750 and the MCP spec call a SHOULD.
	// Recorded in the plan; wrap the handler if a client ever needs it.
	t.Logf("401 challenge: %s", challenge)
	t.Logf("403 challenge: %s", scoped)
}

func TestOneStatelessHandlerServesBothProtocolModels(t *testing.T) {
	t.Parallel()

	s := newSpikeServer(t)

	for _, protocol := range []string{protocolStateless, protocolLegacy} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()

			session := connect(t, s.endpoint, spikeToken, protocol, &captureTransport{})
			result := callTool(t, session, whoamiTool)
			require.False(t, result.IsError)
			// Logged rather than asserted: this is the value a text-only fallback
			// for pre-SEP-2106 clients would be gated on.
			t.Logf("requested %s, the handler saw %s", protocol, structuredJSON(t, result))
		})
	}

	// A raw legacy client: initialize must succeed, and the follow-up request
	// must not need the session id even when one was handed out.
	initBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + protocolLegacy + `","capabilities":{},"clientInfo":{"name":"legacy","version":"v0"}}}`
	status, header, body := postRaw(t, s.endpoint, spikeToken, initBody)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, "protocolVersion")
	sessionID := header.Get("Mcp-Session-Id")

	listBody := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	status, _, body = postRaw(t, s.endpoint, spikeToken, listBody)
	require.Equal(t, http.StatusOK, status)
	require.NotContains(t, body, `"error"`, "a stateless handler must answer without a session id")
	require.Contains(t, body, `"tools"`)
	t.Logf("legacy initialize: Mcp-Session-Id=%q; tools/list without it: ok", sessionID)
}

func TestStructuredOutputWireShape(t *testing.T) {
	t.Parallel()

	s := newSpikeServer(t)

	for _, tc := range []struct {
		name string
		tool string
		// duplicated records whether the SDK adds a TextContent copy of the same
		// JSON next to structuredContent, which doubles what crosses the wire.
		duplicated bool
	}{
		{name: "typed handler", tool: rowsTypedTool, duplicated: true},
		{name: "raw handler", tool: rowsRawTool},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			capture := &captureTransport{}
			session := connect(t, s.endpoint, spikeToken, protocolStateless, capture)
			result := callTool(t, session, tc.tool)
			require.False(t, result.IsError)

			raw, ok := capture.lastResult()
			require.True(t, ok, "the response body must have been captured")

			content, structured := splitResult(t, raw)
			require.NotEmpty(t, structured, "the design relies on structuredContent")

			if tc.duplicated {
				require.Greater(t, len(content), len(structured), "the typed path duplicates the payload as text")
			} else {
				require.LessOrEqual(t, len(content), 2, "a raw handler that sets only StructuredContent sends an empty content array")
			}
			t.Logf("%s: response=%dB content=%dB structuredContent=%dB", tc.name, len(raw), len(content), len(structured))
		})
	}
}

func splitResult(t *testing.T, body string) (content, structured string) {
	t.Helper()

	var envelope struct {
		Result struct {
			Content           []json.RawMessage `json:"content"`
			StructuredContent json.RawMessage   `json:"structuredContent"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &envelope))

	contentBytes, err := json.Marshal(envelope.Result.Content)
	require.NoError(t, err)
	return string(contentBytes), string(envelope.Result.StructuredContent)
}

// TestClientWithTheStandaloneSSEStreamEnabled records whether a client that
// does not disable the optional standalone SSE stream still works against a
// stateless handler (where GET returns 405). It asserts nothing on purpose: it
// exists to decide what our own integration tests and the client-side guidance
// should say.
func TestClientWithTheStandaloneSSEStreamEnabled(t *testing.T) {
	t.Parallel()

	s := newSpikeServer(t)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "spike-client", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcpsdk.StreamableClientTransport{
		Endpoint:   s.endpoint,
		HTTPClient: &http.Client{Transport: &headerTransport{base: http.DefaultTransport, token: spikeToken}},
		MaxRetries: -1,
	}, &mcpsdk.ClientSessionOptions{ProtocolVersion: protocolStateless})
	if err != nil {
		t.Logf("connect failed with the standalone SSE stream enabled: %v", err)
		return
	}
	defer func() { _ = session.Close() }()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: whoamiTool, Arguments: map[string]any{}})
	t.Logf("standalone SSE stream enabled: call err=%v, result nil=%t", err, result == nil)
}

func TestProtectedResourceMetadataSupportsDiscovery(t *testing.T) {
	t.Parallel()

	s := newSpikeServer(t)
	resp, err := http.Get(s.prmURL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "*", resp.Header.Get("Access-Control-Allow-Origin"), "metadata is public and must be fetchable cross-origin")
	require.Contains(t, string(body), `"authorization_servers"`)
	require.Contains(t, string(body), spikeScope)
	t.Logf("protected resource metadata: %s", string(body))
}

func TestMatchesResourceRelaxesOnlyTheTrailingSlash(t *testing.T) {
	t.Parallel()

	const resource = "https://mx.example.com/mcp"

	require.True(t, oauthex.MatchesResource([]string{resource}, resource))
	require.True(t, oauthex.MatchesResource([]string{resource + "/"}, resource), "the trailing slash is the one tolerated difference")
	require.False(t, oauthex.MatchesResource(nil, resource), "an empty audience never matches")
	require.False(t, oauthex.MatchesResource([]string{"https://mx.example.com"}, resource))
	require.False(t, oauthex.MatchesResource([]string{"https://MX.example.com/mcp"}, resource), "case is not folded")
	require.False(t, oauthex.MatchesResource([]string{"https://mx.example.com:443/mcp"}, resource), "a default port is not elided")
}
