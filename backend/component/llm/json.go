package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrInvalidJSON reports that the model's response was not a single JSON document
// this build could decode. The error wraps one of these two sentinels so a caller
// can tell a malformed answer from a semantically rejected one without matching
// message text.
var ErrInvalidJSON = errors.New("the model's response is not valid JSON")

// ErrJSONValidation reports that the model's response decoded but the caller's
// validate function rejected it.
var ErrJSONValidation = errors.New("the model's JSON failed validation")

// ParseJSON decodes one JSON document from a model's answer into T and validates
// it. It is the strict boundary for structured output: the provider was asked for
// JSON, and an answer that is not exactly one documented-shaped document is an
// error, never a partially filled value.
//
// The response may be wrapped in a Markdown code fence, which the decoder strips
// first, because a model that is told "output only JSON" still fences it
// sometimes. Everything after that is strict: unknown fields are rejected,
// trailing content after the document is rejected, and "null" is treated as no
// document at all rather than as a zero value. The returned error carries a
// readable reason (an offset, a field name, the validator's own message) but
// never echoes the whole input, so a prompt or an answer cannot leak into a user
// facing message.
func ParseJSON[T any](text string, validate func(T) error) (T, error) {
	var zero T
	document, err := jsonDocument(text)
	if err != nil {
		return zero, err
	}

	decoder := json.NewDecoder(strings.NewReader(document))
	decoder.DisallowUnknownFields()
	var value T
	if err := decoder.Decode(&value); err != nil {
		return zero, fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}
	// One document only: the decoder consumes trailing whitespace, so a second
	// value here means the model appended prose or a second object.
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return zero, fmt.Errorf("%w: the response contains more than one JSON document", ErrInvalidJSON)
		}
		return zero, fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}
	if validate != nil {
		if err := validate(value); err != nil {
			return zero, fmt.Errorf("%w: %w", ErrJSONValidation, err)
		}
	}
	return value, nil
}

// jsonDocument extracts the JSON document from a model's answer: it trims the
// framing, strips one Markdown code fence, and rejects the degenerate forms that
// would otherwise decode to a zero value.
func jsonDocument(text string) (string, error) {
	trimmed := strings.TrimPrefix(text, "\ufeff")
	trimmed = stripCodeFence(trimmed)
	if trimmed == "" {
		return "", fmt.Errorf("%w: the response is empty", ErrInvalidJSON)
	}
	// "null" is valid JSON but not a document for a caller that expects fields;
	// decoding it would silently produce the zero value and pass a nil validator.
	if trimmed == "null" {
		return "", fmt.Errorf("%w: the response is null", ErrInvalidJSON)
	}
	return trimmed, nil
}

// stripCodeFence extracts the body of a Markdown code fence, when the answer has
// one. A model that was told "output only JSON" still fences it — and often
// precedes the fence with a sentence — so the fence is located anywhere in the
// text and everything outside it is dropped. Text without a fence is returned
// trimmed, and the strict decoder judges it.
func stripCodeFence(text string) string {
	trimmed := strings.TrimSpace(text)
	open := strings.Index(trimmed, "```")
	if open == -1 {
		return trimmed
	}
	body := trimmed[open+len("```"):]
	// The language tag, when present, ends the fence's first line.
	if index := strings.IndexByte(body, '\n'); index >= 0 {
		body = body[index+1:]
	} else {
		body = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(body), "json"))
	}
	// The closing fence is the last one: a JSON string inside cannot contain a
	// fence in practice, and dropping everything after the last fence tolerates
	// a closing sentence the model added after it.
	if closeIndex := strings.LastIndex(body, "```"); closeIndex >= 0 {
		body = body[:closeIndex]
	}
	return strings.TrimSpace(body)
}
