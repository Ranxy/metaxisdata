package tool

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
