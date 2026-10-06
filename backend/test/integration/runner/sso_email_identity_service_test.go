//go:build integration

package runner

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	integrationenv "github.com/Ranxy/metaxisdata/backend/test/integration/env"
)

// setSSOEmailIdentityIDPs names the identity providers whose address claim may
// stand in for a stable subject. The list is the whole setting: naming one
// provider leaves every other one alone.
func setSSOEmailIdentityIDPs(ctx context.Context, t *testing.T, client v1connect.SettingServiceClient, adminToken string, idps ...string) {
	t.Helper()

	names := make([]string, 0, len(idps))
	for _, idp := range idps {
		names = append(names, "idps/"+idp)
	}
	_, err := client.UpdateWorkspaceProfileSetting(ctx, withToken(adminToken, &v1pb.UpdateWorkspaceProfileSettingRequest{
		Setting:    &v1pb.WorkspaceProfileSetting{SsoEmailIdentityIdps: names},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"sso_email_identity_idps"}},
	}))
	require.NoError(t, err)
}

// restoreSSOEmailIdentityIDPs asserts the setting starts empty, as the default
// has to be, and puts it back the way the test found it when the whole test ends:
// it is a workspace-wide setting on a server the rest of the suite shares, so a
// per-subtest cleanup would flip it between subtests.
func restoreSSOEmailIdentityIDPs(ctx context.Context, t *testing.T, client v1connect.SettingServiceClient, adminToken string) {
	t.Helper()

	previous, err := client.GetWorkspaceProfileSetting(ctx, connect.NewRequest(&v1pb.GetWorkspaceProfileSettingRequest{}))
	require.NoError(t, err)
	require.Empty(t, previous.Msg.GetSsoEmailIdentityIdps(), "no provider is trusted with the address claim by default")
	t.Cleanup(func() {
		_, restoreErr := client.UpdateWorkspaceProfileSetting(context.Background(), withToken(adminToken, &v1pb.UpdateWorkspaceProfileSettingRequest{
			Setting:    &v1pb.WorkspaceProfileSetting{SsoEmailIdentityIdps: previous.Msg.GetSsoEmailIdentityIdps()},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"sso_email_identity_idps"}},
		}))
		require.NoError(t, restoreErr)
	})
}

// bindingsOf returns the identity provider subjects an account may sign in with,
// keyed by provider.
func bindingsOf(ctx context.Context, t *testing.T, env *integrationenv.ServiceEnv, email string) map[string]string {
	t.Helper()

	rows, err := env.Store.GetDB().QueryContext(ctx, `
		SELECT binding.idp_resource_id, binding.subject
		FROM principal_idp_binding AS binding
		JOIN principal ON principal.id = binding.principal_id
		WHERE principal.email = $1`, email)
	require.NoError(t, err)
	defer rows.Close()

	bindings := map[string]string{}
	for rows.Next() {
		var resourceID, subject string
		require.NoError(t, rows.Scan(&resourceID, &subject))
		bindings[resourceID] = subject
	}
	require.NoError(t, rows.Err())
	return bindings
}

// TestSSOEmailIdentityAdoptsAnAccountRealServerIntegration pins the opt-in an
// administrator grants one identity provider at a time for deployments whose
// provider exposes no stable subject: a login through a listed provider is
// identified by the provider's address claim and makes the account that already
// carries that address reachable — adopting it, and voiding its password, when it
// had no provider binding at all, and only adding a way in when it already had
// one. A provider that is not listed keeps the default: the address is not an
// identity.
func TestSSOEmailIdentityAdoptsAnAccountRealServerIntegration(t *testing.T) {
	// Not parallel: it registers identity providers and edits workspace-wide
	// settings on the shared server.
	ctx := context.Background()
	env := sharedPostgresServiceEnvNoReset(t)
	httpClient := &http.Client{Timeout: 15 * time.Second}
	adminToken := env.AdminToken()
	userClient := v1connect.NewUserServiceClient(httpClient, env.BaseURL)
	authClient := v1connect.NewAuthServiceClient(httpClient, env.BaseURL)
	settingClient := v1connect.NewSettingServiceClient(httpClient, env.BaseURL)

	ensureIntegrationExternalURL(ctx, t, env, settingClient, adminToken)
	restoreSSOEmailIdentityIDPs(ctx, t, settingClient, adminToken)

	// Two providers: one maps no subject at all (what the setting exists for), the
	// other maps a stable subject (the ordinary configuration).
	emailProvider := newFakeIdentityProvider(t)
	emailIdentityIDP := fmt.Sprintf("sso-email-%d", time.Now().UnixNano())
	registerIntegrationIdentityProvider(ctx, t, env, emailProvider, emailIdentityIDP, &storepb.FieldMapping{
		Identifier:  "email",
		DisplayName: "name",
	})

	subjectProvider := newFakeIdentityProvider(t)
	subjectIDP := fmt.Sprintf("sso-subject-%d", time.Now().UnixNano())
	registerIntegrationIdentityProvider(ctx, t, env, subjectProvider, subjectIDP, &storepb.FieldMapping{
		Identifier:  "email",
		Subject:     "sub",
		DisplayName: "name",
	})

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	const password = "Integration-Pass-1!"
	newMember := func(t *testing.T, email string) string {
		t.Helper()
		created, err := userClient.CreateUser(ctx, withToken(adminToken, &v1pb.CreateUserRequest{
			User: &v1pb.User{
				Email:    email,
				Title:    "Pre-provisioned member",
				Password: password,
				UserType: v1pb.UserType_END_USER,
			},
		}))
		require.NoError(t, err)
		return created.Msg.GetName()
	}

	t.Run("a provider that is not listed may not adopt the account", func(t *testing.T) {
		// The address provider is listed; the subject provider is not, so its login
		// stays on the default rule even though the workspace has opened the path
		// for another provider.
		setSSOEmailIdentityIDPs(ctx, t, settingClient, adminToken, emailIdentityIDP)

		email := fmt.Sprintf("sso-unlisted-%s@example.com", suffix)
		newMember(t, email)
		subjectProvider.setClaims("unlisted-"+suffix, email)
		_, err := loginViaIntegrationIdentityProvider(ctx, t, authClient, subjectIDP)
		require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "unexpected error: %v", err)

		// Nothing was touched: the member's password still works.
		_, err = env.LoginAs(ctx, email, password)
		require.NoError(t, err)
	})

	t.Run("a listed provider adopts the account", func(t *testing.T) {
		setSSOEmailIdentityIDPs(ctx, t, settingClient, adminToken, emailIdentityIDP)

		email := fmt.Sprintf("sso-adopt-%s@example.com", suffix)
		accountName := newMember(t, email)
		oldToken, err := env.LoginAs(ctx, email, password)
		require.NoError(t, err)

		// The provider reports the address in another case: everything the platform
		// stores and matches on is the lower-cased address.
		emailProvider.setClaims("employee-"+suffix, strings.ToUpper(email))
		resp, err := loginViaIntegrationIdentityProvider(ctx, t, authClient, emailIdentityIDP)
		require.NoError(t, err)
		require.True(t, resp.GetAccountAdopted(), "the client has to be told the password is gone")
		require.Equal(t, accountName, resp.GetUser().GetName(), "the account is adopted, not left behind for a new one")
		require.Equal(t, email, resp.GetUser().GetEmail())

		// The password it had no longer works, and neither does the session minted
		// before the change.
		_, err = env.LoginAs(ctx, email, password)
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
		_, err = userClient.GetCurrentUser(ctx, withToken(oldToken, &emptypb.Empty{}))
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))

		require.Equal(t, map[string]string{emailIdentityIDP: email}, bindingsOf(ctx, t, env, email))
	})

	t.Run("a second provider for the same account adds a way in", func(t *testing.T) {
		// One workspace can configure several providers and a person may be enrolled
		// in more than one of them: the second must reach the same account rather
		// than be refused, and must not disturb it.
		setSSOEmailIdentityIDPs(ctx, t, settingClient, adminToken, emailIdentityIDP, subjectIDP)

		email := fmt.Sprintf("sso-two-idps-%s@example.com", suffix)
		accountName := newMember(t, email)

		emailProvider.setClaims("first-"+suffix, email)
		first, err := loginViaIntegrationIdentityProvider(ctx, t, authClient, emailIdentityIDP)
		require.NoError(t, err)
		require.True(t, first.GetAccountAdopted())
		require.NotEmpty(t, first.GetToken())

		subjectProvider.setClaims("second-"+suffix, email)
		second, err := loginViaIntegrationIdentityProvider(ctx, t, authClient, subjectIDP)
		require.NoError(t, err, "a second provider for the same person must sign in")
		require.Equal(t, accountName, second.GetUser().GetName(), "both providers reach one account")
		require.False(t, second.GetAccountAdopted(), "the account already signed in through a provider; nothing was adopted")

		// Adding a way in does not retire the sessions the first provider minted.
		_, err = userClient.GetCurrentUser(ctx, withToken(first.GetToken(), &emptypb.Empty{}))
		require.NoError(t, err)

		require.Equal(t, map[string]string{
			emailIdentityIDP: email,
			subjectIDP:       "second-" + suffix,
		}, bindingsOf(ctx, t, env, email))
	})

	t.Run("a new account is created, not adopted", func(t *testing.T) {
		setSSOEmailIdentityIDPs(ctx, t, settingClient, adminToken, emailIdentityIDP)

		freshEmail := fmt.Sprintf("sso-fresh-%s@example.com", suffix)
		emailProvider.setClaims("fresh-"+suffix, freshEmail)
		resp, err := loginViaIntegrationIdentityProvider(ctx, t, authClient, emailIdentityIDP)
		require.NoError(t, err)
		require.False(t, resp.GetAccountAdopted())
		require.Equal(t, freshEmail, resp.GetUser().GetEmail())
	})

	t.Run("a service account is not adopted", func(t *testing.T) {
		// Adopting one would hand whoever the provider sends an API key for a
		// machine principal.
		setSSOEmailIdentityIDPs(ctx, t, settingClient, adminToken, emailIdentityIDP)

		serviceEmail := fmt.Sprintf("sso-service-%s@example.com", suffix)
		_, err := userClient.CreateUser(ctx, withToken(adminToken, &v1pb.CreateUserRequest{
			User: &v1pb.User{
				Email:    serviceEmail,
				Title:    "Integration service account",
				UserType: v1pb.UserType_SERVICE_ACCOUNT,
			},
		}))
		require.NoError(t, err)

		emailProvider.setClaims("machine-"+suffix, serviceEmail)
		_, err = loginViaIntegrationIdentityProvider(ctx, t, authClient, emailIdentityIDP)
		require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "unexpected error: %v", err)
	})
}
