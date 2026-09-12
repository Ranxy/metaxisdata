package llm

import (
	"context"
	"encoding/json"
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
	require.Equal(t, []string{"gpt-4o", "gpt-4o-mini"}, ids)
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
	require.Equal(t, []string{"m"}, ids)
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
	require.Equal(t, []string{"m1"}, ids)
}
