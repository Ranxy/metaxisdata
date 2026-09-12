package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

type modelObject struct {
	ID string `json:"id"`
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

// FetchModels calls the provider's /v1/models endpoint and returns the list of model IDs.
// apiKey may be empty for providers that don't require authentication for model listing.
func FetchModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	if err := ValidateBaseURL(baseURL); err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/v1/models"

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

	ids := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("no models returned from %s", endpoint)
	}

	return ids, nil
}
