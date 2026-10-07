//go:build integration

package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/store"
)

// The task list joins each task's latest run. When that run is gone - pruned by
// retention, or removed by hand while the task summary survived - the join
// leaves every latest-run column NULL, and scanning NULL into a string aborted
// the whole listing with an `internal` error: one stale task made the Tasks page
// unusable. This drives the real server and the real store, deletes the run out
// of band, and requires the listing to keep working with the dangling pointer
// reported as "no latest run" instead of crashing.
func TestOpenLineageTaskListSurvivesAPrunedLatestRunRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 10 * time.Second}

	key, _, err := env.Store.CreateOpenLineageAPIKey(ctx, "integration-task-list", "integration-test", "")
	require.NoError(t, err)

	// Unique per run: the shared env keeps its state across tests.
	uniqueNano := time.Now().UnixNano()
	namespace := fmt.Sprintf("integration-task-list-ns-%d", uniqueNano)
	jobName := fmt.Sprintf("integration-task-list-job-%d", uniqueNano)
	runID := fmt.Sprintf("integration-task-list-run-%d", uniqueNano)

	event := map[string]any{
		"eventType": "COMPLETE",
		"eventTime": time.Now().UTC().Format(time.RFC3339Nano),
		"run":       map[string]any{"runId": runID},
		"job":       map[string]any{"namespace": namespace, "name": jobName},
		"producer":  "integration-test",
		"inputs":    []map[string]any{{"namespace": namespace, "name": "in-table"}},
		"outputs":   []map[string]any{{"namespace": namespace, "name": "out-table"}},
	}
	body, err := json.Marshal(event)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.BaseURL+"/api/v1/lineage", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	task := func(t *testing.T) *store.OpenLineageTaskMessage {
		t.Helper()
		ns, name := namespace, jobName
		list, err := env.Store.ListOpenLineageTask(ctx, &store.FindOpenLineageTaskMessage{
			JobNamespace: &ns,
			JobName:      &name,
		})
		require.NoError(t, err, "listing tasks must survive a task whose latest run is gone")
		require.Len(t, list, 1)
		return list[0]
	}

	ingested := task(t)
	require.NotEmpty(t, ingested.LatestRunGUID)
	require.Equal(t, "COMPLETE", ingested.LatestEventType)

	// Remove the run behind the task's back, exactly as a partial deletion does.
	_, err = env.Store.GetDB().ExecContext(ctx, `DELETE FROM openlineage_run WHERE run_id = $1`, runID)
	require.NoError(t, err)

	dangling := task(t)
	require.Empty(t, dangling.LatestRunGUID, "a task must not point at a run the API cannot serve")
	require.Empty(t, dangling.LatestRunID)
	require.Empty(t, dangling.LatestEventType)
	require.Empty(t, dangling.LatestAirflowRunLogURL)
	// The aggregate the task stored itself is still there.
	require.Equal(t, int32(1), dangling.RunCount)
}
