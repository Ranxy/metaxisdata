package tool

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// executeTool builds a one-tool set whose body is the given function.
func executeTool(t *testing.T, meta Meta, body func(context.Context, Call, *Invocation) (Result, error)) *Set {
	t.Helper()
	tool := validTool("one")
	tool.Meta = meta
	tool.Execute = body
	registry := NewRegistry()
	require.NoError(t, registry.Register(tool))
	set, err := registry.Resolve([]string{"one"})
	require.NoError(t, err)
	return set
}

func TestExecuteRendersTheStructuredValue(t *testing.T) {
	t.Parallel()

	set := executeTool(t, validTool("one").Meta, func(context.Context, Call, *Invocation) (Result, error) {
		return Result{Value: map[string]any{"answer": 42}}, nil
	})
	result := set.Execute(context.Background(), Call{Name: "one"}, nil)
	require.False(t, result.IsError)
	require.JSONEq(t, `{"answer":42}`, result.Content)
}

func TestExecuteKeepsAnExplicitContent(t *testing.T) {
	t.Parallel()

	set := executeTool(t, validTool("one").Meta, func(context.Context, Call, *Invocation) (Result, error) {
		return Result{Content: "already rendered"}, nil
	})
	result := set.Execute(context.Background(), Call{Name: "one"}, nil)
	require.Equal(t, "already rendered", result.Content)
}

func TestExecuteUnknownToolIsAnErrorResult(t *testing.T) {
	t.Parallel()

	set := executeTool(t, validTool("one").Meta, func(context.Context, Call, *Invocation) (Result, error) {
		return Result{}, nil
	})
	result := set.Execute(context.Background(), Call{Name: "missing"}, nil)
	require.True(t, result.IsError)
	require.Equal(t, CodeUnknownTool, result.Error.Code)
	require.Equal(t, `error: unknown tool "missing"`, result.Content)
}

func TestExecuteNormalizesAPlainError(t *testing.T) {
	t.Parallel()

	set := executeTool(t, validTool("one").Meta, func(context.Context, Call, *Invocation) (Result, error) {
		return Result{}, errors.New("boom")
	})
	result := set.Execute(context.Background(), Call{Name: "one"}, nil)
	require.True(t, result.IsError)
	require.Equal(t, CodeInternal, result.Error.Code)
	require.Equal(t, "error: boom", result.Content)
}

func TestExecuteKeepsAStructuredError(t *testing.T) {
	t.Parallel()

	set := executeTool(t, validTool("one").Meta, func(context.Context, Call, *Invocation) (Result, error) {
		return Result{}, NewError(CodeInvalidArgument, "the intent is required")
	})
	result := set.Execute(context.Background(), Call{Name: "one"}, nil)
	require.True(t, result.IsError)
	require.Equal(t, CodeInvalidArgument, result.Error.Code)
	require.Equal(t, "error: the intent is required", result.Content)
}

func TestExecuteTruncatesAtTheResultBudget(t *testing.T) {
	t.Parallel()

	meta := validTool("one").Meta
	meta.MaxResultBytes = 5
	set := executeTool(t, meta, func(context.Context, Call, *Invocation) (Result, error) {
		return Result{Content: "abcdefgh"}, nil
	})
	result := set.Execute(context.Background(), Call{Name: "one"}, nil)
	require.Equal(t, "abcde", result.Content)
	require.True(t, result.Truncated)
}

func TestExecuteDoesNotTruncateWithoutABudget(t *testing.T) {
	t.Parallel()

	set := executeTool(t, validTool("one").Meta, func(context.Context, Call, *Invocation) (Result, error) {
		return Result{Content: "abcdefgh"}, nil
	})
	result := set.Execute(context.Background(), Call{Name: "one"}, nil)
	require.Equal(t, "abcdefgh", result.Content)
	require.False(t, result.Truncated)
}

func TestExecuteTimesOut(t *testing.T) {
	t.Parallel()

	meta := validTool("one").Meta
	meta.Timeout = 10 * time.Millisecond
	set := executeTool(t, meta, func(ctx context.Context, _ Call, _ *Invocation) (Result, error) {
		<-ctx.Done()
		return Result{}, ctx.Err()
	})
	result := set.Execute(context.Background(), Call{Name: "one"}, nil)
	require.True(t, result.IsError)
	require.Equal(t, CodeTimeout, result.Error.Code)
}

func TestExecuteRecoversAPanic(t *testing.T) {
	t.Parallel()

	set := executeTool(t, validTool("one").Meta, func(context.Context, Call, *Invocation) (Result, error) {
		panic("kaboom")
	})
	result := set.Execute(context.Background(), Call{Name: "one"}, nil)
	require.True(t, result.IsError)
	require.Equal(t, CodeInternal, result.Error.Code)
	require.Equal(t, `error: tool "one" failed unexpectedly`, result.Content)
}

func TestExecutePassesTheInvocation(t *testing.T) {
	t.Parallel()

	var got *Invocation
	set := executeTool(t, validTool("one").Meta, func(_ context.Context, _ Call, inv *Invocation) (Result, error) {
		got = inv
		return Result{Content: "ok"}, nil
	})
	inv := &Invocation{Workspace: "workspaces/-"}
	set.Execute(context.Background(), Call{Name: "one"}, inv)
	require.Same(t, inv, got)
	// A nil invocation is replaced, never passed through.
	set.Execute(context.Background(), Call{Name: "one"}, nil)
	require.NotNil(t, got)
}

// fakePermissionChecker records what a tool asked for and answers with the
// configured verdict.
type fakePermissionChecker struct {
	allowed bool
	asked   permission.Permission
}

func (c *fakePermissionChecker) CheckPermission(_ context.Context, perm permission.Permission, _ *store.UserMessage) (bool, error) {
	c.asked = perm
	return c.allowed, nil
}

func TestExecuteEnforcesTheDeclaredPermission(t *testing.T) {
	t.Parallel()

	meta := validTool("one").Meta
	meta.Permission = "metaxisdata.test.read"
	bodyRan := false
	set := executeTool(t, meta, func(context.Context, Call, *Invocation) (Result, error) {
		bodyRan = true
		return Result{Content: "ok"}, nil
	})

	checker := &fakePermissionChecker{allowed: true}
	result := set.Execute(context.Background(), Call{Name: "one"}, &Invocation{
		Principal: &store.UserMessage{ID: 7},
		Checker:   checker,
	})
	require.False(t, result.IsError)
	require.True(t, bodyRan)
	require.Equal(t, permission.Permission("metaxisdata.test.read"), checker.asked)
}

func TestExecuteRefusesADeniedPermissionWithoutRunningTheBody(t *testing.T) {
	t.Parallel()

	meta := validTool("one").Meta
	meta.Permission = "metaxisdata.test.read"
	bodyRan := false
	set := executeTool(t, meta, func(context.Context, Call, *Invocation) (Result, error) {
		bodyRan = true
		return Result{Content: "ok"}, nil
	})

	result := set.Execute(context.Background(), Call{Name: "one"}, &Invocation{
		Principal: &store.UserMessage{ID: 7},
		Checker:   &fakePermissionChecker{allowed: false},
	})
	require.True(t, result.IsError)
	require.Equal(t, CodePermissionDenied, result.Error.Code)
	require.False(t, bodyRan, "a refused call must not run the tool body")
}

func TestExecuteRefusesACheckWithoutAPrincipal(t *testing.T) {
	t.Parallel()

	meta := validTool("one").Meta
	meta.Permission = "metaxisdata.test.read"
	set := executeTool(t, meta, func(context.Context, Call, *Invocation) (Result, error) {
		return Result{Content: "ok"}, nil
	})

	result := set.Execute(context.Background(), Call{Name: "one"}, &Invocation{
		Checker: &fakePermissionChecker{allowed: true},
	})
	require.True(t, result.IsError)
	require.Equal(t, CodeUnauthenticated, result.Error.Code)
}

func TestExecuteSkipsTheCheckWithoutAChecker(t *testing.T) {
	t.Parallel()

	// A transport that already ran the ACL interceptor omits the checker.
	set := executeTool(t, validTool("one").Meta, func(context.Context, Call, *Invocation) (Result, error) {
		return Result{Content: "ok"}, nil
	})
	result := set.Execute(context.Background(), Call{Name: "one"}, &Invocation{})
	require.False(t, result.IsError)
	require.Equal(t, "ok", result.Content)
}

// fakeObserver records what the pipeline reported.
type fakeObserver struct {
	calls []observedToolCall
}

type observedToolCall struct {
	tool     string
	outcome  string
	duration time.Duration
	bytes    int
}

func (o *fakeObserver) ObserveToolCall(tool, outcome string, duration time.Duration, resultBytes int) {
	o.calls = append(o.calls, observedToolCall{tool: tool, outcome: outcome, duration: duration, bytes: resultBytes})
}

func TestExecuteReportsEveryFinishedCall(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{}
	set := executeTool(t, validTool("one").Meta, func(context.Context, Call, *Invocation) (Result, error) {
		return Result{Content: "ok"}, nil
	})
	set.Execute(context.Background(), Call{Name: "one"}, &Invocation{Observer: observer})

	require.Len(t, observer.calls, 1)
	require.Equal(t, "one", observer.calls[0].tool)
	require.Equal(t, "success", observer.calls[0].outcome)
	require.Equal(t, 2, observer.calls[0].bytes)
	require.NotZero(t, observer.calls[0].duration)
}

func TestExecuteReportsADenialAsAnErrorOutcome(t *testing.T) {
	t.Parallel()

	meta := validTool("one").Meta
	meta.Permission = "metaxisdata.test.read"
	observer := &fakeObserver{}
	set := executeTool(t, meta, func(context.Context, Call, *Invocation) (Result, error) {
		return Result{Content: "ok"}, nil
	})
	set.Execute(context.Background(), Call{Name: "one"}, &Invocation{
		Principal: &store.UserMessage{ID: 7},
		Checker:   &fakePermissionChecker{allowed: false},
		Observer:  observer,
	})

	require.Len(t, observer.calls, 1)
	require.Equal(t, "error", observer.calls[0].outcome)
}
