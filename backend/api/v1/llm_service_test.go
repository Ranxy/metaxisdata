package v1

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// A title-only PATCH with an empty mask must not touch the model list, which
// used to be replaced wholesale and disabled the profile.
func TestBuildLLMProfileUpdateWithAnEmptyMaskIsPartial(t *testing.T) {
	t.Parallel()

	update, err := buildLLMProfileUpdate(&v1pb.LlmProviderProfile{Title: "renamed"}, nil)
	require.NoError(t, err)
	require.NotNil(t, update.Title)
	require.Equal(t, "renamed", *update.Title)
	require.Nil(t, update.Models, "an omitted model list must be left alone")
	require.Nil(t, update.BaseURL)
	require.Nil(t, update.APIKey)
}

// The capacities live on each model entry: both the explicit-mask and the
// partial-update path carry them and validate them.
func TestBuildLLMProfileUpdateCarriesModelCapacities(t *testing.T) {
	t.Parallel()

	models := []*v1pb.LlmProviderModel{
		{Name: "m1", Enabled: true, ContextWindow: 131072, MaxOutputTokens: 8192},
	}
	for _, mask := range [][]string{{"models"}, nil} {
		update, err := buildLLMProfileUpdate(&v1pb.LlmProviderProfile{Models: models}, mask)
		require.NoError(t, err)
		require.Len(t, update.Models, 1)
		require.Equal(t, int32(131072), update.Models[0].ContextWindow)
		require.Equal(t, int32(8192), update.Models[0].MaxOutputTokens)
	}

	// An output budget that leaves no message room is refused for either path.
	for _, mask := range [][]string{{"models"}, nil} {
		_, err := buildLLMProfileUpdate(&v1pb.LlmProviderProfile{Models: []*v1pb.LlmProviderModel{
			{Name: "m1", ContextWindow: 16384},
		}}, mask)
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	}

	// A duplicate or empty model name is refused too.
	for _, models := range [][]*v1pb.LlmProviderModel{
		{{Name: "  "}},
		{{Name: "m1"}, {Name: " m1 "}},
	} {
		_, err := buildLLMProfileUpdate(&v1pb.LlmProviderProfile{Models: models}, []string{"models"})
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	}
}

func TestBuildLLMProfileUpdateWithAMask(t *testing.T) {
	t.Parallel()

	profile := &v1pb.LlmProviderProfile{
		Title:   "renamed",
		BaseUrl: "https://llm.example.com",
		Models:  []*v1pb.LlmProviderModel{{Name: "m1"}},
	}

	update, err := buildLLMProfileUpdate(profile, []string{"title"})
	require.NoError(t, err)
	require.NotNil(t, update.Title)
	require.Nil(t, update.BaseURL)
	require.Nil(t, update.Models)

	// An explicit models mask does replace the list, including with an empty one.
	update, err = buildLLMProfileUpdate(profile, []string{"models"})
	require.NoError(t, err)
	require.NotNil(t, update.Models)
	require.Len(t, update.Models, 1)

	// An explicit base_url mask writes the address, so changing the endpoint is
	// persisted; only the validation of a malformed one is covered below.
	update, err = buildLLMProfileUpdate(&v1pb.LlmProviderProfile{BaseUrl: "https://llm.example.com/v1"}, []string{"base_url"})
	require.NoError(t, err)
	require.NotNil(t, update.BaseURL)
	require.Equal(t, "https://llm.example.com/v1", *update.BaseURL)

	_, err = buildLLMProfileUpdate(&v1pb.LlmProviderProfile{BaseUrl: "not-a-url"}, []string{"base_url"})
	require.Error(t, err)
}

// Listing models for a custom provider used to resolve only the provider type,
// so the user-entered base URL never reached the request and every fetch failed.
func TestResolveFetchModelsBaseURL(t *testing.T) {
	t.Parallel()

	const (
		requestURL = "https://request.example.com"
		storedURL  = "https://stored.example.com"
	)

	tests := []struct {
		name         string
		requestURL   string
		storedURL    string
		providerType v1pb.LLMProviderType
		wantBaseURL  string
	}{
		{
			name:         "the request base URL wins",
			requestURL:   requestURL,
			storedURL:    storedURL,
			providerType: v1pb.LLMProviderType_LLM_PROVIDER_TYPE_CUSTOM,
			wantBaseURL:  requestURL,
		},
		{
			name:         "the stored profile base URL is the fallback",
			storedURL:    storedURL,
			providerType: v1pb.LLMProviderType_LLM_PROVIDER_TYPE_CUSTOM,
			wantBaseURL:  storedURL,
		},
		{
			name:         "a built-in provider type falls back to its default",
			providerType: v1pb.LLMProviderType_LLM_PROVIDER_TYPE_OPENAI,
			wantBaseURL:  "https://api.openai.com",
		},
		{
			name:         "a custom provider type has no default",
			providerType: v1pb.LLMProviderType_LLM_PROVIDER_TYPE_CUSTOM,
			wantBaseURL:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.wantBaseURL, resolveFetchModelsBaseURL(tt.requestURL, tt.storedURL, tt.providerType))
		})
	}
}

// A profile's stored key authenticates only the endpoint stored with it:
// letting a model listing forward it to a caller-supplied URL would turn the
// fetch permission into a way to read the secret.
func TestResolveFetchModelsAPIKey(t *testing.T) {
	t.Parallel()

	const (
		requestKey = "request-key"
		storedKey  = "stored-key"
		storedURL  = "https://llm.example.com"
	)

	// A request key always wins, including against a different endpoint.
	apiKey, err := resolveFetchModelsAPIKey(requestKey, "https://other.example.com", storedKey, storedURL)
	require.NoError(t, err)
	require.Equal(t, requestKey, apiKey)

	// The stored key may authenticate the stored endpoint, whichever way the
	// request spells it.
	for _, requestBaseURL := range []string{"", storedURL, "  " + storedURL + "  "} {
		apiKey, err = resolveFetchModelsAPIKey("", requestBaseURL, storedKey, storedURL)
		require.NoError(t, err)
		require.Equal(t, storedKey, apiKey)
	}

	// A different endpoint without a request key must not reuse the stored secret.
	_, err = resolveFetchModelsAPIKey("", "https://other.example.com", storedKey, storedURL)
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

// The settings page no longer mirrors the server's capacity contract, so the
// list payload must carry every value the page validates against.
func TestProviderLimitsMirrorTheServerContract(t *testing.T) {
	t.Parallel()

	limits := providerLimits()
	require.Equal(t, int32(llm.DefaultMaxOutputTokens), limits.GetDefaultMaxOutputTokens())
	require.Equal(t, int32(llm.MinMaxOutputTokens), limits.GetMinMaxOutputTokens())
	require.Equal(t, int32(llm.MaxMaxOutputTokens), limits.GetMaxMaxOutputTokens())
	require.Equal(t, int32(llm.DefaultContextWindow), limits.GetDefaultContextWindow())
	require.Equal(t, int32(llm.MinContextWindow), limits.GetMinContextWindow())
	require.Equal(t, int32(llm.MaxContextWindow), limits.GetMaxContextWindow())
}
