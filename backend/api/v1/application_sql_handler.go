package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/Ranxy/metaxisdata/backend/common"
	clog "github.com/Ranxy/metaxisdata/backend/common/log"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/sqlsanitize"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// maxApplicationSQLBodySize bounds one report. It matches the OpenLineage ingestion
	// limit; the statement cap below is the tighter of the two in practice.
	maxApplicationSQLBodySize = 8 << 20
	// maxApplicationSQLBatchStatements bounds the work one report may ask for. A minimal
	// statement is under 100 bytes, so the body limit alone would still admit tens of
	// thousands of them.
	maxApplicationSQLBatchStatements = 1000
)

// applicationSQLStore is what the handler needs from the store, so its refusals can be
// tested without a database. *store.Store is the production implementation.
type applicationSQLStore interface {
	AuthenticateApplicationSQLAPIKey(ctx context.Context, plainKey string) (*store.ApplicationSQLIdentity, error)
	GetInstance(ctx context.Context, find *store.FindInstanceMessage) (*store.InstanceMessage, error)
	ReportApplicationSQLs(ctx context.Context, reports []*store.ReportApplicationSQLMessage) ([]*store.ApplicationSQLMessage, error)
	RecordApplicationSQLReport(ctx context.Context, keyID int64, reportID string, accepted, rejected int) error
}

// applicationSQLLineageQueue is the one thing ingestion asks of the lineage runner: analyze
// this statement and write the edges its analysis implies.
type applicationSQLLineageQueue interface {
	QueueLineageAnalysis(metaGUID string, metaType storepb.MetaType)
}

// ApplicationSQLHandler ingests the SQL an application actually ran, with every literal
// already replaced by the application or by this server.
//
// It is a plain Echo route, so it never reaches the ConnectRPC interceptor chain: its
// authentication is the API key below, and its authorization is the scope the key's
// application reported. See .agents/docs/application-sql-ingestion.md, "The reporting contract".
type ApplicationSQLHandler struct {
	store    applicationSQLStore
	analyzer *lineage.Analyzer
	queue    applicationSQLLineageQueue
}

// NewApplicationSQLHandler creates the handler. analyzer is the process-wide one, not a
// second instance: the gate below has to accept exactly what the runner will later analyze.
func NewApplicationSQLHandler(s *store.Store, analyzer *lineage.Analyzer, queue applicationSQLLineageQueue) *ApplicationSQLHandler {
	return &ApplicationSQLHandler{store: s, analyzer: analyzer, queue: queue}
}

// RegisterRoutes mounts the ingestion route. It sits under /api/v1 rather than /v1 because the
// ConnectRPC gateway owns /v1/*, and the only other machine-ingest endpoint in this server
// lives alongside it.
func (h *ApplicationSQLHandler) RegisterRoutes(g *echo.Group) {
	g.POST("/report", h.report)
}

// applicationSQLReportRequest is one report. The application is not named here: it comes from
// the key, which is what stops a report from claiming to be another application.
type applicationSQLReportRequest struct {
	Instance string `json:"instance"`
	Database string `json:"database"`
	Schema   string `json:"schema"`
	// ReportID is optional. Repeating the last accepted one is answered from the previous
	// outcome instead of counting twice.
	ReportID   string                    `json:"reportId"`
	Statements []applicationSQLStatement `json:"statements"`
}

type applicationSQLStatement struct {
	SQL string `json:"sql"`
	// Count is how many times the statement ran since the last report. Omitted means once;
	// a value below one is malformed rather than a zero-execution report, so it is refused
	// with the body.
	Count *int64 `json:"count"`
	// FirstSeenAt and LastSeenAt are optional; an application that keeps no such record
	// omits them and the server's receipt time is used.
	FirstSeenAt *time.Time `json:"firstSeenAt"`
	LastSeenAt  *time.Time `json:"lastSeenAt"`
}

// reportedCount is one statement's execution count, with the omitted case resolved.
func (s applicationSQLStatement) reportedCount() int64 {
	if s.Count == nil {
		return 1
	}
	return *s.Count
}

// applicationSQLRejection reasons are stable strings the application can branch on.
const (
	rejectParseError     = "PARSE_ERROR"
	rejectEmptyStatement = "EMPTY_STATEMENT"
	rejectStoreFailed    = "STORE_FAILED"
)

type applicationSQLItemResult struct {
	Index  int    `json:"index"`
	Status string `json:"status"`
	GUID   string `json:"guid,omitempty"`
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`

	// Statements is how many statements the entry carried. One entry is usually one statement,
	// but a script in one entry is several, and the key's counters count statements: the response
	// reports entries, and the counters are the only place the statement total is kept.
	Statements int `json:"-"`
}

type applicationSQLReportResponse struct {
	Application string                     `json:"application"`
	Accepted    int                        `json:"accepted"`
	Rejected    int                        `json:"rejected"`
	Duplicate   bool                       `json:"duplicate,omitempty"`
	Results     []applicationSQLItemResult `json:"results"`
}

func (h *ApplicationSQLHandler) report(c echo.Context) error {
	ctx := c.Request().Context()

	// The key is the only credential this route accepts, and the application it names is the
	// only identity a report has.
	key := ExtractIngestionKey(c.Request())
	if key == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing Authorization header"})
	}
	identity, err := h.store.AuthenticateApplicationSQLAPIKey(ctx, key)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid API key"})
	}

	body, err := io.ReadAll(io.LimitReader(c.Request().Body, maxApplicationSQLBodySize+1))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "failed to read the request body"})
	}
	if len(body) > maxApplicationSQLBodySize {
		return c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "request body is too large"})
	}

	var request applicationSQLReportRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	// The body is one JSON object. Anything after it is a second document the caller did not
	// mean to send, and accepting it silently would drop whatever it said.
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "the body must be one JSON object"})
	}
	if len(request.Statements) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "at least one statement is required"})
	}
	if len(request.Statements) > maxApplicationSQLBatchStatements {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "too many statements in one report"})
	}
	for index, item := range request.Statements {
		if item.Count != nil && *item.Count < 1 {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("statements[%d].count must be at least 1", index),
			})
		}
	}

	instanceID, err := common.GetInstanceID(strings.TrimSpace(request.Instance))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid instance"})
	}
	instance, err := h.store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &instanceID})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to resolve the instance"})
	}
	if instance == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "instance not found"})
	}
	engine := instance.Metadata.GetEngine()
	if !h.analyzer.Supports(engine) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "the instance's engine has no lineage analyzer"})
	}

	// A repeat of the last report that landed whole is answered from the previous outcome: the
	// work is already done and counting it twice would inflate the frequency.
	if request.ReportID != "" && request.ReportID == identity.Key.LastReportID {
		return c.JSON(http.StatusOK, applicationSQLReportResponse{
			Application: common.FormatApplication(identity.Application.ApplicationID),
			Duplicate:   true,
		})
	}

	// The response counts entries, one per element of `statements`; the key's counters count the
	// statements inside them, which is what their names say and what a script makes differ.
	response := applicationSQLReportResponse{Application: common.FormatApplication(identity.Application.ApplicationID)}
	acceptedStatements, rejectedStatements := 0, 0
	for index, item := range request.Statements {
		result := h.acceptStatement(ctx, identity.Application.ApplicationID, instanceID, engine, request, item, index)
		if result.Status == "ACCEPTED" {
			response.Accepted++
			acceptedStatements += result.Statements
		} else {
			response.Rejected++
			rejectedStatements += result.Statements
		}
		response.Results = append(response.Results, result)
	}

	// Only a report that landed whole is remembered as the last one. A report with a refusal
	// left work undone, and the id is the application's only lever for sending that work again:
	// remembering it here would answer the retry "duplicate" and lose the refused entries for
	// good. Each entry is stored in one transaction, so resending the refused entries stores
	// exactly them.
	reportID := request.ReportID
	if response.Rejected > 0 {
		reportID = ""
	}
	if err := h.store.RecordApplicationSQLReport(ctx, identity.Key.ID, reportID, acceptedStatements, rejectedStatements); err != nil {
		// The report is already stored; failing the response over a counter would invite a
		// retry that counts everything twice.
		slog.Warn("failed to record the application SQL report outcome", clog.WithError(err))
	}
	return c.JSON(http.StatusOK, response)
}

// acceptStatement records one reported entry. Its statements are sanitized and gated before any
// of them is stored, and then stored in one transaction, so an entry either lands whole or not
// at all: a half-written entry would be counted twice when the application retries it.
//
// A statement the server cannot sanitize, or that the analyzer cannot resolve at all, is
// refused rather than stored: it would never produce lineage, and storing it would put a row in
// the database that nothing can ever use. A statement the analyzer can only partly resolve is
// stored anyway, because that is what the runner does with it.
func (h *ApplicationSQLHandler) acceptStatement(
	ctx context.Context,
	applicationID, instanceResourceID string,
	engine storepb.Engine,
	request applicationSQLReportRequest,
	item applicationSQLStatement,
	index int,
) applicationSQLItemResult {
	sqlText := strings.TrimSpace(item.SQL)
	if sqlText == "" {
		return applicationSQLItemResult{Index: index, Status: "REJECTED", Reason: rejectEmptyStatement}
	}

	// One reported item is one statement; a script is split here, above the dialects, and each
	// statement is fingerprinted on its own. The count applies to every statement in it.
	statements := h.analyzer.SplitScript(engine, sqlText)
	if len(statements) == 0 {
		return applicationSQLItemResult{Index: index, Status: "REJECTED", Reason: rejectEmptyStatement}
	}
	// A refusal carries the entry's statement count like an acceptance does: the key's counters
	// count statements, and a refused entry asked for them too.
	rejected := func(reason, detail string) applicationSQLItemResult {
		return applicationSQLItemResult{
			Index:      index,
			Status:     "REJECTED",
			Reason:     reason,
			Detail:     detail,
			Statements: len(statements),
		}
	}

	reports := make([]*store.ReportApplicationSQLMessage, 0, len(statements))
	for _, statement := range statements {
		sanitized, err := sqlsanitize.Sanitize(engine, statement)
		if err != nil {
			// Nothing of this statement is logged: it is the one input the sanitizer could not
			// rewrite, so any literal it carried is still in it. See warnApplicationSQLRejection.
			warnApplicationSQLRejection(applicationID, engine, "", rejectParseError, err)
			return rejected(rejectParseError, err.Error())
		}
		// The gate is the analyzer itself, not a parser call: StarRocks view DDL the analyzer
		// accepts through its view-body fallback is rejected by the bare parser, so a parser
		// gate would refuse statements that can produce complete lineage.
		//
		// A partial analysis is not a refusal. The analyzer returns the edges it did find
		// together with the gap, and the runner stores both, so refusing here would drop
		// lineage the graph can draw for a statement it can otherwise read.
		if _, err := h.analyzer.Analyze(ctx, engine, sqlsanitize.Analyzable(engine, sanitized.Text)); err != nil {
			var unsupported *lineage.UnsupportedStatementError
			if !errors.As(err, &unsupported) {
				warnApplicationSQLRejection(applicationID, engine, sanitized.Text, rejectParseError, err)
				return rejected(rejectParseError, err.Error())
			}
		}

		reports = append(reports, &store.ReportApplicationSQLMessage{
			ApplicationID:      applicationID,
			InstanceResourceID: instanceResourceID,
			DatabaseName:       strings.TrimSpace(request.Database),
			SchemaName:         strings.TrimSpace(request.Schema),
			Engine:             engine,
			SQLText:            sanitized.Text,
			Canonical:          sanitized.Canonical,
			Fingerprint:        sanitized.Fingerprint,
			FingerprintVersion: sqlsanitize.Version,
			Count:              item.reportedCount(),
			FirstSeenAt:        derefTime(item.FirstSeenAt),
			LastSeenAt:         derefTime(item.LastSeenAt),
		})
	}

	recorded, err := h.store.ReportApplicationSQLs(ctx, reports)
	if err != nil {
		slog.Warn("failed to record a reported entry",
			slog.String("application", applicationID),
			slog.String("engine", engine.String()),
			slog.Int("statements", len(reports)),
			slog.String("statement", statementLabelForLog(reports[0].SQLText)),
			clog.WithError(err))
		return rejected(rejectStoreFailed, err.Error())
	}
	// The runner analyzes each statement and writes what its analysis implies. Ingestion does
	// not classify: the read/write rule has one implementation, there.
	for _, statement := range recorded {
		h.queue.QueueLineageAnalysis(statement.GUID, storepb.MetaType_APPLICATION_SQL)
	}
	return applicationSQLItemResult{
		Index:      index,
		Status:     "ACCEPTED",
		GUID:       recorded[0].GUID,
		Statements: len(statements),
	}
}

// warnApplicationSQLRejection is the log D6 requires: a statement that is not stored leaves a
// WARN behind, so a producer whose SQL never arrives is visible to whoever operates the
// deployment.
//
// statement is the sanitized text, or empty when there is no sanitized form of it — the one
// case where the text still holds its literals. An empty label is left out of the line rather
// than logged: a literal in a log is a literal that left the deployment.
func warnApplicationSQLRejection(applicationID string, engine storepb.Engine, statement, reason string, err error) {
	attributes := make([]any, 0, 5)
	attributes = append(attributes,
		slog.String("application", applicationID),
		slog.String("engine", engine.String()),
		slog.String("reason", reason))
	if label := statementLabelForLog(statement); label != "" {
		attributes = append(attributes, slog.String("statement", label))
	}
	attributes = append(attributes, clog.WithError(err))
	slog.Warn("refused a reported statement", attributes...)
}

// statementLabelForLog bounds what a log line carries: a statement can be long, and the first
// line is enough to recognize it.
func statementLabelForLog(statement string) string {
	if i := strings.IndexByte(statement, '\n'); i >= 0 {
		statement = statement[:i]
	}
	if len(statement) > 160 {
		return statement[:160] + "…"
	}
	return statement
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
