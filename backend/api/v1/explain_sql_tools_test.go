package v1

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// denyingToolChecker refuses every permission and records the question.
type denyingToolChecker struct {
	asked []permission.Permission
}

func (c *denyingToolChecker) CheckPermission(_ context.Context, perm permission.Permission, _ *store.UserMessage) (bool, error) {
	c.asked = append(c.asked, perm)
	return false, nil
}

// The ExplainSQL tool contract is part of the model-facing surface: two read-only
// tools with the registered schemas, declared under the database read permission.
func TestExplainSQLToolSetDeclaresTheTwoReadTools(t *testing.T) {
	t.Parallel()

	service := &ExplainSQLService{}
	set, err := service.explainSQLToolSet("", "")
	require.NoError(t, err)
	require.Equal(t, []string{"get_object_schema", "search_objects"}, set.Names())

	schemas := set.Schemas()
	require.Len(t, schemas, 2)
	for _, schema := range schemas {
		require.NotEmpty(t, schema.Description)
		parameters, ok := schema.Parameters.(map[string]any)
		require.True(t, ok)
		require.Equal(t, "object", parameters["type"])
	}
	registered, ok := set.Tool("get_object_schema")
	require.True(t, ok)
	require.True(t, registered.Meta.ReadOnly)
	require.Equal(t, tool.AuthzPermission, registered.Meta.Authz)
	require.Equal(t, string(permission.DatabasesRead), registered.Meta.Permission)
}

// A caller without the declared permission is refused before the tool body runs,
// so an ExplainSQL caller with explain-but-not-databases.read no longer reaches
// the registry lookup.
func TestExplainSQLToolCallsArePermissionGated(t *testing.T) {
	t.Parallel()

	service := &ExplainSQLService{}
	set, err := service.explainSQLToolSet("", "")
	require.NoError(t, err)

	checker := &denyingToolChecker{}
	result := set.Execute(context.Background(), tool.Call{
		Name:      "get_object_schema",
		Arguments: json.RawMessage(`{"name":"users"}`),
	}, &tool.Invocation{
		Principal: &store.UserMessage{ID: 7},
		Checker:   checker,
	})
	require.True(t, result.IsError)
	require.Equal(t, tool.CodePermissionDenied, result.Error.Code)
	require.Contains(t, result.Content, "does not hold "+string(permission.DatabasesRead))
	require.Equal(t, []permission.Permission{permission.DatabasesRead}, checker.asked)
}

// ExplainSQL tool calls are visible in the shared tool metrics under the
// explain_sql product.
func TestExplainSQLToolObserverRecordsTheProduct(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	m := metrics.New()
	require.NoError(t, m.Register(registry))
	explainToolObserver{metrics: m}.ObserveToolCall("get_object_schema", "success", 0, 10)

	families, err := registry.Gather()
	require.NoError(t, err)
	found := false
	for _, family := range families {
		if family.GetName() != "metaxisdata_agent_tool_calls_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["product"] != "explain_sql" || labels["tool"] != "get_object_schema" {
				continue
			}
			require.Equal(t, "success", labels["outcome"])
			require.Equal(t, float64(1), metric.GetCounter().GetValue())
			found = true
		}
	}
	require.True(t, found, "the ExplainSQL tool metric was not recorded")
}
