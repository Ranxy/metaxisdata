//nolint:revive
package log

import (
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"

	pkgerrors "github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common/stacktrace"
)

// maxErrorStackFrames bounds the frames rendered for one error log line.
const maxErrorStackFrames = 20

// LogLevel is the default log severity level.
var LogLevel = new(slog.LevelVar)

// https://sourcegraph.com/github.com/uber-go/zap/-/blob/zapcore/entry.go?L117
// Replace is the default replace attribute.
var Replace = func(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.SourceKey {
		if source, ok := a.Value.Any().(*slog.Source); ok {
			idx := strings.LastIndexByte(source.File, '/')
			if idx == -1 {
				return a
			}
			// Find the penultimate separator.
			idx = strings.LastIndexByte(source.File[:idx], '/')
			if idx == -1 {
				return a
			}
			source.File = source.File[idx+1:]
		}
	}
	return a
}

// Initializes the slog configuration.
func init() {
	LogLevel.Set(slog.LevelInfo)
}

// WithError renders err together with the stack captured closest to its cause.
// The chain is walked explicitly because %+v only prints a pkg/errors stack
// when the outermost error implements fmt.Formatter; connect.Error does not,
// so an error logged at the API boundary would otherwise flatten to a message
// with no origin.
func WithError(err error) slog.Attr {
	return slog.Any("error", errorText{err: err})
}

// stackTracer is the accessor pkg/errors' stack-carrying errors expose.
type stackTracer interface {
	StackTrace() pkgerrors.StackTrace
}

// errorText defers rendering until the handler resolves the record, so a level
// that filters the log line does not pay for walking the chain and formatting
// its stack.
type errorText struct {
	err error
}

func (e errorText) LogValue() slog.Value {
	return slog.StringValue(formatError(e.err))
}

// maxErrorChainDepth bounds the walk so a self-referential Unwrap cannot hang
// the logger.
const maxErrorChainDepth = 100

func formatError(err error) string {
	if err == nil {
		return "<nil>"
	}
	if stack := deepestStack(err); len(stack) > 0 {
		if rendered := formatStack(stack); rendered != "" {
			return err.Error() + "\n" + rendered
		}
	}
	return err.Error()
}

// deepestStack returns the stack captured furthest from err, i.e. the one
// recorded where the failure originated rather than where it was annotated.
// Multi-error nodes (errors.Join, fmt.Errorf with several %w) only expose
// Unwrap() []error, so the walk branches instead of trusting errors.Unwrap.
func deepestStack(err error) pkgerrors.StackTrace {
	var (
		best      pkgerrors.StackTrace
		bestDepth = -1
		visits    int
		visit     func(error, int)
	)
	visit = func(current error, depth int) {
		if current == nil || visits >= maxErrorChainDepth {
			return
		}
		visits++
		if tracer, ok := current.(stackTracer); ok && depth > bestDepth {
			best = tracer.StackTrace()
			bestDepth = depth
		}
		if multi, ok := current.(interface{ Unwrap() []error }); ok {
			for _, child := range multi.Unwrap() {
				visit(child, depth+1)
			}
			return
		}
		visit(errors.Unwrap(current), depth+1)
	}
	visit(err, 0)
	return best
}

// formatStack renders the frames of a pkg/errors stack as "function\n\tfile:line".
func formatStack(st pkgerrors.StackTrace) string {
	pcs := make([]uintptr, len(st))
	for i, frame := range st {
		pcs[i] = uintptr(frame)
	}
	if len(pcs) == 0 {
		return ""
	}

	iter := runtime.CallersFrames(pcs)
	frames := make([]runtime.Frame, 0, len(pcs))
	for {
		frame, more := iter.Next()
		frames = append(frames, frame)
		if !more {
			break
		}
	}
	// Drop the runtime tail (runtime.main, runtime.goexit): it says nothing
	// about the caller's code.
	for len(frames) > 0 && strings.HasPrefix(frames[len(frames)-1].Function, "runtime.") {
		frames = frames[:len(frames)-1]
	}
	if len(frames) > maxErrorStackFrames {
		frames = frames[:maxErrorStackFrames]
	}

	var b strings.Builder
	for _, frame := range frames {
		_, _ = fmt.Fprintf(&b, "%s\n\t%s:%d\n", frame.Function, frame.File, frame.Line)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func Stack(key string) slog.Attr {
	stack := stacktrace.TakeStacktrace(20, 3)
	return slog.Any(key, stack)
}
