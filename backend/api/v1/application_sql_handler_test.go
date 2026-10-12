package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	entries   int
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

func (f *fakeApplicationSQLStore) ReportApplicationSQLs(_ context.Context, reports []*store.ReportApplicationSQLMessage) ([]*store.ApplicationSQLMessage, error) {
	f.entries++
	if f.reportErr != nil {
		// The real store writes the whole entry in one transaction, so a failure stores none
		// of it. The fake keeps that shape: nothing is counted before the error.
		return nil, f.reportErr
	}
	statements := make([]*store.ApplicationSQLMessage, 0, len(reports))
	for _, report := range reports {
		f.reports = append(f.reports, report)
		statements = append(statements, &store.ApplicationSQLMessage{
			GUID:    fmt.Sprintf("guid-%d", len(f.reports)),
			SQLText: report.SQLText,
		})
	}
	return statements, nil
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

// TestApplicationSQLReportCountsStatementsOnTheKey pins the unit of the key's counters: they are
// named for statements, and one entry carrying a script is several of them. The response counts
// entries, because its results are one per entry.
func TestApplicationSQLReportCountsStatementsOnTheKey(t *testing.T) {
	t.Parallel()

	fake := validApplicationSQLStore()
	h, _ := applicationSQLTestHandler(fake)

	body := `{"instance":"instances/inst-1","database":"shop","reportId":"report-9","statements":[{"sql":"SELECT a FROM t; SELECT b FROM u; SELECT c FROM v"},{"sql":"SELECT FROM WHERE"}]}`
	ctx, rec := applicationSQLContext(t, body)
	ctx.Request().Header.Set("Authorization", "Bearer asql_key")
	require.NoError(t, h.report(ctx))

	var response applicationSQLReportResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, 1, response.Accepted, "the response counts entries")
	require.Equal(t, 1, response.Rejected)
	require.Equal(t, []string{":3:1"}, fake.recorded,
		"the key counts the three statements that landed and the one entry that was refused")
}

// TestApplicationSQLReportStoresAnEntryInOneTransaction pins that one entry is one write: its
// statements are sanitized and gated first, then stored together, so a statement the store
// refuses leaves none of its siblings counted. A half-written entry would be counted again when
// the application resends it.
func TestApplicationSQLReportStoresAnEntryInOneTransaction(t *testing.T) {
	t.Parallel()

	fake := validApplicationSQLStore()
	fake.reportErr = errors.New("the database is gone")
	h, queue := applicationSQLTestHandler(fake)

	ctx, rec := applicationSQLContext(t, applicationSQLBody(`{"sql":"SELECT a FROM t; SELECT b FROM u"}`))
	ctx.Request().Header.Set("Authorization", "Bearer asql_key")
	require.NoError(t, h.report(ctx))

	var response applicationSQLReportResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, 1, response.Rejected)
	require.Equal(t, rejectStoreFailed, response.Results[0].Reason)

	require.Equal(t, 1, fake.entries, "the entry asks the store for one write, not one per statement")
	require.Empty(t, fake.reports, "nothing is stored when the entry's write fails")
	require.Empty(t, queue.queued, "nothing is queued for analysis when it was not stored")

	// The same script with a store that works: both statements land, both are queued.
	working := validApplicationSQLStore()
	h, queue = applicationSQLTestHandler(working)
	ctx, rec = applicationSQLContext(t, applicationSQLBody(`{"sql":"SELECT a FROM t; SELECT b FROM u"}`))
	ctx.Request().Header.Set("Authorization", "Bearer asql_key")
	require.NoError(t, h.report(ctx))

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, 1, response.Accepted)
	require.Len(t, working.reports, 2, "one entry's two statements are stored together")
	require.Len(t, queue.queued, 2)
}

// TestApplicationSQLReportAcceptsAPartialAnalysis pins the gate against the runner: an analyzer
// that resolved some of a statement and reported a gap for the rest has produced lineage, and
// the runner stores what it produced. Refusing at ingest would drop that lineage.
func TestApplicationSQLReportAcceptsAPartialAnalysis(t *testing.T) {
	t.Parallel()

	fake := validApplicationSQLStore()
	h, queue := applicationSQLTestHandler(fake)

	// The predicate names an alias that is not in scope: an edge is still derived from the
	// select list, and the analyzer reports the unresolvable reference beside it.
	ctx, rec := applicationSQLContext(t, applicationSQLBody(`{"sql":"SELECT a FROM t WHERE u.x = 1"}`))
	ctx.Request().Header.Set("Authorization", "Bearer asql_key")
	require.NoError(t, h.report(ctx))

	var response applicationSQLReportResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, 1, response.Accepted)
	require.Len(t, fake.reports, 1)
	require.Len(t, queue.queued, 1)
}

// TestApplicationSQLReportNamesTheApplication pins the response field against the integrator
// doc, which shows a resource name like every other surface of this server.
func TestApplicationSQLReportNamesTheApplication(t *testing.T) {
	t.Parallel()

	fake := validApplicationSQLStore()
	h, _ := applicationSQLTestHandler(fake)

	ctx, rec := applicationSQLContext(t, applicationSQLBody(`{"sql":"SELECT a FROM t"}`))
	ctx.Request().Header.Set("Authorization", "Bearer asql_key")
	require.NoError(t, h.report(ctx))

	var response applicationSQLReportResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, "applications/app-one", response.Application)
}

// TestApplicationSQLReportForgetsTheIdOfAPartialReport pins that only a report which landed
// whole is remembered as the last one. The id is the only lever an application has for sending
// the entries that were refused, so spending it on a partial report would lose them.
func TestApplicationSQLReportForgetsTheIdOfAPartialReport(t *testing.T) {
	t.Parallel()

	fake := validApplicationSQLStore()
	h, _ := applicationSQLTestHandler(fake)

	body := `{"instance":"instances/inst-1","database":"shop","reportId":"report-8","statements":[{"sql":"SELECT a FROM t"},{"sql":"SELECT FROM WHERE"}]}`
	ctx, _ := applicationSQLContext(t, body)
	ctx.Request().Header.Set("Authorization", "Bearer asql_key")
	require.NoError(t, h.report(ctx))

	require.Equal(t, []string{":1:1"}, fake.recorded,
		"a report with a refusal is not remembered as the last one, so its id stays usable")
}

// TestApplicationSQLReportLogsNoStatementItCouldNotSanitize is the one place a literal could
// still reach a sink. The statement a WARN names is the sanitized one; when there is no
// sanitized form, the line carries no statement at all.
func TestApplicationSQLReportLogsNoStatementItCouldNotSanitize(t *testing.T) {
	// Not parallel: this test replaces the process-wide logger.
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	fake := validApplicationSQLStore()
	fake.instance.Metadata = &storepb.Instance{Engine: storepb.Engine_POSTGRES}
	h, _ := applicationSQLTestHandler(fake)

	// PostgreSQL cannot tokenize an unterminated string, so this statement is refused.
	ctx, _ := applicationSQLContext(t, applicationSQLBody(`{"sql":"SELECT a FROM t WHERE email = 'alice@example.com' AND name = 'unterminated"}`))
	ctx.Request().Header.Set("Authorization", "Bearer asql_key")
	require.NoError(t, h.report(ctx))

	line := logs.String()
	require.Contains(t, line, "refused a reported statement")
	require.Contains(t, line, rejectParseError)
	require.NotContains(t, line, "alice@example.com", "a literal reached the log")
	require.NotContains(t, line, "unterminated", "the statement reached the log")

	// A statement refused by the gate has a sanitized form, and that is what the line names.
	// The analyzer echoes the text it was given into its own error, and what it was given is
	// the analyzable form, so that echo carries no literal either.
	logs.Reset()
	ctx, _ = applicationSQLContext(t, applicationSQLBody(`{"sql":"SELECT a FROM t WHERE b = 'secret' GROUP BY"}`))
	ctx.Request().Header.Set("Authorization", "Bearer asql_key")
	require.NoError(t, h.report(ctx))
	require.Contains(t, logs.String(), "SELECT a FROM t WHERE b = ? GROUP BY")
	require.NotContains(t, logs.String(), "secret")
}

// TestApplicationSQLReportRejectsAMalformedBodyShape pins the two body shapes that used to slip
// through: a second JSON document after the first, and an execution count below one.
func TestApplicationSQLReportRejectsAMalformedBodyShape(t *testing.T) {
	t.Parallel()

	t.Run("trailing json", func(t *testing.T) {
		t.Parallel()

		fake := validApplicationSQLStore()
		h, _ := applicationSQLTestHandler(fake)
		ctx, _ := applicationSQLContext(t, applicationSQLBody(`{"sql":"SELECT a FROM t"}`)+`{"sql":"SELECT b FROM u"}`)
		ctx.Request().Header.Set("Authorization", "Bearer asql_key")
		require.NoError(t, h.report(ctx))
		require.Equal(t, http.StatusBadRequest, ctx.Response().Status)
		require.Empty(t, fake.reports)
	})

	for _, count := range []string{"0", "-5"} {
		t.Run("count "+count, func(t *testing.T) {
			t.Parallel()

			fake := validApplicationSQLStore()
			h, _ := applicationSQLTestHandler(fake)
			ctx, _ := applicationSQLContext(t, applicationSQLBody(`{"sql":"SELECT a FROM t","count":`+count+`}`))
			ctx.Request().Header.Set("Authorization", "Bearer asql_key")
			require.NoError(t, h.report(ctx))
			require.Equal(t, http.StatusBadRequest, ctx.Response().Status)
			require.Empty(t, fake.reports, "a malformed count must not become an execution")
		})
	}
}
