package v1

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
)

// connectDeadlineMessage is the fixed text a timed-out call returns. The error
// the handler produced names the driver, the model or the SQL that stalled, none
// of which the caller needs and all of which the caller should not learn.
const connectDeadlineMessage = "the request exceeded the server-side deadline"

// DeadlineInterceptor bounds how long one unary call may run.
//
// It cancels the handler's context at the deadline rather than wrapping the
// whole listener, because the streaming endpoints are exempt: a notification
// subscription and a streaming Explain call stay open for many minutes by
// contract. Every store query and every LLM HTTP exchange the handlers make
// honors its context, so a stalled dependency is interrupted and the goroutine
// and connection are released. A handler that ignores its context is not
// preempted; abandoning it in a watchdog goroutine would only move the leak.
type DeadlineInterceptor struct {
	defaultTimeout time.Duration
	overrides      map[string]time.Duration
}

// NewDeadlineInterceptor returns an interceptor that bounds each unary call at
// defaultTimeout, except the procedures named in overrides, which get their own
// bound. A non-positive timeout leaves a call unbounded.
func NewDeadlineInterceptor(defaultTimeout time.Duration, overrides map[string]time.Duration) *DeadlineInterceptor {
	return &DeadlineInterceptor{defaultTimeout: defaultTimeout, overrides: overrides}
}

func (in *DeadlineInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		timeout := in.timeoutFor(req.Spec().Procedure)
		if timeout <= 0 {
			return next(ctx, req)
		}
		deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		resp, err := next(deadlineCtx, req)
		// A handler interrupted by the deadline usually hands back the driver's or
		// the store's own error. The caller gets the timeout instead, so every
		// timed-out call looks the same and the underlying failure text stays out
		// of the response.
		if err != nil && deadlineCtx.Err() == context.DeadlineExceeded {
			return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New(connectDeadlineMessage))
		}
		return resp, err
	}
}

// timeoutFor returns the bound for one procedure.
func (in *DeadlineInterceptor) timeoutFor(procedure string) time.Duration {
	if override, ok := in.overrides[procedure]; ok {
		return override
	}
	return in.defaultTimeout
}

func (*DeadlineInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler passes streams through without a deadline: their life is
// the subscription's or the response stream's, not one request's.
func (*DeadlineInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}
