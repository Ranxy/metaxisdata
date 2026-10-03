//go:build integration

package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// fakeIdentityProvider is a stand-in for an OAuth2 identity provider: it hands
// out an access token and then reports whatever claims the test sets, so one
// test can act as several people.
type fakeIdentityProvider struct {
	srv *httptest.Server

	mu     sync.Mutex
	claims map[string]any
}

func newFakeIdentityProvider(t *testing.T) *fakeIdentityProvider {
	t.Helper()

	provider := &fakeIdentityProvider{}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fake-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, _ *http.Request) {
		provider.mu.Lock()
		claims := provider.claims
		provider.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(claims)
	})
	provider.srv = httptest.NewServer(mux)
	t.Cleanup(provider.srv.Close)
	return provider
}

// setClaims makes the provider report the given person.
func (p *fakeIdentityProvider) setClaims(subject, email string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.claims = map[string]any{"sub": subject, "email": email, "name": "SSO " + subject}
}

// TestSSOLoginBindsToTheIdentityProviderSubjectRealServerIntegration pins the
// SSO half of H3 against a running server and a stand-in identity provider: a
// login resolves against the provider-assigned subject the account is bound to,
// never against the email claim, so taking an address first does not take over
// the account the provider sends there.
func TestSSOLoginBindsToTheIdentityProviderSubjectRealServerIntegration(t *testing.T) {
	// Not parallel: this test registers an identity provider and edits
	// workspace-wide settings on the shared server.
	ctx := context.Background()
	env := sharedPostgresServiceEnvNoReset(t)
	httpClient := &http.Client{Timeout: 15 * time.Second}
	adminToken := env.AdminToken()
	userClient := v1connect.NewUserServiceClient(httpClient, env.BaseURL)
	authClient := v1connect.NewAuthServiceClient(httpClient, env.BaseURL)
	settingClient := v1connect.NewSettingServiceClient(httpClient, env.BaseURL)

	// The authorization-code exchange builds its redirect URL from the
	// workspace's external URL, so it has to be set for the flow to run.
	previous, err := settingClient.GetWorkspaceProfileSetting(ctx, connect.NewRequest(&v1pb.GetWorkspaceProfileSettingRequest{}))
	require.NoError(t, err)
	if previous.Msg.GetExternalUrl() == "" {
		_, err = settingClient.UpdateWorkspaceProfileSetting(ctx, withToken(adminToken, &v1pb.UpdateWorkspaceProfileSettingRequest{
			Setting:    &v1pb.WorkspaceProfileSetting{ExternalUrl: env.BaseURL},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"external_url"}},
		}))
		require.NoError(t, err)
		t.Cleanup(func() {
			_, restoreErr := settingClient.UpdateWorkspaceProfileSetting(context.Background(), withToken(adminToken, &v1pb.UpdateWorkspaceProfileSettingRequest{
				Setting:    &v1pb.WorkspaceProfileSetting{ExternalUrl: previous.Msg.GetExternalUrl()},
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"external_url"}},
			}))
			require.NoError(t, restoreErr)
		})
	}

	// There is no API that configures an identity provider; the row is written
	// straight into the metadata database, exactly as an operator would.
	provider := newFakeIdentityProvider(t)
	idpResourceID := fmt.Sprintf("sso-it-%d", time.Now().UnixNano())
	config, err := protojson.Marshal(&storepb.OAuth2IdentityProviderConfig{
		AuthUrl:      provider.srv.URL + "/authorize",
		TokenUrl:     provider.srv.URL + "/token",
		UserInfoUrl:  provider.srv.URL + "/userinfo",
		ClientId:     "integration-client",
		ClientSecret: "integration-secret",
		FieldMapping: &storepb.FieldMapping{
			Identifier:  "email",
			Subject:     "sub",
			DisplayName: "name",
		},
	})
	require.NoError(t, err)
	_, err = env.Store.GetDB().ExecContext(ctx,
		`INSERT INTO idp (resource_id, name, domain, type, config) VALUES ($1, $2, $3, $4, $5)`,
		idpResourceID, "Integration SSO", "example.com", storepb.IdentityProviderType_OAUTH2.String(), config)
	require.NoError(t, err)

	// loginAsProvider runs one authorization-code flow: fetch a single-use
	// state, then log in as whoever the provider currently reports.
	loginAsProvider := func(t *testing.T) (*v1pb.LoginResponse, error) {
		t.Helper()

		state, err := authClient.CreateSSOState(ctx, connect.NewRequest(&emptypb.Empty{}))
		require.NoError(t, err)

		resp, err := authClient.Login(ctx, connect.NewRequest(&v1pb.LoginRequest{
			IdpName: "idps/" + idpResourceID,
			IdpContext: &v1pb.IdentityProviderContext{
				Context: &v1pb.IdentityProviderContext_Oauth2Context{
					Oauth2Context: &v1pb.OAuth2IdentityProviderContext{
						Code:         "fake-authorization-code",
						State:        state.Msg.GetState(),
						CodeVerifier: "fake-code-verifier",
					},
				},
			},
		}))
		if err != nil {
			return nil, err
		}
		return resp.Msg, nil
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	subject := "employee-" + suffix
	employeeEmail := fmt.Sprintf("sso-employee-%s@example.com", suffix)

	var accountName string
	t.Run("the first login creates an account bound to the subject", func(t *testing.T) {
		provider.setClaims(subject, employeeEmail)

		resp, err := loginAsProvider(t)
		require.NoError(t, err)
		require.NotEmpty(t, resp.GetToken())
		require.Equal(t, employeeEmail, resp.GetUser().GetEmail())
		accountName = resp.GetUser().GetName()
		require.NotEmpty(t, accountName)

		var boundSubject string
		require.NoError(t, env.Store.GetDB().QueryRowContext(ctx,
			`SELECT idp_subject FROM principal WHERE email = $1`, employeeEmail).Scan(&boundSubject))
		require.Equal(t, subject, boundSubject, "the account is bound to the subject, not to the address")
	})

	t.Run("an address taken by someone else is not handed over", func(t *testing.T) {
		// The pre-empting account: registered with the victim's address and a
		// password its owner knows. It has no identity provider binding.
		claimedEmail := fmt.Sprintf("sso-claimed-%s@example.com", suffix)
		_, err := userClient.CreateUser(ctx, withToken(adminToken, &v1pb.CreateUserRequest{
			User: &v1pb.User{
				Email:    claimedEmail,
				Title:    "Pre-empting member",
				Password: "Integration-Pass-1!",
				UserType: v1pb.UserType_END_USER,
			},
		}))
		require.NoError(t, err)

		// The provider now reports that address for a different subject.
		provider.setClaims("real-employee-"+suffix, claimedEmail)
		_, err = loginAsProvider(t)
		require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "unexpected error: %v", err)

		var boundSubject string
		require.NoError(t, env.Store.GetDB().QueryRowContext(ctx,
			`SELECT idp_subject FROM principal WHERE email = $1`, claimedEmail).Scan(&boundSubject))
		require.Empty(t, boundSubject, "the pre-existing account must not be adopted by a login")
	})

	t.Run("deleting the squatting account frees the address", func(t *testing.T) {
		// The remedy the refusal names: the administrator deletes the account
		// that took the address — the API's own delete is a soft one — and the
		// identity provider's user is then provisioned. A deleted row must not
		// keep the address locked out for good.
		squattedEmail := fmt.Sprintf("sso-freed-%s@example.com", suffix)
		squatter, err := userClient.CreateUser(ctx, withToken(adminToken, &v1pb.CreateUserRequest{
			User: &v1pb.User{
				Email:    squattedEmail,
				Title:    "Squatting member",
				Password: "Integration-Pass-1!",
				UserType: v1pb.UserType_END_USER,
			},
		}))
		require.NoError(t, err)

		freedSubject := "freed-employee-" + suffix
		provider.setClaims(freedSubject, squattedEmail)
		_, err = loginAsProvider(t)
		require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "the active account blocks the login")

		_, err = userClient.DeleteUser(ctx, withToken(adminToken, &v1pb.DeleteUserRequest{Name: squatter.Msg.GetName()}))
		require.NoError(t, err)

		resp, err := loginAsProvider(t)
		require.NoError(t, err, "the deleted account must not keep the address")
		require.NotEqual(t, squatter.Msg.GetName(), resp.GetUser().GetName())

		var boundSubject string
		require.NoError(t, env.Store.GetDB().QueryRowContext(ctx,
			`SELECT idp_subject FROM principal WHERE email = $1 AND deleted = FALSE`, squattedEmail).Scan(&boundSubject))
		require.Equal(t, freedSubject, boundSubject)
	})

	t.Run("a repeat login follows the binding, not the address", func(t *testing.T) {
		movedEmail := fmt.Sprintf("sso-employee-moved-%s@example.com", suffix)
		_, err := userClient.UpdateUser(ctx, withToken(adminToken, &v1pb.UpdateUserRequest{
			User:       &v1pb.User{Name: accountName, Email: movedEmail},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"email"}},
		}))
		require.NoError(t, err)

		// The provider still reports the address the account was created with.
		provider.setClaims(subject, employeeEmail)
		resp, err := loginAsProvider(t)
		require.NoError(t, err)
		require.Equal(t, accountName, resp.GetUser().GetName(), "the same person gets the same account")
		require.Equal(t, movedEmail, resp.GetUser().GetEmail(), "the stored address is the administrator's, not the claim")
	})

	t.Run("a deactivated account is not resurrected", func(t *testing.T) {
		_, err := userClient.DeleteUser(ctx, withToken(adminToken, &v1pb.DeleteUserRequest{Name: accountName}))
		require.NoError(t, err)

		provider.setClaims(subject, employeeEmail)
		_, err = loginAsProvider(t)
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err), "signing in must not undo a deactivation")
	})
}
