//go:build integration

package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// TestApplicationSQLReportReachesTheLineageGraphRealServerIntegration drives the whole
// application-SQL path on a real server: an application reports parameterized SQL with a key
// the workspace issued, the statement is stored and mirrored, the runner analyzes it, and the
// graph shows it.
//
// Nothing below this level tests that chain. The handler unit tests stop at the store
// interface, the store tests are hermetic, and the runner's classification test never sees a
// registry. A break anywhere between them — a placeholder the analyzer cannot parse, an edge
// shape the graph walk drops, a mirror the runner cannot find — would leave every suite green.
func TestApplicationSQLReportReachesTheLineageGraphRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)
	httpClient := &http.Client{Timeout: 30 * time.Second}

	require.NoError(t, env.ExecPostgres(ctx, sourceDatabase, `
CREATE TABLE public.appsql_orders (id INT, total NUMERIC(12,2), status TEXT);
`))
	env.SyncDatabase(ctx, t, databaseName)
	guidPrefix := fmt.Sprintf("%s;%s", instanceID, sourceDatabase)
	ordersGUID := waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_TABLE, "appsql_orders")

	// The key names the application, and issuing it is what registers one.
	application, err := env.Store.CreateApplication(ctx, "integration-appsql-"+fmt.Sprint(time.Now().UnixNano()), "integration", "integration-test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = env.Store.DeleteApplication(context.Background(), application.ApplicationID) })
	plainKey, _, err := env.Store.CreateApplicationSQLAPIKey(ctx, application.ApplicationID, "integration", "integration-test")
	require.NoError(t, err)

	// One report carrying the two shapes that exercise both halves of the classification, plus
	// one statement the analyzer must refuse. The literals are deliberately left in: the
	// application is allowed to send them, and the server is required to remove them.
	body := fmt.Sprintf(`{
	  "instance": "instances/%s",
	  "database": %q,
	  "schema": "public",
	  "reportId": "integration-report-1",
	  "statements": [
	    {"sql": "INSERT INTO appsql_orders (id, total, status) VALUES (7, 12.50, 'new')", "count": 3},
	    {"sql": "SELECT id, total FROM appsql_orders WHERE status = 'new' AND total > 10"},
	    {"sql": "SELECT FROM WHERE"}
	  ]
	}`, instanceID, sourceDatabase)

	resp := postApplicationSQLReport(ctx, t, httpClient, env.BaseURL, plainKey, body)
	require.Equal(t, http.StatusOK, resp.status, resp.body)

	var report applicationSQLReportBody
	require.NoError(t, json.Unmarshal([]byte(resp.body), &report))
	require.Equal(t, 2, report.Accepted, "the two analyzable statements are stored")
	require.Equal(t, 1, report.Rejected, "the malformed one is refused alone")
	require.Len(t, report.Results, 3)
	require.Equal(t, "REJECTED", report.Results[2].Status)

	// What was stored is the sanitized text, never what the application sent.
	insertGUID := report.Results[0].GUID
	selectGUID := report.Results[1].GUID
	require.NotEmpty(t, insertGUID)
	require.NotEmpty(t, selectGUID)

	insertStatement, err := env.Store.GetApplicationSQL(ctx, &store.FindApplicationSQLMessage{GUID: &insertGUID})
	require.NoError(t, err)
	require.NotNil(t, insertStatement)
	require.Equal(t, "INSERT INTO appsql_orders (id, total, status) VALUES (?, ?, ?)", insertStatement.SQLText)
	require.NotContains(t, insertStatement.SQLText, "12.05")
	require.Equal(t, int64(3), insertStatement.ExecutionCount, "a report's count is folded into the observation")
	require.Equal(t, sourceDatabase, insertStatement.DatabaseName)
	require.Equal(t, "public", insertStatement.SchemaName)

	// A repeat of the same statement is the same row, not a second one.
	resp = postApplicationSQLReport(ctx, t, httpClient, env.BaseURL, plainKey, body)
	require.Equal(t, http.StatusOK, resp.status, resp.body)
	require.True(t, json.Valid([]byte(resp.body)))
	repeated, err := env.Store.GetApplicationSQL(ctx, &store.FindApplicationSQLMessage{GUID: &insertGUID})
	require.NoError(t, err)
	require.NotNil(t, repeated)
	require.Equal(t, insertStatement.ID, repeated.ID, "the same statement keeps its row across reports")

	// Every reported statement is mirrored, and the runner reads the statement back from that
	// mirror: without it there is no object for the analysis to attach to.
	meta, err := env.Store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUID: &insertGUID})
	require.NoError(t, err)
	require.NotNil(t, meta, "every reported statement is mirrored into the meta registry")
	require.Equal(t, storepb.MetaType_APPLICATION_SQL, meta.ObjectType)
	require.Equal(t, insertStatement.SQLText, meta.Metadata.GetApplicationSqlMetadata().GetSqlText(),
		"the runner analyzes the mirrored text, so the mirror has to carry it")

	// The runner analyzes both statements. This is the hop nothing else covers.
	require.Eventually(t, func() bool {
		version, err := env.Store.GetColumnLineageVersion(ctx, insertGUID, storepb.MetaType_APPLICATION_SQL)
		return err == nil && version != nil && version.AnalyzedAt.After(time.Time{})
	}, 60*time.Second, 500*time.Millisecond, "the runner must analyze a reported statement")

	// A failed analysis still writes a version row, so the wait above proves only that the
	// runner looked. This is what says it succeeded.
	for _, guid := range []string{insertGUID, selectGUID} {
		version, err := env.Store.GetColumnLineageVersion(ctx, guid, storepb.MetaType_APPLICATION_SQL)
		require.NoError(t, err)
		require.NotNil(t, version)
		if version.ErrorMessage != nil {
			require.Empty(t, *version.ErrorMessage, "analyzing %s must not fail", guid)
		}
	}

	insertEdges, err := env.Store.ListColumnLineage(ctx, &store.FindColumnLineageMessage{MetaGUID: &insertGUID})
	require.NoError(t, err)
	require.True(t, hasEdgeTo(insertEdges, ordersGUID),
		"a constant INSERT has no column relation, so its out-edge is the table-level one: %v", describeEdges(insertEdges))

	selectEdges, err := env.Store.ListColumnLineage(ctx, &store.FindColumnLineageMessage{MetaGUID: &selectGUID})
	require.NoError(t, err)
	require.True(t, hasEdgeFrom(selectEdges, ordersGUID),
		"a read reaches the graph through its incoming edge: %v", describeEdges(selectEdges))
	require.False(t, hasEdgeTo(selectEdges, ordersGUID),
		"a read writes nothing: %v", describeEdges(selectEdges))

	// The kind is the runner's answer, written back for the list page.
	insertStatement, err = env.Store.GetApplicationSQL(ctx, &store.FindApplicationSQLMessage{GUID: &insertGUID})
	require.NoError(t, err)
	require.Equal(t, store.ApplicationSQLKindWrite, insertStatement.Kind)
	selectStatement, err := env.Store.GetApplicationSQL(ctx, &store.FindApplicationSQLMessage{GUID: &selectGUID})
	require.NoError(t, err)
	require.Equal(t, store.ApplicationSQLKindRead, selectStatement.Kind)

	// And the graph draws both statements, which is the whole point of storing them.
	require.Eventually(t, func() bool {
		nodes, err := env.Store.ListColumnLineage(ctx, &store.FindColumnLineageMessage{MetaGUID: &selectGUID})
		return err == nil && len(nodes) > 0
	}, 30*time.Second, 500*time.Millisecond)
}

type applicationSQLReportResult struct {
	Index  int    `json:"index"`
	Status string `json:"status"`
	GUID   string `json:"guid"`
	Reason string `json:"reason"`
}

type applicationSQLReportBody struct {
	Accepted int                          `json:"accepted"`
	Rejected int                          `json:"rejected"`
	Results  []applicationSQLReportResult `json:"results"`
}

type httpResult struct {
	status int
	body   string
}

func postApplicationSQLReport(ctx context.Context, t *testing.T, client *http.Client, baseURL, key, body string) httpResult {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/applicationSql/report", bytes.NewReader([]byte(body)))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return httpResult{status: resp.StatusCode, body: string(raw)}
}

// hasEdgeTo reports whether the statement writes the target: a stored out-edge whose column
// is empty is the table-level shape a constant write produces.
func hasEdgeTo(edges []*store.ColumnLineage, targetGUID string) bool {
	for _, edge := range edges {
		if edge.TargetGUID == targetGUID {
			return true
		}
	}
	return false
}

// hasEdgeFrom reports whether the statement reads the source.
func hasEdgeFrom(edges []*store.ColumnLineage, sourceGUID string) bool {
	for _, edge := range edges {
		if edge.SourceGUID == sourceGUID {
			return true
		}
	}
	return false
}

func describeEdges(edges []*store.ColumnLineage) []string {
	out := make([]string, 0, len(edges))
	for _, edge := range edges {
		out = append(out, fmt.Sprintf("%s.%s -> %s.%s", edge.SourceGUID, edge.SourceColumn, edge.TargetGUID, edge.TargetColumn))
	}
	return out
}
