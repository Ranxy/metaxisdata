package agentrunner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/tools/quality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// A triage wake without a model is an analysis that cannot be faked: it parks for a
// person instead of running the deterministic survey.
func TestTriageWakeWithoutAModelParksTheSession(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	session := testSession()
	session.Mode = storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE
	engine := NewEngine(stores)
	engine.now = func() time.Time { return time.Unix(10_000, 0).UTC() }

	outcome, err := engine.Wake(context.Background(), session, Options{})
	require.NoError(t, err)
	require.False(t, outcome.TriageComplete)
	require.True(t, outcome.Blocked)
	require.Equal(t, "needs-human", outcome.BlockedCode)
	require.Contains(t, outcome.BlockedReason, "needs a model")
	require.Nil(t, outcome.NextWakeAt)
	require.Empty(t, stores.memoryInputs)
}

func TestGetTableContextToolReadsTheReader(t *testing.T) {
	t.Parallel()

	engine := NewEngine(newFakeStore())
	engine.tableContext = fakeTableContextReader{}
	result, err := engine.executeTool(context.Background(), testSession(), nil, "get_table_context", "{}", &wakeControl{})
	require.NoError(t, err)
	require.Contains(t, result, `"object":"orders"`)
	require.Contains(t, result, `"downstreamConsumers":3`)
	require.Contains(t, result, `"enumValues":["draft","published"]`)
	// The drafting guidance points at this field, so it must reach the model with
	// the spelling the tool description promises.
	require.Contains(t, result, `"allowedRuleTypes":["ROW_COUNT","ROW_COUNT_CHANGE"]`)
}

func TestDraftChecksToolBoundsColumnsAndReturnsTitles(t *testing.T) {
	t.Parallel()

	var gotColumns []string
	var gotIntent string
	engine := NewEngine(newFakeStore())
	engine.draftChecks = func(_ context.Context, _ string, _ storepb.MetaType, columns []string, intent string, _ int32) ([]string, []dataquality.DraftRejection, *storepb.QualitySuggestionQueueSummary, error) {
		gotColumns = columns
		gotIntent = intent
		return []string{"orders amount not null", "orders id unique"}, nil, nil, nil
	}
	args := `{"intent":"watch order integrity","columns":["a","b","c","d","e","f","g","h","i","j"]}`
	result, err := engine.executeTool(context.Background(), testSession(), nil, "draft_checks", args, &wakeControl{})
	require.NoError(t, err)
	require.Len(t, gotColumns, quality.MaxTriageDraftColumns)
	require.Equal(t, "watch order integrity", gotIntent)
	require.Contains(t, result, `"queued":2`)
	require.Contains(t, result, "orders id unique")
	require.NotContains(t, result, `"rejected"`)
}

// A draft that is not queued is explained to the model: a refused draft carries its
// reason so the intent can be corrected, and a valid draft skipped by the queue is
// counted so the model does not repeat it.
func TestDraftChecksToolExplainsUnqueuedDrafts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		draft DraftChecksFunc
		want  []string
	}{
		{
			name: "a refused draft carries its reason",
			draft: func(context.Context, string, storepb.MetaType, []string, string, int32) ([]string, []dataquality.DraftRejection, *storepb.QualitySuggestionQueueSummary, error) {
				return nil, []dataquality.DraftRejection{{
					Title:  "Status must be a declared enum label",
					Reason: `value "discontinued" is not a declared label of column "status"; valid labels: "active", "draft"`,
				}}, nil, nil
			},
			want: []string{`"queued":0`, `"rejected"`, "Status must be a declared enum label", "valid labels"},
		},
		{
			name: "a valid draft skipped by the queue is counted",
			draft: func(context.Context, string, storepb.MetaType, []string, string, int32) ([]string, []dataquality.DraftRejection, *storepb.QualitySuggestionQueueSummary, error) {
				return nil, nil, &storepb.QualitySuggestionQueueSummary{
					SkippedExistingFact: 2,
					SkippedDecided:      1,
				}, nil
			},
			want: []string{`"queued":0`, `"existingFact":2`, `"decided":1`},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			engine := NewEngine(newFakeStore())
			engine.draftChecks = test.draft
			result, err := engine.executeTool(context.Background(), testSession(), nil, "draft_checks", `{"intent":"check status"}`, &wakeControl{})
			require.NoError(t, err)
			for _, want := range test.want {
				require.Contains(t, result, want)
			}
		})
	}
}

type fakeTableContextReader struct{}

func (fakeTableContextReader) ReadTableContext(context.Context, string, storepb.MetaType) (*TableContext, error) {
	return &TableContext{
		TargetGUID: "target-1",
		TargetType: "TABLE",
		Engine:     "MYSQL",
		Object:     "orders",
		Columns: []TableContextColumn{
			{Name: "id", Type: "int", Key: true},
			{Name: "amount", Type: "decimal", Nullable: true},
			{Name: "status", Type: "enum('draft','published')", EnumValues: []string{"draft", "published"}},
		},
		Downstream:       3,
		AllowedRuleTypes: []string{"ROW_COUNT", "ROW_COUNT_CHANGE"},
	}, nil
}
