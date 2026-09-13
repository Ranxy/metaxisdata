//go:build integration

package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/plugin/openlineage"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// Ingestion maintains each task's counters incrementally instead of re-reading
// every run, so this drives the real server and checks that a redelivered run is
// not counted twice and that the stored latest run follows event_time.
func TestOpenLineageIngestionAggregatesRunsRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 10 * time.Second}

	key, _, err := env.Store.CreateOpenLineageAPIKey(ctx, "integration-test", "integration-test", "")
	require.NoError(t, err)

	namespace, jobName := "integration-aggregate-ns", "integration-aggregate-job"
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	event := func(runID string, at time.Time, withLineage bool) map[string]any {
		built := map[string]any{
			"eventType": "COMPLETE",
			"eventTime": at.Format(time.RFC3339Nano),
			"run":       map[string]any{"runId": runID},
			"job":       map[string]any{"namespace": namespace, "name": jobName},
			"producer":  "integration-test",
		}
		if withLineage {
			built["inputs"] = []map[string]any{{"namespace": namespace, "name": "in-table"}}
			built["outputs"] = []map[string]any{{"namespace": namespace, "name": "out-table"}}
		}
		return built
	}

	post := func(t *testing.T, events ...map[string]any) int {
		t.Helper()
		body, err := json.Marshal(events)
		require.NoError(t, err)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.BaseURL+"/api/v1/lineage/batch", bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		return resp.StatusCode
	}

	task := func(t *testing.T) *store.OpenLineageTaskMessage {
		t.Helper()
		ns, name, jobType := namespace, jobName, "UNSPECIFIED"
		got, err := env.Store.GetOpenLineageTask(ctx, &store.FindOpenLineageTaskMessage{
			JobNamespace: &ns,
			JobName:      &name,
			JobType:      &jobType,
		})
		require.NoError(t, err)
		require.NotNil(t, got)
		return got
	}

	// Two runs: run-1 has no lineage signal, run-2 is the most recent.
	require.Equal(t, http.StatusOK, post(t, event("run-1", base, false), event("run-2", base.Add(time.Hour), false)))
	aggregate := task(t)
	require.Equal(t, int32(2), aggregate.RunCount)
	require.Equal(t, int32(0), aggregate.LineageRunCount)
	require.Equal(t, "run-2", aggregate.LatestRunID)
	require.NotNil(t, aggregate.LatestEventTime)

	// Redelivering run-1 must not count it twice, and its older event time must
	// not displace the stored latest run.
	require.Equal(t, http.StatusOK, post(t, event("run-1", base, false)))
	aggregate = task(t)
	require.Equal(t, int32(2), aggregate.RunCount)
	require.Equal(t, "run-2", aggregate.LatestRunID)

	// A run older than the stored latest counts, but does not become latest.
	require.Equal(t, http.StatusOK, post(t, event("run-3", base.Add(-time.Hour), false)))
	aggregate = task(t)
	require.Equal(t, int32(3), aggregate.RunCount)
	require.Equal(t, "run-2", aggregate.LatestRunID)

	// A newer run carrying datasets becomes latest and adds to the lineage count.
	require.Equal(t, http.StatusOK, post(t, event("run-4", base.Add(2*time.Hour), true)))
	aggregate = task(t)
	require.Equal(t, int32(4), aggregate.RunCount)
	require.Equal(t, int32(1), aggregate.LineageRunCount)
	require.Equal(t, "run-4", aggregate.LatestRunID)
	require.Equal(t, base.Add(2*time.Hour).Unix(), aggregate.LatestEventTime.Unix())

	// Resolving the same dataset again must not rewrite its row.
	datasetName := "in-table"
	dataset, err := env.Store.GetExternalDataset(ctx, &store.FindExternalDatasetMessage{
		Namespace: &namespace,
		Name:      &datasetName,
	})
	require.NoError(t, err)
	require.NotNil(t, dataset)
	require.Equal(t, http.StatusOK, post(t, event("run-5", base.Add(3*time.Hour), true)))
	resolvedAgain, err := env.Store.GetExternalDataset(ctx, &store.FindExternalDatasetMessage{
		Namespace: &namespace,
		Name:      &datasetName,
	})
	require.NoError(t, err)
	require.NotNil(t, resolvedAgain)
	require.Equal(t, dataset.ID, resolvedAgain.ID)
	require.Equal(t, dataset.DatasetType, resolvedAgain.DatasetType)
	require.Equal(t, dataset.UpdatedAt, resolvedAgain.UpdatedAt, "an unchanged dataset must not be rewritten")

	aggregate = task(t)
	require.Equal(t, int32(5), aggregate.RunCount)
	require.Equal(t, int32(2), aggregate.LineageRunCount)
	require.Equal(t, "run-5", aggregate.LatestRunID)

	// Redelivering the lineage run with its datasets removed must decrement the
	// lineage counter rather than leave it stale.
	require.Equal(t, http.StatusOK, post(t, event("run-4", base.Add(2*time.Hour), false)))
	aggregate = task(t)
	require.Equal(t, int32(5), aggregate.RunCount)
	require.Equal(t, int32(1), aggregate.LineageRunCount)

	// A key scoped to another namespace cannot write into this one.
	scopedKey, _, err := env.Store.CreateOpenLineageAPIKey(ctx, "integration-scoped", "integration-test", "some-other-ns")
	require.NoError(t, err)
	body, err := json.Marshal([]map[string]any{event("run-6", base, false)})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.BaseURL+"/api/v1/lineage/batch", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+scopedKey)
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// A batch is persisted in the caller's order even though its tasks are locked in
// a canonical order. Getting that wrong attaches one event's lineage to another
// event's run, so this drives a batch whose task order differs from its event
// order and checks the lineage landed on the run that carried the datasets.
func TestOpenLineageBatchKeepsEventOrderRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 10 * time.Second}

	key, _, err := env.Store.CreateOpenLineageAPIKey(ctx, "integration-order", "integration-test", "")
	require.NoError(t, err)

	namespace := "integration-order-ns"
	// "job-z" sorts after "job-a", so the canonical task order is the reverse of
	// the event order.
	event := func(jobName, runID string, withLineage bool) map[string]any {
		built := map[string]any{
			"eventType": "COMPLETE",
			"eventTime": time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
			"run":       map[string]any{"runId": runID},
			"job":       map[string]any{"namespace": namespace, "name": jobName},
			"producer":  "integration-test",
		}
		if withLineage {
			built["inputs"] = []map[string]any{{"namespace": namespace, "name": "ordered-in"}}
			built["outputs"] = []map[string]any{{"namespace": namespace, "name": "ordered-out"}}
		}
		return built
	}

	// The first event carries the datasets; the second, alphabetically earlier
	// job is locked first if the batch is reordered.
	body, err := json.Marshal([]map[string]any{
		event("job-z", "run-z", true),
		event("job-a", "run-a", false),
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

	runGUID := openlineage.BuildOpenLineageRunGUID(namespace, "job-z", "UNSPECIFIED", "run-z")
	relations := env.WaitForContextLineage(ctx, t, runGUID, v1pb.MetaType_OPENLINEAGE, func(relations []*v1pb.LineageRelation) bool {
		return len(relations) > 0
	})
	require.NotEmpty(t, relations)
}
