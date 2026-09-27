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

// syncRevalidationTimeout bounds the wait for the post-sync pass. It is well
// below the maintenance interval (6 h), which is the only thing that re-checked
// these edges before the sync signalled the runner.
const syncRevalidationTimeout = 45 * time.Second

// Ingestion keeps an edge whose target relation the registry does not have, and
// therefore cannot check the columns it claims: the relation may be a table
// created after the last schema sync, and dropping the edge would lose real
// lineage. The sync that makes the relation known is what makes the claims
// checkable, so this drives the real server through that sequence - post the
// edge, create the table, sync - and requires the claim against a column the
// table does not have to be gone well inside the maintenance interval, which is
// the only thing that used to re-check these edges.
func TestSchemaSyncRevalidatesIngestedLineageRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)
	httpClient := &http.Client{Timeout: 10 * time.Second}

	require.NoError(t, env.ExecPostgres(ctx, sourceDatabase, `
CREATE TABLE public.late_source (id INT, amount NUMERIC(12,2));
`))
	env.SyncDatabase(ctx, t, databaseName)

	guidPrefix := fmt.Sprintf("%s;%s", instanceID, sourceDatabase)
	sourceGUID := waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_TABLE, "late_source")
	// The destination is deliberately absent from the registry. Its GUID is
	// deterministic, so the test knows it before the table exists.
	targetGUID := fmt.Sprintf("%s;public;late_target", guidPrefix)

	key, _, err := env.Store.CreateOpenLineageAPIKey(ctx, "integration-revalidate", "integration-test", "")
	require.NoError(t, err)

	// The namespace names the database the datasets live in, as a JDBC URL does;
	// without it the resolver falls back to the instance's data-source database.
	namespace := fmt.Sprintf("postgres://%s:%s/%s", env.PostgresHost, env.PostgresPort, sourceDatabase)
	sourceDataset := fmt.Sprintf("%s.public.late_source", sourceDatabase)
	sourceField := func(field string) map[string]any {
		return map[string]any{"namespace": namespace, "name": sourceDataset, "field": field}
	}

	// No SQL facet: the producer states its column lineage directly, which is the
	// path that stores such a claim without analyzing it. `amount` is real on the
	// table this event's destination becomes; `ghost_column` is not.
	output := map[string]any{
		"namespace": namespace,
		"name":      fmt.Sprintf("%s.public.late_target", sourceDatabase),
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
	jobNamespace := fmt.Sprintf("integration-revalidate-ns-%d", uniqueNano)
	jobName := fmt.Sprintf("integration-revalidate-job-%d", uniqueNano)
	event := map[string]any{
		"eventType": "COMPLETE",
		"eventTime": time.Now().UTC().Format(time.RFC3339Nano),
		"run":       map[string]any{"runId": fmt.Sprintf("revalidate-run-%d", uniqueNano)},
		"job":       map[string]any{"namespace": jobNamespace, "name": jobName},
		"producer":  "integration-test",
		"inputs":    []map[string]any{{"namespace": namespace, "name": sourceDataset}},
		"outputs":   []map[string]any{output},
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

	targetEdges := func() []*store.ColumnLineage {
		t.Helper()
		edges, err := env.Store.ListColumnLineage(ctx, &store.FindColumnLineageMessage{TargetGUID: &targetGUID})
		require.NoError(t, err)
		return edges
	}

	// The relation is unknown, so both claims are stored unchecked - the ghost
	// column included. That is the state the sync has to correct.
	require.Eventually(t, func() bool {
		var amountClaimed, ghostClaimed bool
		for _, edge := range targetEdges() {
			if edge.SourceGUID != sourceGUID {
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

	// The sync has to have registered the table: until it does, the edge's
	// endpoint is unknown and there is nothing to re-check.
	require.Equal(t, targetGUID, waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_TABLE, "late_target"),
		"the sync must register the table the edge named")

	// The sync is what re-checks the claims: the real column is kept, the ghost
	// one is blanked and the edge survives as a table-level dependency.
	require.Eventually(t, func() bool {
		var keptAmount, stillGhost, degraded bool
		for _, edge := range targetEdges() {
			if edge.SourceGUID != sourceGUID {
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
