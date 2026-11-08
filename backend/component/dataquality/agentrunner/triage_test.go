package agentrunner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
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
	require.Empty(t, stores.investigations)
}

func TestModelToolDefsDependOnMode(t *testing.T) {
	t.Parallel()

	monitor := toolNames(modelToolDefs(storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR))
	require.NotContains(t, monitor, "get_table_context")
	require.NotContains(t, monitor, "draft_checks")

	triage := toolNames(modelToolDefs(storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE))
	require.Contains(t, triage, "get_table_context")
	require.Contains(t, triage, "draft_checks")
	require.Contains(t, triage, "record_investigation")
}

func TestGetTableContextToolReadsTheReader(t *testing.T) {
	t.Parallel()

	engine := NewEngine(newFakeStore())
	engine.tableContext = fakeTableContextReader{}
	result, err := engine.executeTool(context.Background(), testSession(), "get_table_context", "{}", nil, nil, nil, nil, nil)
	require.NoError(t, err)
	require.Contains(t, result, `"object":"orders"`)
	require.Contains(t, result, `"downstreamConsumers":3`)
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
	result, err := engine.executeTool(context.Background(), testSession(), "draft_checks", args, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, gotColumns, maxTriageDraftColumns)
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
		},
		Downstream: 3,
	}, nil
}

func toolNames(defs []llm.ToolDef) []string {
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Function.Name)
	}
	return names
}
