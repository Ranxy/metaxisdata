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
)

// setSSOEmailIdentity turns the email-identity switch on or off. It is a
// workspace-wide setting on a server the rest of the suite shares, so the caller
// reads it and restores it once for the whole test: a per-subtest cleanup would
// flip it back between subtests.
func setSSOEmailIdentity(ctx context.Context, t *testing.T, client v1connect.SettingServiceClient, adminToken string, enabled bool) {
	t.Helper()

	_, err := client.UpdateWorkspaceProfileSetting(ctx, withToken(adminToken, &v1pb.UpdateWorkspaceProfileSettingRequest{
		Setting:    &v1pb.WorkspaceProfileSetting{AllowSsoEmailIdentity: enabled},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"allow_sso_email_identity"}},
	}))
	require.NoError(t, err)
}

// restoreSSOEmailIdentity asserts the switch starts off, as the default has to
// be, and puts it back the way the test found it when the whole test ends.
func restoreSSOEmailIdentity(ctx context.Context, t *testing.T, client v1connect.SettingServiceClient, adminToken string) {
	t.Helper()

	previous, err := client.GetWorkspaceProfileSetting(ctx, connect.NewRequest(&v1pb.GetWorkspaceProfileSettingRequest{}))
	require.NoError(t, err)
	require.False(t, previous.Msg.GetAllowSsoEmailIdentity(), "the switch must be off by default")
	t.Cleanup(func() {
		_, restoreErr := client.UpdateWorkspaceProfileSetting(context.Background(), withToken(adminToken, &v1pb.UpdateWorkspaceProfileSettingRequest{
			Setting:    &v1pb.WorkspaceProfileSetting{AllowSsoEmailIdentity: previous.Msg.GetAllowSsoEmailIdentity()},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"allow_sso_email_identity"}},
		}))
		require.NoError(t, restoreErr)
	})
}

// TestSSOEmailIdentityAdoptsAnAccountRealServerIntegration pins the opt-in an
// administrator can turn on for deployments whose provider exposes no stable
// subject: the login is identified by the provider's address claim and adopts
// the account that already carries that address — binding it and voiding its
// password, which also retires the sessions minted with it. The switch is off by
// default, and even on it never takes over an account another identity already
// owns.
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

	// Off unless an administrator turns it on; the assertion and the restore live
	// in the helper.
	restoreSSOEmailIdentity(ctx, t, settingClient, adminToken)

	// Two providers: one maps a stable subject (the ordinary configuration), the
	// other maps none at all — which is what the switch exists for.
	subjectProvider := newFakeIdentityProvider(t)
	subjectIDP := fmt.Sprintf("sso-subject-%d", time.Now().UnixNano())
	registerIntegrationIdentityProvider(ctx, t, env, subjectProvider, subjectIDP, &storepb.FieldMapping{
		Identifier:  "email",
		Subject:     "sub",
		DisplayName: "name",
	})

	emailProvider := newFakeIdentityProvider(t)
	emailIdentityIDP := fmt.Sprintf("sso-email-%d", time.Now().UnixNano())
	registerIntegrationIdentityProvider(ctx, t, env, emailProvider, emailIdentityIDP, &storepb.FieldMapping{
		Identifier:  "email",
		DisplayName: "name",
	})

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	const password = "Integration-Pass-1!"

	// An account that already carries the address, with a password its member
	// signs in with: the case the switch is about.
	email := fmt.Sprintf("sso-adopt-%s@example.com", suffix)
	created, err := userClient.CreateUser(ctx, withToken(adminToken, &v1pb.CreateUserRequest{
		User: &v1pb.User{
			Email:    email,
			Title:    "Pre-provisioned member",
			Password: password,
			UserType: v1pb.UserType_END_USER,
		},
	}))
	require.NoError(t, err)
	accountName := created.Msg.GetName()
	oldToken, err := env.LoginAs(ctx, email, password)
	require.NoError(t, err)

	t.Run("the switch is off and the account is not adopted", func(t *testing.T) {
		setSSOEmailIdentity(ctx, t, settingClient, adminToken, false)
		setSSOEmailIdentity(ctx, t, settingClient, adminToken, false)

		// The provider maps a subject, so what refuses the login is the adoption
		// rule — not the subject-claim rule the switch also relaxes. A provider
		// without a subject would fail here for the other reason.
		subjectProvider.setClaims("employee-"+suffix, email)
		_, err := loginViaIntegrationIdentityProvider(ctx, t, authClient, subjectIDP)
		require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "unexpected error: %v", err)

		// Nothing was touched: the member's password still works.
		_, err = env.LoginAs(ctx, email, password)
		require.NoError(t, err)
	})

	t.Run("with the switch on the login adopts the account", func(t *testing.T) {
		setSSOEmailIdentity(ctx, t, settingClient, adminToken, true)

		// The provider reports the address in another case: everything the
		// platform stores and matches on is the lower-cased address.
		emailProvider.setClaims("employee-"+suffix, strings.ToUpper(email))
		resp, err := loginViaIntegrationIdentityProvider(ctx, t, authClient, emailIdentityIDP)
		require.NoError(t, err)
		require.True(t, resp.GetAccountAdopted(), "the client has to be told the password is gone")
		require.Equal(t, accountName, resp.GetUser().GetName(), "the account is adopted, not left behind for a new one")
		require.Equal(t, email, resp.GetUser().GetEmail())

		// The password it had no longer works, and neither does the session
		// minted before the change.
		_, err = env.LoginAs(ctx, email, password)
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
		_, err = userClient.GetCurrentUser(ctx, withToken(oldToken, &emptypb.Empty{}))
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))

		// The binding is the address claim, lower-cased, so the next login
		// resolves by it and an administrative rename keeps the account.
		var resourceID, subject string
		require.NoError(t, env.Store.GetDB().QueryRowContext(ctx,
			`SELECT idp_resource_id, idp_subject FROM principal WHERE email = $1`, email).Scan(&resourceID, &subject))
		require.Equal(t, emailIdentityIDP, resourceID)
		require.Equal(t, email, subject)
	})

	t.Run("an account another identity already owns is not adopted", func(t *testing.T) {
		setSSOEmailIdentity(ctx, t, settingClient, adminToken, true)
		ownedEmail := fmt.Sprintf("sso-owned-%s@example.com", suffix)
		subjectProvider.setClaims("owner-"+suffix, ownedEmail)
		resp, err := loginViaIntegrationIdentityProvider(ctx, t, authClient, subjectIDP)
		require.NoError(t, err)
		require.Equal(t, ownedEmail, resp.GetUser().GetEmail())

		// The address-identity provider reports the same address: it must not
		// be handed the account, even with the switch on.
		emailProvider.setClaims("stranger-"+suffix, ownedEmail)
		_, err = loginViaIntegrationIdentityProvider(ctx, t, authClient, emailIdentityIDP)
		require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "unexpected error: %v", err)
	})

	t.Run("a new account is created, not adopted", func(t *testing.T) {
		setSSOEmailIdentity(ctx, t, settingClient, adminToken, true)
		// The flag distinguishes what happened, so a plain first login must not
		// claim an adoption.
		freshEmail := fmt.Sprintf("sso-fresh-%s@example.com", suffix)
		emailProvider.setClaims("fresh-"+suffix, freshEmail)
		resp, err := loginViaIntegrationIdentityProvider(ctx, t, authClient, emailIdentityIDP)
		require.NoError(t, err)
		require.False(t, resp.GetAccountAdopted())
		require.Equal(t, freshEmail, resp.GetUser().GetEmail())
	})

	t.Run("a service account is not adopted", func(t *testing.T) {
		setSSOEmailIdentity(ctx, t, settingClient, adminToken, true)
		// Adopting one would hand whoever the provider sends an API key for a
		// machine principal.
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
