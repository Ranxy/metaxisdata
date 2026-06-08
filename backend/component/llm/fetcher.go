package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

// modelListResponse is the expected JSON shape from a /v1/models endpoint.
type modelListResponse struct {
	Object string        `json:"object"`
	Data   []modelObject `json:"data"`
}

type modelObject struct {
	ID string `json:"id"`
}

// FetchModels calls the provider's /v1/models endpoint and returns the list of model IDs.
// apiKey may be empty for providers that don't require authentication for model listing.
func FetchModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	url := baseURL + "/v1/models"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create models request")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch models")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("unexpected status %d from models endpoint", resp.StatusCode)
	}

	var result modelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, errors.Wrap(err, "failed to decode models response")
	}

	ids := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("no models returned from %s", url)
	}

	return ids, nil
}
