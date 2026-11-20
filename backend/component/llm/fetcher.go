package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// maxModelsResponseBytes caps the /v1/models response. The body is small in
// practice, and reading an unbounded one is a memory-amplification vector.
const maxModelsResponseBytes = 8 << 20

// modelListResponse is the expected JSON shape from a /v1/models endpoint.
type modelListResponse struct {
	Object string        `json:"object"`
	Data   []modelObject `json:"data"`
}

// modelObject is one /v1/models entry. The capacity spellings vary across
// OpenAI-compatible gateways, so every candidate is optional JSON; a field the
// endpoint does not disclose stays nil.
type modelObject struct {
	ID                   string                   `json:"id"`
	ContextWindow        any                      `json:"context_window"`
	ContextWindowCamel   any                      `json:"contextWindow"`
	ContextLength        any                      `json:"context_length"`
	MaxInputTokens       any                      `json:"max_input_tokens"`
	MaxCompletionTokens  any                      `json:"max_completion_tokens"`
	MaxOutputTokens      any                      `json:"max_output_tokens"`
	MaxOutputTokensCamel any                      `json:"maxOutputTokens"`
	MaxTokens            any                      `json:"max_tokens"`
	MaxTokensCamel       any                      `json:"maxTokens"`
	Limit                *modelListingLimit       `json:"limit"`
	TopProvider          *modelListingTopProvider `json:"top_provider"`
}

type modelListingLimit struct {
	Context any `json:"context"`
	Output  any `json:"output"`
}

type modelListingTopProvider struct {
	MaxCompletionTokens any `json:"max_completion_tokens"`
}

// DiscoveredModel is one model an endpoint disclosed. The capacities are
// advisory: zero means the endpoint disclosed none.
type DiscoveredModel struct {
	Name            string
	ContextWindow   int32
	MaxOutputTokens int32
}

// positiveInt32 reads one JSON capacity candidate. Only a positive integral
// number that fits int32 is usable; anything else means "not disclosed".
func positiveInt32(value any) (int32, bool) {
	number, ok := value.(float64)
	if !ok || number <= 0 || number != math.Trunc(number) || number > math.MaxInt32 {
		return 0, false
	}
	return int32(number), true
}

// firstCapacity returns the first usable candidate, or 0 when none is. A
// max_input_tokens field is only an approximation of the combined window, so a
// gateway that discloses it may make compaction trigger earlier than needed.
func firstCapacity(candidates ...any) int32 {
	for _, candidate := range candidates {
		if value, ok := positiveInt32(candidate); ok {
			return value
		}
	}
	return 0
}

// ValidateBaseURL checks that a provider base URL is an absolute http(s) URL
// with a host. Without it an empty value built a relative path and a non-HTTP
// scheme was handed to the HTTP client, both surfacing as confusing transport
// errors instead of a rejected input.
func ValidateBaseURL(baseURL string) error {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return errors.New("base URL is required")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return errors.Wrapf(err, "invalid base URL %q", baseURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.Errorf("base URL %q must use http or https", baseURL)
	}
	if u.Host == "" {
		return errors.Errorf("base URL %q must include a host", baseURL)
	}
	return nil
}

// normalizeAPIBaseURL returns the provider's API root: the base URL without a
// trailing slash or a trailing /v1. Callers append paths that already carry
// /v1, and the settings form suggests the SDK form (.../v1), so leaving it in
// place would request /v1/v1/....
func normalizeAPIBaseURL(baseURL string) string {
	trimmed := strings.TrimSpace(baseURL)
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return strings.TrimRight(trimmed, "/")
	}
	parsed.Path = strings.TrimSuffix(strings.TrimRight(parsed.Path, "/"), "/v1")
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/")
}

// FetchModels calls the provider's /v1/models endpoint and returns the models it
// discloses, with capacities when the endpoint reports them. apiKey may be empty
// for providers that don't require authentication for model listing.
func FetchModels(ctx context.Context, baseURL, apiKey string) ([]DiscoveredModel, error) {
	if err := ValidateBaseURL(baseURL); err != nil {
		return nil, err
	}
	endpoint := normalizeAPIBaseURL(baseURL) + "/v1/models"

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create models request")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch models")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("unexpected status %d from models endpoint", resp.StatusCode)
	}

	var result modelListResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxModelsResponseBytes)).Decode(&result); err != nil {
		return nil, errors.Wrap(err, "failed to decode models response")
	}

	models := make([]DiscoveredModel, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID == "" {
			continue
		}
		contextCandidates := []any{m.ContextWindow, m.ContextWindowCamel, m.ContextLength, m.MaxInputTokens}
		outputCandidates := []any{m.MaxCompletionTokens, m.MaxOutputTokens, m.MaxOutputTokensCamel, m.MaxTokens, m.MaxTokensCamel}
		if m.TopProvider != nil {
			outputCandidates = append([]any{m.TopProvider.MaxCompletionTokens}, outputCandidates...)
		}
		if m.Limit != nil {
			contextCandidates = append(contextCandidates, m.Limit.Context)
			outputCandidates = append(outputCandidates, m.Limit.Output)
		}
		models = append(models, DiscoveredModel{
			Name:            m.ID,
			ContextWindow:   firstCapacity(contextCandidates...),
			MaxOutputTokens: firstCapacity(outputCandidates...),
		})
	}

	if len(models) == 0 {
		return nil, fmt.Errorf("no models returned from %s", endpoint)
	}

	return models, nil
}
