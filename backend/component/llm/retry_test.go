package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// retryProvider records the output budget of every request and answers with the
// configured statuses in order (the last one repeats). A 200 answer streams the
// fixed SSE body.
type retryProvider struct {
	mu       sync.Mutex
	statuses []int
	sseBody  string
	budgets  []int
	requests int
}

func (p *retryProvider) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var request map[string]any
	_ = json.Unmarshal(body, &request)

	p.mu.Lock()
	p.requests++
	status := p.statuses[min(p.requests-1, len(p.statuses)-1)]
	if budget, ok := request["max_tokens"].(float64); ok {
		p.budgets = append(p.budgets, int(budget))
	} else {
		p.budgets = append(p.budgets, 0)
	}
	p.mu.Unlock()

	if status != http.StatusOK {
		http.Error(w, "the output budget is too large", status)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, p.sseBody)
}

func (p *retryProvider) snapshot() (requests int, budgets []int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests, append([]int(nil), p.budgets...)
}

const retrySSEBody = `data: {"choices":[{"delta":{"content":"{\"suggestions\":[]}"},"finish_reason":"stop"}]}
data: [DONE]
`

// collectRawStream drains one raw stream and returns the concatenated content and
// the first error chunk.
func collectRawStream(t *testing.T, stream <-chan rawStreamChunk) (string, error) {
	t.Helper()
	var (
		content string
		callErr error
	)
	for chunk := range stream {
		content += chunk.Content
		if chunk.Error != nil {
			callErr = chunk.Error
		}
	}
	return content, callErr
}

// A 400/422 rejection of an over-large budget is retried once with the fallback
// budget, and the successful second answer is returned.
func TestStreamRawRetriesARejectedOutputBudget(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusBadRequest, http.StatusUnprocessableEntity} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			provider := &retryProvider{statuses: []int{status, http.StatusOK}, sseBody: retrySSEBody}
			server := httptest.NewServer(http.HandlerFunc(provider.handle))
			t.Cleanup(server.Close)

			cfg := AgentConfig{Provider: ResolvedConfig{
				BaseURL: server.URL, ModelName: "test-model", APIKey: "key",
				MaxOutputTokens: DefaultMaxOutputTokens,
			}}
			content, err := collectRawStream(t, streamRaw(context.Background(), cfg, []Message{{Role: "user", Content: "hi"}}))
			require.NoError(t, err)
			require.Contains(t, content, "suggestions")

			requests, budgets := provider.snapshot()
			require.Equal(t, 2, requests, "the request is retried exactly once")
			require.Equal(t, []int{DefaultMaxOutputTokens, fallbackMaxOutputTokens}, budgets)
		})
	}
}

// A second failure is surfaced with the second status.
func TestStreamRawReportsTheSecondStatusWhenTheFallbackAlsoFails(t *testing.T) {
	t.Parallel()

	provider := &retryProvider{statuses: []int{http.StatusBadRequest, http.StatusUnprocessableEntity}, sseBody: retrySSEBody}
	server := httptest.NewServer(http.HandlerFunc(provider.handle))
	t.Cleanup(server.Close)

	cfg := AgentConfig{Provider: ResolvedConfig{
		BaseURL: server.URL, ModelName: "test-model", APIKey: "key",
		MaxOutputTokens: DefaultMaxOutputTokens,
	}}
	_, err := collectRawStream(t, streamRaw(context.Background(), cfg, []Message{{Role: "user", Content: "hi"}}))
	var responseErr *ResponseError
	require.ErrorAs(t, err, &responseErr)
	require.Equal(t, FailureProviderStatus, responseErr.Code)
	require.Equal(t, http.StatusUnprocessableEntity, responseErr.StatusCode)
	require.Equal(t, fallbackMaxOutputTokens, responseErr.OutputTokens,
		"the diagnostic must report the budget the final attempt was sent with")

	requests, _ := provider.snapshot()
	require.Equal(t, 2, requests, "no third attempt")
}

// A stream failure after the retry reports the budget the retried request carried,
// not the configured one: truncation is the common case.
func TestStreamRawRecordsTheAttemptedBudgetOnStreamFailures(t *testing.T) {
	t.Parallel()

	provider := &retryProvider{
		statuses: []int{http.StatusBadRequest, http.StatusOK},
		sseBody:  "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n",
	}
	server := httptest.NewServer(http.HandlerFunc(provider.handle))
	t.Cleanup(server.Close)

	cfg := AgentConfig{Provider: ResolvedConfig{
		BaseURL: server.URL, ModelName: "test-model", APIKey: "key",
		MaxOutputTokens: DefaultMaxOutputTokens,
	}}
	_, err := collectRawStream(t, streamRaw(context.Background(), cfg, []Message{{Role: "user", Content: "hi"}}))
	var responseErr *ResponseError
	require.ErrorAs(t, err, &responseErr)
	require.Equal(t, FailureTruncated, responseErr.Code)
	require.Equal(t, fallbackMaxOutputTokens, responseErr.OutputTokens)

	requests, budgets := provider.snapshot()
	require.Equal(t, 2, requests)
	require.Equal(t, []int{DefaultMaxOutputTokens, fallbackMaxOutputTokens}, budgets)
}

// A stream failure with no retry reports the configured budget that was sent.
func TestStreamRawRecordsTheConfiguredBudgetWithoutARetry(t *testing.T) {
	t.Parallel()

	provider := &retryProvider{
		statuses: []int{http.StatusOK},
		sseBody:  "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n",
	}
	server := httptest.NewServer(http.HandlerFunc(provider.handle))
	t.Cleanup(server.Close)

	cfg := AgentConfig{Provider: ResolvedConfig{
		BaseURL: server.URL, ModelName: "test-model", APIKey: "key",
		MaxOutputTokens: 8192,
	}}
	_, err := collectRawStream(t, streamRaw(context.Background(), cfg, []Message{{Role: "user", Content: "hi"}}))
	var responseErr *ResponseError
	require.ErrorAs(t, err, &responseErr)
	require.Equal(t, FailureTruncated, responseErr.Code)
	require.Equal(t, 8192, responseErr.OutputTokens)
}

// A budget already at the fallback is not retried: the provider rejected the exact
// value that would be sent again.
func TestStreamRawDoesNotRetryABudgetAtTheFallback(t *testing.T) {
	t.Parallel()

	provider := &retryProvider{statuses: []int{http.StatusBadRequest}, sseBody: retrySSEBody}
	server := httptest.NewServer(http.HandlerFunc(provider.handle))
	t.Cleanup(server.Close)

	cfg := AgentConfig{
		Provider:  ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"},
		MaxTokens: fallbackMaxOutputTokens,
	}
	_, err := collectRawStream(t, streamRaw(context.Background(), cfg, []Message{{Role: "user", Content: "hi"}}))
	var responseErr *ResponseError
	require.ErrorAs(t, err, &responseErr)
	require.Equal(t, FailureProviderStatus, responseErr.Code)
	require.Equal(t, fallbackMaxOutputTokens, responseErr.OutputTokens)
	requests, _ := provider.snapshot()
	require.Equal(t, 1, requests)
}

// Only 400/422 are budget signals; other statuses surface immediately.
func TestStreamRawDoesNotRetryOtherStatuses(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusUnauthorized, http.StatusInternalServerError, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			provider := &retryProvider{statuses: []int{status}, sseBody: retrySSEBody}
			server := httptest.NewServer(http.HandlerFunc(provider.handle))
			t.Cleanup(server.Close)

			cfg := AgentConfig{Provider: ResolvedConfig{
				BaseURL: server.URL, ModelName: "test-model", APIKey: "key",
				MaxOutputTokens: DefaultMaxOutputTokens,
			}}
			_, err := collectRawStream(t, streamRaw(context.Background(), cfg, []Message{{Role: "user", Content: "hi"}}))
			var responseErr *ResponseError
			require.ErrorAs(t, err, &responseErr)
			require.Equal(t, FailureProviderStatus, responseErr.Code)
			require.Equal(t, status, responseErr.StatusCode)
			require.Equal(t, DefaultMaxOutputTokens, responseErr.OutputTokens,
				"no retry happened, so the configured budget was the one sent")
			requests, _ := provider.snapshot()
			require.Equal(t, 1, requests)
		})
	}
}

// The retry predicate is the policy: a 400/422 over the fallback budget, once.
func TestShouldRetryWithSmallerOutputBudget(t *testing.T) {
	t.Parallel()

	require.True(t, shouldRetryWithSmallerOutputBudget(http.StatusBadRequest, DefaultMaxOutputTokens))
	require.True(t, shouldRetryWithSmallerOutputBudget(http.StatusUnprocessableEntity, fallbackMaxOutputTokens+1))
	require.False(t, shouldRetryWithSmallerOutputBudget(http.StatusBadRequest, fallbackMaxOutputTokens))
	require.False(t, shouldRetryWithSmallerOutputBudget(http.StatusUnprocessableEntity, fallbackMaxOutputTokens-1))
	require.False(t, shouldRetryWithSmallerOutputBudget(http.StatusUnauthorized, DefaultMaxOutputTokens))
	require.False(t, shouldRetryWithSmallerOutputBudget(http.StatusInternalServerError, DefaultMaxOutputTokens))
	require.False(t, shouldRetryWithSmallerOutputBudget(http.StatusOK, DefaultMaxOutputTokens))
}
