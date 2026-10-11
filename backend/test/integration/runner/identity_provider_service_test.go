//go:build integration

package runner

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	integrationenv "github.com/Ranxy/metaxisdata/backend/test/integration/env"
)

// storedIdentityProviderConfig reads the raw JSONB the API wrote, which is the
// only way to prove the secret is encrypted rather than merely hidden from the
// response.
func storedIdentityProviderConfig(ctx context.Context, t *testing.T, env *integrationenv.ServiceEnv, resourceID string) string {
	t.Helper()

	var config string
	err := env.Store.GetDB().QueryRowContext(ctx,
		`SELECT config FROM idp WHERE resource_id = $1`, resourceID).Scan(&config)
	require.NoError(t, err)
	return config
}

// identityProviderConfigForUpdate is the config a client sends back after loading
// a provider: every non-secret field as stored, and the secret only when the
// operator typed a new one.
func identityProviderConfigForUpdate(baseURL, clientSecret string) *v1pb.IdentityProviderConfig {
	return &v1pb.IdentityProviderConfig{
		Config: &v1pb.IdentityProviderConfig_Oauth2Config{
			Oauth2Config: &v1pb.OAuth2IdentityProviderConfig{
				AuthUrl:      baseURL + "/authorize",
				TokenUrl:     baseURL + "/token",
				UserInfoUrl:  baseURL + "/userinfo",
				ClientId:     "admin-surface-client",
				ClientSecret: clientSecret,
				FieldMapping: &v1pb.FieldMapping{
					Identifier:  "email",
					Subject:     "sub",
					DisplayName: "name",
				},
			},
		},
	}
}

func createIdentityProviderThroughAPI(ctx context.Context, t *testing.T, client v1connect.IdentityProviderServiceClient, token, resourceID string, baseURL string) *v1pb.IdentityProvider {
	t.Helper()

	response, err := client.CreateIdentityProvider(ctx, withToken(token, &v1pb.CreateIdentityProviderRequest{
		IdentityProviderId: resourceID,
		IdentityProvider: &v1pb.IdentityProvider{
			Title:  "Admin Surface SSO",
			Domain: "example.com",
			Type:   v1pb.IdentityProviderType_OAUTH2,
			Config: identityProviderConfigForUpdate(baseURL, "admin-surface-secret"),
		},
	}))
	require.NoError(t, err)
	return response.Msg
}

// TestIdentityProviderAdminSurfaceRealServerIntegration pins the behaviors the
// admin surface exists for: it is the only write path, the secret never leaves
// the server, and a binding can be seen and removed instead of needing SQL.
func TestIdentityProviderAdminSurfaceRealServerIntegration(t *testing.T) {
	// Not parallel: it edits workspace-wide settings on the shared server.
	ctx := context.Background()
	env := sharedPostgresServiceEnvNoReset(t)
	httpClient := &http.Client{Timeout: 15 * time.Second}
	adminToken := env.AdminToken()
	idpClient := v1connect.NewIdentityProviderServiceClient(httpClient, env.BaseURL)
	authClient := v1connect.NewAuthServiceClient(httpClient, env.BaseURL)
	settingClient := v1connect.NewSettingServiceClient(httpClient, env.BaseURL)

	ensureIntegrationExternalURL(ctx, t, env, settingClient, adminToken)

	provider := newFakeIdentityProvider(t)
	idpResourceID := fmt.Sprintf("sso-admin-%d", time.Now().UnixNano())
	idpName := "idps/" + idpResourceID
	subject := "admin-surface-employee"
	provider.setClaims(subject, "admin-surface@example.com")

	t.Cleanup(func() {
		_, _ = idpClient.DeleteIdentityProvider(context.Background(), withToken(adminToken, &v1pb.DeleteIdentityProviderRequest{Name: idpName}))
	})

	t.Run("the login page can list providers without credentials", func(t *testing.T) {
		// No token on the request: this is exactly the call the login page makes
		// before anyone has signed in. What it must not do is require a session.
		_, err := idpClient.ListIdentityProviders(ctx, connect.NewRequest(&v1pb.ListIdentityProvidersRequest{}))
		require.NoError(t, err)
	})

	t.Run("an anonymous caller cannot configure a provider", func(t *testing.T) {
		_, err := idpClient.CreateIdentityProvider(ctx, connect.NewRequest(&v1pb.CreateIdentityProviderRequest{
			IdentityProviderId: "anonymous-attempt",
			IdentityProvider:   &v1pb.IdentityProvider{Type: v1pb.IdentityProviderType_OAUTH2},
		}))
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))

		_, err = idpClient.DeleteIdentityProvider(ctx, connect.NewRequest(&v1pb.DeleteIdentityProviderRequest{Name: idpName}))
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	})

	t.Run("a bad id and a bad config are refused", func(t *testing.T) {
		_, err := idpClient.CreateIdentityProvider(ctx, withToken(adminToken, &v1pb.CreateIdentityProviderRequest{
			IdentityProviderId: "Not-A-Valid-Id",
			IdentityProvider:   &v1pb.IdentityProvider{Type: v1pb.IdentityProviderType_OAUTH2},
		}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

		_, err = idpClient.CreateIdentityProvider(ctx, withToken(adminToken, &v1pb.CreateIdentityProviderRequest{
			IdentityProviderId: "missing-secret",
			IdentityProvider: &v1pb.IdentityProvider{
				Title: "Incomplete",
				Type:  v1pb.IdentityProviderType_OAUTH2,
				Config: &v1pb.IdentityProviderConfig{
					Config: &v1pb.IdentityProviderConfig_Oauth2Config{
						Oauth2Config: &v1pb.OAuth2IdentityProviderConfig{
							AuthUrl:      provider.srv.URL + "/authorize",
							TokenUrl:     provider.srv.URL + "/token",
							UserInfoUrl:  provider.srv.URL + "/userinfo",
							ClientId:     "client",
							FieldMapping: &v1pb.FieldMapping{Identifier: "email"},
						},
					},
				},
			},
		}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	createIdentityProviderThroughAPI(ctx, t, idpClient, adminToken, idpResourceID, provider.srv.URL)

	storedAfterCreate := storedIdentityProviderConfig(ctx, t, env, idpResourceID)

	t.Run("the stored secret is a ciphertext", func(t *testing.T) {
		require.Contains(t, storedAfterCreate, "v1:",
			"the client secret was not encrypted before it reached the row")
		require.NotContains(t, storedAfterCreate, "admin-surface-secret",
			"the client secret is in the row in the clear")
	})

	t.Run("no read returns the secret", func(t *testing.T) {
		got, err := idpClient.GetIdentityProvider(ctx, withToken(adminToken, &v1pb.GetIdentityProviderRequest{Name: idpName}))
		require.NoError(t, err)
		require.Empty(t, got.Msg.GetConfig().GetOauth2Config().GetClientSecret())

		listed, err := idpClient.ListIdentityProviders(ctx, connect.NewRequest(&v1pb.ListIdentityProvidersRequest{}))
		require.NoError(t, err)
		for _, identityProvider := range listed.Msg.GetIdentityProviders() {
			require.Empty(t, identityProvider.GetConfig().GetOauth2Config().GetClientSecret(),
				"provider %q leaked its secret through List", identityProvider.GetName())
		}
	})

	t.Run("the same id cannot be created twice", func(t *testing.T) {
		_, err := idpClient.CreateIdentityProvider(ctx, withToken(adminToken, &v1pb.CreateIdentityProviderRequest{
			IdentityProviderId: idpResourceID,
			IdentityProvider: &v1pb.IdentityProvider{
				Title: "Duplicate",
				Type:  v1pb.IdentityProviderType_OAUTH2,
				Config: &v1pb.IdentityProviderConfig{
					Config: &v1pb.IdentityProviderConfig_Oauth2Config{
						Oauth2Config: &v1pb.OAuth2IdentityProviderConfig{
							AuthUrl:      provider.srv.URL + "/authorize",
							TokenUrl:     provider.srv.URL + "/token",
							UserInfoUrl:  provider.srv.URL + "/userinfo",
							ClientId:     "client",
							ClientSecret: "secret",
							FieldMapping: &v1pb.FieldMapping{Identifier: "email"},
						},
					},
				},
			},
		}))
		require.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(err))
	})

	t.Run("a title-only update does not rewrite the config", func(t *testing.T) {
		_, err := idpClient.UpdateIdentityProvider(ctx, withToken(adminToken, &v1pb.UpdateIdentityProviderRequest{
			IdentityProvider: &v1pb.IdentityProvider{
				Name:  idpName,
				Title: "Renamed SSO",
			},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title"}},
		}))
		require.NoError(t, err)

		got, err := idpClient.GetIdentityProvider(ctx, withToken(adminToken, &v1pb.GetIdentityProviderRequest{Name: idpName}))
		require.NoError(t, err)
		require.Equal(t, "Renamed SSO", got.Msg.GetTitle())
		// The config JSONB holds the outer IdentityProviderConfig, and this read
		// decodes it: a mapping that came back blank here is what a sign-in would
		// then refuse, which is exactly the failure this assertion pins.
		require.Equal(t, "sub", got.Msg.GetConfig().GetOauth2Config().GetFieldMapping().GetSubject(),
			"the stored claim mapping was lost across an update")

		require.Equal(t, storedAfterCreate, storedIdentityProviderConfig(ctx, t, env, idpResourceID),
			"a title-only update rewrote the encrypted config")
	})

	t.Run("a config update that sends no secret keeps the stored one", func(t *testing.T) {
		// The read path never returned the secret, so this is exactly what a client
		// sends when the operator opens the provider and saves without retyping it.
		// The update is accepted only because the server put the stored secret back:
		// validation rejects an empty one.
		_, err := idpClient.UpdateIdentityProvider(ctx, withToken(adminToken, &v1pb.UpdateIdentityProviderRequest{
			IdentityProvider: &v1pb.IdentityProvider{
				Name:   idpName,
				Title:  "Renamed SSO",
				Config: identityProviderConfigForUpdate(provider.srv.URL, ""),
			},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"config"}},
		}))
		require.NoError(t, err)

		require.Contains(t, storedIdentityProviderConfig(ctx, t, env, idpResourceID), "v1:",
			"the config update left no ciphertext behind")

		// And the secret the server presents is still the original one.
		_, err = loginViaIntegrationIdentityProvider(ctx, t, authClient, idpResourceID)
		require.NoError(t, err)
		require.Equal(t, "admin-surface-secret", provider.presentedClientSecret(),
			"the server presented something other than the stored secret")
	})

	t.Run("a config update that replaces the secret encrypts and presents the replacement", func(t *testing.T) {
		before := storedIdentityProviderConfig(ctx, t, env, idpResourceID)

		_, err := idpClient.UpdateIdentityProvider(ctx, withToken(adminToken, &v1pb.UpdateIdentityProviderRequest{
			IdentityProvider: &v1pb.IdentityProvider{
				Name:   idpName,
				Title:  "Renamed SSO",
				Config: identityProviderConfigForUpdate(provider.srv.URL, "replacement-secret"),
			},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"config"}},
		}))
		require.NoError(t, err)

		stored := storedIdentityProviderConfig(ctx, t, env, idpResourceID)
		require.NotEqual(t, before, stored, "the config was not rewritten")
		require.Contains(t, stored, "v1:")
		require.NotContains(t, stored, "replacement-secret",
			"the replacement secret reached the row in the clear")

		_, err = loginViaIntegrationIdentityProvider(ctx, t, authClient, idpResourceID)
		require.NoError(t, err)
		require.Equal(t, "replacement-secret", provider.presentedClientSecret())
	})

	t.Run("the stored secret still authenticates a login", func(t *testing.T) {
		response, err := loginViaIntegrationIdentityProvider(ctx, t, authClient, idpResourceID)
		require.NoError(t, err)
		require.NotEmpty(t, response.GetToken())
	})

	t.Run("a binding can be listed and removed without SQL", func(t *testing.T) {
		listed, err := idpClient.ListIdentityProviderBindings(ctx, withToken(adminToken, &v1pb.ListIdentityProviderBindingsRequest{Idp: idpName}))
		require.NoError(t, err)
		require.Len(t, listed.Msg.GetIdentityProviderBindings(), 1)
		binding := listed.Msg.GetIdentityProviderBindings()[0]
		require.Equal(t, subject, binding.GetSubject())
		require.NotEmpty(t, binding.GetUserEmail())
		require.Contains(t, binding.GetUser(), "users/")

		_, err = idpClient.DeleteIdentityProviderBinding(ctx, withToken(adminToken, &v1pb.DeleteIdentityProviderBindingRequest{
			User:    binding.GetUser(),
			Idp:     idpName,
			Subject: binding.GetSubject(),
		}))
		require.NoError(t, err)

		after, err := idpClient.ListIdentityProviderBindings(ctx, withToken(adminToken, &v1pb.ListIdentityProviderBindingsRequest{Idp: idpName}))
		require.NoError(t, err)
		require.Empty(t, after.Msg.GetIdentityProviderBindings())
	})

	t.Run("a deleted provider is gone", func(t *testing.T) {
		_, err := idpClient.DeleteIdentityProvider(ctx, withToken(adminToken, &v1pb.DeleteIdentityProviderRequest{Name: idpName}))
		require.NoError(t, err)

		_, err = idpClient.GetIdentityProvider(ctx, withToken(adminToken, &v1pb.GetIdentityProviderRequest{Name: idpName}))
		require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})
}
