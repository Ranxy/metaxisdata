package log

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	pkgerrors "github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

//go:noinline
func failureOrigin() error { return pkgerrors.Errorf("unexpected status %d from models endpoint", 404) }

//go:noinline
func failureWrap(err error) error { return pkgerrors.Wrap(err, "failed to fetch models") }

//go:noinline
func joinedFailure() error { return pkgerrors.New("origin in a joined branch") }

// The API boundary logs a mapped Connect error, whose Error() flattens the
// chain. The log line still has to name where the failure originated.
func TestWithErrorRendersCauseStackThroughConnectError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	logger.Error("server error", WithError(connect.NewError(connect.CodeInternal, failureWrap(failureOrigin()))))

	out := buf.String()
	require.Contains(t, out, "failed to fetch models: unexpected status 404 from models endpoint")
	require.Contains(t, out, "failureOrigin")
	// The wrapper's stack points at the annotation site; rendering it would
	// trade the origin for a dead end.
	require.NotContains(t, out, "failureWrap")
}

// A multi-error node only exposes Unwrap() []error; the origin behind it must
// not be mistaken for the end of the chain.
func TestWithErrorFindsOriginBehindMultiError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	logger.Error("parse failed", WithError(pkgerrors.Wrap(fmt.Errorf("%w: %w", errors.New("left"), joinedFailure()), "context")))

	require.Contains(t, buf.String(), "joinedFailure")
}

func TestWithErrorLeavesStacklessErrorsAlone(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	logger.Error("boom", WithError(errors.New("plain failure")))

	require.Contains(t, buf.String(), `error="plain failure"`)
}

func TestWithErrorStaysSingleLineJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	logger.Error("server error", WithError(failureWrap(failureOrigin())))

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	errorText, ok := entry["error"].(string)
	require.True(t, ok)
	require.Contains(t, errorText, "failureOrigin")
}
