//go:build integration

package runner

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// qualityExportChunkLimit is the largest slice the server may send. A client
// concatenates the slices, so the cap is part of the contract rather than an
// implementation detail.
const qualityExportChunkLimit = 256 << 10

// TestDataQualityExportMySQLRealServerIntegration downloads quality evidence
// through the public streaming API against the MySQL fixture: it rebuilds the CSV
// document from the slices, checks the BOM, header and row count, and then reads
// the permanent ledger to prove the streamed call is audited with both the
// request and the handler's row summary.
func TestDataQualityExportMySQLRealServerIntegration(t *testing.T) {
	t.Parallel()

	fixture := setupQualityMySQLFixture(t, true)
	ordersGUID := waitForMetaGUIDByName(fixture.ctx, t, fixture.env,
		fmt.Sprintf("%s;%s", fixture.instanceID, fixture.sourceDatabase), storepb.MetaType_TABLE, "orders")

	// One passing target and one failing target, so the export has something to
	// filter and the directory's "failing" switch selects a real subset.
	rowCountMin, rowCountMax := int64(0), int64(1000)
	passingRule := fixture.createRule(t,
		v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
		v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		ordersGUID,
		&v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_RowCount{RowCount: &v1pb.RowCountConfig{
			MinRows: &rowCountMin, MaxRows: &rowCountMax,
		}}},
	)
	failingMinRows := int64(2)
	failingRule := fixture.createRule(t,
		v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
		v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		fixture.usersGUID,
		&v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_RowCount{RowCount: &v1pb.RowCountConfig{
			MinRows: &failingMinRows,
		}}},
	)
	executions := fixture.runRules(t, passingRule.GetName(), failingRule.GetName())
	var execution *v1pb.QualityExecution
	for _, planned := range executions {
		completed := fixture.waitForCompletedExecution(t, planned.GetName())
		if completed.GetRule() == passingRule.GetName() {
			execution = completed
		}
	}
	require.NotNil(t, execution, "the passing rule must have one execution")

	// P1: the executions export is one CSV document built from the same filter as
	// the list, so it holds exactly the execution the run just finished.
	executionsStream, err := fixture.dataQuality.ExportQualityExecutions(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ExportQualityExecutionsRequest{
		Parent: "workspaces/-",
		Rule:   passingRule.GetName(),
	}))
	require.NoError(t, err, "open the executions export")
	executionRecords := readQualityExportDocument(t, collectQualityExportChunks(t, executionsStream))
	require.Len(t, executionRecords, 2, "one header row plus the single execution")
	require.Equal(t, []string{
		"id", "rule", "revision", "target_guid", "target_path", "instance",
		"trigger", "state", "outcome", "observed_value", "observed_kind",
		"failed_rows", "total_rows", "sampled", "truncated",
		"error_code", "error_message", "attempt", "run", "duration_ms",
		"started_at", "finished_at",
	}, executionRecords[0])

	executionRow := executionRecords[1]
	executionID, err := strconv.ParseInt(execution.GetName()[strings.LastIndex(execution.GetName(), "/")+1:], 10, 64)
	require.NoError(t, err)
	require.Equal(t, executionID, parseQualityExecutionRowID(t, executionRow), "the id column is the execution row id")
	require.Equal(t, passingRule.GetName(), executionRow[1])
	require.Equal(t, "MANUAL", executionRow[6])
	require.Equal(t, "COMPLETED", executionRow[7])
	require.Equal(t, execution.GetOutcome().String()[len("QUALITY_STATUS_"):], executionRow[8])
	require.NotEmpty(t, executionRow[20], "a finished execution carries its start time")
	require.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`, executionRow[20], "timestamps are RFC3339 in UTC")

	// P2: the targets export is the directory's rows, with the counts and the
	// health the aggregate query produces.
	targetsStream, err := fixture.dataQuality.ExportQualityTargets(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ExportQualityTargetsRequest{
		Parent:   "workspaces/-",
		Instance: common.FormatInstance(fixture.instanceID),
	}))
	require.NoError(t, err, "open the targets export")
	targetRecords := readQualityExportDocument(t, collectQualityExportChunks(t, targetsStream))
	require.Len(t, targetRecords, 3, "one header row plus the two targets that carry a rule")
	require.Equal(t, []string{
		"guid", "meta_type", "path",
		"rule_count", "enabled_rule_count", "paused_rule_count", "column_count",
		"open_issue_count", "pending_draft_count", "worst_status",
		"last_run_time", "schema_changed", "orphaned",
		"schedule_interval_seconds", "next_run_time",
	}, targetRecords[0])
	targetRows := make(map[string][]string, len(targetRecords)-1)
	for _, record := range targetRecords[1:] {
		targetRows[record[0]] = record
	}
	require.Len(t, targetRows, 2)
	require.Equal(t, "FAIL", targetRows[fixture.usersGUID][9], "the users row count rule failed on the fixture data")
	require.Equal(t, "PASS", targetRows[ordersGUID][9], "the orders row count rule passed on the fixture data")
	require.Equal(t, "TABLE", targetRows[ordersGUID][1])
	require.True(t, strings.HasSuffix(targetRows[ordersGUID][2], ".orders"), "path %q must end at the rule's table", targetRows[ordersGUID][2])
	for guid, record := range targetRows {
		require.Equal(t, "1", record[3], "target %s carries one rule", guid)
		require.Equal(t, "1", record[4], "target %s carries one enabled rule", guid)
		require.NotEmpty(t, record[10], "target %s ran and carries its last run time", guid)
	}

	// P2b: failing_only is the directory's "failing" switch: it selects the FAIL
	// and ERROR targets and nothing else.
	failingStream, err := fixture.dataQuality.ExportQualityTargets(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ExportQualityTargetsRequest{
		Parent:      "workspaces/-",
		Instance:    common.FormatInstance(fixture.instanceID),
		FailingOnly: true,
	}))
	require.NoError(t, err, "open the failing-only targets export")
	failingRecords := readQualityExportDocument(t, collectQualityExportChunks(t, failingStream))
	require.Len(t, failingRecords, 2, "one header row plus only the failing target")
	require.Equal(t, fixture.usersGUID, failingRecords[1][0])
	require.Equal(t, "FAIL", failingRecords[1][9])

	// P2c: failing_only with a single worst_status is contradictory and refused
	// before anything is streamed.
	invalidStream, err := fixture.dataQuality.ExportQualityTargets(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ExportQualityTargetsRequest{
		Parent:      "workspaces/-",
		FailingOnly: true,
		WorstStatus: v1pb.QualityStatus_QUALITY_STATUS_FAIL,
	}))
	if err == nil {
		// A server-streaming status can arrive with the stream rather than with
		// the call, so drain it before reading the error.
		for invalidStream.Receive() {
			t.Fatal("a contradictory filter must not stream any chunk")
		}
		err = invalidStream.Err()
		_ = invalidStream.Close()
	}
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "a contradictory filter is refused")

	// P2d: the failing execution opens an issue, and the issues export carries it.
	issue := waitForExportQualityIssue(t, fixture, failingRule.GetName())
	issuesStream, err := fixture.dataQuality.ExportQualityIssues(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ExportQualityIssuesRequest{
		Parent: "workspaces/-",
		Rule:   failingRule.GetName(),
	}))
	require.NoError(t, err, "open the issues export")
	issueRecords := readQualityExportDocument(t, collectQualityExportChunks(t, issuesStream))
	require.Len(t, issueRecords, 2, "one header row plus the rule's single open issue")
	require.Equal(t, []string{
		"id", "rule", "title", "target_guid", "target_path",
		"severity", "status", "occurrence_count",
		"first_seen_at", "last_seen_at", "resolved_at",
		"resolution_reason", "resolution_note",
		"acknowledged_at", "acknowledged_by", "muted_until",
	}, issueRecords[0])
	issueRow := issueRecords[1]
	require.Equal(t, issue.GetName()[strings.LastIndex(issue.GetName(), "/")+1:], issueRow[0])
	require.Equal(t, failingRule.GetName(), issueRow[1])
	require.Equal(t, failingRule.GetTitle(), issueRow[2])
	require.Equal(t, fixture.usersGUID, issueRow[3])
	require.Equal(t, "ERROR", issueRow[5])
	require.Equal(t, "OPEN", issueRow[6])
	require.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`, issueRow[8], "timestamps are RFC3339 in UTC")

	// P2e: a selection that matches nothing is a valid header-only file, and an
	// owner with no rules matches nothing rather than every issue.
	emptyExecutionsStream, err := fixture.dataQuality.ExportQualityExecutions(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ExportQualityExecutionsRequest{
		Parent:   "workspaces/-",
		Instance: common.FormatInstance("it-export-empty-instance"),
	}))
	require.NoError(t, err, "open an export that selects nothing")
	emptyExecutionsChunks := collectQualityExportChunks(t, emptyExecutionsStream)
	emptyExecutionsRecords := readQualityExportDocument(t, emptyExecutionsChunks)
	require.Len(t, emptyExecutionsRecords, 1, "only the header row")
	require.Zero(t, emptyExecutionsChunks[len(emptyExecutionsChunks)-1].GetRowCount(), "an empty selection has zero data rows")
	require.False(t, emptyExecutionsChunks[len(emptyExecutionsChunks)-1].GetTruncated())

	emptyOwnerStream, err := fixture.dataQuality.ExportQualityIssues(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ExportQualityIssuesRequest{
		Parent: "workspaces/-",
		Owner:  "users/999999",
	}))
	require.NoError(t, err, "open the owner export")
	emptyOwnerRecords := readQualityExportDocument(t, collectQualityExportChunks(t, emptyOwnerStream))
	require.Len(t, emptyOwnerRecords, 1, "an owner with no rules selects no issue")

	// P3: the export is audited. The ledger row carries the request (which rule
	// was selected) and the handler's summary (how many rows were streamed), the
	// two things a unary audit row always had and streams did not.
	auditClient := v1connect.NewAuditLogServiceClient(&http.Client{Timeout: 30 * time.Second}, fixture.env.BaseURL)
	var auditLog *v1pb.AuditLog
	require.Eventually(t, func() bool {
		response, err := auditClient.ListAuditLogs(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ListAuditLogsRequest{
			Parent:   "workspaces/-",
			PageSize: 100,
			Filter:   `method.matches("ExportQualityExecutions")`,
		}))
		if err != nil {
			return false
		}
		for _, entry := range response.Msg.GetAuditLogs() {
			if entry.GetMethod() != v1connect.DataQualityServiceExportQualityExecutionsProcedure {
				continue
			}
			// The scenario streams more than one export; pick the row for the
			// filtered download this test opened first by its request payload.
			if entry.GetRequest().GetFields()["rule"].GetStringValue() != passingRule.GetName() {
				continue
			}
			auditLog = entry
			return true
		}
		return false
	}, 30*time.Second, 250*time.Millisecond, "the streamed export never reached the audit ledger: %s", fixture.env.ServerLogs())

	require.Equal(t, "ok", auditLog.GetStatus().GetMessage())
	require.Equal(t, passingRule.GetName(), auditLog.GetRequest().GetFields()["rule"].GetStringValue(), "the audit row carries the received request")
	require.Equal(t, float64(1), auditLog.GetResponse().GetFields()["exportedRows"].GetNumberValue(), "the audit row carries the streamed row count")
	require.Equal(t, float64(100000), auditLog.GetResponse().GetFields()["maxRows"].GetNumberValue(), "the audit row carries the applied ceiling")

	// P4: a ceiling that cuts the selection flags the final slice, so a client can
	// warn that the file is not complete. The instance's two executions make
	// max_rows=1 truncate a selection of two.
	truncatedStream, err := fixture.dataQuality.ExportQualityExecutions(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ExportQualityExecutionsRequest{
		Parent:   "workspaces/-",
		Instance: common.FormatInstance(fixture.instanceID),
		MaxRows:  1,
	}))
	require.NoError(t, err, "open the truncated executions export")
	truncatedChunks := collectQualityExportChunks(t, truncatedStream)
	truncatedRecords := readQualityExportDocument(t, truncatedChunks)
	require.Len(t, truncatedRecords, 2, "header plus the one row the ceiling allows")
	require.True(t, truncatedChunks[len(truncatedChunks)-1].GetTruncated(), "the ceiling cut the selection short")

	// P5: a client that cancels an export still leaves a ledger row, with the row
	// summary the handler recorded. Whether the server observes the cancellation
	// (WARNING) or finishes first (INFO) depends on timing; the severity mapping
	// itself is pinned by the component test. The lifetime bound that ends a
	// stalled export is unit-tested with a shortened timeout, since a real one
	// is fifteen minutes.
	cancelCtx, cancelExport := context.WithCancel(fixture.ctx)
	canceledStream, err := fixture.dataQuality.ExportQualityExecutions(cancelCtx, withToken(fixture.env.AdminToken(), &v1pb.ExportQualityExecutionsRequest{
		Parent:  "workspaces/-",
		Rule:    passingRule.GetName(),
		MaxRows: 7,
	}))
	require.NoError(t, err, "the export opens before the client cancels")
	cancelExport()
	for canceledStream.Receive() {
	}
	_ = canceledStream.Close()

	require.Eventually(t, func() bool {
		response, err := auditClient.ListAuditLogs(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ListAuditLogsRequest{
			Parent:   "workspaces/-",
			PageSize: 100,
			Filter:   `method.matches("ExportQualityExecutions")`,
		}))
		if err != nil {
			return false
		}
		for _, entry := range response.Msg.GetAuditLogs() {
			if entry.GetMethod() != v1connect.DataQualityServiceExportQualityExecutionsProcedure {
				continue
			}
			if entry.GetRequest().GetFields()["maxRows"].GetNumberValue() != 7 {
				continue
			}
			auditLog = entry
			return true
		}
		return false
	}, 30*time.Second, 250*time.Millisecond, "the canceled export never reached the audit ledger: %s", fixture.env.ServerLogs())
	require.Equal(t, float64(1), auditLog.GetResponse().GetFields()["exportedRows"].GetNumberValue(),
		"the canceled export still records its row summary")
	require.Contains(t,
		[]v1pb.AuditLogSeverity{v1pb.AuditLogSeverity_INFO, v1pb.AuditLogSeverity_WARNING},
		auditLog.GetSeverity(), "a canceled export is not a server ERROR")
}

// waitForExportQualityIssue waits for the issue one failing rule opens.
func waitForExportQualityIssue(t *testing.T, fixture *qualityFixture, ruleName string) *v1pb.QualityIssue {
	t.Helper()

	var issue *v1pb.QualityIssue
	require.Eventually(t, func() bool {
		response, err := fixture.dataQuality.ListQualityIssues(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ListQualityIssuesRequest{
			Parent: "workspaces/-",
			Rule:   ruleName,
		}))
		if err != nil {
			return false
		}
		if len(response.Msg.GetQualityIssues()) == 0 {
			return false
		}
		issue = response.Msg.GetQualityIssues()[0]
		return true
	}, 30*time.Second, 250*time.Millisecond, "the failing rule never opened an issue: %s", fixture.env.ServerLogs())
	return issue
}

// collectQualityExportChunks drains an export stream into its slices.
func collectQualityExportChunks(t *testing.T, stream *connect.ServerStreamForClient[v1pb.ExportQualityDataChunk]) []*v1pb.ExportQualityDataChunk {
	t.Helper()

	var chunks []*v1pb.ExportQualityDataChunk
	for stream.Receive() {
		chunks = append(chunks, stream.Msg())
	}
	require.NoError(t, stream.Err(), "the export stream failed")
	_ = stream.Close()
	return chunks
}

// readQualityExportDocument rebuilds the file from its slices and pins the
// framing contract: the BOM opens the document, slices stay under the cap, the
// row count is cumulative, and the parsed record count matches it.
func readQualityExportDocument(t *testing.T, chunks []*v1pb.ExportQualityDataChunk) [][]string {
	t.Helper()

	require.NotEmpty(t, chunks, "an export always sends at least the header slice")
	var document []byte
	var lastCount int64
	for i, chunk := range chunks {
		require.LessOrEqual(t, len(chunk.GetData()), qualityExportChunkLimit, "slice %d exceeds the cap", i)
		require.GreaterOrEqual(t, chunk.GetRowCount(), lastCount, "slice %d must report a cumulative row count", i)
		lastCount = chunk.GetRowCount()
		document = append(document, chunk.GetData()...)
	}
	require.True(t, bytes.HasPrefix(document, []byte{0xEF, 0xBB, 0xBF}), "the document must start with the UTF-8 BOM")

	reader := csv.NewReader(bytes.NewReader(document[3:]))
	records, err := reader.ReadAll()
	require.NoError(t, err, "the streamed document must be valid CSV")
	require.NotEmpty(t, records, "the document carries the header row")
	require.Equal(t, int64(len(records)-1), lastCount, "the final row count must match the data rows")
	return records
}

// The id column of an execution row is a decimal row id; parsing it keeps the
// test honest about the column being numeric rather than a resource name.
func parseQualityExecutionRowID(t *testing.T, record []string) int64 {
	t.Helper()
	id, err := strconv.ParseInt(record[0], 10, 64)
	require.NoError(t, err)
	return id
}
