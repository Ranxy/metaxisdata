package tool

import (
	"context"
	"encoding/json"
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

func TestExecuteRendersTheResultContent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body func(context.Context, Call, *Invocation) (Result, error)
		want string
	}{
		{
			name: "a structured value becomes compact JSON",
			body: func(context.Context, Call, *Invocation) (Result, error) {
				return Result{Value: map[string]any{"answer": 42}}, nil
			},
			want: `{"answer":42}`,
		},
		{
			name: "an explicit content is kept verbatim",
			body: func(context.Context, Call, *Invocation) (Result, error) {
				return Result{Content: "already rendered"}, nil
			},
			want: "already rendered",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			set := executeTool(t, validTool("one").Meta, tc.body)
			result := set.Execute(context.Background(), Call{Name: "one"}, nil)
			require.False(t, result.IsError)
			require.Equal(t, tc.want, result.Content)
		})
	}
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

func TestExecuteNormalizesExecutionFailures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		err         error
		wantCode    ErrorCode
		wantContent string
	}{
		{name: "a plain error is an internal failure", err: errors.New("boom"), wantCode: CodeInternal, wantContent: "error: boom"},
		{name: "a structured error keeps its code", err: NewError(CodeInvalidArgument, "the intent is required"), wantCode: CodeInvalidArgument, wantContent: "error: the intent is required"},
		// A cancelled call keeps its own meaning, so a caller can tell a cancelled
		// tool from a broken one.
		{name: "a cancelled call is unavailable", err: context.Canceled, wantCode: CodeUnavailable, wantContent: "error: the tool was cancelled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			set := executeTool(t, validTool("one").Meta, func(context.Context, Call, *Invocation) (Result, error) {
				return Result{}, tc.err
			})
			result := set.Execute(context.Background(), Call{Name: "one"}, nil)
			require.True(t, result.IsError)
			require.Equal(t, tc.wantCode, result.Error.Code)
			require.Equal(t, tc.wantContent, result.Content)
		})
	}
}

func TestExecuteAppliesTheResultBudget(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		maxResultBytes int
		wantContent    string
		wantTruncated  bool
	}{
		{name: "content longer than the budget is cut", maxResultBytes: 5, wantContent: "abcde", wantTruncated: true},
		{name: "a zero budget means no cap", maxResultBytes: 0, wantContent: "abcdefgh", wantTruncated: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			meta := validTool("one").Meta
			meta.MaxResultBytes = tc.maxResultBytes
			set := executeTool(t, meta, func(context.Context, Call, *Invocation) (Result, error) {
				return Result{Content: "abcdefgh"}, nil
			})
			result := set.Execute(context.Background(), Call{Name: "one"}, nil)
			require.Equal(t, tc.wantContent, result.Content)
			require.Equal(t, tc.wantTruncated, result.Truncated)
		})
	}
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
	set.Execute(context.Background(), Call{Name: "one"}, &Invocation{Workspace: "workspaces/-"})
	require.NotNil(t, got)
	require.Equal(t, "workspaces/-", got.Workspace)

	// A product with no checker, metrics or audit sink passes a nil invocation; the
	// body must still get one it can read.
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
	require.Equal(t, "error: the caller does not hold metaxisdata.test.read", result.Content)
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

func TestExecuteReportsEachCallOutcome(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		allowed     bool
		wantOutcome string
	}{
		{name: "a completed call is a success", allowed: true, wantOutcome: "success"},
		{name: "a denied call is an error outcome", allowed: false, wantOutcome: "error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			observer := &fakeObserver{}
			set := executeTool(t, validTool("one").Meta, func(context.Context, Call, *Invocation) (Result, error) {
				return Result{Content: "ok"}, nil
			})
			result := set.Execute(context.Background(), Call{Name: "one"}, &Invocation{
				Principal: &store.UserMessage{ID: 7},
				Checker:   &fakePermissionChecker{allowed: tc.allowed},
				Observer:  observer,
			})

			require.Len(t, observer.calls, 1)
			require.Equal(t, "one", observer.calls[0].tool)
			require.Equal(t, tc.wantOutcome, observer.calls[0].outcome)
			require.Equal(t, len(result.Content), observer.calls[0].bytes)
			require.NotZero(t, observer.calls[0].duration)
		})
	}
}

// fakeAuditSink records the calls the pipeline audited.
type fakeAuditSink struct {
	entries []AuditEntry
}

func (s *fakeAuditSink) RecordToolCall(_ context.Context, entry AuditEntry) {
	s.entries = append(s.entries, entry)
}

func TestExecuteAuditsSideEffectingCallsOnly(t *testing.T) {
	t.Parallel()

	propose := validTool("propose")
	propose.Meta.SideEffect = SideEffectPropose
	read := validTool("read")
	read.Meta.ReadOnly = true

	registry := NewRegistry()
	require.NoError(t, registry.Register(propose))
	require.NoError(t, registry.Register(read))
	set, err := registry.Resolve([]string{"propose", "read"})
	require.NoError(t, err)

	sink := &fakeAuditSink{}
	invocation := &Invocation{
		Principal: &store.UserMessage{ID: 7},
		Workspace: "ws",
		Audit:     sink,
	}
	set.Execute(context.Background(), Call{Name: "propose", Arguments: json.RawMessage(`{"x":1}`)}, invocation)
	set.Execute(context.Background(), Call{Name: "read"}, invocation)

	require.Len(t, sink.entries, 1, "a read must not be audited")
	entry := sink.entries[0]
	require.Equal(t, "propose", entry.Tool)
	require.Equal(t, "ws", entry.Workspace)
	require.Equal(t, 7, entry.Principal.ID)
	require.JSONEq(t, `{"x":1}`, string(entry.Arguments))
	require.Nil(t, entry.Failure)
}

func TestExecuteAuditsARefusedSideEffect(t *testing.T) {
	t.Parallel()

	propose := validTool("propose")
	propose.Meta.SideEffect = SideEffectPropose
	propose.Meta.Permission = "metaxisdata.test.write"
	registry := NewRegistry()
	require.NoError(t, registry.Register(propose))
	set, err := registry.Resolve([]string{"propose"})
	require.NoError(t, err)

	sink := &fakeAuditSink{}
	set.Execute(context.Background(), Call{Name: "propose"}, &Invocation{
		Principal: &store.UserMessage{ID: 7},
		Audit:     sink,
		Checker:   &fakePermissionChecker{allowed: false},
	})
	require.Len(t, sink.entries, 1)
	require.NotNil(t, sink.entries[0].Failure)
	require.Equal(t, CodePermissionDenied, sink.entries[0].Failure.Code)
}

func TestErrorModelContentCarriesTheActionableParts(t *testing.T) {
	t.Parallel()

	plain := NewError(CodeInvalidArgument, "object.name is required")
	require.Equal(t, "object.name is required", plain.ModelContent())

	full := &Error{
		Code:       CodeNotFound,
		Message:    `no table matches "orders"`,
		Hint:       "search first, then address the result",
		Candidates: []string{"orders_a", "orders_b"},
		Retryable:  true,
	}
	content := full.ModelContent()
	require.Contains(t, content, `no table matches "orders"`)
	require.Contains(t, content, "hint: search first, then address the result")
	require.Contains(t, content, "candidates: orders_a, orders_b")
	require.Contains(t, content, "retryable: yes")
}
