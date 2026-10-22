package dataquality

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

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

// newDebugSuggester builds a Suggester whose runtime debug is on and whose capture
// callback and classifier write are observable without a database.
func newDebugSuggester(t *testing.T, onCapture func(reqBody, respBody string)) (*Suggester, *[]struct{ correlationID, code string }) {
	t.Helper()
	profile := &config.Profile{}
	profile.RuntimeDebug.Store(true)
	suggester := NewSuggester(nil, llm.NewRegistry(nil, profile), nil, nil)
	suggester.newDebugLogger = func(_ *store.Store, _, _, _, _ string) llm.DebugLogger {
		return func(reqBody, respBody string) {
			if onCapture != nil {
				onCapture(reqBody, respBody)
			}
		}
	}
	written := &[]struct{ correlationID, code string }{}
	suggester.setDebugErrorCode = func(_ context.Context, correlationID, code string) error {
		*written = append(*written, struct{ correlationID, code string }{correlationID, code})
		return nil
	}
	return suggester, written
}

// With runtime debug on, the raw bodies are captured for the correlation id and the
// diagnostic says so only when the callback actually fired. With debug off, no
// logger is built at all.
func TestJSONCompletionAttachesTheDebugLoggerOnlyInRuntimeDebug(t *testing.T) {
	t.Parallel()

	server := diagnosticProvider(t, `data: {"choices":[{"delta":{"content":"{\"suggestions\":[]}"}}]}
data: {"choices":[{"delta":{},"finish_reason":"stop"}]}
data: [DONE]
`)

	t.Run("debug on", func(t *testing.T) {
		t.Parallel()
		var (
			called      bool
			gotReqBody  string
			gotRespBody string
		)
		suggester, _ := newDebugSuggester(t, func(reqBody, respBody string) {
			called = true
			gotReqBody = reqBody
			gotRespBody = respBody
		})
		_, _, diagnostic, err := suggester.JSONCompletion(context.Background(), diagnosticConfig(server),
			LLMFeatureRuleDraft, RuleDraftPromptVersion, "sys", "user", 0)
		require.NoError(t, err)
		require.True(t, called, "the capture must be attached when runtime debug is on")
		require.True(t, diagnostic.GetDebugCaptured(), "a successful call's bodies were captured")
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

// A truncated answer still reaches the capture callback (the agent loop logs the
// bodies before reporting the parse failure), so the dialog can read them.
func TestJSONCompletionCapturesATruncatedAnswer(t *testing.T) {
	t.Parallel()

	server := diagnosticProvider(t, `data: {"choices":[{"delta":{"content":"{}"}}]}
data: {"choices":[{"delta":{},"finish_reason":"length"}]}
`)
	suggester, written := newDebugSuggester(t, nil)
	_, _, diagnostic, err := suggester.JSONCompletion(context.Background(), diagnosticConfig(server),
		LLMFeatureRuleDraft, RuleDraftPromptVersion, "sys", "user", 0)
	require.Error(t, err)
	require.True(t, diagnostic.GetDebugCaptured(), "the bodies were captured before the failure was reported")
	require.Len(t, *written, 1, "the classifier is stamped on the captured row after classification")
	require.Equal(t, diagnostic.GetCorrelationId(), (*written)[0].correlationID)
	require.Equal(t, string(llm.FailureTruncated), (*written)[0].code)
}

// A provider that fails before returning a response never reaches the capture
// callback, so debug_captured must be false even with runtime debug on.
func TestJSONCompletionDoesNotClaimACaptureWithoutABody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "the provider is down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	suggester, written := newDebugSuggester(t, func(_, _ string) {
		t.Error("a provider-status failure must not invoke the capture callback")
	})
	_, _, diagnostic, err := suggester.JSONCompletion(context.Background(), diagnosticConfig(server),
		LLMFeatureRuleDraft, RuleDraftPromptVersion, "sys", "user", 0)
	require.Error(t, err)
	require.Equal(t, string(llm.FailureProviderStatus), diagnostic.GetErrorCode())
	require.False(t, diagnostic.GetDebugCaptured())
	require.Empty(t, *written, "no captured row exists to stamp")
}

// The success path must not observe the capture flag before the stream goroutine
// has run the debug callback. A slow callback makes the ordering observable: if the
// agent broke out at the Done chunk instead of draining to the channel close, this
// would read captured=false (and race). The regression is also caught by -race.
func TestJSONCompletionWaitsForTheCaptureCallback(t *testing.T) {
	t.Parallel()

	server := diagnosticProvider(t, `data: {"choices":[{"delta":{"content":"{\"suggestions\":[]}"}}]}
data: {"choices":[{"delta":{},"finish_reason":"stop"}]}
data: [DONE]
`)
	var callbackRan atomic.Bool
	suggester, _ := newDebugSuggester(t, func(_, _ string) {
		time.Sleep(20 * time.Millisecond)
		callbackRan.Store(true)
	})
	_, _, diagnostic, err := suggester.JSONCompletion(context.Background(), diagnosticConfig(server),
		LLMFeatureRuleDraft, RuleDraftPromptVersion, "sys", "user", 0)
	require.NoError(t, err)
	require.True(t, callbackRan.Load(), "the returned call must have run the capture callback")
	require.True(t, diagnostic.GetDebugCaptured(), "a successful call whose bodies were captured says so")
}

// A successful call whose text later fails JSON parsing keeps captured=true: Draft
// adds only the PARSE classifier, and the dialog can still open the bodies.
func TestSuccessfulCompletionKeepsTheCaptureFlagIntoAParseFailure(t *testing.T) {
	t.Parallel()

	server := diagnosticProvider(t, `data: {"choices":[{"delta":{"content":"not json"}}]}
data: {"choices":[{"delta":{},"finish_reason":"stop"}]}
data: [DONE]
`)
	suggester, _ := newDebugSuggester(t, nil)
	_, _, diagnostic, err := suggester.JSONCompletion(context.Background(), diagnosticConfig(server),
		LLMFeatureRuleDraft, RuleDraftPromptVersion, "sys", "user", 0)
	require.NoError(t, err)
	require.True(t, diagnostic.GetDebugCaptured())

	ruleDraftParseFailure(diagnostic)
	require.Equal(t, "PARSE", diagnostic.GetErrorCode())
	require.True(t, diagnostic.GetDebugCaptured(), "a later parse failure must not lose the capture")
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
