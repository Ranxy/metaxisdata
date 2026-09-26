//go:build integration

package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// An Airflow SQL task states its own column lineage through a run-level facet:
// the extractor merges every statement of the task into one input/output set,
// attaches that one facet to every output, and attributes an output column by
// finding a same-named column anywhere in the inputs. When two inputs expose the
// same column name that attribution is simply wrong, and no existence check can
// see it - both relations and both columns exist.
//
// This drives the real server with exactly that shape and requires the stored
// edge to name the input the statement actually reads, because the SQL is
// analyzed instead of believed.
func TestOpenLineageSqlFacetUsesTheAnalysedSourceRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)
	httpClient := &http.Client{Timeout: 10 * time.Second}

	require.NoError(t, env.ExecPostgres(ctx, sourceDatabase, `
CREATE TABLE public.facet_left (id INT, amount NUMERIC(12,2));
CREATE TABLE public.facet_right (id INT, amount NUMERIC(12,2));
CREATE TABLE public.facet_dst (id INT, amount NUMERIC(12,2));
CREATE VIEW public.facet_v_left AS SELECT id, amount FROM public.facet_left;
CREATE VIEW public.facet_v_right AS SELECT id, amount FROM public.facet_right;
`))

	env.SyncDatabase(ctx, t, databaseName)
	guidPrefix := fmt.Sprintf("%s;%s", instanceID, sourceDatabase)
	leftGUID := waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_VIEW, "facet_v_left")
	rightGUID := waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_VIEW, "facet_v_right")
	dstGUID := waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_TABLE, "facet_dst")
	require.NotEqual(t, leftGUID, rightGUID)

	key, _, err := env.Store.CreateOpenLineageAPIKey(ctx, "integration-sql-facet", "integration-test", "")
	require.NoError(t, err)

	namespace := fmt.Sprintf("postgres://%s:%s", env.PostgresHost, env.PostgresPort)
	dataset := func(table string) map[string]any {
		return map[string]any{"namespace": namespace, "name": fmt.Sprintf("%s.public.%s", sourceDatabase, table)}
	}
	// The facet claims what the extractor's name-matching produces: amount comes
	// from the left view because it found that column there first.
	claimsWrongSource := map[string]any{
		"_producer":  "integration-test",
		"_schemaURL": "integration-test",
		"fields": map[string]any{
			"amount": map[string]any{"inputFields": []map[string]any{
				{"namespace": namespace, "name": fmt.Sprintf("%s.public.facet_v_left", sourceDatabase), "field": "amount"},
			}},
		},
	}
	output := dataset("facet_dst")
	output["facets"] = map[string]any{"columnLineage": claimsWrongSource}

	uniqueNano := time.Now().UnixNano()
	jobNamespace := fmt.Sprintf("integration-sql-facet-ns-%d", uniqueNano)
	jobName := fmt.Sprintf("integration-sql-facet-job-%d", uniqueNano)
	event := map[string]any{
		"eventType": "COMPLETE",
		"eventTime": time.Now().UTC().Format(time.RFC3339Nano),
		"run":       map[string]any{"runId": fmt.Sprintf("sql-facet-run-%d", uniqueNano)},
		"job": map[string]any{
			"namespace": jobNamespace,
			"name":      jobName,
			"facets": map[string]any{"sql": map[string]any{
				"query": "INSERT INTO public.facet_dst (id, amount) SELECT l.id, r.amount FROM public.facet_v_left l JOIN public.facet_v_right r ON l.id = r.id",
			}},
		},
		"producer": "integration-test",
		"inputs":   []map[string]any{dataset("facet_v_left"), dataset("facet_v_right")},
		"outputs":  []map[string]any{output},
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

	var runGUID string
	require.Eventually(t, func() bool {
		runs, err := env.Store.ListOpenLineageRun(ctx, &store.FindOpenLineageRunMessage{
			JobNamespace: &jobNamespace,
			JobName:      &jobName,
		})
		if err != nil || len(runs) == 0 {
			return false
		}
		runGUID = runs[0].GUID
		return true
	}, 10*time.Second, 200*time.Millisecond)

	lineages, err := env.Store.ListColumnLineage(ctx, &store.FindColumnLineageMessage{MetaGUID: &runGUID})
	require.NoError(t, err)
	require.NotEmpty(t, lineages, "the analysed statement must produce lineage")

	var sources []string
	for _, lineage := range lineages {
		if lineage.TargetGUID == dstGUID && lineage.TargetColumn == "amount" {
			sources = append(sources, lineage.SourceGUID)
		}
	}
	require.Equal(t, []string{rightGUID}, sources,
		"amount is selected from facet_v_right, whatever the producer's facet claimed")
}
