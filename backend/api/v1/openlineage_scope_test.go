package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/openlineage"
)

// A scoped ingestion key may only write to its namespace, and every namespace
// in the event -- job plus each input and output dataset -- has to match, so a
// key for one namespace cannot inject datasets into another.
func TestEventWithinScope(t *testing.T) {
	t.Parallel()

	matching := &openlineage.RunEvent{
		Job:     openlineage.Job{Namespace: "ns-a"},
		Inputs:  []openlineage.Dataset{{Namespace: "ns-a"}},
		Outputs: []openlineage.Dataset{{Namespace: "ns-a"}},
	}
	require.True(t, eventWithinScope(matching, ""), "an unscoped key accepts anything")
	require.True(t, eventWithinScope(matching, "ns-a"))
	require.False(t, eventWithinScope(matching, "ns-b"))

	crossNamespaceDataset := &openlineage.RunEvent{
		Job:     openlineage.Job{Namespace: "ns-a"},
		Outputs: []openlineage.Dataset{{Namespace: "ns-b"}},
	}
	require.False(t, eventWithinScope(crossNamespaceDataset, "ns-a"))

	datasetWithoutNamespace := &openlineage.RunEvent{
		Job:    openlineage.Job{Namespace: "ns-a"},
		Inputs: []openlineage.Dataset{{}},
	}
	require.False(t, eventWithinScope(datasetWithoutNamespace, "ns-a"))
}
