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

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/plugin/openlineage"
)

// The dataset pages used to derive their list, detail and filter menus by
// JSON-parsing the raw payload of every recent run, so ingesting a few thousand
// maximum-size events was enough to make one read request allocate tens of
// gigabytes. This drives the real server end to end: ingestion stores the
// references, the pages aggregate them, and the per-event caps refuse the events
// that would have fed the read.
func TestOpenLineageDatasetPagesReadMaterializedReferencesRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 30 * time.Second}

	key, _, err := env.Store.CreateOpenLineageAPIKey(ctx, "integration-dataset-refs", "integration-test", "")
	require.NoError(t, err)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	namespace := "integration-dataset-ns-" + suffix
	orders := "public.orders-" + suffix
	daily := "public.daily_orders-" + suffix
	base := time.Now().UTC().Add(-time.Hour)

	post := func(t *testing.T, event map[string]any) int {
		t.Helper()
		body, err := json.Marshal(event)
		require.NoError(t, err)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.BaseURL+"/api/v1/lineage", bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		return resp.StatusCode
	}

	event := func(runID, jobName string, at time.Time) map[string]any {
		return map[string]any{
			"eventType": "COMPLETE",
			"eventTime": at.Format(time.RFC3339Nano),
			"run": map[string]any{
				"runId": runID,
				"facets": map[string]any{
					"airflow": map[string]any{"taskInstance": map[string]any{"log_url": "https://airflow.example.com/dags/d/runs/" + runID}},
				},
			},
			"job": map[string]any{
				"namespace": namespace,
				"name":      jobName,
				"facets":    map[string]any{"jobType": map[string]any{"jobType": "TASK", "integration": "airflow"}},
			},
			"producer": "integration-test",
			"inputs": []map[string]any{{
				"namespace": namespace,
				"name":      orders,
				"facets": map[string]any{
					"schema": map[string]any{"fields": []map[string]any{
						{"name": "order_id", "type": "INT"},
						{"name": "amount", "type": "NUMERIC"},
					}},
				},
			}},
			"outputs": []map[string]any{{
				"namespace": namespace,
				"name":      daily,
				"facets": map[string]any{
					"schema": map[string]any{"fields": []map[string]any{
						{"name": "order_id", "type": "INT"},
						{"name": "total", "type": "NUMERIC"},
					}},
					"columnLineage": map[string]any{"fields": map[string]any{
						"order_id": map[string]any{"inputFields": []map[string]any{
							{"namespace": namespace, "name": orders, "field": "order_id"},
						}},
					}},
				},
			}},
		}
	}

	require.Equal(t, http.StatusOK, post(t, event("run-1", "job-a", base)))
	require.Equal(t, http.StatusOK, post(t, event("run-2", "job-b", base.Add(time.Minute))))

	client := v1connect.NewOpenLineageServiceClient(httpClient, env.BaseURL)

	// The two datasets are aggregated from the stored references: the input was
	// read by both jobs, the output written by both, and only the output carries
	// the column-lineage facet.
	list, err := client.ListOpenLineageDatasets(ctx, withToken(env.AdminToken(), &v1pb.ListOpenLineageDatasetsRequest{Namespace: namespace}))
	require.NoError(t, err)
	require.Len(t, list.Msg.GetDatasets(), 2)

	byName := make(map[string]*v1pb.OpenLineageDatasetResource, 2)
	for _, dataset := range list.Msg.GetDatasets() {
		byName[dataset.GetName()] = dataset
	}

	input := byName[orders]
	require.NotNil(t, input, "the input dataset must be listed")
	require.Equal(t, int32(2), input.GetSourceJobCount())
	require.Equal(t, int32(0), input.GetTargetJobCount())
	require.False(t, input.GetInternal())
	require.False(t, input.GetSupportsColumnLineage())
	require.Equal(t, openlineage.FormatExternalGUID(namespace, orders), input.GetGuid())

	output := byName[daily]
	require.NotNil(t, output, "the output dataset must be listed")
	require.Equal(t, int32(0), output.GetSourceJobCount())
	require.Equal(t, int32(2), output.GetTargetJobCount())
	require.True(t, output.GetSupportsColumnLineage())
	require.Equal(t, []string{"airflow"}, output.GetIntegrations())
	require.Equal(t, []string{"openlineage"}, output.GetSources())
	require.NotNil(t, output.GetLastSeen())
	require.Equal(t, base.Add(time.Minute).Unix(), output.GetLastSeen().AsTime().Unix())

	// The detail reads the same references: the schema the widest run stated,
	// the column-lineage readiness, the jobs and the recent runs.
	detail, err := client.GetOpenLineageDataset(ctx, withToken(env.AdminToken(), &v1pb.GetOpenLineageDatasetRequest{Guid: output.GetGuid()}))
	require.NoError(t, err)
	require.Equal(t, daily, detail.Msg.GetDataset().GetName())
	require.Equal(t, int32(2), detail.Msg.GetDataset().GetTargetJobCount())
	require.True(t, detail.Msg.GetDataset().GetSupportsColumnLineage())

	require.Len(t, detail.Msg.GetSchemaFields(), 2)
	require.Equal(t, "order_id", detail.Msg.GetSchemaFields()[0].GetName())
	require.True(t, detail.Msg.GetSchemaFields()[0].GetColumnLineageReady())
	require.Equal(t, "total", detail.Msg.GetSchemaFields()[1].GetName())
	require.False(t, detail.Msg.GetSchemaFields()[1].GetColumnLineageReady())

	require.Len(t, detail.Msg.GetRelatedJobs(), 2)
	require.Len(t, detail.Msg.GetRecentRuns(), 2)
	require.Equal(t, "run-2", detail.Msg.GetRecentRuns()[0].GetRunId())
	require.True(t, detail.Msg.GetRecentRuns()[0].GetWritesDataset())

	// The filter menus take the dataset dimensions from the references too.
	values, err := env.Store.ListOpenLineageDatasetFilterValues(ctx)
	require.NoError(t, err)
	var datasetNamespaces []string
	for _, value := range values {
		require.NotEmpty(t, value.Value)
		require.Positive(t, value.Count)
		if value.Dimension == "dataset_namespace" {
			datasetNamespaces = append(datasetNamespaces, value.Value)
		}
	}
	require.Contains(t, datasetNamespaces, namespace)

	// The run list and the task list render the Airflow link from the column
	// ingestion wrote, and the run detail still returns the payload.
	runs, err := client.ListOpenLineageRuns(ctx, withToken(env.AdminToken(), &v1pb.ListOpenLineageRunsRequest{JobNamespace: namespace}))
	require.NoError(t, err)
	require.Len(t, runs.Msg.GetRuns(), 2)
	for _, run := range runs.Msg.GetRuns() {
		require.Empty(t, run.GetRawPayload(), "the run list must not carry raw payloads")
		require.Equal(t, "https://airflow.example.com/dags/d", run.GetAirflowDagUrl())
		require.Contains(t, run.GetAirflowRunLogUrl(), "/dags/d/runs/")
	}

	single, err := client.GetOpenLineageRun(ctx, withToken(env.AdminToken(), &v1pb.GetOpenLineageRunRequest{Name: runs.Msg.GetRuns()[0].GetName()}))
	require.NoError(t, err)
	require.NotEmpty(t, single.Msg.GetRawPayload(), "the run detail still serves the payload")

	tasks, err := client.ListOpenLineageTasks(ctx, withToken(env.AdminToken(), &v1pb.ListOpenLineageTasksRequest{JobNamespace: namespace}))
	require.NoError(t, err)
	require.Len(t, tasks.Msg.GetTasks(), 2)
	for _, task := range tasks.Msg.GetTasks() {
		require.Equal(t, "https://airflow.example.com/dags/d", task.GetAirflowDagUrl())
	}

	// An event over the per-event size limit is refused before it is stored: the
	// body is still well under the request limit, so the per-event cap is what
	// catches it.
	t.Run("a single event over the per-event limit is refused", func(t *testing.T) {
		t.Parallel()
		oversized := event("run-huge", "job-huge", base)
		oversized["padding"] = strings.Repeat("x", openlineage.MaxEventSize)
		require.Equal(t, http.StatusRequestEntityTooLarge, post(t, oversized))
	})

	// An identity too long for the unique index is an invalid event, not a
	// permanently failing one.
	t.Run("an overlong identity is an invalid event", func(t *testing.T) {
		t.Parallel()
		overlong := event("run-long", strings.Repeat("x", openlineage.MaxJobNameLength+1), base)
		require.Equal(t, http.StatusBadRequest, post(t, overlong))
	})

	// Deleting a run takes its references with it (this is what the retention
	// prune does), so the dataset page stops offering what no run references.
	_, err = env.Store.GetDB().ExecContext(ctx, `DELETE FROM openlineage_run WHERE job_namespace = $1`, namespace)
	require.NoError(t, err)

	var remaining int
	require.NoError(t, env.Store.GetDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM openlineage_run_dataset WHERE namespace = $1`, namespace,
	).Scan(&remaining))
	require.Zero(t, remaining, "the run's references must cascade with the run")

	list, err = client.ListOpenLineageDatasets(ctx, withToken(env.AdminToken(), &v1pb.ListOpenLineageDatasetsRequest{Namespace: namespace}))
	require.NoError(t, err)
	require.Empty(t, list.Msg.GetDatasets())
}
