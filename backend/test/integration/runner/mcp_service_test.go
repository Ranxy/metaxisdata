//go:build integration

package runner

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	integrationenv "github.com/Ranxy/metaxisdata/backend/test/integration/env"
)

// These scenarios drive the MCP endpoint and its OAuth 2.1 authorization server
// against the real server: registration, the browser flow with PKCE, the token
// exchange, and tool calls. The browser steps are driven with the admin's bearer
// token because the protocol endpoints read the session from the Authorization
// header as well as from the cookie, so no browser is needed.

const (
	mcpClientRedirect = "http://127.0.0.1:51004/callback"
	mcpResourcePath   = "/mcp"
	prmPath           = "/.well-known/oauth-protected-resource"
	asMetadataPath    = "/.well-known/oauth-authorization-server"
	mcpScope          = "metaxisdata.mcp.read"
	// mcpAccept is what the MCP streamable transport asks for.
	mcpAccept = "application/json, text/event-stream"
)

// mcpClients bundles the ConnectRPC clients these scenarios need, each bound to
// the credential given.
type mcpClients struct {
	setting  v1connect.SettingServiceClient
	oauth    v1connect.OAuthServiceClient
	database v1connect.DatabaseServiceClient
	user     v1connect.UserServiceClient
	audit    v1connect.AuditLogServiceClient
}

func newMCPClients(env *integrationenv.ServiceEnv, token string) *mcpClients {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	if token != "" {
		httpClient.Transport = bearerRoundTripper{base: http.DefaultTransport, token: token}
	}
	return &mcpClients{
		setting:  v1connect.NewSettingServiceClient(httpClient, env.BaseURL),
		oauth:    v1connect.NewOAuthServiceClient(httpClient, env.BaseURL),
		database: v1connect.NewDatabaseServiceClient(httpClient, env.BaseURL),
		user:     v1connect.NewUserServiceClient(httpClient, env.BaseURL),
		audit:    v1connect.NewAuditLogServiceClient(httpClient, env.BaseURL),
	}
}

// enableMCP turns the surface on for the duration of one test and restores the
// settings afterwards, because they are workspace-wide and the rest of the suite
// shares this server.
func enableMCP(ctx context.Context, t *testing.T, env *integrationenv.ServiceEnv, clients *mcpClients) string {
	t.Helper()

	previous, err := clients.setting.GetWorkspaceProfileSetting(ctx, connect.NewRequest(&v1pb.GetWorkspaceProfileSettingRequest{}))
	require.NoError(t, err)
	t.Cleanup(func() {
		restore, err := clients.setting.UpdateWorkspaceProfileSetting(ctx, connect.NewRequest(&v1pb.UpdateWorkspaceProfileSettingRequest{
			Setting:    &v1pb.WorkspaceProfileSetting{ExternalUrl: previous.Msg.GetExternalUrl(), McpEnabled: previous.Msg.GetMcpEnabled()},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"external_url", "mcp_enabled"}},
		}))
		require.NoError(t, err)
		require.Equal(t, previous.Msg.GetMcpEnabled(), restore.Msg.GetMcpEnabled())
	})

	// The server's own address is a loopback URL: the one address allowed to be
	// plain http, which is exactly what a test server is.
	_, err = clients.setting.UpdateWorkspaceProfileSetting(ctx, connect.NewRequest(&v1pb.UpdateWorkspaceProfileSettingRequest{
		Setting:    &v1pb.WorkspaceProfileSetting{ExternalUrl: env.BaseURL, McpEnabled: true},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"external_url", "mcp_enabled"}},
	}))
	require.NoError(t, err)
	return env.BaseURL + mcpResourcePath
}

// TestMCPAuthorizationAndToolsRealServerIntegration walks the whole flow a
// client performs: discover, register, authorize, consent, complete, exchange,
// then call a tool with the token it received.
func TestMCPAuthorizationAndToolsRealServerIntegration(t *testing.T) {
	// Not parallel: this test changes workspace-wide settings.
	ctx := context.Background()
	env := sharedMySQLServiceEnvNoReset(t)
	adminToken := env.AdminToken()
	admin := newMCPClients(env, adminToken)
	resource := enableMCP(ctx, t, env, admin)

	protocol := &http.Client{
		Timeout: 30 * time.Second,
		// The flow is a chain of redirects the test asserts on, so it must see
		// them instead of following them.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	t.Run("discovery is anonymous", func(t *testing.T) {
		protectedResource := getJSON(t, protocol, env.BaseURL+prmPath, "")
		require.Equal(t, resource, protectedResource["resource"])
		require.Equal(t, []any{env.BaseURL}, protectedResource["authorization_servers"])
		require.Equal(t, []any{mcpScope}, protectedResource["scopes_supported"])

		metadata := getJSON(t, protocol, env.BaseURL+asMetadataPath, "")
		require.Equal(t, env.BaseURL, metadata["issuer"])
		require.Equal(t, []any{"S256"}, metadata["code_challenge_methods_supported"])
		require.Equal(t, []any{"authorization_code"}, metadata["grant_types_supported"])
		require.NotContains(t, metadata, "jwks_uri")
	})

	clientID := registerClient(t, protocol, env.BaseURL, mcpClientRedirect)
	verifier := strings.Repeat("integration-verifier-", 3)
	challenge := pkceChallenge(verifier)

	requestID := authorize(t, protocol, env.BaseURL, resource, clientID, adminToken, challenge)

	t.Run("the consent page sees the request and decides it once", func(t *testing.T) {
		name := "oauthAuthorizationRequests/" + requestID
		pending, err := admin.oauth.GetOAuthAuthorizationRequest(ctx, connect.NewRequest(&v1pb.GetOAuthAuthorizationRequestRequest{Name: name}))
		require.NoError(t, err)
		require.Equal(t, mcpClientRedirect, pending.Msg.GetRedirectUri())
		require.Equal(t, resource, pending.Msg.GetResource())
		require.Equal(t, []string{mcpScope}, pending.Msg.GetScopes())
		require.NotEmpty(t, pending.Msg.GetRequestIp())

		// The record carries the authorization code once it is minted; the
		// response has no field for it and must never grow one.
		payload, err := json.Marshal(pending.Msg)
		require.NoError(t, err)
		require.NotContains(t, string(payload), "code")

		_, err = admin.oauth.ApproveOAuthAuthorizationRequest(ctx, connect.NewRequest(&v1pb.ApproveOAuthAuthorizationRequestRequest{Name: name, Approve: true}))
		require.NoError(t, err)

		_, err = admin.oauth.ApproveOAuthAuthorizationRequest(ctx, connect.NewRequest(&v1pb.ApproveOAuthAuthorizationRequestRequest{Name: name, Approve: true}))
		require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "a decision is recorded once")
	})

	code := complete(t, protocol, env.BaseURL, requestID, adminToken)

	t.Run("the code is single use", func(t *testing.T) {
		first := exchange(t, protocol, env.BaseURL, resource, clientID, mcpClientRedirect, code, verifier)
		require.NotEmpty(t, first["access_token"])
		require.Equal(t, "Bearer", first["token_type"])
		require.Equal(t, mcpScope, first["scope"])

		replay := exchangeStatus(t, protocol, env.BaseURL, resource, clientID, mcpClientRedirect, code, verifier)
		require.Equal(t, http.StatusBadRequest, replay, "a redeemed code cannot be replayed")
	})

	t.Run("a wrong verifier is refused", func(t *testing.T) {
		// A second grant, because the first code is already consumed — and a
		// grant only completes once its request has been approved.
		secondID := authorize(t, protocol, env.BaseURL, resource, clientID, adminToken, challenge)
		approveRequest(ctx, t, admin, secondID)
		secondCode := complete(t, protocol, env.BaseURL, secondID, adminToken)
		status := exchangeStatus(t, protocol, env.BaseURL, resource, clientID, mcpClientRedirect, secondCode, "a-different-verifier-that-is-long-enough")
		require.Equal(t, http.StatusBadRequest, status)
	})

	t.Run("the audience keeps the two token classes apart", func(t *testing.T) {
		status, header, _ := rawRequest(t, protocol, http.MethodPost, env.BaseURL+mcpResourcePath, "", mcpAccept, "application/json",
			`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
		require.Equal(t, http.StatusUnauthorized, status, "no token is a 401 before any tool runs")
		challengeHeader := header.Get("WWW-Authenticate")
		require.Contains(t, challengeHeader, `resource_metadata="`+env.BaseURL+prmPath+`"`)
		require.Contains(t, challengeHeader, "invalid_token")

		status, _, _ = rawRequest(t, protocol, http.MethodPost, env.BaseURL+mcpResourcePath, adminToken, mcpAccept, "application/json",
			`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
		require.Equal(t, http.StatusUnauthorized, status, "a web or CLI token must not open the MCP endpoint")
	})

	t.Run("a token opens the tools", func(t *testing.T) {
		token := authorizeAndExchange(ctx, t, admin, protocol, env.BaseURL, resource, clientID, adminToken, verifier, challenge)
		session := connectMCP(t, env.BaseURL+mcpResourcePath, token)

		tools, err := session.ListTools(ctx, nil)
		require.NoError(t, err)
		require.Len(t, tools.Tools, 9)

		expected, err := admin.user.GetCurrentUser(ctx, connect.NewRequest(&emptypb.Empty{}))
		require.NoError(t, err)
		result, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "whoami", Arguments: map[string]any{}})
		require.NoError(t, err)
		require.False(t, result.IsError, "whoami failed: %s", toolText(t, result))
		payload, err := json.Marshal(result.StructuredContent)
		require.NoError(t, err)
		require.Contains(t, string(payload), expected.Msg.GetEmail())

		instances, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "list_instances", Arguments: map[string]any{}})
		require.NoError(t, err)
		require.False(t, instances.IsError, "list_instances failed: %s", toolText(t, instances))
	})

	t.Run("a denied request tells the client", func(t *testing.T) {
		deniedID := authorize(t, protocol, env.BaseURL, resource, clientID, adminToken, challenge)
		denyRequest(ctx, t, admin, deniedID)

		status, header, _ := rawRequest(t, protocol, http.MethodGet, env.BaseURL+"/oauth/authorize/complete?request_id="+url.QueryEscape(deniedID), adminToken, "", "", "")
		require.Equal(t, http.StatusFound, status)
		location, err := url.Parse(header.Get("Location"))
		require.NoError(t, err)
		require.Equal(t, "access_denied", location.Query().Get("error"), "the client must stop waiting for a callback")
		require.Equal(t, "state-1", location.Query().Get("state"))
		require.Equal(t, env.BaseURL, location.Query().Get("iss"), "RFC 9207: the mix-up defence holds on the denial path too")
	})

	t.Run("a tool call leaves an audit row", func(t *testing.T) {
		// The tools subtest ran a call; the permanent ledger is the only place
		// that says so, and the ConnectRPC read baseline is not audited at all.
		logs, err := admin.audit.ListAuditLogs(ctx, connect.NewRequest(&v1pb.ListAuditLogsRequest{
			Parent:   "workspaces/-",
			PageSize: 1000,
		}))
		require.NoError(t, err)

		methods := map[string]bool{}
		for _, entry := range logs.Msg.GetAuditLogs() {
			methods[entry.GetMethod()] = true
		}
		require.True(t, methods["mcp/tools/call:whoami"], "the tool call must be in the ledger")
	})

	t.Run("turning the switch off closes the surface", func(t *testing.T) {
		_, err := admin.setting.UpdateWorkspaceProfileSetting(ctx, connect.NewRequest(&v1pb.UpdateWorkspaceProfileSettingRequest{
			Setting:    &v1pb.WorkspaceProfileSetting{ExternalUrl: env.BaseURL, McpEnabled: false},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"mcp_enabled"}},
		}))
		require.NoError(t, err)

		for _, path := range []string{mcpResourcePath, prmPath, asMetadataPath} {
			status, _, _ := rawRequest(t, protocol, http.MethodGet, env.BaseURL+path, adminToken, "", "", "")
			require.Equal(t, http.StatusNotFound, status, "%s must not exist while the surface is off", path)
		}
		// Registration is a POST; the SPA's fallback answers a GET on any path.
		status, _, _ := rawRequest(t, protocol, http.MethodPost, env.BaseURL+"/oauth/register", adminToken, "", "application/json", `{"redirect_uris":["http://127.0.0.1:51004/callback"]}`)
		require.Equal(t, http.StatusNotFound, status, "registration must not exist while the surface is off")
	})
}

// TestMCPRegistrationValidatesRealServerIntegration covers the checks that need
// the real store: an unregistered redirect URI is refused, and a redirect URI
// that is not https or loopback is refused at registration time.
func TestMCPRegistrationValidatesRealServerIntegration(t *testing.T) {
	ctx := context.Background()
	env := sharedMySQLServiceEnvNoReset(t)
	admin := newMCPClients(env, env.AdminToken())
	enableMCP(ctx, t, env, admin)

	protocol := &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	t.Run("a non-loopback http redirect URI is refused", func(t *testing.T) {
		status, _, body := rawRequest(t, protocol, http.MethodPost, env.BaseURL+"/oauth/register", "", "", "application/json",
			`{"client_name":"bad","redirect_uris":["http://app.example.com/callback"]}`)
		require.Equal(t, http.StatusBadRequest, status)
		require.Contains(t, body, "invalid_redirect_uri")
	})

	t.Run("an unregistered redirect URI is refused without redirecting", func(t *testing.T) {
		clientID := registerClient(t, protocol, env.BaseURL, mcpClientRedirect)
		query := url.Values{
			"response_type":         {"code"},
			"client_id":             {clientID},
			"redirect_uri":          {"http://127.0.0.1:51004/other"},
			"code_challenge":        {pkceChallenge(strings.Repeat("x", 43))},
			"code_challenge_method": {"S256"},
			"resource":              {env.BaseURL + mcpResourcePath},
		}
		status, header, _ := rawRequest(t, protocol, http.MethodGet, env.BaseURL+"/oauth/authorize?"+query.Encode(), env.AdminToken(), "", "", "")
		require.Equal(t, http.StatusBadRequest, status)
		require.Empty(t, header.Get("Location"), "redirecting to an unvalidated URI is the open redirect this endpoint prevents")
	})
}

// --- protocol helpers ---

func registerClient(t *testing.T, client *http.Client, baseURL, redirectURI string) string {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"client_name":   "integration",
		"redirect_uris": []string{redirectURI},
	})
	require.NoError(t, err)
	status, _, response := rawRequest(t, client, http.MethodPost, baseURL+"/oauth/register", "", "", "application/json", string(body))
	require.Equal(t, http.StatusCreated, status, response)

	var registered struct {
		ClientID string `json:"client_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(response), &registered))
	require.NotEmpty(t, registered.ClientID)
	return registered.ClientID
}

func pkceChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func authorize(t *testing.T, client *http.Client, baseURL, resource, clientID, token, challenge string) string {
	t.Helper()

	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {mcpClientRedirect},
		"scope":                 {mcpScope},
		"state":                 {"state-1"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"resource":              {resource},
	}
	status, header, body := rawRequest(t, client, http.MethodGet, baseURL+"/oauth/authorize?"+query.Encode(), token, "", "", "")
	require.Equal(t, http.StatusFound, status, body)

	location, err := url.Parse(header.Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/oauth/consent", location.Path)
	requestID := location.Query().Get("request_id")
	require.NotEmpty(t, requestID)
	return requestID
}

func complete(t *testing.T, client *http.Client, baseURL, requestID, token string) string {
	t.Helper()

	status, header, body := rawRequest(t, client, http.MethodGet, baseURL+"/oauth/authorize/complete?request_id="+url.QueryEscape(requestID), token, "", "", "")
	require.Equal(t, http.StatusFound, status, body)

	location, err := url.Parse(header.Get("Location"))
	require.NoError(t, err)
	require.Equal(t, mcpClientRedirect, location.Scheme+"://"+location.Host+location.Path)
	require.Equal(t, "state-1", location.Query().Get("state"))
	require.Equal(t, baseURL, location.Query().Get("iss"), "RFC 9207: the issuer lets a client detect a mix-up")
	require.NotEmpty(t, location.Query().Get("code"))
	return location.Query().Get("code")
}

func exchange(t *testing.T, client *http.Client, baseURL, resource, clientID, redirectURI, code, verifier string) map[string]any {
	t.Helper()

	status, _, body := exchangeRequest(t, client, baseURL, resource, clientID, redirectURI, code, verifier)
	require.Equal(t, http.StatusOK, status, body)

	var token map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &token))
	return token
}

func exchangeStatus(t *testing.T, client *http.Client, baseURL, resource, clientID, redirectURI, code, verifier string) int {
	t.Helper()

	status, _, _ := exchangeRequest(t, client, baseURL, resource, clientID, redirectURI, code, verifier)
	return status
}

func exchangeRequest(t *testing.T, client *http.Client, baseURL, resource, clientID, redirectURI, code, verifier string) (int, http.Header, string) {
	t.Helper()

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
		"resource":      {resource},
	}
	return rawRequest(t, client, http.MethodPost, baseURL+"/oauth/token", "", "", "application/x-www-form-urlencoded", form.Encode())
}

// authorizeAndExchange runs one complete grant and returns the access token.
func authorizeAndExchange(ctx context.Context, t *testing.T, admin *mcpClients, client *http.Client, baseURL, resource, clientID, token, verifier, challenge string) string {
	t.Helper()

	requestID := authorize(t, client, baseURL, resource, clientID, token, challenge)
	approveRequest(ctx, t, admin, requestID)
	code := complete(t, client, baseURL, requestID, token)
	granted, ok := exchange(t, client, baseURL, resource, clientID, mcpClientRedirect, code, verifier)["access_token"].(string)
	require.True(t, ok, "the token response carries an access token")
	return granted
}

// approveRequest records the decision the consent page would send.
func approveRequest(ctx context.Context, t *testing.T, admin *mcpClients, requestID string) {
	t.Helper()

	_, err := admin.oauth.ApproveOAuthAuthorizationRequest(ctx, connect.NewRequest(&v1pb.ApproveOAuthAuthorizationRequestRequest{
		Name:    "oauthAuthorizationRequests/" + requestID,
		Approve: true,
	}))
	require.NoError(t, err)
}

// denyRequest records the other decision.
func denyRequest(ctx context.Context, t *testing.T, admin *mcpClients, requestID string) {
	t.Helper()

	_, err := admin.oauth.ApproveOAuthAuthorizationRequest(ctx, connect.NewRequest(&v1pb.ApproveOAuthAuthorizationRequestRequest{
		Name:    "oauthAuthorizationRequests/" + requestID,
		Approve: false,
	}))
	require.NoError(t, err)
}

func connectMCP(t *testing.T, endpoint, token string) *mcpsdk.ClientSession {
	t.Helper()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "integration", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcpsdk.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           &http.Client{Transport: bearerRoundTripper{base: http.DefaultTransport, token: token}},
		DisableStandaloneSSE: true,
	}, &mcpsdk.ClientSessionOptions{ProtocolVersion: "2026-07-28"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// toolText renders what a failed tool call told the model, so an assertion says
// why instead of only that it failed.
func toolText(t *testing.T, result *mcpsdk.CallToolResult) string {
	t.Helper()

	payload, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	return string(payload)
}

func getJSON(t *testing.T, client *http.Client, url, token string) map[string]any {
	t.Helper()

	status, _, body := rawRequest(t, client, http.MethodGet, url, token, "", "", "")
	require.Equal(t, http.StatusOK, status, body)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &parsed))
	return parsed
}

// rawRequest sends one request and returns the status, the headers and the body
// as text. It never follows a redirect, so the tests can assert on one.
func rawRequest(t *testing.T, client *http.Client, method, url, token, accept, contentType, body string) (int, http.Header, string) {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequestWithContext(context.Background(), method, url, reader)
	require.NoError(t, err)
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()

	payload, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, response.Header.Clone(), string(payload)
}
