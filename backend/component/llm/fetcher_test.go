package llm

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"relative path", "/v1", true},
		{"missing host", "https://", true},
		{"unsupported scheme", "file:///etc/passwd", true},
		{"plain host", "api.example.com", true},
		{"http", "http://localhost:11434", false},
		{"https", "https://api.example.com/v1", false},
		{"https with trailing space", "  https://api.example.com  ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateBaseURL(tt.baseURL)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestFetchModelsRejectsInvalidBaseURLBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	// The validation error proves the request was never attempted: an attempted
	// request against a relative URL would fail while building or sending it.
	_, err := FetchModels(context.Background(), "/v1", "key")
	require.ErrorContains(t, err, "must use http or https")
}

func TestFetchModelsReadsModelIDs(t *testing.T) {
	t.Parallel()

	var gotAuth, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4o"},{"id":"gpt-4o-mini"},{"id":""}]}`))
	}))
	defer server.Close()

	ids, err := FetchModels(context.Background(), server.URL+"/", "secret-key")
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-4o", "gpt-4o-mini"}, modelNames(ids))
	require.Equal(t, "/v1/models", gotPath)
	require.Equal(t, "Bearer secret-key", gotAuth)
}

func TestFetchModelsOmitsAuthorizationWithoutKey(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"m"}]}`))
	}))
	defer server.Close()

	ids, err := FetchModels(context.Background(), server.URL, "")
	require.NoError(t, err)
	require.Equal(t, []string{"m"}, modelNames(ids))
}

func TestFetchModelsRejectsFailures(t *testing.T) {
	t.Parallel()

	t.Run("non-200 status", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		_, err := FetchModels(context.Background(), server.URL, "key")
		require.ErrorContains(t, err, "unexpected status 401")
	})

	t.Run("empty model list", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"data":[]}`))
		}))
		defer server.Close()

		_, err := FetchModels(context.Background(), server.URL, "")
		require.ErrorContains(t, err, "no models returned")
	})

	t.Run("body is bounded", func(t *testing.T) {
		t.Parallel()

		// More than maxModelsResponseBytes of JSON: the LimitReader truncates it
		// and the decoder must fail rather than accept a partial list.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[`))
			padding := `{"id":"` + strings.Repeat("x", 1<<20) + `"},`
			for written := 0; written < maxModelsResponseBytes; written += len(padding) {
				_, _ = w.Write([]byte(padding))
			}
			_, _ = w.Write([]byte(`{"id":"last"}]}`))
		}))
		defer server.Close()

		_, err := FetchModels(context.Background(), server.URL, "")
		require.ErrorContains(t, err, "failed to decode models response")
	})
}

func TestFetchModelsToleratesUnknownFields(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		payload, err := json.Marshal(map[string]any{
			"object":  "list",
			"unknown": true,
			"data": []map[string]any{
				{"id": "m1", "owned_by": "someone"},
			},
		})
		require.NoError(t, err)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	ids, err := FetchModels(context.Background(), server.URL, "")
	require.NoError(t, err)
	require.Equal(t, []string{"m1"}, modelNames(ids))
}

// modelNames keeps the existing assertions readable now that FetchModels also
// returns capacities.
func modelNames(models []DiscoveredModel) []string {
	names := make([]string, 0, len(models))
	for _, model := range models {
		names = append(names, model.Name)
	}
	return names
}

// Gateways disclose capacities under different spellings; the first usable value
// wins and a listing without any keeps zero (server default).
func TestFetchModelsReadsDisclosedCapacities(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"openrouter","context_length":200000,"top_provider":{"max_completion_tokens":16000}},
			{"id":"enriched","contextWindow":128000,"maxOutputTokens":8192},
			{"id":"nested","limit":{"context":32000,"output":4096}},
			{"id":"snake","context_window":64000,"max_tokens":2048},
			{"id":"plain"}
		]}`))
	}))
	defer server.Close()

	models, err := FetchModels(context.Background(), server.URL, "")
	require.NoError(t, err)
	require.Len(t, models, 5)
	require.Equal(t, DiscoveredModel{Name: "openrouter", ContextWindow: 200000, MaxOutputTokens: 16000}, models[0])
	require.Equal(t, DiscoveredModel{Name: "enriched", ContextWindow: 128000, MaxOutputTokens: 8192}, models[1])
	require.Equal(t, DiscoveredModel{Name: "nested", ContextWindow: 32000, MaxOutputTokens: 4096}, models[2])
	require.Equal(t, DiscoveredModel{Name: "snake", ContextWindow: 64000, MaxOutputTokens: 2048}, models[3])
	require.Equal(t, DiscoveredModel{Name: "plain"}, models[4])
}

// A capacity that is not a positive integer is treated as not disclosed.
func TestPositiveInt32(t *testing.T) {
	t.Parallel()

	require.Equal(t, int32(4096), mustCapacity(t, 4096.0))
	_, ok := positiveInt32(0.0)
	require.False(t, ok)
	_, ok = positiveInt32(-1.0)
	require.False(t, ok)
	_, ok = positiveInt32(1.5)
	require.False(t, ok)
	_, ok = positiveInt32("4096")
	require.False(t, ok)
	_, ok = positiveInt32(math.MaxInt32 + 1)
	require.False(t, ok)
}

func mustCapacity(t *testing.T, value float64) int32 {
	t.Helper()
	capacity, ok := positiveInt32(value)
	require.True(t, ok)
	return capacity
}

func TestNormalizeAPIBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{"API root", "https://api.openai.com", "https://api.openai.com"},
		{"SDK form", "https://api.openai.com/v1", "https://api.openai.com"},
		{"SDK form with a trailing slash", "https://api.openai.com/v1/", "https://api.openai.com"},
		{"trailing slash only", "https://api.openai.com/", "https://api.openai.com"},
		{"path prefix is kept", "https://openrouter.ai/api", "https://openrouter.ai/api"},
		{"path prefix before the SDK form", "https://openrouter.ai/api/v1", "https://openrouter.ai/api"},
		{"a host named v1 is not a path", "https://v1", "https://v1"},
		{"surrounding whitespace", "  https://api.openai.com/v1  ", "https://api.openai.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, normalizeAPIBaseURL(tt.baseURL))
		})
	}
}

// The settings form suggests the SDK form (.../v1), so listing models from it
// must not request /v1/v1/models.
func TestFetchModelsAcceptsAnSDKStyleBaseURL(t *testing.T) {
	t.Parallel()

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"data":[{"id":"m"}]}`))
	}))
	defer server.Close()

	ids, err := FetchModels(context.Background(), server.URL+"/v1", "key")
	require.NoError(t, err)
	require.Equal(t, []string{"m"}, modelNames(ids))
	require.Equal(t, "/v1/models", gotPath)
}
