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
	integrationenv "github.com/Ranxy/metaxisdata/backend/test/integration/env"
)

// syncRevalidationTimeout bounds the wait for the post-sync pass. It is well
// below the maintenance interval (6 h), which is the only thing that re-checked
// these edges before the sync signalled the runner.
const syncRevalidationTimeout = 45 * time.Second

// postLineageEvent posts one OpenLineage event and returns the GUID of the run it
// was stored as.
func postLineageEvent(ctx context.Context, t *testing.T, env *integrationenv.ServiceEnv, event map[string]any) string {
	t.Helper()

	key, _, err := env.Store.CreateOpenLineageAPIKey(ctx, "integration-lineage-database", "integration-test", "")
	require.NoError(t, err)
	body, err := json.Marshal(event)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.BaseURL+"/api/v1/lineage", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	job, ok := event["job"].(map[string]any)
	require.True(t, ok)
	jobNamespace, ok := job["namespace"].(string)
	require.True(t, ok, "the event names the job namespace the run is stored under")
	jobName, ok := job["name"].(string)
	require.True(t, ok, "the event names the job the run is stored under")

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
	return runGUID
}

// edgesOfRun reads back the edges one run stored.
func edgesOfRun(ctx context.Context, t *testing.T, env *integrationenv.ServiceEnv, runGUID string) []*store.ColumnLineage {
	t.Helper()
	edges, err := env.Store.ListColumnLineage(ctx, &store.FindColumnLineageMessage{MetaGUID: &runGUID})
	require.NoError(t, err)
	return edges
}

// Ingestion keeps an edge whose target relation the registry does not have, and
// therefore cannot check the columns it claims: the relation may be a table
// created after the last schema sync, and dropping the edge would lose real
// lineage. The sync that makes the relation known is what makes the claims
// checkable, so this drives the real server through that sequence - post the
// edge, create the table, sync - and requires the claim against a column the
// table does not have to be gone well inside the maintenance interval.
//
// The namespace mapping is what pins the instance: auto-match answers the first
// instance registered on a host:port, and this environment registers one per test
// on the same server. The mapping names no database, which leaves the database to
// the precedence under test, and the namespace names a decoy one so that only the
// dataset name can put the edge on the right relation.
func TestSchemaSyncRevalidatesIngestedLineageRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)

	require.NoError(t, env.ExecPostgres(ctx, sourceDatabase, `
CREATE TABLE public.late_source (id INT, amount NUMERIC(12,2));
`))
	env.SyncDatabase(ctx, t, databaseName)

	namespace := fmt.Sprintf("postgres://%s:%s/decoy_%s", env.PostgresHost, env.PostgresPort, sourceDatabase)
	mapping, err := env.Store.CreateNamespaceMapping(ctx, &store.NamespaceMappingMessage{
		Namespace:          namespace,
		InstanceResourceID: instanceID,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = env.Store.DeleteNamespaceMapping(context.Background(), mapping.ID) })

	sourceDataset := fmt.Sprintf("%s.public.late_source", sourceDatabase)
	targetDataset := fmt.Sprintf("%s.public.late_target", sourceDatabase)
	sourceField := func(field string) map[string]any {
		return map[string]any{"namespace": namespace, "name": sourceDataset, "field": field}
	}

	// No SQL facet: the producer states its column lineage directly, which is the
	// path that stores such a claim without analyzing it. `amount` is real on the
	// table this event's destination becomes; `ghost_column` is not.
	output := map[string]any{
		"namespace": namespace,
		"name":      targetDataset,
		"facets": map[string]any{"columnLineage": map[string]any{
			"_producer":  "integration-test",
			"_schemaURL": "integration-test",
			"fields": map[string]any{
				"amount":       map[string]any{"inputFields": []map[string]any{sourceField("amount")}},
				"ghost_column": map[string]any{"inputFields": []map[string]any{sourceField("id")}},
			},
		}},
	}

	uniqueNano := time.Now().UnixNano()
	runGUID := postLineageEvent(ctx, t, env, map[string]any{
		"eventType": "COMPLETE",
		"eventTime": time.Now().UTC().Format(time.RFC3339Nano),
		"run":       map[string]any{"runId": fmt.Sprintf("revalidate-run-%d", uniqueNano)},
		"job": map[string]any{
			"namespace": fmt.Sprintf("integration-revalidate-ns-%d", uniqueNano),
			"name":      fmt.Sprintf("integration-revalidate-job-%d", uniqueNano),
		},
		"producer": "integration-test",
		"inputs":   []map[string]any{{"namespace": namespace, "name": sourceDataset}},
		"outputs":  []map[string]any{output},
	})

	// The dataset name carries the database, so this is the only GUID the edge may
	// name - not the decoy the namespace holds, and not the instance's data source
	// (`postgres`), which is the defect F9 records.
	targetGUID := fmt.Sprintf("%s;%s;public;late_target", instanceID, sourceDatabase)
	sourceGUID := fmt.Sprintf("%s;%s;public;late_source", instanceID, sourceDatabase)

	// The relation is unknown, so both claims are stored unchecked - the ghost
	// column included. That is the state the sync has to correct.
	require.Eventually(t, func() bool {
		var amountClaimed, ghostClaimed bool
		for _, edge := range edgesOfRun(ctx, t, env, runGUID) {
			if edge.TargetGUID != targetGUID || edge.SourceGUID != sourceGUID {
				continue
			}
			amountClaimed = amountClaimed || edge.TargetColumn == "amount"
			ghostClaimed = ghostClaimed || edge.TargetColumn == "ghost_column"
		}
		return amountClaimed && ghostClaimed
	}, syncRevalidationTimeout, 200*time.Millisecond, "an unknown relation's claims are stored unchecked")

	// The table appears, with no ghost_column on it.
	require.NoError(t, env.ExecPostgres(ctx, sourceDatabase, `
CREATE TABLE public.late_target (id INT, amount NUMERIC(12,2));
`))
	env.SyncDatabase(ctx, t, databaseName)

	// The sync is what re-checks the claims: the real column is kept, the ghost
	// one is blanked and the edge survives as a table-level dependency.
	require.Eventually(t, func() bool {
		var keptAmount, stillGhost, degraded bool
		for _, edge := range edgesOfRun(ctx, t, env, runGUID) {
			if edge.TargetGUID != targetGUID || edge.SourceGUID != sourceGUID {
				continue
			}
			stillGhost = stillGhost || edge.TargetColumn == "ghost_column"
			keptAmount = keptAmount || (edge.SourceColumn == "amount" && edge.TargetColumn == "amount")
			degraded = degraded || (edge.SourceColumn == "" && edge.TargetColumn == "")
		}
		return keptAmount && !stillGhost && degraded
	}, syncRevalidationTimeout, 500*time.Millisecond,
		"a schema sync must re-check the claims of ingested lineage it made resolvable")
}

// A dataset name that carries its database keeps it, whatever the instance's data
// source says. The resolver answers "internal" either way, so an edge that took
// the data source's database (the server's default) points at a relation the
// registry does not have and nothing warns about it.
func TestFacetLineageUsesTheDatasetNameDatabaseRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, _, sourceDatabase, _ := setupPostgresServiceDatabase(t)

	// No mapping and no database in the namespace: the event resolves by host:port,
	// and whichever instance answers, the dataset name is the only thing that can
	// name the database.
	namespace := fmt.Sprintf("postgres://%s:%s", env.PostgresHost, env.PostgresPort)
	sourceDataset := fmt.Sprintf("%s.public.name_source", sourceDatabase)
	targetDataset := fmt.Sprintf("%s.public.name_target", sourceDatabase)

	uniqueNano := time.Now().UnixNano()
	runGUID := postLineageEvent(ctx, t, env, map[string]any{
		"eventType": "COMPLETE",
		"eventTime": time.Now().UTC().Format(time.RFC3339Nano),
		"run":       map[string]any{"runId": fmt.Sprintf("name-db-run-%d", uniqueNano)},
		"job": map[string]any{
			"namespace": fmt.Sprintf("integration-name-db-ns-%d", uniqueNano),
			"name":      fmt.Sprintf("integration-name-db-job-%d", uniqueNano),
		},
		"producer": "integration-test",
		"inputs":   []map[string]any{{"namespace": namespace, "name": sourceDataset}},
		"outputs": []map[string]any{{
			"namespace": namespace,
			"name":      targetDataset,
			"facets": map[string]any{"columnLineage": map[string]any{
				"_producer":  "integration-test",
				"_schemaURL": "integration-test",
				"fields": map[string]any{
					"amount": map[string]any{"inputFields": []map[string]any{
						{"namespace": namespace, "name": sourceDataset, "field": "amount"},
					}},
				},
			}},
		}},
	})

	targetSuffix := ";" + sourceDatabase + ";public;name_target"
	var found bool
	for _, edge := range edgesOfRun(ctx, t, env, runGUID) {
		if strings.HasSuffix(edge.TargetGUID, targetSuffix) {
			found = true
			require.Equal(t, "amount", edge.TargetColumn)
		}
	}
	require.True(t, found,
		"the edge has to name the database its dataset name carries, not the instance's data source: %v",
		edgesOfRun(ctx, t, env, runGUID))
}

// A SQL-facet event may name its datasets at "schema.table" and carry the
// database only in the namespace, as a JDBC URL does. The statement then has to
// be analyzed in that database: the analyzer builds its relations from the anchor
// GUID the resolver produced, so a context that took the database from the
// instance's data source instead (the server's default) puts every relation on a
// GUID the registry does not have.
//
// The database is asserted through the stored edges rather than through a
// registered table, because which instance answers the namespace is not this
// test's choice: every test instance in this environment points at the same
// server.
func TestOpenLineageSqlFacetUsesTheNamespaceDatabaseRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, _, sourceDatabase, _ := setupPostgresServiceDatabase(t)

	namespace := fmt.Sprintf("postgres://%s:%s/%s", env.PostgresHost, env.PostgresPort, sourceDatabase)
	dataset := func(table string) map[string]any {
		return map[string]any{"namespace": namespace, "name": fmt.Sprintf("public.%s", table)}
	}

	uniqueNano := time.Now().UnixNano()
	runGUID := postLineageEvent(ctx, t, env, map[string]any{
		"eventType": "COMPLETE",
		"eventTime": time.Now().UTC().Format(time.RFC3339Nano),
		"run":       map[string]any{"runId": fmt.Sprintf("sql-fallback-run-%d", uniqueNano)},
		"job": map[string]any{
			"namespace": fmt.Sprintf("integration-sql-fallback-ns-%d", uniqueNano),
			"name":      fmt.Sprintf("integration-sql-fallback-job-%d", uniqueNano),
			"facets": map[string]any{"sql": map[string]any{
				"query": "INSERT INTO public.fallback_dst (id, amount) SELECT id, amount FROM public.fallback_src",
			}},
		},
		"producer": "integration-test",
		"inputs":   []map[string]any{dataset("fallback_src")},
		"outputs":  []map[string]any{dataset("fallback_dst")},
	})

	targetSuffix := ";" + sourceDatabase + ";public;fallback_dst"
	var analyzed []*store.ColumnLineage
	for _, edge := range edgesOfRun(ctx, t, env, runGUID) {
		if strings.HasSuffix(edge.TargetGUID, targetSuffix) {
			analyzed = append(analyzed, edge)
		}
	}
	require.NotEmpty(t, analyzed,
		"the statement's output has to name the database the namespace carries, not the instance's data source: %v",
		edgesOfRun(ctx, t, env, runGUID))

	var columns []string
	for _, edge := range analyzed {
		columns = append(columns, edge.TargetColumn)
		require.True(t, strings.HasSuffix(edge.SourceGUID, ";"+sourceDatabase+";public;fallback_src"),
			"the statement's input belongs to the same database; source is %q", edge.SourceGUID)
	}
	require.ElementsMatch(t, []string{"id", "amount"}, columns)
}
