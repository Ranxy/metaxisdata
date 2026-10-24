package dataquality

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// waiters reports how many callers joined the in-flight call for key. It lets a
// test wait for a follower to actually enter the call instead of sleeping.
func (g *analysisGroup) waiters(key string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if call, ok := g.calls[key]; ok {
		return call.waiters
	}
	return 0
}

func analysisResult(text string) (string, *llm.Usage, *storepb.QualityLLMDiagnostic, error) {
	return text, &llm.Usage{TotalTokens: 3}, &storepb.QualityLLMDiagnostic{CorrelationId: "c1"}, nil
}

// Identical in-flight analyses share one model call: a double click pays once.
func TestAnalysisGroupCoalescesIdenticalCalls(t *testing.T) {
	t.Parallel()

	group := newAnalysisGroup()
	var calls atomic.Int32
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	run := func(context.Context) (string, *llm.Usage, *storepb.QualityLLMDiagnostic, error) {
		if calls.Add(1) == 1 {
			started <- struct{}{}
		}
		<-release
		return analysisResult("shared")
	}

	// The leader registers the in-flight call and parks inside it.
	leaderResult := make(chan string, 1)
	go func() {
		text, _, _, err := group.Do(context.Background(), "same-key", run)
		if err != nil {
			leaderResult <- ""
			return
		}
		leaderResult <- text
	}()
	<-started

	// A second caller joins the same in-flight call instead of starting its own.
	followerEntered := make(chan struct{})
	followerResult := make(chan string, 1)
	go func() {
		close(followerEntered)
		text, _, _, err := group.Do(context.Background(), "same-key", run)
		if err != nil {
			followerResult <- ""
			return
		}
		followerResult <- text
	}()
	<-followerEntered
	require.Eventually(t, func() bool { return group.waiters("same-key") == 1 }, time.Second, time.Millisecond)
	close(release)

	require.Equal(t, "shared", <-leaderResult)
	require.Equal(t, "shared", <-followerResult)
	require.Equal(t, int32(1), calls.Load(), "identical prompts must be answered by one model call")
}

// Different keys are different questions and stay separate.
func TestAnalysisGroupSeparatesDistinctCalls(t *testing.T) {
	t.Parallel()

	group := newAnalysisGroup()
	var calls atomic.Int32
	run := func(context.Context) (string, *llm.Usage, *storepb.QualityLLMDiagnostic, error) {
		calls.Add(1)
		return analysisResult("answer")
	}
	for _, key := range []string{"a", "b", "c"} {
		_, _, _, err := group.Do(context.Background(), key, run)
		require.NoError(t, err)
	}
	require.Equal(t, int32(3), calls.Load())
}

// A caller whose context gives up stops waiting; the call itself keeps going for
// the callers that are still alive.
func TestAnalysisGroupFollowerCanStopWaiting(t *testing.T) {
	t.Parallel()

	group := newAnalysisGroup()
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	run := func(context.Context) (string, *llm.Usage, *storepb.QualityLLMDiagnostic, error) {
		started <- struct{}{}
		<-release
		return analysisResult("done")
	}
	go func() { _, _, _, _ = group.Do(context.Background(), "key", run) }()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, err := group.Do(ctx, "key", run)
	require.ErrorIs(t, err, context.Canceled)
	close(release)
}

// When the leader's request is canceled, a follower that is still alive retries
// instead of inheriting the leader's cancellation.
func TestAnalysisGroupRetriesAfterLeaderCancellation(t *testing.T) {
	t.Parallel()

	group := newAnalysisGroup()
	leaderDone := make(chan struct{})
	releaseLeader := make(chan struct{})
	var calls atomic.Int32
	run := func(context.Context) (string, *llm.Usage, *storepb.QualityLLMDiagnostic, error) {
		calls.Add(1)
		return analysisResult("retried")
	}

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderResult := make(chan error, 1)
	go func() {
		_, _, _, err := group.Do(leaderCtx, "key", func(ctx context.Context) (string, *llm.Usage, *storepb.QualityLLMDiagnostic, error) {
			close(leaderDone)
			<-releaseLeader
			cancelLeader()
			return "", nil, nil, ctx.Err()
		})
		leaderResult <- err
	}()
	<-leaderDone

	// The follower is alive while the leader's call is still in flight.
	followerEntered := make(chan struct{})
	followerResult := make(chan string, 1)
	var followerErr error
	go func() {
		close(followerEntered)
		text, _, _, err := group.Do(context.Background(), "key", run)
		followerErr = err
		followerResult <- text
	}()
	<-followerEntered
	require.Eventually(t, func() bool { return group.waiters("key") == 1 }, time.Second, time.Millisecond)
	close(releaseLeader)

	require.Error(t, <-leaderResult)
	require.Equal(t, "retried", <-followerResult)
	require.NoError(t, followerErr)
	require.Equal(t, int32(1), calls.Load(), "the retry is the first successful model call")
}
