package v1

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/component/audit"
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

	structured, raw := audit.MarshalAuditMessage(message)
	require.NotNil(t, structured)
	require.Equal(t, audit.RedactedValue, structured.GetFields()["password"].GetStringValue())
	require.Equal(t, audit.RedactedValue, audit.GetNestedString(raw, "idpContext"))
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
				require.Equal(t, audit.RedactedValue, raw["key"])
			},
		},
		{
			name: "data source credentials",
			message: &v1pb.DataSource{
				Name:          "instances/i1/dataSources/admin",
				SslCert:       "-----BEGIN CERTIFICATE-----",
				SslKey:        "-----BEGIN PRIVATE KEY-----",
				SshPrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----",
			},
			assert: func(t *testing.T, raw map[string]any) {
				require.Equal(t, audit.RedactedValue, raw["sslCert"])
				require.Equal(t, audit.RedactedValue, raw["sslKey"])
				require.Equal(t, audit.RedactedValue, raw["sshPrivateKey"])
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			structured, raw := audit.MarshalAuditMessage(tc.message)
			require.NotNil(t, structured)
			tc.assert(t, raw)
		})
	}
}

// deviceCode is the polling secret returned by CreateDeviceLogin. The audit
// interceptor records responses too, so it has to be redacted; userCode is what
// the user types and stays readable.
func TestMarshalAuditMessageRedactsTheDeviceLoginSecret(t *testing.T) {
	t.Parallel()

	structured, raw := audit.MarshalAuditMessage(&v1pb.CreateDeviceLoginResponse{
		DeviceCode:              "polling-secret",
		UserCode:                "7Q2X-9M4K",
		VerificationUri:         "https://mx.example.com/device",
		VerificationUriComplete: "https://mx.example.com/device?user_code=7Q2X-9M4K",
	})
	require.NotNil(t, structured)

	require.Equal(t, audit.RedactedValue, raw["deviceCode"])
	require.Equal(t, "7Q2X-9M4K", raw["userCode"])
	require.Equal(t, "https://mx.example.com/device", raw["verificationUri"])
}

func TestIsSensitiveAuditField(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"key", "sslKey", "sslCert", "passwd", "pwd", "bearer", "jwt", "session", "password", "apiKey", "accessKeyId", "sshPrivateKey", "deviceCode", "device_code"} {
		require.True(t, audit.IsSensitiveAuditField(field), "expected %q to be redacted", field)
	}
	for _, field := range []string{"email", "name", "host", "port", "description", "userCode"} {
		require.False(t, audit.IsSensitiveAuditField(field), "expected %q to be kept", field)
	}
	// The marker list redacts anything whose name contains "password" or
	// "session", which is why the flag an adopted SSO login reports is named
	// accountAdopted: renaming it after either of them would erase the takeover
	// from the ledger.
	require.False(t, audit.IsSensitiveAuditField("accountAdopted"))
	require.True(t, audit.IsSensitiveAuditField("passwordInvalidated"), "the name a password-named flag would have had")
}

func TestIsNilConnectValue(t *testing.T) {
	t.Parallel()

	var typedNilResponse *connect.Response[v1pb.LoginResponse]
	var anyResponse connect.AnyResponse = typedNilResponse

	require.True(t, isNilConnectValue(nil))
	require.True(t, isNilConnectValue(anyResponse))
	require.False(t, isNilConnectValue(connect.NewResponse(&v1pb.LoginResponse{})))
}

// A streaming audit row must carry the origin of the call. This branch used to
// pass an empty peer address, so every audited stream recorded a row with no IP
// at all — invisible today because the only streaming RPC is not audited, and a
// silent loss the moment one is.
func TestStreamingAuditMetadataCarriesThePeerAddress(t *testing.T) {
	t.Parallel()

	metadata := streamingRequestMetadata(&fakeStreamingConn{procedure: "/p", peerAddr: "203.0.113.5:4040"}, nil)
	require.Equal(t, "203.0.113.5", metadata.GetIp())
}
