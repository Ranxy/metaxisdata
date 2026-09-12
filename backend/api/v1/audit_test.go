package v1

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

func TestMarshalAuditMessageRedactsSensitiveFields(t *testing.T) {
	t.Parallel()

	message := &v1pb.LoginRequest{
		Email:    "alice@example.com",
		Password: "super-secret",
		IdpContext: &v1pb.IdentityProviderContext{
			Context: &v1pb.IdentityProviderContext_Oauth2Context{
				Oauth2Context: &v1pb.OAuth2IdentityProviderContext{Code: "oauth-code"},
			},
		},
	}

	structured, raw, err := marshalAuditMessage(message)
	require.NoError(t, err)
	require.NotNil(t, structured)
	require.Equal(t, redactedValue, structured.GetFields()["password"].GetStringValue())
	require.Equal(t, redactedValue, getNestedString(raw, "idpContext"))
	require.Equal(t, "alice@example.com", structured.GetFields()["email"].GetStringValue())
}

// Credential-bearing payloads whose field names are not caught by a substring
// marker must still be redacted before they are persisted.
func TestMarshalAuditMessageRedactsSecrets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message proto.Message
		assert  func(t *testing.T, raw map[string]any)
	}{
		{
			// CreateAPIKey promises the plaintext ingestion key is returned
			// only once; persisting it in audit_log.response breaks that.
			name:    "create api key response",
			message: &v1pb.CreateAPIKeyResponse{Key: "mt_ingestion_secret"},
			assert: func(t *testing.T, raw map[string]any) {
				require.Equal(t, redactedValue, raw["key"])
			},
		},
		{
			name: "data source credentials",
			message: &v1pb.DataSource{
				Id:      "admin",
				SslCert: "-----BEGIN CERTIFICATE-----",
				SslKey:  "-----BEGIN PRIVATE KEY-----",
				IamExtension: &v1pb.DataSource_GcpCredential{
					GcpCredential: &v1pb.DataSource_GCPCredential{Content: `{"private_key":"..."}`},
				},
			},
			assert: func(t *testing.T, raw map[string]any) {
				require.Equal(t, redactedValue, raw["sslCert"])
				require.Equal(t, redactedValue, raw["sslKey"])
				// The whole credential object is redacted by its field name.
				require.Equal(t, redactedValue, raw["gcpCredential"])
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			structured, raw, err := marshalAuditMessage(tc.message)
			require.NoError(t, err)
			require.NotNil(t, structured)
			tc.assert(t, raw)
		})
	}
}

func TestIsSensitiveAuditField(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"key", "content", "sslKey", "sslCert", "keytab", "passwd", "pwd", "bearer", "jwt", "session", "password", "apiKey", "accessKeyId"} {
		require.True(t, isSensitiveAuditField(field), "expected %q to be redacted", field)
	}
	for _, field := range []string{"email", "name", "host", "port", "description"} {
		require.False(t, isSensitiveAuditField(field), "expected %q to be kept", field)
	}
}

func TestIsNilConnectValue(t *testing.T) {
	t.Parallel()

	var typedNilResponse *connect.Response[v1pb.LoginResponse]
	var anyResponse connect.AnyResponse = typedNilResponse

	require.True(t, isNilConnectValue(nil))
	require.True(t, isNilConnectValue(anyResponse))
	require.False(t, isNilConnectValue(connect.NewResponse(&v1pb.LoginResponse{})))
}
