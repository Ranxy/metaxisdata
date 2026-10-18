package runner

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type testRunner struct {
	ran bool
}

func (*testRunner) Name() string { return "test" }

func (r *testRunner) Run(context.Context) { r.ran = true }

type panickingRunner struct{}

func (*panickingRunner) Name() string { return "panicking" }

func (*panickingRunner) Run(context.Context) { panic("boom") }

// Go is the server's single entry point for a background loop: it must call Run
// and release the wait group count.
func TestGoRunsTheRunnerAndReleasesTheWaitGroup(t *testing.T) {
	t.Parallel()

	var wg sync.WaitGroup
	r := &testRunner{}
	Go(context.Background(), &wg, r)
	wg.Wait()
	require.True(t, r.ran)
}

// A bug in one background loop must be logged, not propagated into the process:
// the other runners and the web server keep running.
func TestGoRecoversAPanickingRunner(t *testing.T) {
	t.Parallel()

	var wg sync.WaitGroup
	require.NotPanics(t, func() {
		Go(context.Background(), &wg, &panickingRunner{})
		wg.Wait()
	})
}
