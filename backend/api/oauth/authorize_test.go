package oauth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMatchesRedirectURI(t *testing.T) {
	t.Parallel()

	const registered = "https://app.example.com/callback"

	matching := []struct {
		name      string
		requested string
	}{
		{name: "exact", requested: registered},
		{name: "exact with the registered query", requested: "https://app.example.com/callback?a=b"},
	}
	for _, tc := range matching {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			registeredURI := registered
			if tc.requested != registered {
				registeredURI = tc.requested
			}
			require.True(t, MatchesRedirectURI([]string{registeredURI}, tc.requested))
		})
	}

	rejected := []struct {
		name      string
		requested string
	}{
		{name: "a suffix", requested: registered + ".evil.com"},
		{name: "a different path", requested: "https://app.example.com/other"},
		{name: "a different host", requested: "https://app.example.com.evil.com/callback"},
		{name: "a different scheme", requested: "http://app.example.com/callback"},
		{name: "an added query the registration does not carry", requested: registered + "?a=b"},
		{name: "a fragment", requested: registered + "#frag"},
		{name: "an empty value", requested: ""},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.False(t, MatchesRedirectURI([]string{registered}, tc.requested))
		})
	}

	t.Run("an empty registration accepts nothing", func(t *testing.T) {
		t.Parallel()
		require.False(t, MatchesRedirectURI(nil, registered))
	})

	t.Run("loopback ignores the port but not the path", func(t *testing.T) {
		t.Parallel()

		for _, host := range []string{"127.0.0.1", "localhost", "[::1]"} {
			require.True(t, MatchesRedirectURI(
				[]string{"http://" + host + ":51004/callback"},
				"http://"+host+":61234/callback",
			), "a native client picks an ephemeral port at runtime")
			require.False(t, MatchesRedirectURI(
				[]string{"http://" + host + ":51004/callback"},
				"http://"+host+":61234/other",
			))
			require.False(t, MatchesRedirectURI(
				[]string{"https://" + host + ":51004/callback"},
				"http://"+host+":61234/callback",
			), "the loopback relaxation must not also relax the scheme")
		}
	})
}

func TestValidateRedirectURI(t *testing.T) {
	t.Parallel()

	valid := []string{
		"https://app.example.com/callback",
		"https://app.example.com:8443/callback",
		"http://127.0.0.1:51004/callback",
		"http://localhost/callback",
		"http://[::1]:8080/callback",
	}
	for _, uri := range valid {
		t.Run("valid "+uri, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, ValidateRedirectURI(uri))
		})
	}

	invalid := []string{
		"",
		"app.example.com/callback",
		"http://app.example.com/callback",
		"com.example.app:/callback",
		"https://app.example.com/callback#frag",
		"https://*.example.com/callback",
	}
	for _, uri := range invalid {
		t.Run("invalid "+uri, func(t *testing.T) {
			t.Parallel()
			require.Error(t, ValidateRedirectURI(uri))
		})
	}
}

func TestVerifyPKCE(t *testing.T) {
	t.Parallel()

	// The worked example from RFC 7636 appendix B.
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	const challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	t.Run("a matching verifier is accepted", func(t *testing.T) {
		t.Parallel()
		require.True(t, VerifyPKCE(challenge, verifier))
	})

	t.Run("a wrong verifier is refused", func(t *testing.T) {
		t.Parallel()
		require.False(t, VerifyPKCE(challenge, verifier+"x"))
	})

	t.Run("an empty challenge or verifier is refused", func(t *testing.T) {
		t.Parallel()
		require.False(t, VerifyPKCE("", verifier))
		require.False(t, VerifyPKCE(challenge, ""))
	})

	t.Run("a plain challenge never matches", func(t *testing.T) {
		t.Parallel()
		require.False(t, VerifyPKCE(verifier, verifier), "only S256 is supported")
	})
}

func TestScopes(t *testing.T) {
	t.Parallel()

	require.True(t, RequestedScopesValid(nil), "an empty scope request is granted the one scope that exists")
	require.True(t, RequestedScopesValid([]string{MCPReadScope}))
	require.False(t, RequestedScopesValid([]string{"other.scope"}))
	require.False(t, RequestedScopesValid([]string{MCPReadScope, "other.scope"}))

	require.Nil(t, ParseScopeList(""))
	require.Nil(t, ParseScopeList("   "))
	require.Equal(t, []string{MCPReadScope}, ParseScopeList("  "+MCPReadScope+"  "))
	require.Equal(t, []string{MCPReadScope, "b"}, ParseScopeList(MCPReadScope+" b"))
}
