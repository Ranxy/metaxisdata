// Package output renders command results. stdout carries exactly one JSON
// document, because that is what an agent parses; everything a person needs to
// read goes to stderr.
package output

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Format is the rendering the caller asked for.
type Format string

const (
	// FormatJSON is the default: one JSON document on stdout.
	FormatJSON Format = "json"
	// FormatTable is for people.
	FormatTable Format = "table"
)

// ParseFormat validates the --format value.
func ParseFormat(value string) (Format, error) {
	switch Format(value) {
	case FormatJSON, FormatTable:
		return Format(value), nil
	default:
		return "", fmt.Errorf("unknown format %q, expected %s or %s", value, FormatJSON, FormatTable)
	}
}

// Row is one table row.
type Row []string

// Renderer writes results to stdout and progress to stderr.
type Renderer struct {
	format Format
	stdout io.Writer
	stderr io.Writer
}

// New returns a renderer writing to the process streams.
func New(format Format) *Renderer {
	return NewWith(format, os.Stdout, os.Stderr)
}

// NewWith returns a renderer writing to the given streams, for tests.
func NewWith(format Format, stdout, stderr io.Writer) *Renderer {
	return &Renderer{format: format, stdout: stdout, stderr: stderr}
}

// Format reports the requested rendering.
func (r *Renderer) Format() Format {
	return r.format
}

// Progress writes a human-readable line to stderr. It is never part of the
// machine-readable contract.
func (r *Renderer) Progress(format string, args ...any) {
	// Writing progress is best effort: a closed stderr must not turn a
	// successful command into a failure.
	_, _ = fmt.Fprintf(r.stderr, format+"\n", args...)
}

// Message renders a protobuf message. Table mode uses the supplied rows, and
// falls back to JSON when the caller had none to give.
func (r *Renderer) Message(message proto.Message, rows []Row) error {
	if r.format == FormatTable && rows != nil {
		return r.Table(rows)
	}
	return r.ProtoJSON(message)
}

// Envelope renders a command-specific result shape in JSON mode and the
// supplied rows in table mode.
func (r *Renderer) Envelope(value any, rows []Row) error {
	if r.format == FormatTable && rows != nil {
		return r.Table(rows)
	}
	return r.JSON(value)
}

// JSON writes any value as one indented JSON document.
func (r *Renderer) JSON(value any) error {
	encoder := json.NewEncoder(r.stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// protoJSONOptions is the single rendering of a protobuf message, used both for
// a top-level result and for a message embedded in an envelope.
//
// protojson is what the audit log, the JSONB columns and the REST gateway use,
// so the field names match everywhere. EmitUnpopulated is on because the output
// is a contract an agent parses: a stable shape (an empty list is [], an unset
// string is "", a false flag is present) is worth more than the few bytes that
// omitting them would save.
var protoJSONOptions = protojson.MarshalOptions{Indent: "  ", EmitUnpopulated: true}

// ProtoJSON writes a protobuf message using protojson.
func (r *Renderer) ProtoJSON(message proto.Message) error {
	// protojson.Marshal is allowed by the lint configuration; the unmarshal
	// wrapper is the one that is not.
	payload, err := protoJSONOptions.Marshal(message)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(r.stdout, string(payload))
	return err
}

// ProtoValue renders a protobuf message for embedding in an envelope.
//
// A message put into an envelope as-is would be encoded by encoding/json, which
// reads the struct tags protoc-gen-go emits: snake_case. The same relation would
// then look different depending on whether it was the whole result or one entry
// of a list.
func ProtoValue(message proto.Message) (json.RawMessage, error) {
	if message == nil {
		return json.RawMessage("null"), nil
	}
	payload, err := protoJSONOptions.Marshal(message)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(payload), nil
}

// ProtoValues renders a list of protobuf messages for embedding in an envelope.
// An empty list encodes as [], never as null.
func ProtoValues[T proto.Message](messages []T) (json.RawMessage, error) {
	values := make([]json.RawMessage, 0, len(messages))
	for _, message := range messages {
		value, err := ProtoValue(message)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(payload), nil
}

// EnsureSlice returns a non-nil slice so an empty list encodes as [] rather
// than null: a caller iterating the result should not have to special-case it.
func EnsureSlice[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

// Table renders rows as an aligned table on stdout.
func (r *Renderer) Table(rows []Row) error {
	writer := tabwriter.NewWriter(r.stdout, 0, 4, 2, ' ', 0)
	for _, row := range rows {
		if _, err := fmt.Fprintln(writer, strings.Join(row, "\t")); err != nil {
			return err
		}
	}
	return writer.Flush()
}

// Error is the machine-readable error envelope. Hint carries an actionable next
// step, which is what lets an agent recover without guessing.
type Error struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Hint    string   `json:"hint,omitempty"`
	Details []string `json:"details,omitempty"`
}

type errorEnvelope struct {
	Error Error `json:"error"`
}

// WriteError writes the error envelope to stderr.
func (r *Renderer) WriteError(err Error) {
	payload, marshalErr := json.MarshalIndent(errorEnvelope{Error: err}, "", "  ")
	if marshalErr != nil {
		_, _ = fmt.Fprintf(r.stderr, "{\"error\":{\"code\":%q,\"message\":%q}}\n", err.Code, err.Message)
		return
	}
	_, _ = fmt.Fprintln(r.stderr, string(payload))
}

// CodeOf maps an error onto the CLI's stable error code, which is what an agent
// branches on. The matching exit codes live in the client package.
func CodeOf(err error) string {
	if err == nil {
		return ""
	}
	// An error from the command layer may name its own code, which is how a
	// situation the Connect codes do not describe (a device login that has to be
	// restarted) gets a code of its own.
	var coded interface{ Code() string }
	if errors.As(err, &coded) {
		return coded.Code()
	}
	// A local deadline or cancellation is not a Connect error, and reporting it
	// as an internal failure would hide the one thing worth knowing.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout"
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		return "internal"
	}
	switch connectErr.Code() {
	case connect.CodeUnauthenticated:
		return "unauthenticated"
	case connect.CodePermissionDenied:
		return "permission_denied"
	case connect.CodeNotFound:
		return "not_found"
	case connect.CodeInvalidArgument, connect.CodeFailedPrecondition:
		return "invalid_argument"
	case connect.CodeDeadlineExceeded, connect.CodeCanceled:
		return "timeout"
	case connect.CodeResourceExhausted:
		return "resource_exhausted"
	case connect.CodeAlreadyExists:
		return "already_exists"
	case connect.CodeUnavailable:
		// The server could not be reached, which is worth telling apart from a
		// server that answered with a fault of its own.
		return "unavailable"
	case connect.CodeUnimplemented:
		return "unimplemented"
	default:
		return "internal"
	}
}
