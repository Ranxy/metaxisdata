package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOAuthRedirectURIsRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []string
	}{
		{"nil list", nil},
		{"empty list", []string{}},
		{"single loopback URI", []string{"http://127.0.0.1:8976/callback"}},
		{"URI with a query string", []string{"https://app.example.com/oauth/callback?tenant=acme&flow=mcp"}},
		{"several URIs keep their order", []string{"https://a.example.com/cb", "http://localhost:1234/cb"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := encodeOAuthRedirectURIs(tt.in)
			require.NoError(t, err)
			require.True(t, json.Valid(encoded), "encoded value %s must be valid JSON", encoded)

			decoded, err := decodeOAuthRedirectURIs(encoded)
			require.NoError(t, err)
			if len(tt.in) == 0 {
				// The column is NOT NULL, so an absent list must round-trip as
				// an empty array rather than as null.
				require.JSONEq(t, "[]", string(encoded))
				require.Empty(t, decoded)
				return
			}
			require.Equal(t, tt.in, decoded)
		})
	}
}

func TestDecodeOAuthRedirectURIsHandlesNullAndRejectsNonArray(t *testing.T) {
	t.Parallel()

	// A pre-existing row with a NULL column must not crash the read path.
	decoded, err := decodeOAuthRedirectURIs(nil)
	require.NoError(t, err)
	require.Empty(t, decoded)

	_, err = decodeOAuthRedirectURIs([]byte(`{"uri": "https://example.com"}`))
	require.Error(t, err)
}

func TestOAuthClientFromScan(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	lastUsed := created.Add(time.Hour)

	client, err := oauthClientFromScan(
		"client-1", "Claude Code",
		[]byte(`["https://app.example.com/oauth/callback?tenant=acme&flow=mcp"]`),
		"none", created, &lastUsed,
	)
	require.NoError(t, err)
	require.Equal(t, &OAuthClient{
		ClientID:                "client-1",
		ClientName:              "Claude Code",
		RedirectURIs:            []string{"https://app.example.com/oauth/callback?tenant=acme&flow=mcp"},
		TokenEndpointAuthMethod: "none",
		CreatedTime:             created,
		LastUsedTime:            &lastUsed,
	}, client)

	// A client that never exchanged a token has a NULL last_used_time.
	client, err = oauthClientFromScan("client-2", "", []byte(`[]`), "none", created, nil)
	require.NoError(t, err)
	require.Nil(t, client.LastUsedTime)
	require.Empty(t, client.RedirectURIs)

	_, err = oauthClientFromScan("client-3", "", []byte(`not json`), "none", created, nil)
	require.Error(t, err)
}
