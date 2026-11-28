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

func TestDraftChecksToolBoundsColumnsAndNamesItsDrafts(t *testing.T) {
	t.Parallel()

	var gotColumns []string
	var gotIntent string
	engine := NewEngine(newFakeStore())
	engine.draftChecks = func(_ context.Context, _ string, _ storepb.MetaType, columns []string, intent string, _ int32) ([]DraftedCheck, []dataquality.DraftRejection, *storepb.QualitySuggestionQueueSummary, error) {
		gotColumns = columns
		gotIntent = intent
		return []DraftedCheck{
			{ID: "qualitySuggestions/draft-1", Title: "orders amount not null", RuleType: "NULL_RATE", Confidence: 0.8},
			{ID: "qualitySuggestions/draft-2", Title: "orders id unique", RuleType: "UNIQUE"},
		}, nil, nil, nil
	}
	args := `{"intent":"watch order integrity","columns":["a","b","c","d","e","f","g","h","i","j"]}`
	result, err := engine.executeTool(context.Background(), testSession(), nil, "draft_checks", args, &wakeControl{})
	require.NoError(t, err)
	require.Len(t, gotColumns, quality.MaxTriageDraftColumns)
	require.Equal(t, "watch order integrity", gotIntent)
	require.Contains(t, result, `"queued":2`)
	require.Contains(t, result, `"id":"qualitySuggestions/draft-1"`, "the model must be able to withdraw what it just queued")
	require.Contains(t, result, `"ruleType":"NULL_RATE"`)
	require.Contains(t, result, `"confidence":0.8`)
	require.Contains(t, result, "orders id unique")
	require.Contains(t, result, "withdraw_draft")
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
			draft: func(context.Context, string, storepb.MetaType, []string, string, int32) ([]DraftedCheck, []dataquality.DraftRejection, *storepb.QualitySuggestionQueueSummary, error) {
				return nil, []dataquality.DraftRejection{{
					Title:  "Status must be a declared enum label",
					Reason: `value "discontinued" is not a declared label of column "status"; valid labels: "active", "draft"`,
				}}, nil, nil
			},
			want: []string{`"queued":0`, `"rejected"`, "Status must be a declared enum label", "valid labels"},
		},
		{
			name: "a valid draft skipped by the queue is counted",
			draft: func(context.Context, string, storepb.MetaType, []string, string, int32) ([]DraftedCheck, []dataquality.DraftRejection, *storepb.QualitySuggestionQueueSummary, error) {
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

// withdraw_draft is the author's exit from the review queue. The wake must pass the
// agent principal and the draft the model named to the application layer; the tool
// body and the spec already keep the arguments, and this proves the dispatch branch.
func TestWithdrawDraftToolReachesTheApplicationLayer(t *testing.T) {
	t.Parallel()

	var gotPrincipal int32
	var gotDraft, gotReason string
	engine := NewEngine(newFakeStore())
	engine.withdraw = func(_ context.Context, principalID int32, draftResourceID, reason string) (quality.WithdrawOutcome, error) {
		gotPrincipal, gotDraft, gotReason = principalID, draftResourceID, reason
		return quality.WithdrawOutcome{Draft: "qualitySuggestions/s1", Reason: reason, State: "WITHDRAWN"}, nil
	}
	result, err := engine.executeTool(context.Background(), testSession(), nil, "withdraw_draft",
		`{"draft":"qualitySuggestions/s1","reason":"the target is emptied nightly"}`, &wakeControl{})
	require.NoError(t, err)
	require.Equal(t, int32(7), gotPrincipal)
	require.Equal(t, "qualitySuggestions/s1", gotDraft)
	require.Equal(t, "the target is emptied nightly", gotReason)
	require.Contains(t, result, `"state":"WITHDRAWN"`)
}
