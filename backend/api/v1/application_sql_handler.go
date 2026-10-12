package v1

import (
	"bytes"
	"context"
	"encoding/json"
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
	ReportApplicationSQL(ctx context.Context, report *store.ReportApplicationSQLMessage) (*store.ApplicationSQLMessage, error)
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
	SQL   string `json:"sql"`
	Count int64  `json:"count"`
	// FirstSeenAt and LastSeenAt are optional; an application that keeps no such record
	// omits them and the server's receipt time is used.
	FirstSeenAt *time.Time `json:"firstSeenAt"`
	LastSeenAt  *time.Time `json:"lastSeenAt"`
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
	if len(request.Statements) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "at least one statement is required"})
	}
	if len(request.Statements) > maxApplicationSQLBatchStatements {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "too many statements in one report"})
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

	// A repeat of the last accepted report is answered from the previous outcome: the work is
	// already done and counting it twice would inflate the frequency.
	if request.ReportID != "" && request.ReportID == identity.Key.LastReportID {
		return c.JSON(http.StatusOK, applicationSQLReportResponse{
			Application: identity.Application.ApplicationID,
			Duplicate:   true,
		})
	}

	response := applicationSQLReportResponse{Application: identity.Application.ApplicationID}
	for index, item := range request.Statements {
		result := h.acceptStatement(ctx, identity.Application.ApplicationID, instanceID, engine, request, item, index)
		if result.Status == "ACCEPTED" {
			response.Accepted++
		} else {
			response.Rejected++
		}
		response.Results = append(response.Results, result)
	}

	if err := h.store.RecordApplicationSQLReport(ctx, identity.Key.ID, request.ReportID, response.Accepted, response.Rejected); err != nil {
		// The report is already stored; failing the response over a counter would invite a
		// retry that counts everything twice.
		slog.Warn("failed to record the application SQL report outcome", clog.WithError(err))
	}
	return c.JSON(http.StatusOK, response)
}

// acceptStatement records one reported statement. A statement the server cannot sanitize or
// the analyzer cannot parse is refused rather than stored: it would never produce lineage, and
// storing it would put a row in the database that nothing can ever use.
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

	var firstGUID string
	for _, statement := range statements {
		sanitized, err := sqlsanitize.Sanitize(engine, statement)
		if err != nil {
			warnApplicationSQLRejection(applicationID, engine, statement, rejectParseError, err)
			return applicationSQLItemResult{Index: index, Status: "REJECTED", Reason: rejectParseError, Detail: err.Error()}
		}
		// The gate is the analyzer itself, not a parser call: StarRocks view DDL the analyzer
		// accepts through its view-body fallback is rejected by the bare parser, so a parser
		// gate would refuse statements that can produce complete lineage.
		if _, err := h.analyzer.Analyze(ctx, engine, sqlsanitize.Analyzable(engine, sanitized.Text)); err != nil {
			warnApplicationSQLRejection(applicationID, engine, sanitized.Text, rejectParseError, err)
			return applicationSQLItemResult{Index: index, Status: "REJECTED", Reason: rejectParseError, Detail: err.Error()}
		}

		report := &store.ReportApplicationSQLMessage{
			ApplicationID:      applicationID,
			InstanceResourceID: instanceResourceID,
			DatabaseName:       strings.TrimSpace(request.Database),
			SchemaName:         strings.TrimSpace(request.Schema),
			Engine:             engine,
			SQLText:            sanitized.Text,
			Canonical:          sanitized.Canonical,
			Fingerprint:        sanitized.Fingerprint,
			FingerprintVersion: sqlsanitize.Version,
			Count:              max(item.Count, 1),
			FirstSeenAt:        derefTime(item.FirstSeenAt),
			LastSeenAt:         derefTime(item.LastSeenAt),
		}
		recorded, err := h.store.ReportApplicationSQL(ctx, report)
		if err != nil {
			slog.Warn("failed to record a reported statement",
				slog.String("application", applicationID),
				slog.String("engine", engine.String()),
				slog.String("statement", sanitized.Text),
				clog.WithError(err))
			return applicationSQLItemResult{Index: index, Status: "REJECTED", Reason: rejectStoreFailed, Detail: err.Error()}
		}
		if firstGUID == "" {
			firstGUID = recorded.GUID
		}
		// The runner analyzes the statement and writes what its analysis implies. Ingestion
		// does not classify: the read/write rule has one implementation, there.
		h.queue.QueueLineageAnalysis(recorded.GUID, storepb.MetaType_APPLICATION_SQL)
	}
	return applicationSQLItemResult{Index: index, Status: "ACCEPTED", GUID: firstGUID}
}

// warnApplicationSQLRejection is the log D6 requires: a statement that is not stored leaves a
// WARN behind, so a producer whose SQL never arrives is visible to whoever operates the
// deployment.
func warnApplicationSQLRejection(applicationID string, engine storepb.Engine, statement, reason string, err error) {
	slog.Warn("refused a reported statement",
		slog.String("application", applicationID),
		slog.String("engine", engine.String()),
		slog.String("reason", reason),
		slog.String("statement", statementLabelForLog(statement)),
		clog.WithError(err))
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
