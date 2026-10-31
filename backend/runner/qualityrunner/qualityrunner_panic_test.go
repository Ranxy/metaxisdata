package qualityrunner

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGuardScheduledGroupRecovers keeps the scheduler's panic boundary honest.
// The scheduled workers are started with a bare goroutine, so they are outside
// the boundary runner.Go installs for the runner's own goroutine: a panic that
// escapes one batch would otherwise terminate the process and with it the API
// and every other background runner.
func TestGuardScheduledGroupRecovers(t *testing.T) {
	t.Parallel()

	r := &Runner{}
	require.NotPanics(t, func() {
		r.guardScheduledGroup("instance-panics", func() {
			panic("batch blew up")
		})
	})
}
