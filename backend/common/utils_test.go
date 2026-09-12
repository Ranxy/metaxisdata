//nolint:revive
package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenLineageResourceNames(t *testing.T) {
	t.Parallel()

	t.Run("namespace mapping round trip", func(t *testing.T) {
		t.Parallel()

		name := FormatNamespaceMapping(42)
		require.Equal(t, "openlineage/namespaceMappings/42", name)
		id, err := GetNamespaceMappingID(name)
		require.NoError(t, err)
		require.Equal(t, int64(42), id)
	})

	t.Run("api key round trip", func(t *testing.T) {
		t.Parallel()

		name := FormatAPIKey(7)
		require.Equal(t, "openlineage/apiKeys/7", name)
		id, err := GetAPIKeyID(name)
		require.NoError(t, err)
		require.Equal(t, int64(7), id)
	})

	t.Run("run and task keep their guid", func(t *testing.T) {
		t.Parallel()

		guid := "openlineage:run:default:etl_dag.transform_orders:a1b2c3d4"
		name := FormatOpenLineageRun(guid)
		require.Equal(t, "openlineage/runs/"+guid, name)
		parsed, err := GetOpenLineageRunGUID(name)
		require.NoError(t, err)
		require.Equal(t, guid, parsed)

		taskGUID := "openlineage:task:default:etl_dag.transform_orders"
		parsed, err = GetOpenLineageTaskGUID(FormatOpenLineageTask(taskGUID))
		require.NoError(t, err)
		require.Equal(t, taskGUID, parsed)
	})

	t.Run("rejects malformed names", func(t *testing.T) {
		t.Parallel()

		for _, name := range []string{
			"",
			"openlineage",
			"openlineage/namespaceMappings",
			"openlineage/namespaceMappings/",
			"openlineage/namespaceMappings/abc",
			"openlineage/namespaceMappings/1/extra",
			"instances/i1",
		} {
			_, err := GetNamespaceMappingID(name)
			require.Errorf(t, err, "name %q", name)
		}

		_, err := GetOpenLineageRunGUID("openlineage/tasks/openlineage:task:x")
		require.Error(t, err, "a task name must not parse as a run name")
		_, err = GetOpenLineageTaskGUID("")
		require.Error(t, err)
		_, err = GetAPIKeyID("openlineage/apiKeys/1/2")
		require.Error(t, err)
	})
}
