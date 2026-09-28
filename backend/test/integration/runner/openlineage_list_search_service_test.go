//go:build integration

package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/store"
)

// The list pages moved their free-text box out of the browser and onto the
// query, so the substring match is now SQL: POSITION over a concat_ws haystack.
// Only PostgreSQL can parse that, which is why this drives the real store rather
// than a hermetic unit test.
//
// It also pins the two properties the browser filter had and an ILIKE would have
// quietly changed: the match is case-insensitive, and a % or _ in the input is a
// literal character instead of a wildcard.
func TestOpenLineageListSearchMatchesSubstringRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 10 * time.Second}

	key, _, err := env.Store.CreateOpenLineageAPIKey(ctx, "integration-list-search", "integration-test", "")
	require.NoError(t, err)

	// Unique per run: the shared env keeps its state across tests.
	uniqueNano := time.Now().UnixNano()
	alphaNS := fmt.Sprintf("search-alpha-ns-%d", uniqueNano)
	alphaJob := fmt.Sprintf("search-alpha-job-%d", uniqueNano)
	alphaRun := fmt.Sprintf("search-alpha-run-%d", uniqueNano)
	betaNS := fmt.Sprintf("search-beta-ns-%d", uniqueNano)
	betaJob := fmt.Sprintf("search-beta-job-%d", uniqueNano)
	betaRun := fmt.Sprintf("search-beta-run-%d", uniqueNano)

	event := func(namespace, jobName, runID string) map[string]any {
		return map[string]any{
			"eventType": "COMPLETE",
			"eventTime": time.Now().UTC().Format(time.RFC3339Nano),
			"run":       map[string]any{"runId": runID},
			"job":       map[string]any{"namespace": namespace, "name": jobName},
			"producer":  "integration-test",
			"inputs":    []map[string]any{{"namespace": namespace, "name": "in-table"}},
			"outputs":   []map[string]any{{"namespace": namespace, "name": "out-table"}},
		}
	}

	body, err := json.Marshal([]map[string]any{
		event(alphaNS, alphaJob, alphaRun),
		event(betaNS, betaJob, betaRun),
	})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.BaseURL+"/api/v1/lineage/batch", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	searchRuns := func(t *testing.T, search string) []*store.OpenLineageRunMessage {
		t.Helper()
		list, err := env.Store.ListOpenLineageRun(ctx, &store.FindOpenLineageRunMessage{Search: &search})
		require.NoError(t, err)
		return list
	}
	searchTasks := func(t *testing.T, search string) []*store.OpenLineageTaskMessage {
		t.Helper()
		list, err := env.Store.ListOpenLineageTask(ctx, &store.FindOpenLineageTaskMessage{Search: &search})
		require.NoError(t, err)
		return list
	}

	// A term from the job name finds that job's run, and only that one: the
	// namespace of the other event carries the other prefix.
	matched := searchRuns(t, alphaJob)
	require.Len(t, matched, 1)
	require.Equal(t, alphaRun, matched[0].RunID)

	// The same term in upper case finds the same row.
	require.Len(t, searchRuns(t, strings.ToUpper(alphaJob)), 1)

	// A prefix that both the namespace and the job name of one event carry still
	// matches once, and never reaches the other event.
	byPrefix := searchRuns(t, "search-alpha-")
	require.Len(t, byPrefix, 1)
	require.Equal(t, alphaRun, byPrefix[0].RunID)

	// '%' is an ordinary character. Under ILIKE this would match the row.
	require.Empty(t, searchRuns(t, alphaRun+"%"))
	require.Empty(t, searchTasks(t, alphaJob+"%"))

	matchedTasks := searchTasks(t, alphaJob)
	require.Len(t, matchedTasks, 1)
	require.Equal(t, alphaJob, matchedTasks[0].JobName)

	// The term also reaches a task through its namespace and its run id.
	require.Len(t, searchTasks(t, alphaNS), 1)
	require.Len(t, searchRuns(t, alphaRun), 1)
}

// The filter menus read one UNION ALL across both OpenLineage tables, which only
// PostgreSQL can parse, so this drives the real store. The shared environment
// carries other tests' rows, so this asserts membership and shape rather than
// exact counts.
func TestOpenLineageFilterValuesRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 10 * time.Second}

	key, _, err := env.Store.CreateOpenLineageAPIKey(ctx, "integration-filter-values", "integration-test", "")
	require.NoError(t, err)

	uniqueNano := time.Now().UnixNano()
	namespace := fmt.Sprintf("filter-values-ns-%d", uniqueNano)
	jobName := fmt.Sprintf("filter-values-job-%d", uniqueNano)

	body, err := json.Marshal(map[string]any{
		// Only COMPLETE events become rows; a FAIL event is skipped outright.
		"eventType": "COMPLETE",
		"eventTime": time.Now().UTC().Format(time.RFC3339Nano),
		"run":       map[string]any{"runId": fmt.Sprintf("filter-values-run-%d", uniqueNano)},
		"job": map[string]any{
			"namespace": namespace,
			"name":      jobName,
			// Job type, integration and processing type all live in this facet.
			"facets": map[string]any{
				"jobType": map[string]any{"jobType": "TASK", "integration": "airflow", "processingType": "BATCH"},
			},
		},
		"producer": "integration-test",
		"inputs":   []map[string]any{{"namespace": namespace, "name": "in-table"}},
	})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.BaseURL+"/api/v1/lineage", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	values, err := env.Store.ListOpenLineageFilterValues(ctx)
	require.NoError(t, err)

	dimension := func(name string) []*store.OpenLineageFilterValue {
		t.Helper()
		var found []*store.OpenLineageFilterValue
		for _, value := range values {
			require.NotEmpty(t, value.Value, "an empty value must never reach a filter menu")
			require.Positive(t, value.Count)
			if value.Dimension == name {
				found = append(found, value)
			}
		}
		return found
	}

	// The run just ingested shows up in each dimension it feeds, read from the
	// run table (namespace, event type, source) and the task table (job type).
	require.Contains(t, filterValueNames(dimension("job_namespace")), namespace)
	require.Contains(t, filterValueNames(dimension("job_type")), "TASK")
	require.Contains(t, filterValueNames(dimension("event_type")), "COMPLETE")
	require.Contains(t, filterValueNames(dimension("source")), "openlineage")

	// Each dimension arrives most common first, which is the order the store
	// promises and the menus rely on.
	for _, name := range []string{"job_namespace", "job_type", "event_type", "source"} {
		found := dimension(name)
		for i := 1; i < len(found); i++ {
			require.GreaterOrEqual(t, found[i-1].Count, found[i].Count)
		}
	}
}

func filterValueNames(values []*store.OpenLineageFilterValue) []string {
	names := make([]string, 0, len(values))
	for _, value := range values {
		names = append(names, value.Value)
	}
	return names
}
