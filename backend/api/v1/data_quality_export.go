package v1

import (
	"bytes"
	"context"
	"encoding/csv"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	clog "github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/audit"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// CSV export handlers. Each export streams one CSV document built from the same
// filters as the matching list RPC, so a file and the page a user sees agree
// about what "matching" means.

const (
	// qualityExportMaxRows is both the row ceiling a request that leaves
	// max_rows at zero gets and the hard cap a larger request is coerced to. The
	// ceiling bounds the work a single download may cause: one hundred pages of
	// one thousand rows.
	qualityExportMaxRows = 100000
	// qualityExportPageSize is how many rows one internal list call reads.
	qualityExportPageSize = 1000
	// qualityExportChunkBytes is the largest slice a client receives, and
	// qualityExportFlushBytes is the tail the writer keeps back so the slice that
	// reaches max_rows can be flagged truncated. A streaming export therefore
	// sends 256KiB slices; the final flush sends the remainder in slices of at
	// most qualityExportChunkBytes.
	qualityExportFlushBytes = 64 << 10
	qualityExportChunkBytes = 256 << 10
	// qualityExportBOM opens the document with a UTF-8 byte-order mark, so a
	// spreadsheet detects the encoding without a manual choice.
	qualityExportBOM = "\ufeff"
	// qualityExportTimeout bounds one export end to end, including its store
	// reads. The row ceiling already caps the work at about one hundred pages;
	// this is the backstop for a stalled query or a client that walked away, so
	// one of the few export slots cannot be held until the process restarts.
	qualityExportTimeout = 15 * time.Minute
)

// ExportQualityExecutions streams the executions the request's filters select as
// one CSV document.
func (s *DataQualityService) ExportQualityExecutions(ctx context.Context, req *connect.Request[v1pb.ExportQualityExecutionsRequest], stream *connect.ServerStream[v1pb.ExportQualityDataChunk]) (err error) {
	release, err := s.acquireQualityExportSlot(ctx)
	if err != nil {
		return err
	}
	defer release()
	// The bound covers the filter resolution too: a stalled read there also holds
	// one of the export slots.
	exportCtx, cancel := context.WithTimeout(ctx, qualityExportTimeout)
	defer cancel()
	defer func() {
		err = qualityExportError(exportCtx, v1connect.DataQualityServiceExportQualityExecutionsProcedure, err)
	}()
	workspaceID, err := s.workspaceID(exportCtx, req.Msg.GetParent())
	if err != nil {
		return err
	}
	maxRows := clampQualityExportMaxRows(req.Msg.GetMaxRows())

	find := &store.FindQualityExecutionMessage{}
	if err := s.applyQualityExecutionListFilters(exportCtx, find, qualityExecutionListRequestFromExport(req.Msg)); err != nil {
		return err
	}
	page := func(ctx context.Context, limit, offset int) ([][]string, error) {
		pageFind := *find
		pageFind.Limit = &limit
		pageFind.Offset = &offset
		executions, err := s.store.ListQualityExecutions(ctx, &pageFind)
		if err != nil {
			return nil, errors.Wrap(err, "failed to list the quality executions")
		}
		ruleNames, err := s.qualityRuleNames(ctx, workspaceID, qualityExecutionRuleIDs(executions))
		if err != nil {
			return nil, err
		}
		records := make([][]string, 0, len(executions))
		for _, execution := range executions {
			records = append(records, qualityExecutionExportRecord(workspaceID, execution, ruleNames[execution.RuleID]))
		}
		return records, nil
	}
	return writeQualityCSVExport(exportCtx, stream, qualityExecutionExportHeader(), maxRows, page)
}

// ExportQualityIssues streams the issues the request's filters select as one CSV
// document.
func (s *DataQualityService) ExportQualityIssues(ctx context.Context, req *connect.Request[v1pb.ExportQualityIssuesRequest], stream *connect.ServerStream[v1pb.ExportQualityDataChunk]) (err error) {
	release, err := s.acquireQualityExportSlot(ctx)
	if err != nil {
		return err
	}
	defer release()
	exportCtx, cancel := context.WithTimeout(ctx, qualityExportTimeout)
	defer cancel()
	defer func() {
		err = qualityExportError(exportCtx, v1connect.DataQualityServiceExportQualityIssuesProcedure, err)
	}()
	workspaceID, err := s.workspaceID(exportCtx, req.Msg.GetParent())
	if err != nil {
		return err
	}
	maxRows := clampQualityExportMaxRows(req.Msg.GetMaxRows())

	find := &store.FindQualityIssueMessage{}
	if err := applyQualityIssueListFilters(find, qualityIssueListRequestFromExport(req.Msg)); err != nil {
		return err
	}
	if ruleName := strings.TrimSpace(req.Msg.GetRule()); ruleName != "" {
		_, ruleResourceID, err := s.ruleResourceID(exportCtx, ruleName)
		if err != nil {
			return err
		}
		rule, err := s.fetchQualityRule(exportCtx, ruleResourceID)
		if err != nil {
			return err
		}
		find.RuleID = &rule.ID
	}
	// An owner with no rules matches no issue. The owner filter is an issue
	// filter by rule ids, and an empty id list would drop the predicate and widen
	// the export, so the empty case is handled here instead.
	matchesNothing := false
	if owner := strings.TrimSpace(req.Msg.GetOwner()); owner != "" {
		ruleIDs, err := s.qualityRuleIDsOfOwner(exportCtx, owner)
		if err != nil {
			return err
		}
		if len(ruleIDs) == 0 {
			matchesNothing = true
		} else {
			find.RuleIDs = ruleIDs
		}
	}
	page := func(ctx context.Context, limit, offset int) ([][]string, error) {
		if matchesNothing {
			return nil, nil
		}
		pageFind := *find
		pageFind.Limit = &limit
		pageFind.Offset = &offset
		issues, err := s.store.ListQualityIssues(ctx, &pageFind)
		if err != nil {
			return nil, errors.Wrap(err, "failed to list the quality issues")
		}
		rules, err := s.store.ListQualityRulesByIDs(ctx, qualityIssueRuleIDs(issues))
		if err != nil {
			return nil, errors.Wrap(err, "failed to resolve the quality rules of the issues")
		}
		ruleByID := make(map[int64]*store.QualityRuleMessage, len(rules))
		for _, rule := range rules {
			ruleByID[rule.ID] = rule
		}
		records := make([][]string, 0, len(issues))
		for _, issue := range issues {
			records = append(records, qualityIssueExportRecord(workspaceID, issue, ruleByID[issue.RuleID]))
		}
		return records, nil
	}
	return writeQualityCSVExport(exportCtx, stream, qualityIssueExportHeader(), maxRows, page)
}

// ExportQualityTargets streams the targets the request's filters select as one
// CSV document.
func (s *DataQualityService) ExportQualityTargets(ctx context.Context, req *connect.Request[v1pb.ExportQualityTargetsRequest], stream *connect.ServerStream[v1pb.ExportQualityDataChunk]) (err error) {
	release, err := s.acquireQualityExportSlot(ctx)
	if err != nil {
		return err
	}
	defer release()
	if err := validateQualityTargetExportFilters(req.Msg); err != nil {
		return err
	}
	exportCtx, cancel := context.WithTimeout(ctx, qualityExportTimeout)
	defer cancel()
	defer func() {
		err = qualityExportError(exportCtx, v1connect.DataQualityServiceExportQualityTargetsProcedure, err)
	}()
	if _, err := s.workspaceID(exportCtx, req.Msg.GetParent()); err != nil {
		return err
	}
	maxRows := clampQualityExportMaxRows(req.Msg.GetMaxRows())

	worstStatus, err := qualityTargetWorstStatusFromV1(req.Msg.GetWorstStatus())
	if err != nil {
		return err
	}
	find := &store.FindQualityTargetMessage{
		WorstStatus:     worstStatus,
		HasOpenIssue:    req.Msg.GetHasOpenIssue(),
		HasPendingDraft: req.Msg.GetHasPendingDraft(),
		Order:           qualityTargetOrderFromV1(req.Msg.GetOrderBy()),
	}
	if req.Msg.GetFailingOnly() {
		// The directory's "failing" switch is FAIL or ERROR; the store filter
		// carries the pair in one predicate, so one page sequence stays ordered.
		find.WorstStatuses = []storepb.QualityStatus{
			storepb.QualityStatus_QUALITY_STATUS_FAIL,
			storepb.QualityStatus_QUALITY_STATUS_ERROR,
		}
	}
	applyQualityTargetListFilters(find, qualityTargetListRequestFromExport(req.Msg))
	enrich := s.newQualityTargetExportEnricher()
	page := func(ctx context.Context, limit, offset int) ([][]string, error) {
		pageFind := *find
		pageFind.Limit = &limit
		pageFind.Offset = &offset
		targets, err := s.store.ListQualityTargets(ctx, &pageFind)
		if err != nil {
			return nil, errors.Wrap(err, "failed to list the quality targets")
		}
		if err := enrich.enrich(ctx, targets); err != nil {
			return nil, err
		}
		records := make([][]string, 0, len(targets))
		for _, target := range targets {
			records = append(records, qualityTargetExportRecord(target))
		}
		return records, nil
	}
	return writeQualityCSVExport(exportCtx, stream, qualityTargetExportHeader(), maxRows, page)
}

// qualityTargetExportEnricher fills the two columns the aggregate query cannot
// answer on its own, mirroring what ListQualityTargets does for the directory:
// the checked column count comes from the rule configs, and "orphaned" comes from
// the registry read. The resolution is cached per target for the export's
// lifetime, so a GUID that appears again (possible while rows are written during
// OFFSET paging) is not resolved twice.
type qualityTargetExportEnricher struct {
	service *DataQualityService
	columns map[store.MetaGUIDKey]int32
	orphans map[store.MetaGUIDKey]bool
}

func (s *DataQualityService) newQualityTargetExportEnricher() *qualityTargetExportEnricher {
	return &qualityTargetExportEnricher{
		service: s,
		columns: make(map[store.MetaGUIDKey]int32),
		orphans: make(map[store.MetaGUIDKey]bool),
	}
}

// enrich fills one page's targets, reading the registry state only for the
// targets this export has not resolved yet.
func (e *qualityTargetExportEnricher) enrich(ctx context.Context, targets []*store.QualityTargetMessage) error {
	missing := make([]*store.QualityTargetMessage, 0, len(targets))
	for _, target := range targets {
		key := store.MetaGUIDKey{GUID: target.GUID, ObjectType: target.Type}
		if columnCount, ok := e.columns[key]; ok {
			target.ColumnCount = columnCount
			target.Orphaned = e.orphans[key]
			continue
		}
		missing = append(missing, target)
	}
	if len(missing) == 0 {
		return nil
	}
	keys := make([]store.MetaGUIDKey, 0, len(missing))
	for _, target := range missing {
		keys = append(keys, store.MetaGUIDKey{GUID: target.GUID, ObjectType: target.Type})
	}
	pageRules, err := e.service.store.ListQualityRulesByTargets(ctx, keys)
	if err != nil {
		return errors.Wrap(err, "failed to read the targets' rules")
	}
	columnSets := make(map[store.MetaGUIDKey]map[string]struct{}, len(missing))
	for _, rule := range pageRules {
		column := dataquality.ConfigColumn(rule.Config)
		if column == "" {
			continue
		}
		key := store.MetaGUIDKey{GUID: rule.TargetGUID, ObjectType: rule.TargetType}
		if columnSets[key] == nil {
			columnSets[key] = map[string]struct{}{}
		}
		columnSets[key][column] = struct{}{}
	}
	orphanReasons, err := e.service.targetOrphanReasons(ctx, pageRules)
	if err != nil {
		return err
	}
	for _, rule := range pageRules {
		if orphanReasons[rule.ID] != "" {
			e.orphans[store.MetaGUIDKey{GUID: rule.TargetGUID, ObjectType: rule.TargetType}] = true
		}
	}
	for _, target := range missing {
		key := store.MetaGUIDKey{GUID: target.GUID, ObjectType: target.Type}
		columnCount := int32(len(columnSets[key]))
		e.columns[key] = columnCount
		target.ColumnCount = columnCount
		target.Orphaned = e.orphans[key]
	}
	return nil
}

// validateQualityTargetExportFilters rejects a request whose filters contradict
// each other. failing_only is the directory's "failing" switch (FAIL or ERROR);
// pairing it with a single worst_status asks for an intersection that is empty
// for every status but those two, so it is easier to refuse than to guess.
func validateQualityTargetExportFilters(request *v1pb.ExportQualityTargetsRequest) error {
	if request.GetFailingOnly() && request.GetWorstStatus() != v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED {
		return connectErrorInvalid("failing_only and worst_status cannot be combined")
	}
	return nil
}

// acquireQualityExportSlot takes one of the concurrent-export slots, mapping a
// canceled or timed-out request to the connect status it deserves; the raw
// context error would otherwise be reported as an internal failure.
func (s *DataQualityService) acquireQualityExportSlot(ctx context.Context) (func(), error) {
	release, err := s.acquireExportSlot(ctx)
	switch {
	case err == nil:
		return release, nil
	case errors.Is(err, context.Canceled):
		return nil, connect.NewError(connect.CodeCanceled, err)
	case errors.Is(err, context.DeadlineExceeded):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, err)
	default:
		return nil, err
	}
}

// qualityExportPage returns one page of already-formatted CSV records. It
// returns fewer than limit records only when the underlying list is exhausted.
type qualityExportPage func(ctx context.Context, limit, offset int) ([][]string, error)

// qualityChunkSender is the part of a server stream an export uses; a pointer to
// connect.ServerStream implements it.
type qualityChunkSender interface {
	Send(*v1pb.ExportQualityDataChunk) error
}

// qualityExportError maps an export's failure after its bounded context ends.
// The lifetime bound gets its own status and exactly one warning, so an operator
// can tell a server-stopped export from a real read failure; a successful export
// is returned unchanged even if the bound fired a moment later.
func qualityExportError(ctx context.Context, procedure string, err error) error {
	if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return err
	}
	slog.Warn("quality export reached its lifetime bound", "procedure", procedure, clog.WithError(err))
	return connect.NewError(connect.CodeDeadlineExceeded, errors.New("the export exceeded its lifetime bound and was stopped"))
}

// writeQualityCSVExport streams one CSV document and records its row summary on
// the audit row, so a download leaves both the file and the ledger entry.
func writeQualityCSVExport(ctx context.Context, stream qualityChunkSender, header []string, maxRows int, page qualityExportPage) error {
	exported, err := streamQualityCSV(ctx, stream, header, maxRows, page)
	audit.RecordStreamResult(ctx, map[string]any{
		"exportedRows": exported,
		"maxRows":      int64(maxRows),
	})
	return err
}

// streamQualityCSV writes the header and then up to maxRows data rows, fetching
// one page at a time. The export is a sequence of list queries, not one snapshot:
// a row written while the pages are read may land on either side of a page
// boundary, which is acceptable for a download and never promises transactional
// consistency.
//
// The loop stops when a page returns fewer rows than it asked for, or when
// max_rows is reached. Records are only ever emitted whole: a page is truncated
// at a row boundary, never in the middle of a record. When the ceiling does cut
// rows, the document's last slice is flagged truncated.
func streamQualityCSV(ctx context.Context, stream qualityChunkSender, header []string, maxRows int, page qualityExportPage) (int64, error) {
	writer := newQualityExportCSVWriter(stream)
	if err := writer.writeHeader(header); err != nil {
		return writer.rows, err
	}
	offset := 0
	truncated := false
	for exported := 0; exported < maxRows; {
		limit := min(qualityExportPageSize, maxRows-exported)
		records, err := page(ctx, limit, offset)
		if err != nil {
			return writer.rows, err
		}
		written := 0
		for _, record := range records {
			if exported >= maxRows {
				break
			}
			if err := writer.writeRecord(record); err != nil {
				return writer.rows, err
			}
			exported++
			written++
		}
		offset += len(records)
		if exported >= maxRows {
			// The ceiling cut the page short, or it matched the selection exactly.
			// The extra one-row query is deliberate and cheap: only a look behind
			// the ceiling can tell "exactly max_rows rows" from "more rows exist".
			truncated = written < len(records)
			if !truncated {
				extra, err := page(ctx, 1, offset)
				if err != nil {
					return writer.rows, err
				}
				truncated = len(extra) > 0
			}
			break
		}
		if len(records) < limit {
			break
		}
	}
	if err := writer.finish(truncated); err != nil {
		return writer.rows, err
	}
	return writer.rows, nil
}

// clampQualityExportMaxRows applies the ceiling documented on every export
// request: zero means the server default, and a larger value is coerced to the
// hard cap.
func clampQualityExportMaxRows(requested int32) int {
	if requested <= 0 {
		return qualityExportMaxRows
	}
	return min(int(requested), qualityExportMaxRows)
}

// qualityExportCSVWriter assembles the CSV document and sends it in slices. The
// row count reported by a slice only counts records whose bytes are fully
// contained in that slice, so a split inside a long record neither loses nor
// double-counts it; the final slice always reports the export's row count.
type qualityExportCSVWriter struct {
	stream  qualityChunkSender
	pending []byte
	rowEnds []int
	rows    int64
}

func newQualityExportCSVWriter(stream qualityChunkSender) *qualityExportCSVWriter {
	return &qualityExportCSVWriter{stream: stream}
}

// writeHeader writes the BOM and header row. The header is not a data row, so it
// never contributes to row_count.
func (w *qualityExportCSVWriter) writeHeader(header []string) error {
	var buffer bytes.Buffer
	if _, err := buffer.WriteString(qualityExportBOM); err != nil {
		return errors.Wrap(err, "failed to write the UTF-8 BOM")
	}
	if err := appendCSVRecord(&buffer, header); err != nil {
		return err
	}
	w.pending = append(w.pending, buffer.Bytes()...)
	return nil
}

// writeRecord appends one data row and sends the slices that are safely ahead
// of the tail once enough is pending.
func (w *qualityExportCSVWriter) writeRecord(record []string) error {
	var buffer bytes.Buffer
	if err := appendCSVRecord(&buffer, record); err != nil {
		return err
	}
	w.pending = append(w.pending, buffer.Bytes()...)
	w.rowEnds = append(w.rowEnds, len(w.pending))
	if len(w.pending) >= qualityExportChunkBytes+qualityExportFlushBytes {
		return w.flushReady()
	}
	return nil
}

// flushReady sends the slices that are safely ahead of the tail. The tail is kept
// pending until finish, so the document's last slice can carry the truncated flag
// once the export knows the ceiling cut it.
func (w *qualityExportCSVWriter) flushReady() error {
	for len(w.pending) > qualityExportChunkBytes+qualityExportFlushBytes {
		if err := w.sendSlice(qualityExportChunkBytes, false); err != nil {
			return err
		}
	}
	return nil
}

// finish sends the remaining bytes. The final slice carries the truncated flag
// when the row ceiling cut the selection short.
func (w *qualityExportCSVWriter) finish(truncated bool) error {
	for len(w.pending) > 0 {
		size := min(len(w.pending), qualityExportChunkBytes)
		if err := w.sendSlice(size, truncated && size == len(w.pending)); err != nil {
			return err
		}
	}
	return nil
}

// sendSlice sends one slice and advances the cumulative row count. A record
// counts for a slice only once its last byte is in it, so a split inside a long
// record neither loses nor double-counts it.
func (w *qualityExportCSVWriter) sendSlice(size int, truncated bool) error {
	for len(w.rowEnds) > 0 && w.rowEnds[0] <= size {
		w.rows++
		w.rowEnds = w.rowEnds[1:]
	}
	for i := range w.rowEnds {
		w.rowEnds[i] -= size
	}
	data := make([]byte, size)
	copy(data, w.pending[:size])
	w.pending = w.pending[size:]
	return w.stream.Send(&v1pb.ExportQualityDataChunk{Data: data, RowCount: w.rows, Truncated: truncated})
}

// appendCSVRecord encodes one CSV record, quoting it with encoding/csv's rules.
func appendCSVRecord(buffer *bytes.Buffer, record []string) error {
	writer := csv.NewWriter(buffer)
	if err := writer.Write(record); err != nil {
		return errors.Wrap(err, "failed to format a CSV record")
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return errors.Wrap(err, "failed to format a CSV record")
	}
	return nil
}

// qualityExportFormulaTriggers are the first bytes a spreadsheet may treat as the
// start of a formula.
const qualityExportFormulaTriggers = "=+-@\t\r"

// qualityExportText neutralizes one free-text or identity cell for spreadsheet
// consumption: a cell whose first byte triggers a formula gets a leading single
// quote, which Excel/Sheets/LibreOffice render as text. It must be applied only
// to text cells — numeric, boolean, timestamp and enum columns keep their exact
// value, so a negative observation stays "-5".
func qualityExportText(value string) string {
	if value == "" {
		return value
	}
	if strings.IndexByte(qualityExportFormulaTriggers, value[0]) >= 0 {
		return "'" + value
	}
	return value
}

// qualityExecutionListRequestFromExport projects an export request onto the list
// request whose filter mapping it shares, so the two RPCs cannot drift.
func qualityExecutionListRequestFromExport(request *v1pb.ExportQualityExecutionsRequest) *v1pb.ListQualityExecutionsRequest {
	return &v1pb.ListQualityExecutionsRequest{
		Parent:     request.GetParent(),
		State:      request.GetState(),
		Outcome:    request.GetOutcome(),
		Trigger:    request.GetTrigger(),
		Rule:       request.GetRule(),
		TargetGuid: request.GetTargetGuid(),
		Instance:   request.GetInstance(),
		Initiator:  request.GetInitiator(),
		StartTime:  request.GetStartTime(),
		EndTime:    request.GetEndTime(),
		Run:        request.GetRun(),
	}
}

// qualityIssueListRequestFromExport projects the filters the list mapper owns.
// The rule and owner filters name other resources, so the export resolves them
// exactly as ListQualityIssues does rather than through this projection.
func qualityIssueListRequestFromExport(request *v1pb.ExportQualityIssuesRequest) *v1pb.ListQualityIssuesRequest {
	return &v1pb.ListQualityIssuesRequest{
		Status:     request.GetStatus(),
		Severity:   request.GetSeverity(),
		TargetGuid: request.GetTargetGuid(),
		StartTime:  request.GetStartTime(),
		EndTime:    request.GetEndTime(),
	}
}

// qualityTargetListRequestFromExport projects the path and search filters onto
// the list request whose mapper the export shares. The attention filters and the
// order are set on the store filter directly: failing_only has no list
// counterpart, so the two requests are not interchangeable.
func qualityTargetListRequestFromExport(request *v1pb.ExportQualityTargetsRequest) *v1pb.ListQualityTargetsRequest {
	return &v1pb.ListQualityTargetsRequest{
		Instance: request.GetInstance(),
		Database: request.GetDatabase(),
		Schema:   request.GetSchema(),
		Table:    request.GetTable(),
		Search:   request.GetSearch(),
	}
}

// qualityExecutionExportHeader names the execution columns. The names match the
// proto fields so a consumer can map a column back to the API without a table.
// The "truncated" column is the execution's own failure-sample truncation flag;
// the chunk message's truncated field means the row ceiling cut the export, and
// the two are unrelated.
func qualityExecutionExportHeader() []string {
	return []string{
		"id", "rule", "revision", "target_guid", "target_path", "instance",
		"trigger", "state", "outcome", "observed_value", "observed_kind",
		"failed_rows", "total_rows", "sampled", "truncated",
		"error_code", "error_message", "attempt", "run", "duration_ms",
		"started_at", "finished_at",
	}
}

// qualityExecutionExportRecord renders one execution row.
func qualityExecutionExportRecord(workspaceID string, execution *store.QualityExecutionMessage, ruleName string) []string {
	instance := ""
	if execution.InstanceResourceID != "" {
		instance = common.FormatInstance(execution.InstanceResourceID)
	}
	return []string{
		strconv.FormatInt(execution.ID, 10),
		qualityExportText(ruleName),
		strconv.FormatInt(int64(execution.RuleRevision), 10),
		qualityExportText(execution.TargetGUID),
		qualityExportText(qualityTargetPathString(execution.TargetPath)),
		qualityExportText(instance),
		qualityExportEnum(execution.Trigger.String(), "QUALITY_EXECUTION_TRIGGER_"),
		qualityExportEnum(execution.State.String(), "QUALITY_EXECUTION_STATE_"),
		qualityExportEnum(execution.Status.String(), "QUALITY_STATUS_"),
		qualityExportFloat(execution.ObservedValue),
		qualityExportEnum(execution.ObservedKind.String(), "QUALITY_OBSERVED_KIND_"),
		strconv.FormatInt(execution.FailedRows, 10),
		strconv.FormatInt(execution.TotalRows, 10),
		strconv.FormatBool(execution.Sampled),
		strconv.FormatBool(execution.Truncated),
		execution.ErrorCode,
		qualityExportText(execution.ErrorMessage),
		strconv.FormatInt(int64(execution.Attempt), 10),
		qualityExportText(formatQualityRunName(workspaceID, execution.RunID)),
		strconv.FormatInt(int64(execution.DurationMS), 10),
		qualityExportTime(execution.StartedAt),
		qualityExportTime(execution.FinishedAt),
	}
}

// qualityIssueExportHeader names the issue columns.
func qualityIssueExportHeader() []string {
	return []string{
		"id", "rule", "title", "target_guid", "target_path",
		"severity", "status", "occurrence_count",
		"first_seen_at", "last_seen_at", "resolved_at",
		"resolution_reason", "resolution_note",
		"acknowledged_at", "acknowledged_by", "muted_until",
	}
}

// qualityIssueExportRecord renders one issue row. The title and path belong to
// the rule, which owns both, so they are empty only when the rule is gone.
func qualityIssueExportRecord(workspaceID string, issue *store.QualityIssueMessage, rule *store.QualityRuleMessage) []string {
	ruleName, title := "", ""
	var targetPath *storepb.QualityTargetPath
	if rule != nil {
		ruleName = formatQualityRuleName(workspaceID, rule.ResourceID)
		title = rule.Title
		targetPath = rule.TargetPath
	}
	return []string{
		qualityExportText(issue.ResourceID),
		qualityExportText(ruleName),
		qualityExportText(title),
		qualityExportText(issue.TargetGUID),
		qualityExportText(qualityTargetPathString(targetPath)),
		qualityExportEnum(issue.Severity.String(), "QUALITY_SEVERITY_"),
		qualityExportEnum(issue.Status.String(), "QUALITY_ISSUE_STATUS_"),
		strconv.FormatInt(int64(issue.OccurrenceCount), 10),
		qualityExportTime(issue.FirstSeenAt),
		qualityExportTime(issue.LastSeenAt),
		qualityExportTimePtr(issue.ResolvedAt),
		qualityExportEnum(issue.ResolutionReason.String(), "QUALITY_RESOLUTION_REASON_"),
		qualityExportText(issue.ResolutionNote),
		qualityExportTimePtr(issue.AcknowledgedAt),
		qualityExportText(formatQualityOwner(issue.AcknowledgedBy)),
		qualityExportTimePtr(issue.MutedUntil),
	}
}

// qualityTargetExportHeader names the target columns. pending_label_count is
// deliberately absent: this file is the rule directory, and labels are a separate
// read (the report and the target page render them).
func qualityTargetExportHeader() []string {
	return []string{
		"guid", "meta_type", "path",
		"rule_count", "enabled_rule_count", "paused_rule_count", "column_count",
		"open_issue_count", "pending_draft_count", "worst_status",
		"last_run_time", "schema_changed", "orphaned",
		"schedule_interval_seconds", "next_run_time",
	}
}

// qualityTargetExportRecord renders one target row.
func qualityTargetExportRecord(target *store.QualityTargetMessage) []string {
	interval := ""
	if target.ScheduleInterval > 0 {
		interval = strconv.FormatInt(int64(target.ScheduleInterval/time.Second), 10)
	}
	return []string{
		qualityExportText(target.GUID),
		// MetaType's proto value names carry no QUALITY_-style prefix, so it is
		// rendered bare rather than through a prefix strip.
		qualityExportEnum(target.Type.String(), ""),
		qualityExportText(qualityTargetPathString(target.Path)),
		strconv.FormatInt(int64(target.RuleCount), 10),
		strconv.FormatInt(int64(target.EnabledCount), 10),
		strconv.FormatInt(int64(target.PausedCount), 10),
		strconv.FormatInt(int64(target.ColumnCount), 10),
		strconv.FormatInt(int64(target.OpenIssueCount), 10),
		strconv.FormatInt(int64(target.PendingDraftCount), 10),
		qualityExportEnum(target.WorstStatus.String(), "QUALITY_STATUS_"),
		qualityExportTimePtr(target.LastRunTime),
		strconv.FormatBool(target.SchemaChanged),
		strconv.FormatBool(target.Orphaned),
		interval,
		qualityExportTimePtr(target.NextRunAt),
	}
}

// qualityExportTime renders a timestamp as RFC3339 in UTC; an unset time renders
// empty rather than as the epoch.
func qualityExportTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func qualityExportTimePtr(value *time.Time) string {
	if value == nil {
		return ""
	}
	return qualityExportTime(*value)
}

// qualityExportFloat renders a numeric observation as-is; an absent one renders
// empty so a legitimate zero stays distinguishable from "not observed".
func qualityExportFloat(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}

// qualityExportEnum renders a proto enum value as its readable short name, e.g.
// QUALITY_STATUS_FAIL becomes FAIL. The zero value renders empty because the
// column carries no value rather than a named one.
func qualityExportEnum(name, prefix string) string {
	if name == "" || name == prefix+"UNSPECIFIED" {
		return ""
	}
	return strings.TrimPrefix(name, prefix)
}

// qualityTargetPathString renders a target path snapshot as the dotted path the
// UI shows: instance.database.schema.object[.column].
func qualityTargetPathString(path *storepb.QualityTargetPath) string {
	if path == nil {
		return ""
	}
	segments := make([]string, 0, 5)
	for _, segment := range []string{path.GetInstance(), path.GetDatabase(), path.GetSchema(), path.GetObject(), path.GetColumn()} {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	return strings.Join(segments, ".")
}
