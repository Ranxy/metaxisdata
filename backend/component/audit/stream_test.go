package audit

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// A streaming audit row must name the request that was sent. A server-streaming
// call receives exactly one, so the recorder keeps the first and ignores what
// follows.
func TestStreamRecorderKeepsTheFirstRequest(t *testing.T) {
	t.Parallel()

	recorder := NewStreamRecorder()
	recorder.RecordRequest(&v1pb.ExportQualityExecutionsRequest{Parent: "workspaces/-", MaxRows: 7})
	recorder.RecordRequest(&v1pb.ExportQualityExecutionsRequest{Parent: "workspaces/other"})

	request, ok := recorder.Request().(*v1pb.ExportQualityExecutionsRequest)
	require.True(t, ok)
	require.Equal(t, "workspaces/-", request.GetParent())
	require.Equal(t, int32(7), request.GetMaxRows())
}

// Values that are not proto messages, and messages received before any exist,
// must not break the recorder: a non-audited stream or a hand-built connection
// never provides one.
func TestStreamRecorderIgnoresNonProtoMessages(t *testing.T) {
	t.Parallel()

	recorder := NewStreamRecorder()
	recorder.RecordRequest(nil)
	recorder.RecordRequest("not a proto message")
	recorder.RecordRequest(map[string]any{"parent": "workspaces/-"})

	require.Nil(t, recorder.Request())
}

// The recorder clones the request, so a later mutation by the handler or by the
// connection cannot rewrite what the audit row says was asked for.
func TestStreamRecorderClonesTheRequest(t *testing.T) {
	t.Parallel()

	recorder := NewStreamRecorder()
	request := &v1pb.ExportQualityExecutionsRequest{Parent: "workspaces/-"}
	recorder.RecordRequest(request)
	request.Parent = "workspaces/mutated"

	recorded, ok := recorder.Request().(*v1pb.ExportQualityExecutionsRequest)
	require.True(t, ok)
	require.Equal(t, "workspaces/-", recorded.GetParent())
}

func TestRecordStreamResultMergesSummaries(t *testing.T) {
	t.Parallel()

	recorder := NewStreamRecorder()
	ctx := WithStreamRecorder(context.Background(), recorder)
	RecordStreamResult(ctx, map[string]any{"exportedRows": int64(3)})
	RecordStreamResult(ctx, map[string]any{"exportedRows": int64(4), "maxRows": int64(100000)})

	result := recorder.Result()
	require.Equal(t, int64(4), result["exportedRows"])
	require.Equal(t, int64(100000), result["maxRows"])
}

// A handler called outside an audited stream — a unit test, a direct call — has
// no recorder on its context; reporting a result must be a no-op, not a panic.
func TestRecordStreamResultWithoutRecorderIsSafe(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		RecordStreamResult(context.Background(), map[string]any{"exportedRows": 1})
		RecordStreamResult(context.Background(), nil)
	})
}

// Result returns a copy: a caller that mutates it must not change what the
// interceptor is about to persist.
func TestStreamRecorderResultIsACopy(t *testing.T) {
	t.Parallel()

	recorder := NewStreamRecorder()
	recorder.recordResult(map[string]any{"exportedRows": int64(2)})

	result := recorder.Result()
	result["exportedRows"] = int64(99)

	require.Equal(t, int64(2), recorder.Result()["exportedRows"])
}

// The result summary goes through the same redaction and bounds as a proto
// response, so a handler cannot put an unbounded or credential-named value into
// the permanent ledger.
func TestMarshalAuditResultSanitizesAndBounds(t *testing.T) {
	t.Parallel()

	oversized := strings.Repeat("x", MaxAuditFieldBytes+100)
	structured, raw := MarshalAuditResult(map[string]any{
		"exportedRows": int64(3),
		"token":        "secret",
		"errorMessage": oversized,
	})

	require.NotNil(t, structured)
	require.Equal(t, RedactedValue, raw["token"])
	require.LessOrEqual(t, len(strings.TrimSuffix(raw["errorMessage"].(string), auditTruncationSuffix)), MaxAuditFieldBytes)
	require.Equal(t, float64(3), structured.GetFields()["exportedRows"].GetNumberValue())

	empty, emptyRaw := MarshalAuditResult(nil)
	require.Nil(t, empty)
	require.Nil(t, emptyRaw)
}

// The request payload and the result payload must round-trip into one audit row
// shape: request as the proto JSON, response as the handler's summary.
func TestStreamAuditPayloadsCarryRequestAndResult(t *testing.T) {
	t.Parallel()

	recorder := NewStreamRecorder()
	ctx := WithStreamRecorder(context.Background(), recorder)
	recorder.RecordRequest(&v1pb.ExportQualityExecutionsRequest{Parent: "workspaces/-"})
	RecordStreamResult(ctx, map[string]any{"exportedRows": int64(2), "maxRows": int64(100000)})

	requestStruct, _ := MarshalAuditMessage(recorder.Request())
	responseStruct, _ := MarshalAuditResult(recorder.Result())
	require.Equal(t, "workspaces/-", requestStruct.GetFields()["parent"].GetStringValue())
	require.Equal(t, float64(2), responseStruct.GetFields()["exportedRows"].GetNumberValue())
	require.Equal(t, float64(100000), responseStruct.GetFields()["maxRows"].GetNumberValue())
}
