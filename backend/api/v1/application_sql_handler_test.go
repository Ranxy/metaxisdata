package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/engines"
	"github.com/Ranxy/metaxisdata/backend/plugin/sqlsanitize"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// applicationSQLAnalyzer is the process-wide analyzer over every registered dialect. A nil
// catalog is enough for the statements these tests use.
var applicationSQLAnalyzer = lineage.NewAnalyzer(nil, engines.Registrations()...)

// fakeApplicationSQLStore stands in for the database. The handler's refusals are all decided
// before it is asked to write, so a test can prove that by leaving it empty.
type fakeApplicationSQLStore struct {
	identity    *store.ApplicationSQLIdentity
	authErr     error
	instance    *store.InstanceMessage
	instanceErr error

	reports   []*store.ReportApplicationSQLMessage
	reportErr error
	recorded  []string
}

func (f *fakeApplicationSQLStore) AuthenticateApplicationSQLAPIKey(context.Context, string) (*store.ApplicationSQLIdentity, error) {
	if f.authErr != nil {
		return nil, f.authErr
	}
	return f.identity, nil
}

func (f *fakeApplicationSQLStore) GetInstance(context.Context, *store.FindInstanceMessage) (*store.InstanceMessage, error) {
	return f.instance, f.instanceErr
}

func (f *fakeApplicationSQLStore) ReportApplicationSQL(_ context.Context, report *store.ReportApplicationSQLMessage) (*store.ApplicationSQLMessage, error) {
	if f.reportErr != nil {
		return nil, f.reportErr
	}
	f.reports = append(f.reports, report)
	return &store.ApplicationSQLMessage{GUID: fmt.Sprintf("guid-%d", len(f.reports)), SQLText: report.SQLText}, nil
}

func (f *fakeApplicationSQLStore) RecordApplicationSQLReport(_ context.Context, _ int64, reportID string, accepted, rejected int) error {
	f.recorded = append(f.recorded, fmt.Sprintf("%s:%d:%d", reportID, accepted, rejected))
	return nil
}

type fakeApplicationSQLQueue struct {
	queued []string
}

func (f *fakeApplicationSQLQueue) QueueLineageAnalysis(metaGUID string, _ storepb.MetaType) {
	f.queued = append(f.queued, metaGUID)
}

// validApplicationSQLStore is a store whose key resolves and whose instance is a MySQL one.
// A test that wants a different outcome overrides one field.
func validApplicationSQLStore() *fakeApplicationSQLStore {
	return &fakeApplicationSQLStore{
		identity: &store.ApplicationSQLIdentity{
			Key:         &store.ApplicationSQLAPIKeyMessage{ID: 7},
			Application: &store.ApplicationMessage{ApplicationID: "app-one", Name: "one"},
		},
		instance: &store.InstanceMessage{
			ResourceID: "inst-1",
			Metadata:   &storepb.Instance{Engine: storepb.Engine_MYSQL},
		},
	}
}

func applicationSQLTestHandler(fake *fakeApplicationSQLStore) (*ApplicationSQLHandler, *fakeApplicationSQLQueue) {
	queue := &fakeApplicationSQLQueue{}
	return &ApplicationSQLHandler{store: fake, analyzer: applicationSQLAnalyzer, queue: queue}, queue
}

func applicationSQLContext(t *testing.T, body string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/applicationSql/report", strings.NewReader(body))
	rec := httptest.NewRecorder()
	return echo.New().NewContext(req, rec), rec
}

// reportRequest is one statement with a valid scope.
func applicationSQLBody(statements string) string {
	return fmt.Sprintf(`{"instance":"instances/inst-1","database":"shop","schema":"","statements":[%s]}`, statements)
}

func TestApplicationSQLReportRequiresAKey(t *testing.T) {
	t.Parallel()

	fake := validApplicationSQLStore()
	h, queue := applicationSQLTestHandler(fake)

	ctx, _ := applicationSQLContext(t, applicationSQLBody(`{"sql":"SELECT a FROM t"}`))
	require.NoError(t, h.report(ctx))

	require.Equal(t, http.StatusUnauthorized, ctx.Response().Status)
	require.Empty(t, fake.reports, "an unauthenticated report must not reach the store")
	require.Empty(t, queue.queued)
}

func TestApplicationSQLReportRejectsAnUnknownKey(t *testing.T) {
	t.Parallel()

	fake := validApplicationSQLStore()
	fake.authErr = common.Errorf(common.NotAuthorized, "invalid API key")
	h, _ := applicationSQLTestHandler(fake)

	ctx, _ := applicationSQLContext(t, applicationSQLBody(`{"sql":"SELECT a FROM t"}`))
	ctx.Request().Header.Set("Authorization", "Bearer asql_forged")
	require.NoError(t, h.report(ctx))

	require.Equal(t, http.StatusUnauthorized, ctx.Response().Status)
	require.Empty(t, fake.reports)
}

func TestApplicationSQLReportEnforcesTheBodyAndBatchLimits(t *testing.T) {
	t.Parallel()

	t.Run("body ceiling", func(t *testing.T) {
		t.Parallel()

		fake := validApplicationSQLStore()
		h, _ := applicationSQLTestHandler(fake)
		ctx, _ := applicationSQLContext(t, strings.Repeat("x", maxApplicationSQLBodySize+1))
		ctx.Request().Header.Set("Authorization", "Bearer asql_key")
		require.NoError(t, h.report(ctx))
		require.Equal(t, http.StatusRequestEntityTooLarge, ctx.Response().Status)
	})

	t.Run("statement ceiling", func(t *testing.T) {
		t.Parallel()

		fake := validApplicationSQLStore()
		h, _ := applicationSQLTestHandler(fake)
		items := make([]string, 0, maxApplicationSQLBatchStatements+1)
		for i := 0; i <= maxApplicationSQLBatchStatements; i++ {
			items = append(items, `{"sql":"SELECT 1"}`)
		}
		ctx, _ := applicationSQLContext(t, applicationSQLBody(strings.Join(items, ",")))
		ctx.Request().Header.Set("Authorization", "Bearer asql_key")
		require.NoError(t, h.report(ctx))
		require.Equal(t, http.StatusBadRequest, ctx.Response().Status)
		require.Empty(t, fake.reports)
	})

	t.Run("no statements", func(t *testing.T) {
		t.Parallel()

		fake := validApplicationSQLStore()
		h, _ := applicationSQLTestHandler(fake)
		ctx, _ := applicationSQLContext(t, applicationSQLBody(""))
		ctx.Request().Header.Set("Authorization", "Bearer asql_key")
		require.NoError(t, h.report(ctx))
		require.Equal(t, http.StatusBadRequest, ctx.Response().Status)
	})
}

func TestApplicationSQLReportResolvesTheScope(t *testing.T) {
	t.Parallel()

	t.Run("malformed instance name", func(t *testing.T) {
		t.Parallel()

		fake := validApplicationSQLStore()
		h, _ := applicationSQLTestHandler(fake)
		ctx, _ := applicationSQLContext(t, `{"instance":"not-a-resource","statements":[{"sql":"SELECT 1"}]}`)
		ctx.Request().Header.Set("Authorization", "Bearer asql_key")
		require.NoError(t, h.report(ctx))
		require.Equal(t, http.StatusBadRequest, ctx.Response().Status)
	})

	t.Run("unknown instance", func(t *testing.T) {
		t.Parallel()

		fake := validApplicationSQLStore()
		fake.instance = nil
		h, _ := applicationSQLTestHandler(fake)
		ctx, _ := applicationSQLContext(t, applicationSQLBody(`{"sql":"SELECT a FROM t"}`))
		ctx.Request().Header.Set("Authorization", "Bearer asql_key")
		require.NoError(t, h.report(ctx))
		require.Equal(t, http.StatusNotFound, ctx.Response().Status)
	})

	t.Run("engine without an analyzer", func(t *testing.T) {
		t.Parallel()

		fake := validApplicationSQLStore()
		fake.instance.Metadata = &storepb.Instance{Engine: storepb.Engine_OCEANBASE}
		h, _ := applicationSQLTestHandler(fake)
		ctx, _ := applicationSQLContext(t, applicationSQLBody(`{"sql":"SELECT a FROM t"}`))
		ctx.Request().Header.Set("Authorization", "Bearer asql_key")
		require.NoError(t, h.report(ctx))
		require.Equal(t, http.StatusBadRequest, ctx.Response().Status)
		require.Empty(t, fake.reports, "an engine without an analyzer is refused before any write")
	})
}

// TestApplicationSQLReportRejectsPerStatement pins that a bad statement does not take its
// neighbours down with it: an application that ships one malformed query must keep reporting
// the rest.
func TestApplicationSQLReportRejectsPerStatement(t *testing.T) {
	t.Parallel()

	fake := validApplicationSQLStore()
	h, queue := applicationSQLTestHandler(fake)

	ctx, rec := applicationSQLContext(t, applicationSQLBody(`{"sql":"SELECT a FROM t"},{"sql":"SELECT FROM WHERE"},{"sql":"SELECT b FROM u","count":5}`))
	ctx.Request().Header.Set("Authorization", "Bearer asql_key")
	require.NoError(t, h.report(ctx))

	require.Equal(t, http.StatusOK, ctx.Response().Status)
	var response applicationSQLReportResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, 2, response.Accepted)
	require.Equal(t, 1, response.Rejected)
	require.Len(t, response.Results, 3)
	require.Equal(t, "ACCEPTED", response.Results[0].Status)
	require.Equal(t, "REJECTED", response.Results[1].Status)
	require.Equal(t, rejectParseError, response.Results[1].Reason)
	require.Equal(t, "ACCEPTED", response.Results[2].Status)

	// Only the accepted statements were stored, and only they were queued for analysis.
	require.Len(t, fake.reports, 2)
	require.Len(t, queue.queued, 2)
	require.Equal(t, "shop", fake.reports[0].DatabaseName)
	require.Equal(t, int16(sqlsanitize.Version), fake.reports[0].FingerprintVersion)
	require.NotEmpty(t, fake.reports[0].Fingerprint)
	require.Equal(t, int64(1), fake.reports[0].Count, "an omitted count means one execution")
	require.Equal(t, int64(5), fake.reports[1].Count)
}

func TestApplicationSQLReportSwallowsARepeatedReport(t *testing.T) {
	t.Parallel()

	fake := validApplicationSQLStore()
	fake.identity.Key.LastReportID = "report-42"
	h, queue := applicationSQLTestHandler(fake)

	ctx, rec := applicationSQLContext(t, `{"instance":"instances/inst-1","database":"shop","reportId":"report-42","statements":[{"sql":"SELECT a FROM t"}]}`)
	ctx.Request().Header.Set("Authorization", "Bearer asql_key")
	require.NoError(t, h.report(ctx))

	require.Equal(t, http.StatusOK, ctx.Response().Status)
	var response applicationSQLReportResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.True(t, response.Duplicate)
	require.Empty(t, fake.reports, "a repeated report must not count twice")
	require.Empty(t, fake.recorded)
	require.Empty(t, queue.queued)
}

func TestApplicationSQLReportRecordsTheOutcomeOnTheKey(t *testing.T) {
	t.Parallel()

	fake := validApplicationSQLStore()
	h, _ := applicationSQLTestHandler(fake)

	body := `{"instance":"instances/inst-1","database":"shop","reportId":"report-7","statements":[{"sql":"SELECT a FROM t"}]}`
	ctx, _ := applicationSQLContext(t, body)
	ctx.Request().Header.Set("Authorization", "Bearer asql_key")
	require.NoError(t, h.report(ctx))

	require.Equal(t, []string{"report-7:1:0"}, fake.recorded,
		"the accepted and rejected counts, and the report id a retry would repeat, are stored on the key")
}
