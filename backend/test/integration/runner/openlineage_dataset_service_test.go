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

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/plugin/openlineage"
	"github.com/Ranxy/metaxisdata/backend/store"
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

	t.Run("a single event over the per-event limit is refused", func(t *testing.T) {
		t.Parallel()

		// An event over the per-event size limit is refused before it is stored:
		// the body is still well under the request limit, so the per-event cap is
		// what catches it.
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
}

// The run and task lists read the values an event wrote into the run row, so
// every one of them has to be bounded; and the facets the dataset detail expands
// are stored capped, because one dataset's facets otherwise make that read
// unbounded. This drives the real server and inspects what was stored.
func TestOpenLineageIngestionBoundsTheStoredValuesRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 30 * time.Second}

	key, _, err := env.Store.CreateOpenLineageAPIKey(ctx, "integration-stored-bounds", "integration-test", "")
	require.NoError(t, err)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	namespace := "integration-bounds-ns-" + suffix
	base := time.Now().UTC().Add(-time.Minute)

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
			"run":       map[string]any{"runId": runID},
			"job":       map[string]any{"namespace": namespace, "name": jobName},
			"producer":  "integration-test",
		}
	}

	client := v1connect.NewOpenLineageServiceClient(httpClient, env.BaseURL)

	storedRun := func(t *testing.T, jobName string) *store.OpenLineageRunMessage {
		t.Helper()
		stored, err := env.Store.GetOpenLineageRun(ctx, &store.FindOpenLineageRunMessage{
			JobNamespace: &namespace,
			JobName:      &jobName,
		})
		require.NoError(t, err)
		require.NotNil(t, stored)
		return stored
	}

	// An Airflow link over the cap is dropped instead of stored, but the event
	// itself is accepted and its payload is still served.
	t.Run("an over-long Airflow link is dropped", func(t *testing.T) {
		t.Parallel()

		overlong := event("run-long-link", "job-long-link", base)
		overlong["run"].(map[string]any)["facets"] = map[string]any{
			"airflow": map[string]any{"taskInstance": map[string]any{
				"log_url": "https://airflow.example.com/dags/big/" + strings.Repeat("z", openlineage.MaxAirflowRunLogURLLength),
			}},
		}
		require.Equal(t, http.StatusOK, post(t, overlong))

		require.Empty(t, storedRun(t, "job-long-link").AirflowRunLogURL)

		runs, err := client.ListOpenLineageRuns(ctx, withToken(env.AdminToken(), &v1pb.ListOpenLineageRunsRequest{
			JobNamespace: namespace,
			JobName:      "job-long-link",
		}))
		require.NoError(t, err)
		require.Len(t, runs.Msg.GetRuns(), 1)
		require.Empty(t, runs.Msg.GetRuns()[0].GetAirflowRunLogUrl())

		detail, err := client.GetOpenLineageRun(ctx, withToken(env.AdminToken(), &v1pb.GetOpenLineageRunRequest{
			Name: runs.Msg.GetRuns()[0].GetName(),
		}))
		require.NoError(t, err)
		require.NotEmpty(t, detail.Msg.GetRawPayload(), "the event itself is still stored")
	})

	// A value over its cap is an invalid event, whether it is the producer or a
	// columnLineage string that reaches an indexed column.
	t.Run("an over-long value is an invalid event", func(t *testing.T) {
		t.Parallel()

		overlongProducer := event("run-long-producer", "job-long-producer", base)
		overlongProducer["producer"] = strings.Repeat("p", openlineage.MaxProducerLength+1)
		require.Equal(t, http.StatusBadRequest, post(t, overlongProducer))

		overlongEventType := event("run-long-type", "job-long-type", base)
		overlongEventType["eventType"] = strings.Repeat("e", openlineage.MaxEventTypeLength+1)
		require.Equal(t, http.StatusBadRequest, post(t, overlongEventType))

		overlongColumn := event("run-long-column", "job-long-column", base)
		overlongColumn["outputs"] = []map[string]any{{
			"namespace": namespace,
			"name":      "out-long-column-" + suffix,
			"facets": map[string]any{"columnLineage": map[string]any{"fields": map[string]any{
				strings.Repeat("c", openlineage.MaxColumnNameLength+1): map[string]any{},
			}}},
		}}
		require.Equal(t, http.StatusBadRequest, post(t, overlongColumn))
	})

	// The schema facet stored for the detail is capped: the event that carries a
	// very wide schema is accepted, and the detail still serves the fields that
	// fit instead of expanding an unbounded JSONB.
	t.Run("a very wide schema is stored capped", func(t *testing.T) {
		t.Parallel()

		wideName := "public.wide-" + suffix
		fields := make([]map[string]any, 0, 3000)
		for i := range 3000 {
			fields = append(fields, map[string]any{
				"name":        fmt.Sprintf("column_%04d", i),
				"type":        "VARCHAR(255)",
				"description": strings.Repeat("d", 40),
			})
		}

		wide := event("run-wide", "job-wide", base)
		wide["outputs"] = []map[string]any{{
			"namespace": namespace,
			"name":      wideName,
			"facets":    map[string]any{"schema": map[string]any{"fields": fields}},
		}}
		require.Equal(t, http.StatusOK, post(t, wide))

		list, err := client.ListOpenLineageDatasets(ctx, withToken(env.AdminToken(), &v1pb.ListOpenLineageDatasetsRequest{
			Namespace: namespace,
			Search:    wideName,
		}))
		require.NoError(t, err)
		require.Len(t, list.Msg.GetDatasets(), 1)

		detail, err := client.GetOpenLineageDataset(ctx, withToken(env.AdminToken(), &v1pb.GetOpenLineageDatasetRequest{
			Guid: list.Msg.GetDatasets()[0].GetGuid(),
		}))
		require.NoError(t, err)
		require.NotEmpty(t, detail.Msg.GetSchemaFields())
		require.Less(t, len(detail.Msg.GetSchemaFields()), 3000, "the fields that do not fit are dropped")
	})

	// One event may name many datasets; the references are written in chunks, and
	// the dataset page lists them all.
	t.Run("many datasets in one event are all referenced", func(t *testing.T) {
		t.Parallel()

		namespaceMany := namespace + "-many"
		many := event("run-many", "job-many", base)
		many["job"].(map[string]any)["namespace"] = namespaceMany
		outputs := make([]map[string]any, 0, 150)
		for i := range 150 {
			outputs = append(outputs, map[string]any{
				"namespace": namespaceMany,
				"name":      fmt.Sprintf("out-%03d", i),
			})
		}
		many["outputs"] = outputs
		require.Equal(t, http.StatusOK, post(t, many))

		list, err := client.ListOpenLineageDatasets(ctx, withToken(env.AdminToken(), &v1pb.ListOpenLineageDatasetsRequest{
			Namespace: namespaceMany,
			PageSize:  200,
		}))
		require.NoError(t, err)
		require.Len(t, list.Msg.GetDatasets(), 150)

		// The aggregate maintenance is batched, so a redelivery that drops most of
		// them has to take their rows back out at scale: only the datasets the run
		// still writes may remain, and the ones it dropped may not be left behind.
		many["outputs"] = outputs[:100]
		require.Equal(t, http.StatusOK, post(t, many))

		list, err = client.ListOpenLineageDatasets(ctx, withToken(env.AdminToken(), &v1pb.ListOpenLineageDatasetsRequest{
			Namespace: namespaceMany,
			PageSize:  200,
		}))
		require.NoError(t, err)
		require.Len(t, list.Msg.GetDatasets(), 100)

		var dropped int
		require.NoError(t, env.Store.GetDB().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM openlineage_dataset WHERE namespace = $1 AND name = $2`, namespaceMany, "out-149",
		).Scan(&dropped))
		require.Zero(t, dropped, "a dataset the redelivery dropped is removed")
	})

	// A group's value arrays are ordered, so a redelivery cannot reorder what the
	// page shows, and the newest run's integration is the one a job reports.
	t.Run("the value arrays are ordered", func(t *testing.T) {
		t.Parallel()

		shared := "public.shared-" + suffix
		for i, integration := range []string{"zeta", "alpha"} {
			built := event(fmt.Sprintf("run-shared-%d", i), fmt.Sprintf("job-shared-%d", i), base.Add(time.Duration(i)*time.Second))
			built["job"].(map[string]any)["facets"] = map[string]any{
				"jobType": map[string]any{"jobType": "TASK", "integration": integration},
			}
			built["outputs"] = []map[string]any{{"namespace": namespace, "name": shared}}
			require.Equal(t, http.StatusOK, post(t, built))
		}

		list, err := client.ListOpenLineageDatasets(ctx, withToken(env.AdminToken(), &v1pb.ListOpenLineageDatasetsRequest{
			Namespace: namespace,
			Search:    shared,
		}))
		require.NoError(t, err)
		require.Len(t, list.Msg.GetDatasets(), 1)
		require.Equal(t, []string{"alpha", "zeta"}, list.Msg.GetDatasets()[0].GetIntegrations())
		require.Equal(t, int32(2), list.Msg.GetDatasets()[0].GetTargetJobCount())

		detail, err := client.GetOpenLineageDataset(ctx, withToken(env.AdminToken(), &v1pb.GetOpenLineageDatasetRequest{
			Guid: list.Msg.GetDatasets()[0].GetGuid(),
		}))
		require.NoError(t, err)
		require.Len(t, detail.Msg.GetRelatedJobs(), 2)
		require.Equal(t, "alpha", detail.Msg.GetRelatedJobs()[0].GetIntegration(), "a job reports its newest run's integration")
	})

	// A link within the cap is stored as-is, and the columns a list reads stay
	// within the caps the validator enforces.
	t.Run("a link within the cap is stored", func(t *testing.T) {
		t.Parallel()

		logURL := "https://airflow.example.com/dags/ok/" + strings.Repeat("o", openlineage.MaxAirflowRunLogURLLength-len("https://airflow.example.com/dags/ok/"))
		withinCap := event("run-within-link", "job-within-link", base)
		withinCap["run"].(map[string]any)["facets"] = map[string]any{
			"airflow": map[string]any{"taskInstance": map[string]any{"log_url": logURL}},
		}
		require.Equal(t, http.StatusOK, post(t, withinCap))

		stored := storedRun(t, "job-within-link")
		require.Equal(t, logURL, stored.AirflowRunLogURL)
		require.LessOrEqual(t, len(stored.AirflowRunLogURL), openlineage.MaxAirflowRunLogURLLength)
		require.LessOrEqual(t, len(stored.Producer), openlineage.MaxProducerLength)
	})
}

// The dataset list, its filter menus and the detail's summary read an aggregate
// the ingest transaction maintains, so the aggregate has to move in both
// directions: a redelivery that keeps a reference must not count it twice, and
// one that drops a reference must take its contribution back out. This drives the
// real server and inspects both the aggregate rows and what the page shows.
func TestOpenLineageDatasetAggregateFollowsARedeliveryRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 30 * time.Second}

	key, _, err := env.Store.CreateOpenLineageAPIKey(ctx, "integration-dataset-redelivery", "integration-test", "")
	require.NoError(t, err)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	namespace := "integration-dataset-redelivery-ns-" + suffix
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

	// An event that reads the datasets in inputs and writes daily, with the
	// column-lineage facet on the output. A redelivery that drops the output passes
	// no outputs.
	event := func(runID, jobName string, at time.Time, inputs []map[string]any, outputs []string) map[string]any {
		written := make([]map[string]any, 0, len(outputs))
		for _, name := range outputs {
			written = append(written, map[string]any{
				"namespace": namespace,
				"name":      name,
				"facets": map[string]any{
					"columnLineage": map[string]any{"fields": map[string]any{
						"order_id": map[string]any{"inputFields": []map[string]any{
							{"namespace": namespace, "name": orders, "field": "order_id"},
						}},
					}},
				},
			})
		}
		return map[string]any{
			"eventType": "COMPLETE",
			"eventTime": at.Format(time.RFC3339Nano),
			"run":       map[string]any{"runId": runID},
			"job": map[string]any{
				"namespace": namespace,
				"name":      jobName,
				"facets":    map[string]any{"jobType": map[string]any{"jobType": "TASK", "integration": "airflow"}},
			},
			"producer": "integration-test",
			"inputs":   inputs,
			"outputs":  written,
		}
	}
	readOrders := []map[string]any{{"namespace": namespace, "name": orders}}

	aggregate := func(t *testing.T, name string) (refCount, sourceJobs, targetJobs, columnLineageRefs int64) {
		t.Helper()
		require.NoError(t, env.Store.GetDB().QueryRowContext(ctx, `
			SELECT ref_count, source_job_count, target_job_count, column_lineage_ref_count
			FROM openlineage_dataset WHERE namespace = $1 AND name = $2
		`, namespace, name).Scan(&refCount, &sourceJobs, &targetJobs, &columnLineageRefs))
		return refCount, sourceJobs, targetJobs, columnLineageRefs
	}

	client := v1connect.NewOpenLineageServiceClient(httpClient, env.BaseURL)

	require.Equal(t, http.StatusOK, post(t, event("run-1", "job-a", base, readOrders, []string{daily})))
	require.Equal(t, http.StatusOK, post(t, event("run-2", "job-b", base.Add(time.Minute), readOrders, []string{daily})))

	refCount, sourceJobs, targetJobs, columnLineageRefs := aggregate(t, orders)
	require.Equal(t, int64(2), refCount)
	require.Equal(t, int64(2), sourceJobs)
	require.Equal(t, int64(0), targetJobs)
	require.Equal(t, int64(0), columnLineageRefs)

	refCount, _, targetJobs, columnLineageRefs = aggregate(t, daily)
	require.Equal(t, int64(2), refCount)
	require.Equal(t, int64(2), targetJobs)
	require.Equal(t, int64(2), columnLineageRefs, "both runs state the facet")

	var members int
	require.NoError(t, env.Store.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM openlineage_dataset_member
		WHERE namespace = $1 AND name = $2 AND kind = 'task:output'
	`, namespace, daily).Scan(&members))
	require.Equal(t, 2, members, "one member row per job, not per run")

	// Redelivering run-2 — the newest one — without the output takes its reference
	// back out of the aggregate: the job count and the reference count move back,
	// and last-seen has to fall back to the reference that remains rather than stay
	// on the one that went away.
	require.Equal(t, http.StatusOK, post(t, event("run-2", "job-b", base.Add(time.Minute), readOrders, nil)))

	refCount, _, targetJobs, columnLineageRefs = aggregate(t, daily)
	require.Equal(t, int64(1), refCount, "the dropped reference must not be counted")
	require.Equal(t, int64(1), targetJobs)
	require.Equal(t, int64(1), columnLineageRefs, "run-1 still states the facet")

	var lastSeen time.Time
	require.NoError(t, env.Store.GetDB().QueryRowContext(ctx,
		`SELECT last_seen FROM openlineage_dataset WHERE namespace = $1 AND name = $2`, namespace, daily,
	).Scan(&lastSeen))
	require.Equal(t, base.Unix(), lastSeen.UTC().Unix(), "the newest remaining reference sets last-seen")

	list, err := client.ListOpenLineageDatasets(ctx, withToken(env.AdminToken(), &v1pb.ListOpenLineageDatasetsRequest{
		Namespace: namespace,
		Search:    daily,
	}))
	require.NoError(t, err)
	require.Len(t, list.Msg.GetDatasets(), 1)
	require.Equal(t, int32(1), list.Msg.GetDatasets()[0].GetTargetJobCount())
	require.True(t, list.Msg.GetDatasets()[0].GetSupportsColumnLineage())

	detail, err := client.GetOpenLineageDataset(ctx, withToken(env.AdminToken(), &v1pb.GetOpenLineageDatasetRequest{
		Guid: list.Msg.GetDatasets()[0].GetGuid(),
	}))
	require.NoError(t, err)
	require.Equal(t, int32(1), detail.Msg.GetDataset().GetTargetJobCount(), "the summary reads the aggregate, not the history")
	require.Len(t, detail.Msg.GetRelatedJobs(), 1)

	// Redelivering the other run without the output leaves the dataset without a
	// reference at all, which removes it from the page and from the aggregate.
	require.Equal(t, http.StatusOK, post(t, event("run-1", "job-a", base, readOrders, nil)))

	var remaining int
	require.NoError(t, env.Store.GetDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM openlineage_dataset WHERE namespace = $1 AND name = $2`, namespace, daily,
	).Scan(&remaining))
	require.Zero(t, remaining, "a dataset with no reference left is removed")

	require.NoError(t, env.Store.GetDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM openlineage_dataset_member WHERE namespace = $1 AND name = $2`, namespace, daily,
	).Scan(&remaining))
	require.Zero(t, remaining, "its member rows go with it")

	list, err = client.ListOpenLineageDatasets(ctx, withToken(env.AdminToken(), &v1pb.ListOpenLineageDatasetsRequest{Namespace: namespace}))
	require.NoError(t, err)
	require.Len(t, list.Msg.GetDatasets(), 1, "only the dataset both runs still read is left")
	require.Equal(t, orders, list.Msg.GetDatasets()[0].GetName())
	require.Equal(t, int32(2), list.Msg.GetDatasets()[0].GetSourceJobCount())
}

// Runs are kept until the retention window prunes them, and the prune deletes
// references in bulk without telling the aggregate which ones went. This drives
// the real prune and inspects what it left behind: the datasets whose last
// reference it removed have to be gone from the aggregate, its member rows and
// the page.
func TestOpenLineageDatasetAggregateFollowsTheRetentionPruneRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 30 * time.Second}

	key, _, err := env.Store.CreateOpenLineageAPIKey(ctx, "integration-dataset-prune", "integration-test", "")
	require.NoError(t, err)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	namespace := "integration-dataset-prune-ns-" + suffix
	kept := "public.kept-" + suffix
	pruned := "public.pruned-" + suffix
	// Far enough back that the cutoff below cannot touch another test's runs,
	// which are all ingested around now.
	base := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

	post := func(t *testing.T, runID, jobName string, at time.Time, outputs []string) {
		t.Helper()
		datasets := make([]map[string]any, 0, len(outputs))
		for _, name := range outputs {
			datasets = append(datasets, map[string]any{"namespace": namespace, "name": name})
		}
		body, err := json.Marshal(map[string]any{
			"eventType": "COMPLETE",
			"eventTime": at.Format(time.RFC3339Nano),
			"run":       map[string]any{"runId": runID},
			"job":       map[string]any{"namespace": namespace, "name": jobName},
			"producer":  "integration-test",
			"outputs":   datasets,
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

	// run-1 writes both datasets, so the prune removes one reference of each but
	// only takes the second out entirely.
	post(t, "run-1", "job-a", base, []string{kept, pruned})
	post(t, "run-2", "job-b", base.Add(time.Hour), []string{kept})

	countAggregates := func(t *testing.T) int {
		t.Helper()
		var count int
		require.NoError(t, env.Store.GetDB().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM openlineage_dataset WHERE namespace = $1`, namespace,
		).Scan(&count))
		return count
	}
	require.Equal(t, 2, countAggregates(t))

	deleted, err := env.Store.DeleteOpenLineageRunsBefore(ctx, base.Add(30*time.Minute))
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted, "only run-1 is inside the window")

	var remaining int
	require.NoError(t, env.Store.GetDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM openlineage_dataset WHERE namespace = $1 AND name = $2`, namespace, pruned,
	).Scan(&remaining))
	require.Zero(t, remaining, "the dataset whose only reference was pruned is gone")

	require.NoError(t, env.Store.GetDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM openlineage_dataset_member WHERE namespace = $1 AND name = $2`, namespace, pruned,
	).Scan(&remaining))
	require.Zero(t, remaining, "its member rows go with it")

	// The dataset another run still writes keeps its aggregate, rebuilt from the
	// references that remain.
	var refCount, targetJobs int64
	require.NoError(t, env.Store.GetDB().QueryRowContext(ctx, `
		SELECT ds.ref_count, ds.target_job_count
		FROM openlineage_dataset ds
		WHERE ds.namespace = $1 AND ds.name = $2
	`, namespace, kept).Scan(&refCount, &targetJobs))
	require.Equal(t, int64(1), refCount)
	require.Equal(t, int64(1), targetJobs)

	var memberRows, memberRefs int64
	require.NoError(t, env.Store.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(ref_count), 0) FROM openlineage_dataset_member
		WHERE namespace = $1 AND name = $2 AND kind = 'task:output'
	`, namespace, kept).Scan(&memberRows, &memberRefs))
	require.Equal(t, int64(1), memberRows, "the member rows are rebuilt from the remaining references")
	require.Equal(t, int64(1), memberRefs)

	list, err := v1connect.NewOpenLineageServiceClient(httpClient, env.BaseURL).ListOpenLineageDatasets(ctx, withToken(env.AdminToken(), &v1pb.ListOpenLineageDatasetsRequest{Namespace: namespace}))
	require.NoError(t, err)
	require.Len(t, list.Msg.GetDatasets(), 1)
	require.Equal(t, kept, list.Msg.GetDatasets()[0].GetName())
	require.Equal(t, int32(1), list.Msg.GetDatasets()[0].GetTargetJobCount())
}

// The list filters within the dataset window, and the detail resolves the
// requested GUID against that same window. A filtered list therefore cannot offer a
// dataset the detail answers 404 for: a dataset the cap pushed out of the window is
// neither listed nor openable, however well it matches a filter. This pins the cap
// as a bound the filters cannot reach past, which is what makes the two read paths
// agree.
func TestOpenLineageDatasetWindowKeepsTheListAndDetailAlignedRealServerIntegration(t *testing.T) {
	// Deliberately not parallel: this test fills the whole window, so it must not
	// run beside a test that expects its own datasets to still be inside it.
	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 60 * time.Second}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	filler := "integration-dataset-filler-ns-" + suffix
	older := "integration-dataset-older-ns-" + suffix
	t.Cleanup(func() {
		_, err := env.Store.GetDB().ExecContext(ctx,
			`DELETE FROM openlineage_dataset WHERE namespace = $1 OR namespace = $2`, filler, older)
		require.NoError(t, err)
	})

	// One row past the cap, dated far enough in the future that the window is made
	// of exactly these; the oldest one therefore overflows it. The second namespace
	// is older than every one of them, so only a filter that reached past the window
	// could offer it.
	_, err := env.Store.GetDB().ExecContext(ctx, `
		INSERT INTO openlineage_dataset (
			namespace, name, last_seen, ref_count, source_job_count, target_job_count, column_lineage_ref_count
		)
		SELECT $1, 'public.table_' || lpad(i::text, 5, '0'), TIMESTAMPTZ '2100-01-01 00:00:00+00' - make_interval(secs => i), 1, 1, 0, 0
		FROM generate_series(1, 5001) AS i
	`, filler)
	require.NoError(t, err)
	_, err = env.Store.GetDB().ExecContext(ctx, `
		INSERT INTO openlineage_dataset (
			namespace, name, last_seen, ref_count, source_job_count, target_job_count, column_lineage_ref_count
		)
		SELECT $1, 'public.old_table', TIMESTAMPTZ '2000-01-01 00:00:00+00', 1, 1, 0, 0
	`, older)
	require.NoError(t, err)

	newest := store.OpenLineageDatasetPair{Namespace: filler, Name: "public.table_00001"}
	overflowed := store.OpenLineageDatasetPair{Namespace: filler, Name: "public.table_05001"}
	pushedOut := store.OpenLineageDatasetPair{Namespace: older, Name: "public.old_table"}

	aggregates, err := env.Store.ListOpenLineageDatasetAggregate(ctx, &store.FindOpenLineageDatasetMessage{Namespace: &filler})
	require.NoError(t, err)
	require.Len(t, aggregates, 5000, "the cap bounds what one request reads")
	require.Equal(t, newest.Name, aggregates[0].Name)
	for _, aggregate := range aggregates {
		require.NotEqual(t, overflowed.Name, aggregate.Name, "a filter must not reach past the window")
	}

	aggregates, err = env.Store.ListOpenLineageDatasetAggregate(ctx, &store.FindOpenLineageDatasetMessage{Namespace: &older})
	require.NoError(t, err)
	require.Empty(t, aggregates, "a filter must not reach past the window")

	window, err := env.Store.ListOpenLineageDatasetWindow(ctx)
	require.NoError(t, err)
	require.Len(t, window, 5000)
	require.Contains(t, window, newest)
	require.NotContains(t, window, overflowed)
	require.NotContains(t, window, pushedOut)

	// A dataset the list shows is one the detail reads, even though none of these
	// rows has a reference: the summary is the maintained aggregate.
	detail, err := env.Store.GetOpenLineageDatasetDetail(ctx, []store.OpenLineageDatasetPair{newest})
	require.NoError(t, err)
	require.NotNil(t, detail)
	require.Equal(t, int32(1), detail.SourceJobCount)

	// And the API keeps the two in step in both directions: the dataset inside the
	// window opens, and the one the cap pushed out answers not-found.
	client := v1connect.NewOpenLineageServiceClient(httpClient, env.BaseURL)
	opened, err := client.GetOpenLineageDataset(ctx, withToken(env.AdminToken(), &v1pb.GetOpenLineageDatasetRequest{
		Guid: openlineage.FormatExternalGUID(newest.Namespace, newest.Name),
	}))
	require.NoError(t, err)
	require.Equal(t, newest.Name, opened.Msg.GetDataset().GetName())

	_, err = client.GetOpenLineageDataset(ctx, withToken(env.AdminToken(), &v1pb.GetOpenLineageDatasetRequest{
		Guid: openlineage.FormatExternalGUID(pushedOut.Namespace, pushedOut.Name),
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

// A batch is one transaction, so it can name far more datasets than one batched
// aggregate statement carries. The maintenance is chunked rather than sent whole,
// and this drives the real server with two events of a thousand datasets each:
// every dataset and its members have to be there afterwards, and the redelivery
// that drops them has to take them back out.
func TestOpenLineageDatasetAggregateChunksALargeBatchRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 60 * time.Second}

	key, _, err := env.Store.CreateOpenLineageAPIKey(ctx, "integration-dataset-chunks", "integration-test", "")
	require.NoError(t, err)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	namespace := "integration-dataset-chunks-ns-" + suffix
	base := time.Now().UTC().Add(-time.Hour)
	t.Cleanup(func() {
		_, err := env.Store.GetDB().ExecContext(ctx, `DELETE FROM openlineage_run WHERE job_namespace = $1`, namespace)
		require.NoError(t, err)
		_, err = env.Store.GetDB().ExecContext(ctx, `DELETE FROM openlineage_dataset WHERE namespace = $1`, namespace)
		require.NoError(t, err)
	})

	event := func(runID, jobName string, datasets []string) map[string]any {
		outputs := make([]map[string]any, 0, len(datasets))
		for _, name := range datasets {
			outputs = append(outputs, map[string]any{"namespace": namespace, "name": name})
		}
		return map[string]any{
			"eventType": "COMPLETE",
			"eventTime": base.Format(time.RFC3339Nano),
			"run":       map[string]any{"runId": runID},
			"job": map[string]any{
				"namespace": namespace,
				"name":      jobName,
				"facets":    map[string]any{"jobType": map[string]any{"jobType": "TASK", "integration": "airflow"}},
			},
			"producer": "integration-test",
			"outputs":  outputs,
		}
	}
	post := func(t *testing.T, body any) int {
		t.Helper()
		payload, err := json.Marshal(body)
		require.NoError(t, err)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.BaseURL+"/api/v1/lineage", bytes.NewReader(payload))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		return resp.StatusCode
	}
	names := func(prefix string, count int) []string {
		result := make([]string, 0, count)
		for index := range count {
			result = append(result, fmt.Sprintf("%s-%04d", prefix, index))
		}
		return result
	}
	count := func(t *testing.T, query string) int {
		t.Helper()
		var total int
		require.NoError(t, env.Store.GetDB().QueryRowContext(ctx, query, namespace).Scan(&total))
		return total
	}

	first := names("public.first", openlineage.MaxEventDatasets)
	second := names("public.second", openlineage.MaxEventDatasets)

	require.Equal(t, http.StatusOK, post(t, []map[string]any{
		event("run-batch-1", "job-batch-1", first),
		event("run-batch-2", "job-batch-2", second),
	}))

	require.Equal(t, 2*openlineage.MaxEventDatasets, count(t,
		`SELECT COUNT(*) FROM openlineage_dataset WHERE namespace = $1`))
	require.Equal(t, 2*openlineage.MaxEventDatasets, count(t,
		`SELECT COUNT(*) FROM openlineage_dataset_member WHERE namespace = $1 AND kind = 'task:output'`))
	require.Equal(t, 2*openlineage.MaxEventDatasets, count(t,
		`SELECT COUNT(*) FROM openlineage_dataset_member WHERE namespace = $1 AND kind = 'integration'`))

	// The same two runs, each writing one dataset: the references they dropped have
	// to leave the aggregate with them.
	require.Equal(t, http.StatusOK, post(t, []map[string]any{
		event("run-batch-1", "job-batch-1", first[:1]),
		event("run-batch-2", "job-batch-2", second[:1]),
	}))

	require.Equal(t, 2, count(t, `SELECT COUNT(*) FROM openlineage_dataset WHERE namespace = $1`))
	require.Equal(t, 2, count(t, `SELECT COUNT(*) FROM openlineage_dataset_member WHERE namespace = $1 AND kind = 'task:output'`))
	require.Equal(t, 2, count(t, `SELECT COUNT(*) FROM openlineage_dataset_member WHERE namespace = $1 AND kind = 'integration'`))
}

// A prune deletes references in bulk and then rebuilds the aggregates they belonged
// to. It reads the references behind that rebuild only after taking the aggregate
// rows' locks, in the order the ingest path takes them, so a writer that is still in
// flight when the prune reaches its dataset is folded into the rebuild instead of
// being overwritten by a recomputation made from a snapshot it is not in. This holds
// an aggregate row the way an ingest in flight does — its references written, its row
// locked, its transaction uncommitted — and asserts the prune waits for that lock
// rather than recomputing under it.
func TestOpenLineageDatasetPruneLocksBeforeItRecomputesRealServerIntegration(t *testing.T) {
	// Deliberately not parallel: it inspects the running statements of the shared
	// server, and another test's prune runs the same statements.
	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 30 * time.Second}

	key, _, err := env.Store.CreateOpenLineageAPIKey(ctx, "integration-dataset-prune-lock", "integration-test", "")
	require.NoError(t, err)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	namespace := "integration-dataset-prune-lock-ns-" + suffix
	locked := "public.locked-" + suffix
	// Far enough back that the cutoff below cannot touch another test's runs; the
	// writer in flight is far enough forward that it is not in the prune's scope.
	expired := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	inFlight := time.Now().UTC().Add(-time.Minute)
	t.Cleanup(func() {
		_, err := env.Store.GetDB().ExecContext(ctx, `DELETE FROM openlineage_run WHERE job_namespace = $1`, namespace)
		require.NoError(t, err)
		_, err = env.Store.GetDB().ExecContext(ctx, `DELETE FROM openlineage_dataset WHERE namespace = $1`, namespace)
		require.NoError(t, err)
	})

	body, err := json.Marshal(map[string]any{
		"eventType": "COMPLETE",
		"eventTime": expired.Format(time.RFC3339Nano),
		"run":       map[string]any{"runId": "run-expired"},
		"job":       map[string]any{"namespace": namespace, "name": "job-expired"},
		"producer":  "integration-test",
		"outputs":   []map[string]any{{"namespace": namespace, "name": locked}},
	})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.BaseURL+"/api/v1/lineage", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, resp.Body.Close())

	// The writer in flight: its run and its reference are written, its aggregate row
	// is locked the way the ingest path locks it, and nothing is committed yet.
	inFlightTx, err := env.Store.GetDB().BeginTx(ctx, nil)
	require.NoError(t, err)
	defer inFlightTx.Rollback()

	taskGUID := "openlineage:task:TASK:" + namespace + ":job-in-flight"
	var runPK int64
	require.NoError(t, inFlightTx.QueryRowContext(ctx, `
		INSERT INTO openlineage_run (guid, task_guid, run_id, job_namespace, job_name, job_type, event_type, event_time, raw_payload)
		VALUES ($1, $2, 'run-in-flight', $3, 'job-in-flight', 'TASK', 'COMPLETE', $4, '{}'::jsonb)
		RETURNING id
	`, "openlineage:run:TASK:"+namespace+":job-in-flight:run-in-flight", taskGUID, namespace, inFlight).Scan(&runPK))
	_, err = inFlightTx.ExecContext(ctx, `
		INSERT INTO openlineage_run_dataset (run_pk, task_guid, namespace, name, direction, event_time)
		VALUES ($1, $2, $3, $4, 'output', $5)
	`, runPK, taskGUID, namespace, locked, inFlight)
	require.NoError(t, err)
	_, err = inFlightTx.ExecContext(ctx, `
		UPDATE openlineage_dataset SET ref_count = ref_count + 1, target_job_count = target_job_count + 1,
			last_seen = GREATEST(last_seen, $3), updated_at = NOW()
		WHERE namespace = $1 AND name = $2
	`, namespace, locked, inFlight)
	require.NoError(t, err)

	pruned := make(chan error, 1)
	go func() {
		_, err := env.Store.DeleteOpenLineageRunsBefore(ctx, expired.Add(24*time.Hour))
		pruned <- err
	}()

	// While that writer holds the row, the prune has to be waiting for the lock. A
	// recomputation it had already started — the rebuild groups the references, the
	// empty-row delete probes them — would be made from a snapshot the writer is not
	// in, which is the overwrite this pins.
	// While that writer holds the row, the prune has to be waiting for the aggregate
	// rows rather than reading the references it would recompute them from: a
	// recomputation made from a snapshot the writer is not in is the overwrite this
	// pins. The blocked statement is asked for by name so the failure says which.
	var waiting string
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := env.Store.GetDB().QueryRowContext(ctx, `
			SELECT query FROM pg_stat_activity
			WHERE datname = current_database() AND state = 'active' AND pid <> pg_backend_pid()
				AND query LIKE '%expired_openl' || 'ineage_dataset%'
			LIMIT 1`).Scan(&waiting); err != nil {
			// No matching statement is running yet.
			time.Sleep(20 * time.Millisecond)
			continue
		}
		break
	}
	require.NotEmpty(t, waiting, "the prune should be waiting on the aggregate rows")
	require.Contains(t, waiting, "openlineage_dataset", "the prune waits on the aggregates")
	require.NotContains(t, waiting, "openlineage_run_dataset",
		"the prune must not read the references while a writer holds the aggregate row")

	require.NoError(t, inFlightTx.Commit())
	require.NoError(t, <-pruned)

	// The rebuild ran after that commit, so the reference it left behind is part of
	// the dataset and the counters describe it.
	var refCount, targetJobs int64
	require.NoError(t, env.Store.GetDB().QueryRowContext(ctx, `
		SELECT ref_count, target_job_count FROM openlineage_dataset WHERE namespace = $1 AND name = $2
	`, namespace, locked).Scan(&refCount, &targetJobs))
	require.Equal(t, int64(1), refCount)
	require.Equal(t, int64(1), targetJobs)

	var lastSeen time.Time
	require.NoError(t, env.Store.GetDB().QueryRowContext(ctx,
		`SELECT last_seen FROM openlineage_dataset WHERE namespace = $1 AND name = $2`, namespace, locked,
	).Scan(&lastSeen))
	require.Equal(t, inFlight.Unix(), lastSeen.UTC().Unix())
}

// The dataset aggregate follows the references a run leaves behind, and a run deleted
// outside the store's own paths takes its references and not the aggregate the ingest
// built from them. The sweep is what stops the pages offering datasets nothing
// references, and it has to leave a dataset another run still references alone.
func TestOpenLineageEmptyDatasetSweepRealServerIntegration(t *testing.T) {
	// Deliberately not parallel: it sweeps every dataset aggregate in the shared
	// database, and another test deletes a run behind the store's back too.
	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 30 * time.Second}

	key, _, err := env.Store.CreateOpenLineageAPIKey(ctx, "integration-dataset-sweep", "integration-test", "")
	require.NoError(t, err)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	namespace := "integration-dataset-sweep-ns-" + suffix
	removed := "public.removed-" + suffix
	kept := "public.kept-" + suffix
	base := time.Now().UTC().Add(-time.Hour)
	t.Cleanup(func() {
		_, err := env.Store.GetDB().ExecContext(ctx, `DELETE FROM openlineage_run WHERE job_namespace = $1`, namespace)
		require.NoError(t, err)
		_, err = env.Store.GetDB().ExecContext(ctx, `DELETE FROM openlineage_dataset WHERE namespace = $1`, namespace)
		require.NoError(t, err)
	})

	post := func(t *testing.T, runID, jobName, dataset string) {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"eventType": "COMPLETE",
			"eventTime": base.Format(time.RFC3339Nano),
			"run":       map[string]any{"runId": runID},
			"job":       map[string]any{"namespace": namespace, "name": jobName},
			"producer":  "integration-test",
			"outputs":   []map[string]any{{"namespace": namespace, "name": dataset}},
		})
		require.NoError(t, err)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.BaseURL+"/api/v1/lineage", bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.NoError(t, resp.Body.Close())
	}
	count := func(t *testing.T) int {
		t.Helper()
		var total int
		require.NoError(t, env.Store.GetDB().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM openlineage_dataset WHERE namespace = $1`, namespace).Scan(&total))
		return total
	}

	post(t, "run-1", "job-1", removed)
	post(t, "run-2", "job-2", kept)
	require.Equal(t, 2, count(t))

	// The run behind `removed` is deleted by hand, the way an operator's cleanup
	// does: its references cascade, its aggregate stays.
	_, err = env.Store.GetDB().ExecContext(ctx, `DELETE FROM openlineage_run WHERE job_namespace = $1 AND run_id = 'run-1'`, namespace)
	require.NoError(t, err)
	require.Equal(t, 2, count(t), "the aggregate outlives the references")

	// A row an ingest touched recently is left alone: a writer that has not committed
	// is invisible to this pass.
	deleted, err := env.Store.DeleteEmptyOpenLineageDatasets(ctx, time.Now().UTC().Add(-time.Minute))
	require.NoError(t, err)
	require.Zero(t, deleted)
	require.Equal(t, 2, count(t))

	// Once the row is old enough, the dataset nothing references goes and the one the
	// surviving run still writes stays.
	deleted, err = env.Store.DeleteEmptyOpenLineageDatasets(ctx, time.Now().UTC().Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted, "only the dataset nothing references is swept")
	require.Equal(t, 1, count(t))

	var remaining string
	require.NoError(t, env.Store.GetDB().QueryRowContext(ctx,
		`SELECT name FROM openlineage_dataset WHERE namespace = $1`, namespace).Scan(&remaining))
	require.Equal(t, kept, remaining)
}
