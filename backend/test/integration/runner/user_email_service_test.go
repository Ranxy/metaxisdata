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
)

// TestUpdateUserEmailRequiresAdminRealServerIntegration pins the email half of
// H3: an address is how the account is recognized outside the workspace, so
// moving it — an admin moving anyone's, including their own — needs
// metaxisdata.users.update. A member who could repoint their own address could
// otherwise claim one an identity provider hands to someone else.
func TestUpdateUserEmailRequiresAdminRealServerIntegration(t *testing.T) {
	// Not parallel: one subtest edits the shared workspace IAM policy.
	ctx := context.Background()
	env := sharedPostgresServiceEnvNoReset(t)
	httpClient := &http.Client{Timeout: 5 * time.Second}
	adminToken := env.AdminToken()
	userClient := v1connect.NewUserServiceClient(httpClient, env.BaseURL)
	iamClient := v1connect.NewIamServiceClient(httpClient, env.BaseURL)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	const password = "Integration-Pass-1!"
	email := fmt.Sprintf("email-rule-%s@example.com", suffix)
	movedEmail := fmt.Sprintf("email-rule-moved-%s@example.com", suffix)

	created, err := userClient.CreateUser(ctx, withToken(adminToken, &v1pb.CreateUserRequest{
		User: &v1pb.User{
			Email:    email,
			Title:    "Email rule member",
			Password: password,
			UserType: v1pb.UserType_END_USER,
		},
	}))
	require.NoError(t, err)
	userName := created.Msg.GetName()

	memberToken, err := env.LoginAs(ctx, email, password)
	require.NoError(t, err)

	t.Run("a member cannot move their own address", func(t *testing.T) {
		_, err := userClient.UpdateUser(ctx, withToken(memberToken, &v1pb.UpdateUserRequest{
			User:       &v1pb.User{Name: userName, Email: movedEmail},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"email"}},
		}))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "unexpected error: %v", err)

		// Nothing was written, so the member still signs in with the address
		// they already had.
		_, err = env.LoginAs(ctx, email, password)
		require.NoError(t, err)
	})

	t.Run("a member still updates their own profile", func(t *testing.T) {
		_, err := userClient.UpdateUser(ctx, withToken(memberToken, &v1pb.UpdateUserRequest{
			User:       &v1pb.User{Name: userName, Title: "Renamed member"},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title"}},
		}))
		require.NoError(t, err)
	})

	t.Run("an admin moves another member's address", func(t *testing.T) {
		_, err := userClient.UpdateUser(ctx, withToken(adminToken, &v1pb.UpdateUserRequest{
			User:       &v1pb.User{Name: userName, Email: movedEmail},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"email"}},
		}))
		require.NoError(t, err)

		_, err = env.LoginAs(ctx, movedEmail, password)
		require.NoError(t, err, "the member signs in with the new address")
		_, err = env.LoginAs(ctx, email, password)
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err), "the old address is gone")
	})

	t.Run("an admin moves their own address", func(t *testing.T) {
		original, err := iamClient.GetWorkspaceIamPolicy(ctx, withToken(adminToken, &v1pb.GetWorkspaceIamPolicyRequest{}))
		require.NoError(t, err)
		t.Cleanup(func() {
			_, restoreErr := iamClient.SetWorkspaceIamPolicy(context.Background(), withToken(adminToken, &v1pb.SetWorkspaceIamPolicyRequest{
				Policy: original.Msg.GetPolicy(),
			}))
			require.NoError(t, restoreErr)
		})

		bindings := append([]*v1pb.Binding{}, original.Msg.GetPolicy().GetBindings()...)
		bindings = append(bindings, &v1pb.Binding{Role: "roles/workspaceAdmin", Members: []string{userName}})
		_, err = iamClient.SetWorkspaceIamPolicy(ctx, withToken(adminToken, &v1pb.SetWorkspaceIamPolicyRequest{
			Policy: &v1pb.IamPolicy{Bindings: bindings},
			Etag:   original.Msg.GetEtag(),
		}))
		require.NoError(t, err)

		ownEmail := fmt.Sprintf("email-rule-admin-%s@example.com", suffix)
		_, err = userClient.UpdateUser(ctx, withToken(memberToken, &v1pb.UpdateUserRequest{
			User:       &v1pb.User{Name: userName, Email: ownEmail},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"email"}},
		}))
		require.NoError(t, err, "a workspace admin may move their own address")

		_, err = env.LoginAs(ctx, ownEmail, password)
		require.NoError(t, err)
	})
}
