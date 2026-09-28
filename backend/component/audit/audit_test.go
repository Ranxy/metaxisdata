package audit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The OAuth2 authorization-code flow carries its credentials under both
// camelCase protojson names and snake_case form names. Either spelling must be
// redacted before an audit row is persisted.
func TestSensitiveAuditFieldsAreRedacted(t *testing.T) {
	t.Parallel()

	for _, field := range []string{
		"codeVerifier", "code_verifier",
		"clientSecret", "client_secret",
		"authorizationCode", "authorizationcode", "authorization_code",
		"deviceCode", "password",
	} {
		raw := map[string]any{
			"name":   "instances/i1",
			field:    "credential-value",
			"nested": map[string]any{field: "credential-value"},
		}
		SanitizeAuditValue(raw)

		require.True(t, IsSensitiveAuditField(field), "expected %q to be sensitive", field)
		require.Equal(t, RedactedValue, raw[field], "expected %q to be redacted", field)
		nested, ok := raw["nested"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, RedactedValue, nested[field], "expected nested %q to be redacted", field)
		require.Equal(t, "instances/i1", raw["name"])
	}
}
