package agentrunner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/tools/quality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestTriageWakeWithoutAModelRetries(t *testing.T) {
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

func TestModelToolDefsDependOnModeAndCapabilities(t *testing.T) {
	t.Parallel()

	agent := capabilityTestAgent("suggest", "adopt", "patrol")
	monitor := toolSpecNames(toolsFor(monitorSpec(), agent))
	require.NotContains(t, monitor, "get_table_context")
	require.NotContains(t, monitor, "draft_checks")
	require.Contains(t, monitor, "record_asset_fact")
	require.Contains(t, monitor, "adopt_pending_drafts")
	require.NotContains(t, monitor, "record_investigation")
	require.NotContains(t, monitor, "record_decision")

	triage := toolSpecNames(toolsFor(triageSpec(), agent))
	require.Contains(t, triage, "get_table_context")
	require.Contains(t, triage, "draft_checks")
	require.Contains(t, triage, "record_asset_fact")
	require.Contains(t, triage, "adopt_pending_drafts")
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
}

func TestDraftChecksToolBoundsColumnsAndReturnsTitles(t *testing.T) {
	t.Parallel()

	var gotColumns []string
	var gotIntent string
	engine := NewEngine(newFakeStore())
	engine.draftChecks = func(_ context.Context, _ string, _ storepb.MetaType, columns []string, intent string, _ int32) ([]string, error) {
		gotColumns = columns
		gotIntent = intent
		return []string{"orders amount not null", "orders id unique"}, nil
	}
	args := `{"intent":"watch order integrity","columns":["a","b","c","d","e","f","g","h","i","j"]}`
	result, err := engine.executeTool(context.Background(), testSession(), nil, "draft_checks", args, &wakeControl{})
	require.NoError(t, err)
	require.Len(t, gotColumns, quality.MaxTriageDraftColumns)
	require.Equal(t, "watch order integrity", gotIntent)
	require.Contains(t, result, `"queued":2`)
	require.Contains(t, result, "orders id unique")
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
		Downstream: 3,
	}, nil
}
