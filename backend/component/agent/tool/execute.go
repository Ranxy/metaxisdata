package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// Execute runs one call through the shared pipeline: resolve, bound, run,
// normalize, render and budget. It never panics and never leaves a failure
// unclassified, so an ordinary tool failure is a result the model can read
// instead of an event that ends the wake.
func (s *Set) Execute(ctx context.Context, call Call, inv *Invocation) Result {
	if s == nil {
		return errorResult(NewError(CodeInternal, "the tool set is not configured"))
	}
	t, ok := s.byName[call.Name]
	if !ok {
		return errorResult(NewError(CodeUnknownTool, fmt.Sprintf("unknown tool %q", call.Name)))
	}
	if inv == nil {
		inv = &Invocation{}
	}

	execCtx := ctx
	if t.Meta.Timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, t.Meta.Timeout)
		defer cancel()
	}

	result, err := run(execCtx, t, call, inv)
	if err != nil {
		result = errorResult(normalizeError(err))
	}
	if result.Content == "" {
		content, renderErr := renderValue(result.Value)
		if renderErr != nil {
			return errorResult(NewError(CodeInternal, renderErr.Error()))
		}
		result.Content = content
	}
	if t.Meta.MaxResultBytes > 0 && len(result.Content) > t.Meta.MaxResultBytes {
		result.Content = common.TruncateUTF8Bytes(result.Content, t.Meta.MaxResultBytes)
		result.Truncated = true
	}
	return result
}

// run invokes one tool body with panic recovery. The recovered value is logged,
// never put into the model-visible message.
func run(ctx context.Context, t Tool, call Call, inv *Invocation) (result Result, err error) {
	logger := inv.Logger
	if logger == nil {
		logger = slog.Default()
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Error("tool panicked",
				slog.String("tool", t.Name),
				slog.Any("panic", recovered),
				slog.String("stack", string(debug.Stack())))
			result = Result{}
			err = NewError(CodeInternal, fmt.Sprintf("tool %q failed unexpectedly", t.Name))
		}
	}()
	return t.Execute(ctx, call, inv)
}

// normalizeError maps execution failures onto the tool error vocabulary. A
// cancelled or expired context keeps its own meaning so a caller can tell a slow
// tool from a broken one.
func normalizeError(err error) *Error {
	var toolErr *Error
	if errors.As(err, &toolErr) {
		return toolErr
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return &Error{Code: CodeTimeout, Message: "the tool timed out", Retryable: true}
	case errors.Is(err, context.Canceled):
		return &Error{Code: CodeUnavailable, Message: "the tool was cancelled", Retryable: true}
	default:
		return &Error{Code: CodeInternal, Message: err.Error()}
	}
}

// errorResult builds the result a failed call hands back to the model.
func errorResult(err *Error) Result {
	message := err.Message
	if message == "" {
		message = "the tool failed"
	}
	return Result{IsError: true, Error: err, Content: "error: " + message}
}

// renderValue renders a structured value as compact JSON, the same shape both
// transports send.
func renderValue(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("failed to render the tool result: %w", err)
	}
	return string(payload), nil
}
