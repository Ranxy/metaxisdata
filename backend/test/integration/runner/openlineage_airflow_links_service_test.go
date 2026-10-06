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

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// The Airflow links are derived from a facet the producer writes - anyone with
// an ingestion key can set it - and every client binds them to an href, so a
// `javascript:` URL would run in the origin of the member who opens the run.
// This drives the real server end to end: ingest such an event and require the
// API to hand back no link at all, while a real web address still comes back
// with its Dag URL derived.
func TestOpenLineageAirflowLinksRejectNonWebSchemesRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 10 * time.Second}

	key, _, err := env.Store.CreateOpenLineageAPIKey(ctx, "integration-airflow-links", "integration-test", "")
	require.NoError(t, err)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	namespace := "integration-airflow-links-ns-" + suffix

	post := func(t *testing.T, jobName, logURL string) {
		t.Helper()

		body, err := json.Marshal(map[string]any{
			"eventType": "COMPLETE",
			"eventTime": time.Now().UTC().Format(time.RFC3339Nano),
			"run": map[string]any{
				"runId": "run-" + jobName,
				"facets": map[string]any{
					"airflow": map[string]any{
						"taskInstance": map[string]any{"log_url": logURL},
					},
				},
			},
			"job":      map[string]any{"namespace": namespace, "name": jobName},
			"producer": "integration-test",
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
	}

	runOf := func(t *testing.T, jobName string) *v1pb.OpenLineageRun {
		t.Helper()

		client := v1connect.NewOpenLineageServiceClient(httpClient, env.BaseURL)
		resp, err := client.ListOpenLineageRuns(ctx, withToken(env.AdminToken(), &v1pb.ListOpenLineageRunsRequest{
			JobNamespace: namespace,
			JobName:      jobName,
		}))
		require.NoError(t, err)
		require.Len(t, resp.Msg.GetRuns(), 1, "the ingested run must be served back")
		return resp.Msg.GetRuns()[0]
	}

	t.Run("a javascript facet yields no link", func(t *testing.T) {
		t.Parallel()

		post(t, "injected", "javascript:alert(document.cookie)")

		run := runOf(t, "injected")
		require.Empty(t, run.GetAirflowRunLogUrl())
		require.Empty(t, run.GetAirflowDagUrl())
	})

	t.Run("a web facet still yields its links", func(t *testing.T) {
		t.Parallel()

		post(t, "real", "http://airflow.example.com:8080/dags/datax/runs/manual__1/tasks/sync?try_number=1")

		run := runOf(t, "real")
		require.Equal(t, "http://airflow.example.com:8080/dags/datax/runs/manual__1/tasks/sync?try_number=1", run.GetAirflowRunLogUrl())
		require.Equal(t, "http://airflow.example.com:8080/dags/datax", run.GetAirflowDagUrl())
	})
}
