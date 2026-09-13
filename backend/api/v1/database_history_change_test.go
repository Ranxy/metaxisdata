package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPluralize(t *testing.T) {
	t.Parallel()

	require.Equal(t, "column", pluralize("column", 1))
	require.Equal(t, "columns", pluralize("column", 2))
	require.Equal(t, "indexes", pluralize("index", 2))
	require.Equal(t, "properties", pluralize("property", 3))
	require.Equal(t, "foreign keys", pluralize("foreign key", 2))
	require.Equal(t, "check constraints", pluralize("check constraint", 0))
	require.Equal(t, "changes", pluralize("change", 2))
}
