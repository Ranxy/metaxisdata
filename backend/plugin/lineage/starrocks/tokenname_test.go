package starrocks

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTheTokenNameTableIsBuiltAtInit is a tripwire, not a behaviour test: omni builds its
// token-name table on first use without a lock, so tokenname.go builds it at package
// initialization (see .agents/docs/omni-upstream-defects.md §13). Deleting that file leaves
// the table to whichever analysis calls first, and no test observes that reliably — a
// whole-package run builds it while parsing, long before the parallel tests start — so this
// flag is what keeps the workaround from being removed in silence.
func TestTheTokenNameTableIsBuiltAtInit(t *testing.T) {
	t.Parallel()

	require.True(t, tokenNameTableIsBuilt,
		"tokenname.go's init is what closed the race .agents/docs/omni-upstream-defects.md §13 records")
}
