//go:build integration

package qualityagent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const agentProviderAPIKey = "integration-agent-key"

type providerMessage struct {
	Role       string             `json:"role"`
	Content    string             `json:"content"`
	ToolCallID string             `json:"tool_call_id"`
	ToolCalls  []providerToolCall `json:"tool_calls"`
}

type providerToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type providerRequest struct {
	Model    string            `json:"model"`
	Messages []providerMessage `json:"messages"`
}

type providerResponse struct {
	Content   string
	ToolCalls []providerToolCall
}

// fakeAgentProvider is an OpenAI-compatible endpoint with a deterministic policy. It
// detects the lead from its system prompt and the target from the shared shape, then
// follows the test's intended coordination path. Every call is recorded so a missing
// or unexpected turn fails with the real prompt history.
type fakeAgentProvider struct {
	server *httptest.Server
	mu     sync.Mutex
	calls  []providerRequest
}

func newFakeAgentProvider(t *testing.T) *fakeAgentProvider {
	t.Helper()

	provider := &fakeAgentProvider{}
	provider.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+agentProviderAPIKey {
			http.Error(w, "unexpected credential", http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var request providerRequest
		if err := json.Unmarshal(body, &request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		provider.mu.Lock()
		provider.calls = append(provider.calls, request)
		provider.mu.Unlock()

		response := provider.respond(request)
		w.Header().Set("Content-Type", "application/json")
		payload := map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{
					"content":    response.Content,
					"tool_calls": response.ToolCalls,
				},
				"finish_reason": finishReason(response),
			}},
			"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 10, "total_tokens": 110},
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(provider.server.Close)
	return provider
}

func (p *fakeAgentProvider) URL() string { return p.server.URL }

func (p *fakeAgentProvider) recordedCalls() []providerRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]providerRequest(nil), p.calls...)
}

func (p *fakeAgentProvider) respond(request providerRequest) providerResponse {
	role := providerRole(request)
	switch role {
	case "lead":
		return p.leadResponse(request)
	case "target":
		return p.targetResponse(request)
	default:
		return providerResponse{Content: "unexpected role"}
	}
}

func providerRole(request providerRequest) string {
	for _, message := range request.Messages {
		if message.Role != "system" {
			continue
		}
		switch {
		case strings.Contains(message.Content, "You are the lead of a quality agent"):
			return "lead"
		case strings.Contains(message.Content, "You are a fixed subagent of the quality agent"):
			return "target"
		default:
		}
	}
	return "unknown"
}

func lastUserContent(request providerRequest) string {
	for index := len(request.Messages) - 1; index >= 0; index-- {
		if request.Messages[index].Role == "user" {
			return request.Messages[index].Content
		}
	}
	return ""
}

func hasToolResult(request providerRequest, needle string) bool {
	for _, message := range request.Messages {
		if message.Role == "tool" && strings.Contains(message.Content, needle) {
			return true
		}
	}
	return false
}

func requestNameFromHistory(request providerRequest) string {
	for _, message := range request.Messages {
		if message.Role != "tool" || !strings.Contains(message.Content, "\"requests\"") {
			continue
		}
		var payload struct {
			Requests []struct {
				Request string `json:"request"`
			} `json:"requests"`
		}
		if err := json.Unmarshal([]byte(message.Content), &payload); err != nil {
			continue
		}
		if len(payload.Requests) > 0 {
			return payload.Requests[0].Request
		}
	}
	return ""
}

func toolCallResponse(id, name, arguments string) providerResponse {
	var call providerToolCall
	call.ID = id
	call.Type = "function"
	call.Function.Name = name
	call.Function.Arguments = arguments
	return providerResponse{ToolCalls: []providerToolCall{call}}
}

func (*fakeAgentProvider) targetResponse(request providerRequest) providerResponse {
	if strings.Contains(lastUserContent(request), "[triage wake") {
		return providerResponse{Content: "nothing to triage in this fixture"}
	}
	if strings.Contains(lastUserContent(request), "[answer]") {
		return providerResponse{Content: "the answer arrived"}
	}
	if !hasToolResult(request, "\"delivered\"") {
		return toolCallResponse("call_question", "send_lead_message",
			`{"intent":"QUESTION","message":"Should we raise the timeout?","question_key":"timeout"}`)
	}
	return providerResponse{Content: "asked the lead"}
}

func (*fakeAgentProvider) leadResponse(request providerRequest) providerResponse {
	requestName := requestNameFromHistory(request)
	if strings.Contains(lastUserContent(request), "A person asked something above") {
		if requestName == "" {
			return toolCallResponse("call_list_before_answer", "list_requests", `{}`)
		}
		return toolCallResponse("call_answer", "answer_request",
			`{"request":"`+requestName+`","answer":"raise it to five minutes"}`)
	}
	if requestName == "" {
		return toolCallResponse("call_list", "list_requests", `{}`)
	}
	if !hasToolResult(request, "\"ask\"") {
		return toolCallResponse("call_forward", "forward_request",
			`{"requests":["`+requestName+`"],"question":"Should we raise the timeout?"}`)
	}
	return providerResponse{Content: "forwarded to the owner"}
}

func finishReason(response providerResponse) string {
	if len(response.ToolCalls) > 0 {
		return "tool_calls"
	}
	return "stop"
}
