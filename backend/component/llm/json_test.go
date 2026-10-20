package llm

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// jsonTestRule is the decoded shape the tests validate into. Its fields are
// tagged the way the data-quality prompts document them.
type jsonTestRule struct {
	Type       string   `json:"type"`
	Confidence float64  `json:"confidence"`
	Columns    []string `json:"columns"`
}

func decodeJSONTestRule(t *testing.T) func(jsonTestRule) error {
	t.Helper()
	return func(rule jsonTestRule) error {
		if rule.Type == "" {
			return errors.New("type is required")
		}
		if rule.Confidence < 0 || rule.Confidence > 1 {
			return errors.New("confidence must be between 0 and 1")
		}
		return nil
	}
}

func TestParseJSONDecodesOneDocument(t *testing.T) {
	t.Parallel()

	rule, err := ParseJSON[jsonTestRule](`{"type":"NULL_PERCENT","confidence":0.8,"columns":["email"]}`, decodeJSONTestRule(t))
	require.NoError(t, err)
	require.Equal(t, "NULL_PERCENT", rule.Type)
	require.InDelta(t, 0.8, rule.Confidence, 1e-9)
	require.Equal(t, []string{"email"}, rule.Columns)

	// Whitespace and surrounding newlines are framing, not content.
	rule, err = ParseJSON[jsonTestRule]("  \n\t{\"type\":\"UNIQUE\"}\n\n", nil)
	require.NoError(t, err)
	require.Equal(t, "UNIQUE", rule.Type)

	// A non-object document is decodable too; the caller's T decides the shape.
	values, err := ParseJSON[[]int]("[1, 2, 3]", nil)
	require.NoError(t, err)
	require.Equal(t, []int{1, 2, 3}, values)
}

func TestParseJSONStripsCodeFences(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		text string
	}{
		{
			name: "fenced with a language tag",
			text: "```json\n{\"type\":\"UNIQUE\"}\n```",
		},
		{
			name: "fenced without a language tag",
			text: "```\n{\"type\":\"UNIQUE\"}\n```",
		},
		{
			name: "fence after an introductory sentence",
			text: "Here is the rule you asked for:\n```json\n{\"type\":\"UNIQUE\"}\n```\nLet me know if you need more.",
		},
		{
			name: "single line fence",
			text: "```json {\"type\":\"UNIQUE\"}```",
		},
		{
			name: "byte order mark",
			text: "\ufeff{\"type\":\"UNIQUE\"}",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rule, err := ParseJSON[jsonTestRule](test.text, nil)
			require.NoError(t, err)
			require.Equal(t, "UNIQUE", rule.Type)
		})
	}
}

// The decoder is strict about the document's shape: an undocumented field is a
// wrong answer, not a field to ignore.
func TestParseJSONRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	_, err := ParseJSON[jsonTestRule](`{"type":"UNIQUE","invented":"x"}`, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidJSON)
	require.Contains(t, err.Error(), "unknown field")
	require.NotErrorIs(t, err, ErrJSONValidation)
}

// Validation runs after decoding and its failure is distinguishable from a decode
// failure, so a caller can map "the model answered nonsense" separately from "the
// model answered but the answer is unacceptable".
func TestParseJSONRunsValidation(t *testing.T) {
	t.Parallel()

	_, err := ParseJSON[jsonTestRule](`{"type":"","confidence":0.5}`, decodeJSONTestRule(t))
	require.Error(t, err)
	require.ErrorIs(t, err, ErrJSONValidation)
	require.NotErrorIs(t, err, ErrInvalidJSON)
	require.Contains(t, err.Error(), "type is required")

	_, err = ParseJSON[jsonTestRule](`{"type":"UNIQUE","confidence":1.5}`, decodeJSONTestRule(t))
	require.Error(t, err)
	require.ErrorIs(t, err, ErrJSONValidation)
	require.Contains(t, err.Error(), "confidence")

	// A nil validator means the caller only wants the decode.
	rule, err := ParseJSON[jsonTestRule](`{"type":"UNIQUE"}`, nil)
	require.NoError(t, err)
	require.Equal(t, "UNIQUE", rule.Type)
}

// A response that is not exactly one JSON document is an error: empty, prose,
// "null", a type mismatch, or two documents in a row.
func TestParseJSONRejectsMalformedResponses(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		text string
		want string
	}{
		{name: "empty", text: "", want: "empty"},
		{name: "whitespace only", text: "   \n\t", want: "empty"},
		{name: "empty fence", text: "```json\n```", want: "empty"},
		{name: "prose", text: "Sure! I would suggest a uniqueness rule.", want: "invalid character"},
		{name: "null", text: "null", want: "null"},
		{name: "truncated", text: `{"type":"UNIQUE"`, want: "unexpected EOF"},
		{name: "two documents", text: `{"type":"UNIQUE"}{"type":"RANGE"}`, want: "more than one JSON document"},
		{
			name: "type mismatch",
			text: `{"type":42}`,
			want: "cannot unmarshal",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseJSON[jsonTestRule](test.text, nil)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrInvalidJSON)
			require.Contains(t, err.Error(), test.want)
			require.NotErrorIs(t, err, ErrJSONValidation)
		})
	}
}

// A user-facing error must not echo the model's answer: the answer can contain
// prompt text, sample values or credentials the server read.
func TestParseJSONErrorsDoNotEchoTheResponse(t *testing.T) {
	t.Parallel()

	const secret = "s3cr3t-value-that-must-not-leak"
	// Invalid for a missing closing brace, with the secret inside the document.
	_, err := ParseJSON[jsonTestRule](`{"type":"`+secret, nil)
	require.Error(t, err)
	require.NotContains(t, err.Error(), secret)

	// Validation errors are the validator's own message, not the document.
	_, err = ParseJSON[jsonTestRule](`{"type":"UNIQUE","confidence":0.1}`, func(jsonTestRule) error {
		return errors.New("the answer is not acceptable")
	})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "UNIQUE")
	require.Contains(t, err.Error(), "not acceptable")
}

// A fence whose body has trailing prose inside it still fails: the fence is
// framing, not a license to append text to the document.
func TestParseJSONRejectsTrailingContentInsideAFence(t *testing.T) {
	t.Parallel()

	_, err := ParseJSON[jsonTestRule](`{"type":"UNIQUE"} and that is my answer`, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidJSON)
}

func TestStripCodeFence(t *testing.T) {
	t.Parallel()

	require.Equal(t, `{"a":1}`, stripCodeFence("```json\n{\"a\":1}\n```"))
	require.Equal(t, `{"a":1}`, stripCodeFence("Here you go:\n```\n{\"a\":1}\n```\nDone."))
	require.Equal(t, `{"a":1}`, stripCodeFence(`{"a":1}`))
	require.Equal(t, `{"a":1}`, stripCodeFence("  ```  \n{\"a\":1}\n```  "))
	// An unterminated fence keeps its body: the decoder reports the real problem
	// rather than the stripper swallowing the answer.
	require.Equal(t, `{"a":1}`, stripCodeFence("```json\n{\"a\":1}"))
	require.True(t, strings.Contains(stripCodeFence("no fence here"), "no fence"))
}
