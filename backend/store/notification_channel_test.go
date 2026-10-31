package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The sealed credential reads both the current shape and the pre-release PR-1
// shape, so a channel created on that build does not silently lose its endpoint.
func TestUnmarshalNotificationChannelSecret(t *testing.T) {
	t.Parallel()

	secret, err := unmarshalNotificationChannelSecret(`{"endpoint":"https://example.com/hook","signingSecret":"s3cret"}`)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/hook", secret.GetEndpoint())
	require.Equal(t, "s3cret", secret.GetSigningSecret())

	legacy, err := unmarshalNotificationChannelSecret(`{"webhookUrl":"https://example.com/legacy","webhookSigningSecret":"old"}`)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/legacy", legacy.GetEndpoint())
	require.Equal(t, "old", legacy.GetSigningSecret())

	_, err = unmarshalNotificationChannelSecret(`not json`)
	require.Error(t, err)
}
