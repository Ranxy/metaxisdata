package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireResponseError unwraps the classifier out of a call error.
func requireResponseError(t *testing.T, err error) *ResponseError {
	t.Helper()
	require.Error(t, err)
	var responseErr *ResponseError
	require.True(t, errors.As(err, &responseErr), "error %v is not a *ResponseError", err)
	return responseErr
}

// The SSE parser classifies each way a stream can be unusable, keeping the
// message callers already saw.
func TestParseStreamTypedErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		body         string
		code         FailureCode
		finishReason string
		message      string
	}{
		{
			name:         "output token limit",
			body:         "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n",
			code:         FailureTruncated,
			finishReason: "length",
			message:      "truncated",
		},
		{
			name:         "unexpected finish reason",
			body:         "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"content_filter\"}]}\n",
			code:         FailureUnexpectedFinish,
			finishReason: "content_filter",
			message:      "unexpected finish_reason",
		},
		{
			name:    "dropped stream",
			body:    "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n",
			code:    FailureStreamIncomplete,
			message: "without a finish_reason",
		},
		{
			name:    "malformed chunk",
			body:    "data: {not json}\n",
			code:    FailureMalformed,
			message: "malformed SSE",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := collectStream(t, test.body)
			responseErr := requireResponseError(t, err)
			require.Equal(t, test.code, responseErr.Code)
			require.Equal(t, test.finishReason, responseErr.FinishReason)
			require.Contains(t, err.Error(), test.message)
		})
	}
}

// An oversized response is a classified failure, not a silent truncation.
func TestParseStreamClassifiesAnOversizedResponse(t *testing.T) {
	t.Parallel()

	ch := make(chan rawStreamChunk, 8)
	err := parseStream(context.Background(), &maxBytesReader{r: strings.NewReader("0123456789"), remaining: 4}, ch)
	responseErr := requireResponseError(t, err)
	require.Equal(t, FailureResponseTooLarge, responseErr.Code)
}

// A gateway that ignores stream=true is parsed as one completion; its stop reason
// is classified like the SSE path.
func TestParseNonStreamingCompletionTypedErrors(t *testing.T) {
	t.Parallel()

	ch := make(chan rawStreamChunk, 8)
	err := parseNonStreamingCompletion(context.Background(),
		`{"choices":[{"message":{"content":"partial"},"finish_reason":"length"}]}`, ch)
	responseErr := requireResponseError(t, err)
	require.Equal(t, FailureTruncated, responseErr.Code)
	require.Equal(t, "length", responseErr.FinishReason)

	err = parseNonStreamingCompletion(context.Background(), `{"choices":[]}`, ch)
	responseErr = requireResponseError(t, err)
	require.Equal(t, FailureMalformed, responseErr.Code)

	err = parseNonStreamingCompletion(context.Background(), `not json`, ch)
	responseErr = requireResponseError(t, err)
	require.Equal(t, FailureMalformed, responseErr.Code)

	err = parseNonStreamingCompletion(context.Background(),
		`{"choices":[{"message":{"content":"partial"},"finish_reason":"content_filter"}]}`, ch)
	responseErr = requireResponseError(t, err)
	require.Equal(t, FailureUnexpectedFinish, responseErr.Code)
	require.Equal(t, "content_filter", responseErr.FinishReason)
}

// A non-OK provider status carries the HTTP status in the classifier.
func TestAgentLoopClassifiesANonOKProviderStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "the provider is down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	events := collectAgentEvents(t, RunAgentLoop(context.Background(), AgentConfig{
		Provider: ResolvedConfig{BaseURL: server.URL, ModelName: "test-model"},
		Feature:  "test",
		Metrics:  nil,
	}))
	var callErr error
	for _, event := range events {
		if event.Type == AgentEventError {
			callErr = event.Error
		}
	}
	responseErr := requireResponseError(t, callErr)
	require.Equal(t, FailureProviderStatus, responseErr.Code)
	require.Equal(t, http.StatusServiceUnavailable, responseErr.StatusCode)
	require.Contains(t, callErr.Error(), "returned status 503")
}

// A completion that finishes without any content is a malformed response, not an
// unclassified failure: the classifier is reachable for the empty-answer case.
func TestAgentLoopClassifiesAnEmptyCompletion(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}
data: [DONE]
`)
	}))
	t.Cleanup(server.Close)

	events := collectAgentEvents(t, RunAgentLoop(context.Background(), AgentConfig{
		Provider: ResolvedConfig{BaseURL: server.URL, ModelName: "test-model"},
		Feature:  "test",
	}))
	var callErr error
	for _, event := range events {
		if event.Type == AgentEventError {
			callErr = event.Error
		}
	}
	responseErr := requireResponseError(t, callErr)
	require.Equal(t, FailureMalformed, responseErr.Code)
	require.Contains(t, callErr.Error(), "empty response")
}

// A request that cannot be made at all is a provider-request failure.
func TestAgentLoopClassifiesAProviderRequestFailure(t *testing.T) {
	t.Parallel()

	// A closed server refuses the connection deterministically.
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	baseURL := server.URL
	server.Close()

	events := collectAgentEvents(t, RunAgentLoop(context.Background(), AgentConfig{
		Provider: ResolvedConfig{BaseURL: baseURL, ModelName: "test-model"},
		Feature:  "test",
	}))
	var callErr error
	for _, event := range events {
		if event.Type == AgentEventError {
			callErr = event.Error
		}
	}
	responseErr := requireResponseError(t, callErr)
	require.Equal(t, FailureProviderRequest, responseErr.Code)
}

// The sentinel the size-limited reader returns still unwraps through the
// classifier, which is what the parser checks.
func TestResponseErrorUnwrap(t *testing.T) {
	t.Parallel()

	cause := io.ErrUnexpectedEOF
	responseErr := &ResponseError{Code: FailureProviderRequest, Err: cause}
	require.ErrorIs(t, responseErr, io.ErrUnexpectedEOF)
	require.Equal(t, cause.Error(), responseErr.Error())
	require.Equal(t, "PROVIDER_REQUEST", (&ResponseError{Code: FailureProviderRequest}).Error())
	require.Nil(t, (*ResponseError)(nil).Unwrap())
	require.Empty(t, (*ResponseError)(nil).Error())
}
