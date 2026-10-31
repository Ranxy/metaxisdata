package v1

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// recordingChunkSender captures the slices an export would stream.
type recordingChunkSender struct {
	chunks []*v1pb.ExportQualityDataChunk
	err    error
}

func (s *recordingChunkSender) Send(chunk *v1pb.ExportQualityDataChunk) error {
	if s.err != nil {
		return s.err
	}
	s.chunks = append(s.chunks, chunk)
	return nil
}

// document concatenates every slice, the way a client rebuilds the file.
// lastChunk is the slice the export finished on; it carries the truncated flag.
func (s *recordingChunkSender) lastChunk() *v1pb.ExportQualityDataChunk {
	return s.chunks[len(s.chunks)-1]
}

func (s *recordingChunkSender) document() []byte {
	var document []byte
	for _, chunk := range s.chunks {
		document = append(document, chunk.GetData()...)
	}
	return document
}

func TestClampQualityExportMaxRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		requested int32
		want      int
	}{
		{name: "zero means the server default", requested: 0, want: qualityExportMaxRows},
		{name: "negative means the server default", requested: -5, want: qualityExportMaxRows},
		{name: "a smaller value is honored", requested: 25, want: 25},
		{name: "the cap itself is honored", requested: qualityExportMaxRows, want: qualityExportMaxRows},
		{name: "a larger value is coerced to the cap", requested: qualityExportMaxRows + 1, want: qualityExportMaxRows},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, clampQualityExportMaxRows(tt.requested))
		})
	}
}

// The document starts with the BOM and the header, and the row count reported by
// each slice is cumulative and ends at the export's row count.
func TestQualityExportCSVWriterStreamsBOMHeaderAndCumulativeRows(t *testing.T) {
	t.Parallel()

	sender := &recordingChunkSender{}
	writer := newQualityExportCSVWriter(sender)
	header := []string{"id", "rule"}
	require.NoError(t, writer.writeHeader(header))

	// Enough rows that the writer flushes while rows are still arriving, so the
	// slice bounds and the cumulative count are exercised rather than the final
	// one-shot flush.
	for i := range qualityExportPageSize {
		require.NoError(t, writer.writeRecord([]string{strconv.Itoa(i), strings.Repeat("r", 400)}))
	}
	require.NoError(t, writer.finish(false))

	require.NotEmpty(t, sender.chunks)
	var lastCount int64
	for i, chunk := range sender.chunks {
		require.LessOrEqual(t, len(chunk.GetData()), qualityExportChunkBytes, "slice %d exceeds the cap", i)
		require.GreaterOrEqual(t, chunk.GetRowCount(), lastCount, "slice %d must report a cumulative count", i)
		lastCount = chunk.GetRowCount()
		if i < len(sender.chunks)-1 {
			require.GreaterOrEqual(t, len(chunk.GetData()), qualityExportFlushBytes,
				"only the final slice may be shorter than the flush threshold")
		}
	}
	require.Equal(t, int64(qualityExportPageSize), lastCount)
	require.Equal(t, int64(qualityExportPageSize), writer.rows)

	document := sender.document()
	require.True(t, bytes.HasPrefix(document, []byte(qualityExportBOM)), "the first bytes must be the UTF-8 BOM")

	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(document, []byte(qualityExportBOM))))
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, qualityExportPageSize+1)
	require.Equal(t, header, records[0])
	require.Equal(t, []string{"0", strings.Repeat("r", 400)}, records[1])
	require.Equal(t, []string{strconv.Itoa(qualityExportPageSize - 1), strings.Repeat("r", 400)}, records[qualityExportPageSize])
}

// A record longer than one slice is split across slices, but it is counted only
// once and only once its last byte was sent.
func TestQualityExportCSVWriterSplitsLongRecordsWithoutDoubleCounting(t *testing.T) {
	t.Parallel()

	sender := &recordingChunkSender{}
	writer := newQualityExportCSVWriter(sender)
	require.NoError(t, writer.writeHeader([]string{"id", "value"}))

	huge := strings.Repeat("x", qualityExportChunkBytes+1024)
	require.NoError(t, writer.writeRecord([]string{"1", huge}))
	require.NoError(t, writer.finish(false))

	require.Len(t, sender.chunks, 2, "the oversized record needs exactly two slices")
	require.Zero(t, sender.chunks[0].GetRowCount(), "the row is not complete yet at the first slice")
	require.Equal(t, int64(1), sender.chunks[1].GetRowCount())
	require.Equal(t, int64(1), writer.rows)

	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(sender.document(), []byte(qualityExportBOM))))
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, []string{"1", huge}, records[1])
}

// A page that returns fewer rows than it was asked for means the list is
// exhausted; the export stops instead of issuing another query.
func TestStreamQualityCSVStopsOnAShortPage(t *testing.T) {
	t.Parallel()

	sender := &recordingChunkSender{}
	calls := 0
	page := func(_ context.Context, limit, offset int) ([][]string, error) {
		calls++
		require.Equal(t, qualityExportPageSize, limit)
		require.Zero(t, offset)
		return [][]string{{"1"}, {"2"}}, nil
	}

	exported, err := streamQualityCSV(t.Context(), sender, []string{"id"}, qualityExportMaxRows, page)
	require.NoError(t, err)
	require.Equal(t, int64(2), exported)
	require.Equal(t, 1, calls)
}

// max_rows truncates the document at a row boundary: a page that carries more
// records than the ceiling allows still writes only whole records up to it.
func TestStreamQualityCSVTruncatesAtTheRowCeiling(t *testing.T) {
	t.Parallel()

	sender := &recordingChunkSender{}
	calls := 0
	page := func(_ context.Context, _ int, offset int) ([][]string, error) {
		calls++
		require.Zero(t, offset)
		// Deliberately more than the requested limit, to pin that the loop, not
		// the page, enforces the ceiling and never emits a partial record.
		return [][]string{{"1"}, {"2"}, {"3"}, {"4"}, {"5"}}, nil
	}

	exported, err := streamQualityCSV(t.Context(), sender, []string{"id"}, 2, page)
	require.NoError(t, err)
	require.Equal(t, int64(2), exported)
	require.Equal(t, 1, calls)

	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(sender.document(), []byte(qualityExportBOM))))
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Equal(t, [][]string{{"id"}, {"1"}, {"2"}}, records)
}

// A send failure stops the export and is returned to the handler so the audit row
// records the failure.
func TestStreamQualityCSVReturnsSendErrors(t *testing.T) {
	t.Parallel()

	sendErr := errors.New("client went away")
	sender := &recordingChunkSender{err: sendErr}
	_, err := streamQualityCSV(t.Context(), sender, []string{"id"}, 10, func(_ context.Context, _, _ int) ([][]string, error) {
		return [][]string{{"1"}}, nil
	})
	require.ErrorIs(t, err, sendErr)
}

func TestQualityExportScalarFormatting(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", qualityExportEnum(storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED.String(), "QUALITY_STATUS_"))
	require.Equal(t, "FAIL", qualityExportEnum(storepb.QualityStatus_QUALITY_STATUS_FAIL.String(), "QUALITY_STATUS_"))
	require.Equal(t, "MANUAL", qualityExportEnum(storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL.String(), "QUALITY_EXECUTION_TRIGGER_"))
	require.Equal(t, "TABLE", qualityExportEnum(storepb.MetaType_TABLE.String(), ""))
	require.Empty(t, qualityExportEnum(storepb.MetaType_UNSPECIFIED.String(), ""))

	require.Empty(t, qualityExportTime(time.Time{}))
	inTimezone := time.Date(2026, 10, 31, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	require.Equal(t, "2026-10-31T04:00:00Z", qualityExportTime(inTimezone))

	require.Empty(t, qualityExportTimePtr(nil))
	future := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	require.Equal(t, "2026-11-01T00:00:00Z", qualityExportTimePtr(&future))

	require.Empty(t, qualityExportFloat(nil))
	zero, ratio := 0.0, 1.5
	require.Equal(t, "0", qualityExportFloat(&zero))
	require.Equal(t, "1.5", qualityExportFloat(&ratio))
}

func TestQualityTargetPathString(t *testing.T) {
	t.Parallel()

	require.Empty(t, qualityTargetPathString(nil))
	require.Empty(t, qualityTargetPathString(&storepb.QualityTargetPath{}))
	require.Equal(t, "inst.db.public.orders.email", qualityTargetPathString(&storepb.QualityTargetPath{
		Instance: "inst", Database: "db", Schema: "public", Object: "orders", Column: "email",
	}))
	// Missing segments are skipped rather than leaving empty dots.
	require.Equal(t, "inst.orders", qualityTargetPathString(&storepb.QualityTargetPath{
		Instance: "inst", Object: "orders",
	}))
}

func TestQualityExecutionExportRecord(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)
	finished := started.Add(2 * time.Minute)
	observed := 3.5
	execution := &store.QualityExecutionMessage{
		ID:                 11,
		RunID:              7,
		RuleID:             5,
		RuleRevision:       2,
		TargetGUID:         "inst;db;public;orders",
		TargetPath:         &storepb.QualityTargetPath{Instance: "inst", Database: "db", Schema: "public", Object: "orders"},
		InstanceResourceID: "mysql-1",
		State:              storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED,
		Trigger:            storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
		Attempt:            3,
		Status:             storepb.QualityStatus_QUALITY_STATUS_FAIL,
		ObservedValue:      &observed,
		ObservedKind:       storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
		FailedRows:         2,
		TotalRows:          10,
		Sampled:            true,
		DurationMS:         1234,
		StartedAt:          started,
		FinishedAt:         finished,
	}

	record := qualityExecutionExportRecord("ws", execution, "workspaces/ws/qualityRules/abc")
	require.Len(t, record, len(qualityExecutionExportHeader()))
	require.Equal(t, []string{
		"11", "workspaces/ws/qualityRules/abc", "2", "inst;db;public;orders", "inst.db.public.orders", "instances/mysql-1",
		"MANUAL", "COMPLETED", "FAIL", "3.5", "COUNT", "2", "10", "true", "false", "", "",
		"3", "workspaces/ws/qualityRuns/7", "1234",
		"2026-10-31T10:00:00Z", "2026-10-31T10:02:00Z",
	}, record)
}

// An execution that has not finished yet renders an empty finish time rather
// than the epoch.
func TestQualityExecutionExportRecordLeavesUnsetTimesEmpty(t *testing.T) {
	t.Parallel()

	execution := &store.QualityExecutionMessage{
		ID:        12,
		RunID:     3,
		StartedAt: time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC),
	}
	record := qualityExecutionExportRecord("ws", execution, "")
	require.Equal(t, "", record[9], "an absent observed value must stay distinguishable from zero")
	require.Equal(t, "workspaces/ws/qualityRuns/3", record[18])
	require.Equal(t, "", record[21], "an unfinished execution has no finish time")
	require.Equal(t, "", record[8], "an unspecified outcome renders empty")
}

func TestQualityIssueExportRecord(t *testing.T) {
	t.Parallel()

	firstSeen := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	lastSeen := firstSeen.Add(time.Hour)
	resolvedAt := lastSeen.Add(time.Hour)
	acknowledgedAt := lastSeen.Add(30 * time.Minute)
	acknowledgedBy := int32(9)
	rule := &store.QualityRuleMessage{
		ID:         5,
		ResourceID: "abc",
		Title:      "external_ref must be unique",
		TargetPath: &storepb.QualityTargetPath{Instance: "inst", Database: "db", Schema: "public", Object: "orders"},
	}
	issue := &store.QualityIssueMessage{
		ResourceID:       "issue-1",
		RuleID:           5,
		TargetGUID:       "inst;db;public;orders",
		Status:           storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED,
		Severity:         storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		OccurrenceCount:  4,
		FirstSeenAt:      firstSeen,
		LastSeenAt:       lastSeen,
		ResolvedAt:       &resolvedAt,
		ResolutionReason: storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_FIXED,
		ResolutionNote:   "deduplicated the reference",
		AcknowledgedBy:   &acknowledgedBy,
		AcknowledgedAt:   &acknowledgedAt,
	}

	record := qualityIssueExportRecord("ws", issue, rule)
	require.Len(t, record, len(qualityIssueExportHeader()))
	require.Equal(t, []string{
		"issue-1", "workspaces/ws/qualityRules/abc", "external_ref must be unique", "inst;db;public;orders", "inst.db.public.orders",
		"ERROR", "RESOLVED", "4",
		"2026-10-30T09:00:00Z", "2026-10-30T10:00:00Z", "2026-10-30T11:00:00Z",
		"FIXED", "deduplicated the reference",
		"2026-10-30T10:30:00Z", "users/9", "",
	}, record)

	// The title and path belong to the rule; a deleted rule leaves them empty
	// instead of failing the export.
	withoutRule := qualityIssueExportRecord("ws", issue, nil)
	require.Equal(t, "", withoutRule[1])
	require.Equal(t, "", withoutRule[2])
	require.Equal(t, "", withoutRule[4])
}

func TestQualityTargetExportRecord(t *testing.T) {
	t.Parallel()

	lastRun := time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC)
	nextRun := lastRun.Add(time.Hour)
	target := &store.QualityTargetMessage{
		GUID:              "inst;db;public;orders",
		Type:              storepb.MetaType_TABLE,
		Path:              &storepb.QualityTargetPath{Instance: "inst", Database: "db", Schema: "public", Object: "orders"},
		RuleCount:         5,
		EnabledCount:      3,
		PausedCount:       2,
		ColumnCount:       1,
		OpenIssueCount:    4,
		PendingDraftCount: 2,
		WorstStatus:       storepb.QualityStatus_QUALITY_STATUS_FAIL,
		LastRunTime:       &lastRun,
		SchemaChanged:     true,
		Orphaned:          false,
		ScheduleInterval:  time.Hour,
		NextRunAt:         &nextRun,
	}

	record := qualityTargetExportRecord(target)
	require.Len(t, record, len(qualityTargetExportHeader()))
	require.Equal(t, []string{
		"inst;db;public;orders", "TABLE", "inst.db.public.orders",
		"5", "3", "2", "1", "4", "2", "FAIL",
		"2026-10-31T08:00:00Z", "true", "false", "3600", "2026-10-31T09:00:00Z",
	}, record)
}

// A target that never ran and has no schedule renders empty cells instead of
// zero values or the epoch.
func TestQualityTargetExportRecordLeavesUnsetValuesEmpty(t *testing.T) {
	t.Parallel()

	record := qualityTargetExportRecord(&store.QualityTargetMessage{GUID: "g", Type: storepb.MetaType_VIEW})
	require.Equal(t, "", record[9], "an unspecified worst status renders empty")
	require.Equal(t, "", record[10], "a target that never ran has no last run time")
	require.Equal(t, "", record[13], "an unscheduled target has no interval")
	require.Equal(t, "", record[14], "an unscheduled target has no next run")
}

func TestValidateQualityTargetExportFilters(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateQualityTargetExportFilters(&v1pb.ExportQualityTargetsRequest{}))
	require.NoError(t, validateQualityTargetExportFilters(&v1pb.ExportQualityTargetsRequest{FailingOnly: true}))
	require.NoError(t, validateQualityTargetExportFilters(&v1pb.ExportQualityTargetsRequest{WorstStatus: v1pb.QualityStatus_QUALITY_STATUS_FAIL}))

	err := validateQualityTargetExportFilters(&v1pb.ExportQualityTargetsRequest{
		FailingOnly: true,
		WorstStatus: v1pb.QualityStatus_QUALITY_STATUS_FAIL,
	})
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	require.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
}

// A ceiling that cuts rows flags the document's final slice, so a client can warn
// that the file is not the whole selection. A selection that matches the ceiling
// exactly is not flagged.
func TestStreamQualityCSVFlagsTruncationOnlyWhenRowsRemain(t *testing.T) {
	t.Parallel()

	all := [][]string{{"1"}, {"2"}, {"3"}}
	page := func(_ context.Context, limit, offset int) ([][]string, error) {
		if limit < 1 {
			return nil, errors.New("a page must request at least one row")
		}
		if offset >= len(all) {
			return nil, nil
		}
		return all[offset:min(offset+limit, len(all))], nil
	}

	t.Run("cap + 1 flags the file", func(t *testing.T) {
		t.Parallel()
		sender := &recordingChunkSender{}
		exported, err := streamQualityCSV(t.Context(), sender, []string{"id"}, 2, page)
		require.NoError(t, err)
		require.Equal(t, int64(2), exported)
		require.True(t, sender.lastChunk().GetTruncated(), "the final slice must say the file is short")
		for i, chunk := range sender.chunks[:len(sender.chunks)-1] {
			require.Falsef(t, chunk.GetTruncated(), "slice %d must not be flagged", i)
		}
	})

	t.Run("an exact cap does not flag the file", func(t *testing.T) {
		t.Parallel()
		sender := &recordingChunkSender{}
		exported, err := streamQualityCSV(t.Context(), sender, []string{"id"}, 3, page)
		require.NoError(t, err)
		require.Equal(t, int64(3), exported)
		require.False(t, sender.lastChunk().GetTruncated(), "nothing was left behind")
	})

	t.Run("the ceiling cuts a page short", func(t *testing.T) {
		t.Parallel()
		sender := &recordingChunkSender{}
		widePage := func(_ context.Context, _ int, _ int) ([][]string, error) {
			return [][]string{{"1"}, {"2"}, {"3"}, {"4"}}, nil
		}
		exported, err := streamQualityCSV(t.Context(), sender, []string{"id"}, 2, widePage)
		require.NoError(t, err)
		require.Equal(t, int64(2), exported)
		require.True(t, sender.lastChunk().GetTruncated())
	})
}

// Every filter a List RPC accepts must exist on the matching export, with the
// same wire type, so a future filter or a width change cannot silently drift
// between the page and the file download. An export may add only max_rows (its
// ceiling instead of paging) and, where the client does the filtering, a
// documented export-only switch.
func TestQualityExportFiltersMirrorTheListFilters(t *testing.T) {
	t.Parallel()

	paging := map[protoreflect.Name]bool{"page_size": true, "page_token": true}
	tests := []struct {
		name       string
		export     protoreflect.MessageDescriptor
		list       protoreflect.MessageDescriptor
		exportOnly map[protoreflect.Name]bool
	}{
		{
			name:       "executions",
			export:     (&v1pb.ExportQualityExecutionsRequest{}).ProtoReflect().Descriptor(),
			list:       (&v1pb.ListQualityExecutionsRequest{}).ProtoReflect().Descriptor(),
			exportOnly: map[protoreflect.Name]bool{"max_rows": true},
		},
		{
			name:       "issues",
			export:     (&v1pb.ExportQualityIssuesRequest{}).ProtoReflect().Descriptor(),
			list:       (&v1pb.ListQualityIssuesRequest{}).ProtoReflect().Descriptor(),
			exportOnly: map[protoreflect.Name]bool{"max_rows": true},
		},
		{
			name:   "targets",
			export: (&v1pb.ExportQualityTargetsRequest{}).ProtoReflect().Descriptor(),
			list:   (&v1pb.ListQualityTargetsRequest{}).ProtoReflect().Descriptor(),
			// failing_only is the directory's client-side "failing" switch; the list
			// cannot express it and filters client-side.
			exportOnly: map[protoreflect.Name]bool{"max_rows": true, "failing_only": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			listFields := make(map[protoreflect.Name]protoreflect.FieldDescriptor, tt.list.Fields().Len())
			for i := range tt.list.Fields().Len() {
				field := tt.list.Fields().Get(i)
				listFields[field.Name()] = field
			}
			for name, listField := range listFields {
				if paging[name] {
					continue
				}
				exportField := tt.export.Fields().ByName(name)
				require.NotNilf(t, exportField, "%s filter %q is missing from the export", tt.name, name)
				requireSameExportFieldShape(t, tt.name, listField, exportField)
			}
			for i := range tt.export.Fields().Len() {
				name := tt.export.Fields().Get(i).Name()
				require.Truef(t, listFields[name] != nil || tt.exportOnly[name],
					"export field %q has no %s-list counterpart; add it to the list or document it as export-only", name, tt.name)
			}
			require.NotNilf(t, tt.export.Fields().ByName("max_rows"), "%s export has no row ceiling", tt.name)
		})
	}
}

// requireSameExportFieldShape pins that an export filter and its list
// counterpart agree on the protobuf type, not merely the name: a width or enum
// change on one side would otherwise pass the parity guard.
func requireSameExportFieldShape(t *testing.T, side string, listField, exportField protoreflect.FieldDescriptor) {
	t.Helper()
	require.Equalf(t, listField.Kind(), exportField.Kind(),
		"%s filter %q is %s on the list but %s on the export", side, listField.Name(), listField.Kind(), exportField.Kind())
	if listField.Kind() == protoreflect.MessageKind || listField.Kind() == protoreflect.GroupKind {
		require.Equalf(t, listField.Message().FullName(), exportField.Message().FullName(),
			"%s filter %q names different message types on the list and the export", side, listField.Name())
	}
	if listField.Kind() == protoreflect.EnumKind {
		require.Equalf(t, listField.Enum().FullName(), exportField.Enum().FullName(),
			"%s filter %q names different enum types on the list and the export", side, listField.Name())
	}
}

// A stalled store read must not hold an export slot forever: the handler's
// lifetime bound maps the failure to DeadlineExceeded instead of reporting a
// generic internal error.
func TestQualityExportErrorMapsTheLifetimeBound(t *testing.T) {
	t.Parallel()

	expired, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
	defer cancel()
	<-expired.Done()

	procedure := v1connect.DataQualityServiceExportQualityExecutionsProcedure
	require.Equal(t, connect.CodeDeadlineExceeded,
		connect.CodeOf(qualityExportError(expired, procedure, errors.New("the store read stalled"))))
	require.NoError(t, qualityExportError(expired, procedure, nil),
		"a completed export is not rewritten just because the bound fired afterwards")

	boom := errors.New("the store failed")
	require.ErrorIs(t, qualityExportError(t.Context(), procedure, boom), boom,
		"an active context leaves a real failure untouched")
}

// A spreadsheet may execute a free-text cell that starts with a formula trigger.
// The export prefixes a single quote on text cells and only on text cells.
func TestQualityExportTextNeutralizesFormulas(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", qualityExportText(""))
	require.Equal(t, "alice", qualityExportText("alice"))
	require.Equal(t, "a=b", qualityExportText("a=b"), "a trigger that is not the first byte is not a formula")
	require.Equal(t, "数据", qualityExportText("数据"))
	for _, trigger := range []string{"=1+1", "+cmd", "-5", "@SUM(A1)", "\tleading tab", "\rleading carriage return"} {
		require.Equalf(t, "'"+trigger, qualityExportText(trigger), "trigger %q must be neutralized", trigger)
	}
}

// The issue title is external text: a title that looks like a formula must reach
// the CSV as literal text.
func TestQualityIssueExportRecordNeutralizesAFormulaTitle(t *testing.T) {
	t.Parallel()

	rule := &store.QualityRuleMessage{ID: 1, ResourceID: "rule-1", Title: "=cmd()"}
	issue := &store.QualityIssueMessage{
		ResourceID: "issue-1",
		RuleID:     1,
		Status:     storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN,
		Severity:   storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
	}
	record := qualityIssueExportRecord("ws", issue, rule)
	require.Equal(t, "'=cmd()", record[2])
}

// Numeric columns are never neutralized: a negative observation is a number, not
// a formula, while the error message next to it stays text.
func TestQualityExecutionExportRecordKeepsNegativeNumbers(t *testing.T) {
	t.Parallel()

	observed := -5.0
	execution := &store.QualityExecutionMessage{
		ID:            1,
		ObservedValue: &observed,
		Status:        storepb.QualityStatus_QUALITY_STATUS_FAIL,
		ErrorMessage:  "-5 must stay numeric",
	}
	record := qualityExecutionExportRecord("ws", execution, "")
	require.Equal(t, "-5", record[9], "a negative observation must keep its exact numeric value")
	require.Equal(t, "'-5 must stay numeric", record[16], "an error message is text and is neutralized")
}
