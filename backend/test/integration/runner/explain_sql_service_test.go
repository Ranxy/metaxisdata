//go:build integration

package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// explainFakeAPIKey is a placeholder, never a credential: the fake provider
// checks that the server forwards the key stored on the profile, so the
// scenario proves the stored secret path works without a real provider key
// existing anywhere in the repository or the environment.
const explainFakeAPIKey = "integration-fake-key"

// explainProviderTurn is the part of an OpenAI chat request body the scenario
// asserts on.
type explainProviderTurn struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id"`
}

// explainProviderTool is one tool declaration the server sends to the model.
type explainProviderTool struct {
	Type     string `json:"type"`
	Function struct {
		Name string `json:"name"`
	} `json:"function"`
}

// explainProviderRequest is one request the fake provider received.
type explainProviderRequest struct {
	Model    string                `json:"model"`
	Messages []explainProviderTurn `json:"messages"`
	Tools    []explainProviderTool `json:"tools"`
}

// fakeExplainProvider is a local OpenAI-compatible endpoint. The ExplainSQL
// service appends /v1/chat/completions to the profile's base URL, so pointing a
// throwaway provider profile at this server drives the real agent loop against
// the real metadata store without any model, network egress, or API key.
//
// The first turn answers with a get_object_schema tool call; the second turn
// answers with the explanation text. It records every request so the test can
// prove which tools the service declared and that the tool result came back
// from the store.
type fakeExplainProvider struct {
	server *httptest.Server
	// mu guards requests: the server handles the provider calls on its own
	// goroutines while the test asserts after the stream finished.
	mu       sync.Mutex
	requests []explainProviderRequest
}

func newFakeExplainProvider(t *testing.T) *fakeExplainProvider {
	t.Helper()

	provider := &fakeExplainProvider{}
	provider.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+explainFakeAPIKey {
			http.Error(w, "unexpected credential", http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var request explainProviderRequest
		if err := json.Unmarshal(body, &request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		provider.mu.Lock()
		provider.requests = append(provider.requests, request)
		turn := len(provider.requests)
		provider.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if turn == 1 {
			// A non-streaming completion object is decoded by the same parser the
			// SSE stream uses, so the stub only has to be a valid completion.
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"","tool_calls":[{"id":"call_schema","type":"function","function":{"name":"get_object_schema","arguments":"{\"name\":\"users\"}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"The statement reads the users table.\n\n## Columns\nIt projects id and email."},"finish_reason":"stop"}],"usage":{"prompt_tokens":9,"completion_tokens":7,"total_tokens":16}}`)
	}))
	t.Cleanup(provider.server.Close)
	return provider
}

func (p *fakeExplainProvider) URL() string {
	return p.server.URL
}

func (p *fakeExplainProvider) recordedRequests() []explainProviderRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]explainProviderRequest(nil), p.requests...)
}

// TestExplainSQLToolsRealServerIntegration drives the shipping ExplainSQL loop
// against the real server and the real metadata store with a local fake
// provider: the model is asked to call get_object_schema, the tool resolves the
// table through the shared metadata tool set and the request's IAM principal,
// and the model's next turn receives the real schema JSON before it answers.
//
// The refusal branch of the same gate cannot be reached through the public API:
// every authenticated principal holds databases.read through the
// allUsers -> workspaceMember baseline, so a caller who may explain SQL always
// passes the check. The denial itself is covered hermetically by
// TestExplainSQLToolCallsArePermissionGated, which asserts the pipeline refuses
// before the tool body runs.
func TestExplainSQLToolsRealServerIntegration(t *testing.T) {
	fixture := setupQualityMySQLFixture(t, false)
	ctx := fixture.ctx
	adminToken := fixture.env.AdminToken()
	provider := newFakeExplainProvider(t)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	llmClient := v1connect.NewLLMServiceClient(httpClient, fixture.env.BaseURL)
	profile, err := llmClient.CreateLLMProviderProfile(ctx, withToken(adminToken, &v1pb.CreateLLMProviderProfileRequest{
		Profile: &v1pb.LlmProviderProfile{
			Title:   "integration fake provider",
			Type:    v1pb.LLMProviderType_LLM_PROVIDER_TYPE_OPENAI,
			BaseUrl: provider.URL(),
			ApiKey:  explainFakeAPIKey,
			Models:  []*v1pb.LlmProviderModel{{Name: "integration-fake", Enabled: true}},
		},
	}))
	require.NoError(t, err, "create the throwaway provider profile")
	t.Cleanup(func() {
		_, _ = llmClient.DeleteLLMProviderProfile(context.Background(), withToken(adminToken, &v1pb.DeleteLLMProviderProfileRequest{Name: profile.Msg.GetName()}))
	})

	explainClient := v1connect.NewExplainSQLServiceClient(httpClient, fixture.env.BaseURL)
	scopePrefix := fmt.Sprintf("%s;%s", fixture.instanceID, fixture.sourceDatabase)
	// The SQL is unique so a cached explanation of another scenario's statement
	// cannot answer this call; force_regenerate is the second guard.
	sqlText := fmt.Sprintf("SELECT id, email FROM users WHERE id = %d", time.Now().UnixNano())
	stream, err := explainClient.ExplainSQL(ctx, withToken(adminToken, &v1pb.ExplainSQLRequest{
		SqlText:         sqlText,
		ScopePrefix:     scopePrefix,
		ProviderName:    profile.Msg.GetName(),
		ForceRegenerate: true,
	}))
	require.NoError(t, err, "start the explanation stream")

	var content strings.Builder
	var metadata *v1pb.ExplainSQLMetadata
	for stream.Receive() {
		message := stream.Msg()
		_, _ = content.WriteString(message.GetContent())
		if message.GetMetadata() != nil {
			metadata = message.GetMetadata()
		}
	}
	require.NoError(t, stream.Err(), "stream failed: %s", fixture.env.ServerLogs())
	require.Contains(t, content.String(), "users table")
	require.NotNil(t, metadata, "the final metadata payload is missing")
	require.Contains(t, metadata.GetSummary(), "users table")
	require.Equal(t, profile.Msg.GetName(), metadata.GetProvider())
	require.Equal(t, "integration-fake", metadata.GetModel())
	require.False(t, metadata.GetFromCache())

	requests := provider.recordedRequests()
	require.Len(t, requests, 2, "one tool turn then the answer: %s", fixture.env.ServerLogs())

	// The first turn carries the request's tool set: both catalog tools, the
	// same names and shapes ExplainSQL shipped before the catalog migration.
	require.Equal(t, "integration-fake", requests[0].Model)
	toolNames := make([]string, 0, len(requests[0].Tools))
	for _, declared := range requests[0].Tools {
		toolNames = append(toolNames, declared.Function.Name)
	}
	require.ElementsMatch(t, []string{"get_object_schema", "search_objects"}, toolNames)

	// The second turn carries the tool result the pipeline produced: the real
	// users table's columns, not an in-band lookup error.
	var toolResult string
	for _, message := range requests[1].Messages {
		if message.Role == "tool" {
			toolResult = message.Content
		}
	}
	require.NotEmpty(t, toolResult, "the model's second turn has no tool result: %s", fixture.env.ServerLogs())
	require.Contains(t, toolResult, `"columns"`)
	require.Contains(t, toolResult, `"users"`)
	require.NotContains(t, toolResult, `"error"`)
}
