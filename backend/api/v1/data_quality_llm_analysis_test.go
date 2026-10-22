package v1

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The markdown panel renders the hypotheses, their evidence, the next step and the
// confidence.
func TestRenderExplanationMarkdown(t *testing.T) {
	t.Parallel()

	markdown := renderExplanationMarkdown(dataquality.ExplanationOutput{
		Summary:    "the freshest row is stale",
		Confidence: 0.7,
		Hypotheses: []dataquality.ExplanationHypothesis{
			{Cause: "replication lag", Evidence: []string{"last sync 40m ago", "no errors"}, NextStep: "check the replica lag"},
			{Cause: "the job stopped", Evidence: []string{"no rows since 09:00"}, NextStep: ""},
		},
	})
	require.Contains(t, markdown, "the freshest row is stale")
	require.Contains(t, markdown, "### 1. replication lag")
	require.Contains(t, markdown, "- last sync 40m ago")
	require.Contains(t, markdown, "**Next step:** check the replica lag")
	require.Contains(t, markdown, "### 2. the job stopped")
	require.NotContains(t, markdown, "**Next step:**\n\n\n")
	require.Contains(t, markdown, "_confidence 0.70_")
}

// The stored execution maps to the prompt's plain snapshot without losing the
// evidence a reader needs.
func TestQualityExecutionSnapshot(t *testing.T) {
	t.Parallel()

	observed := 3.0
	started := time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)
	snapshot := qualityExecutionSnapshot(&store.QualityExecutionMessage{
		Status:        storepb.QualityStatus_QUALITY_STATUS_FAIL,
		ObservedValue: &observed,
		ObservedKind:  storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
		FailedRows:    3,
		TotalRows:     1200,
		Sampled:       true,
		SampleRows:    1000,
		Truncated:     true,
		Thresholds:    &storepb.QualityThresholds{FailValue: proto.Float64(5)},
		ErrorCode:     dataquality.ErrCodeBadData,
		ErrorMessage:  "bad data",
		StartedAt:     started,
	})
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, snapshot.Status)
	require.NotNil(t, snapshot.ObservedValue)
	require.InDelta(t, 3, *snapshot.ObservedValue, 1e-9)
	require.Equal(t, int64(3), snapshot.FailedRows)
	require.Equal(t, int64(1200), snapshot.TotalRows)
	require.True(t, snapshot.Sampled)
	require.Equal(t, int64(1000), snapshot.SampleRows)
	require.True(t, snapshot.Truncated)
	require.NotNil(t, snapshot.Thresholds)
	require.Equal(t, dataquality.ErrCodeBadData, snapshot.ErrorCode)
	require.Equal(t, started, snapshot.StartedAt)
}

func TestStoredMetadataColumnCount(t *testing.T) {
	t.Parallel()

	require.Equal(t, 2, storedMetadataColumnCount(&storepb.StoredMetadata{Type: &storepb.StoredMetadata_TableMetadata{
		TableMetadata: &storepb.TableMetadata{Columns: []*storepb.ColumnMetadata{{Name: "a"}, {Name: "b"}}},
	}}))
	require.Equal(t, 1, storedMetadataColumnCount(&storepb.StoredMetadata{Type: &storepb.StoredMetadata_ViewMetadata{
		ViewMetadata: &storepb.ViewMetadata{Columns: []*storepb.ColumnMetadata{{Name: "a"}}},
	}}))
	require.Zero(t, storedMetadataColumnCount(&storepb.StoredMetadata{}))
}

func TestLineageEndpoint(t *testing.T) {
	t.Parallel()

	require.Equal(t, "orders", lineageEndpoint("instances/ins1;shop;public;orders", ""))
	require.Equal(t, "orders.email", lineageEndpoint("instances/ins1;shop;public;orders", "email"))
}

// A nil request cannot reach the store; the handlers report the historical
// unimplemented status instead of panicking.
func TestNilAnalysisRequestsAreRejected(t *testing.T) {
	t.Parallel()

	service := &DataQualityService{}
	_, err := service.SuggestQualityRule(t.Context(), nil)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	err = service.ExplainQualityExecution(t.Context(), nil, nil)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

// A malformed execution name and a non-numeric execution id are refused before
// any store read.
func TestParseQualityExecutionIDRejectsMalformedNames(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"qualityExecutions/abc",
		"workspaces/ws/qualityRuns/12",
		"workspaces/ws/qualityRuleResults/12",
		"workspaces/ws/qualityExecutions/not-a-number",
		"workspaces/ws/qualityExecutions/-3",
		"workspaces/ws/qualityExecutions/0",
		"workspaces/ws/qualityExecutions/",
	} {
		_, _, err := parseQualityExecutionID(name)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "name %q", name)
	}

	workspace, id, err := parseQualityExecutionID("workspaces/ws/qualityExecutions/42")
	require.NoError(t, err)
	require.Equal(t, "ws", workspace)
	require.Equal(t, int64(42), id)
}
