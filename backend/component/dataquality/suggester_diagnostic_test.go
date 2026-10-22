package dataquality

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
	"github.com/Ranxy/metaxisdata/backend/config"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// diagnosticProvider serves one fixed SSE body.
func diagnosticProvider(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

// diagnosticConfig points one completion at the fake provider.
func diagnosticConfig(server *httptest.Server) llm.ResolvedConfig {
	return llm.ResolvedConfig{
		ProfileName: "profiles/test",
		ModelName:   "test-model",
		BaseURL:     server.URL,
		APIKey:      "test-key",
	}
}

// A provider stop at the token limit is classified, carries the provider facts and
// returns an actionable message instead of a generic internal error.
func TestJSONCompletionDiagnosesATruncatedAnswer(t *testing.T) {
	t.Parallel()

	server := diagnosticProvider(t, `data: {"choices":[{"delta":{"content":"{\"suggestions\":"}}]}
data: {"choices":[{"delta":{},"finish_reason":"length"}]}
`)
	suggester := NewSuggester(nil, nil, nil, nil)
	text, usage, diagnostic, err := suggester.JSONCompletion(context.Background(), diagnosticConfig(server),
		LLMFeatureRuleDraft, RuleDraftPromptVersion, "system prompt", "user prompt", 0)

	require.Empty(t, text)
	// A stream that errors returns before the agent loop reports usage, so a
	// truncated call has no usage to report; the failure is still classified.
	require.Nil(t, usage)
	require.Error(t, err)
	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	require.Contains(t, err.Error(), "the model stopped at the output token limit")
	require.Contains(t, err.Error(), "max_tokens=8192")
	require.Contains(t, err.Error(), "narrower intent")

	require.NotNil(t, diagnostic)
	require.Equal(t, string(llm.FailureTruncated), diagnostic.GetErrorCode())
	require.Equal(t, "length", diagnostic.GetFinishReason())
	require.Equal(t, int32(MaxLLMOutputTokens), diagnostic.GetMaxOutputTokens())
	require.NotEmpty(t, diagnostic.GetCorrelationId())
	require.Equal(t, "profiles/test", diagnostic.GetProvider())
	require.Equal(t, "test-model", diagnostic.GetModel())
	require.Equal(t, RuleDraftPromptVersion, diagnostic.GetPromptVersion())
	require.Equal(t, int64(len("system prompt")+len("user prompt")), diagnostic.GetPromptChars())
	require.False(t, diagnostic.GetDebugCaptured(), "no bodies are captured without runtime debug")
}

// A successful call returns no classifier code and carries the provider's usage.
func TestJSONCompletionSuccessHasNoErrorCode(t *testing.T) {
	t.Parallel()

	server := diagnosticProvider(t, `data: {"choices":[{"delta":{"content":"{\"suggestions\":[]}"}}]}
data: {"choices":[{"delta":{},"finish_reason":"stop"}]}
data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}
data: [DONE]
`)
	suggester := NewSuggester(nil, nil, nil, nil)
	text, usage, diagnostic, err := suggester.JSONCompletion(context.Background(), diagnosticConfig(server),
		LLMFeatureRuleDraft, RuleDraftPromptVersion, "sys", "user", MaxLLMOutputTokens)

	require.NoError(t, err)
	require.Equal(t, `{"suggestions":[]}`, text)
	require.NotNil(t, usage)
	require.Equal(t, int64(10), usage.PromptTokens)
	require.Equal(t, int64(5), usage.CompletionTokens)
	require.Equal(t, int64(15), usage.TotalTokens)
	require.NotNil(t, diagnostic)
	require.Empty(t, diagnostic.GetErrorCode())
	require.Empty(t, diagnostic.GetFinishReason())
	require.NotEmpty(t, diagnostic.GetCorrelationId())
	require.False(t, diagnostic.GetDebugCaptured())
}

// A provider failure that is not a response error still gets a classifier.
func TestJSONCompletionClassifiesAnUnclassifiedFailure(t *testing.T) {
	t.Parallel()

	server := diagnosticProvider(t, `data: {"choices":[{"delta":{},"finish_reason":"content_filter"}]}
`)
	suggester := NewSuggester(nil, nil, nil, nil)
	_, _, diagnostic, err := suggester.JSONCompletion(context.Background(), diagnosticConfig(server),
		LLMFeatureRuleDraft, RuleDraftPromptVersion, "sys", "user", 0)

	require.Error(t, err)
	require.Contains(t, err.Error(), "correlation_id=")
	require.Equal(t, string(llm.FailureUnexpectedFinish), diagnostic.GetErrorCode())
	require.Equal(t, "content_filter", diagnostic.GetFinishReason())
}

// With runtime debug on, the raw bodies are captured for the correlation id; the
// diagnostic says so. With it off, no logger is built at all.
func TestJSONCompletionAttachesTheDebugLoggerOnlyInRuntimeDebug(t *testing.T) {
	t.Parallel()

	server := diagnosticProvider(t, `data: {"choices":[{"delta":{"content":"{\"suggestions\":[]}"}}]}
data: {"choices":[{"delta":{},"finish_reason":"stop"}]}
data: [DONE]
`)

	t.Run("debug on", func(t *testing.T) {
		t.Parallel()
		profile := &config.Profile{}
		profile.RuntimeDebug.Store(true)
		suggester := NewSuggester(nil, llm.NewRegistry(nil, profile), nil, nil)
		var (
			called      bool
			gotKind     string
			gotCorrel   string
			gotReqBody  string
			gotRespBody string
		)
		suggester.newDebugLogger = func(_ *store.Store, _, _, kind, correlationID string) llm.DebugLogger {
			return func(reqBody, respBody string) {
				called = true
				gotKind = kind
				gotCorrel = correlationID
				gotReqBody = reqBody
				gotRespBody = respBody
			}
		}
		_, _, diagnostic, err := suggester.JSONCompletion(context.Background(), diagnosticConfig(server),
			LLMFeatureRuleDraft, RuleDraftPromptVersion, "sys", "user", 0)
		require.NoError(t, err)
		require.True(t, called, "the capture must be attached when runtime debug is on")
		require.Equal(t, LLMFeatureRuleDraft, gotKind)
		require.Equal(t, diagnostic.GetCorrelationId(), gotCorrel)
		require.NotEmpty(t, gotReqBody)
		require.NotEmpty(t, gotRespBody)
	})

	t.Run("debug off", func(t *testing.T) {
		t.Parallel()
		suggester := NewSuggester(nil, nil, nil, nil)
		suggester.newDebugLogger = func(_ *store.Store, _, _, _, _ string) llm.DebugLogger {
			t.Fatal("a debug logger was built without runtime debug")
			return nil
		}
		_, _, diagnostic, err := suggester.JSONCompletion(context.Background(), diagnosticConfig(server),
			LLMFeatureRuleDraft, RuleDraftPromptVersion, "sys", "user", 0)
		require.NoError(t, err)
		require.False(t, diagnostic.GetDebugCaptured())
	})
}

// A model answer that is not the contracted JSON is a parse failure, distinct from
// a provider failure: the diagnostic keeps the call facts and changes the code.
func TestRuleDraftParseFailure(t *testing.T) {
	t.Parallel()

	diagnostic := &storepb.QualityLLMDiagnostic{
		Provider:      "profiles/test",
		Model:         "test-model",
		CorrelationId: "corr-1",
	}
	ruleDraftParseFailure(diagnostic)
	require.Equal(t, "PARSE", diagnostic.GetErrorCode())
	require.Equal(t, "corr-1", diagnostic.GetCorrelationId())
	require.Equal(t, "profiles/test", diagnostic.GetProvider())
}
