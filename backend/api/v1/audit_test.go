package v1

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

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

func TestIsNilConnectValue(t *testing.T) {
	t.Parallel()

	var typedNilResponse *connect.Response[v1pb.LoginResponse]
	var anyResponse connect.AnyResponse = typedNilResponse

	require.True(t, isNilConnectValue(nil))
	require.True(t, isNilConnectValue(anyResponse))
	require.False(t, isNilConnectValue(connect.NewResponse(&v1pb.LoginResponse{})))
}
