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
	"google.golang.org/protobuf/types/known/emptypb"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// TestWorkspaceIamPolicyRealServerIntegration exercises the IAM subsystem end to
// end against a real server: a member starts on the read baseline, gains a
// custom role through the workspace policy (directly and through a group), sees
// the change in GetCurrentUser, and the policy write path rejects both a stale
// etag and a policy that would leave the workspace without an admin.
func TestWorkspaceIamPolicyRealServerIntegration(t *testing.T) {
	env := sharedPostgresServiceEnvNoReset(t)
	httpClient := &http.Client{Timeout: 5 * time.Second}
	ctx := context.Background()

	adminToken := env.AdminToken()
	roleClient := v1connect.NewRoleServiceClient(httpClient, env.BaseURL)
	groupClient := v1connect.NewGroupServiceClient(httpClient, env.BaseURL)
	iamClient := v1connect.NewIamServiceClient(httpClient, env.BaseURL)
	userClient := v1connect.NewUserServiceClient(httpClient, env.BaseURL)

	// Leave the shared workspace policy as we found it: other scenarios run
	// against the same server.
	original, err := iamClient.GetWorkspaceIamPolicy(ctx, withToken(adminToken, &v1pb.GetWorkspaceIamPolicyRequest{}))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, restoreErr := iamClient.SetWorkspaceIamPolicy(context.Background(), withToken(adminToken, &v1pb.SetWorkspaceIamPolicyRequest{
			Policy: original.Msg.GetPolicy(),
			Etag:   "",
		}))
		require.NoError(t, restoreErr)
	})

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	roleName := "roles/it" + suffix
	listRolesRole := "roles/lr" + suffix

	_, err = roleClient.CreateRole(ctx, withToken(adminToken, &v1pb.CreateRoleRequest{
		Role: &v1pb.Role{
			Name:        roleName,
			Title:       "Integration reader",
			Permissions: []string{"metaxisdata.roles.list"},
		},
	}))
	require.NoError(t, err)
	_, err = roleClient.CreateRole(ctx, withToken(adminToken, &v1pb.CreateRoleRequest{
		Role: &v1pb.Role{
			Name:        listRolesRole,
			Title:       "Integration user reader",
			Permissions: []string{"metaxisdata.users.list"},
		},
	}))
	require.NoError(t, err)

	email := fmt.Sprintf("iam-%s@example.com", suffix)
	const password = "Integration-Password-1!"
	created, err := userClient.CreateUser(ctx, withToken(adminToken, &v1pb.CreateUserRequest{
		User: &v1pb.User{Email: email, Title: "IAM integration member", Password: password, UserType: v1pb.UserType_END_USER},
	}))
	require.NoError(t, err)
	memberName := created.Msg.GetName()
	require.NotEmpty(t, memberName)

	memberToken, err := env.LoginAs(ctx, email, password)
	require.NoError(t, err)

	// The member baseline covers reads but not role administration.
	_, err = roleClient.ListRoles(ctx, withToken(memberToken, &v1pb.ListRolesRequest{}))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "a member must not list roles")

	policy := original.Msg.GetPolicy()
	bindings := append([]*v1pb.Binding{}, policy.GetBindings()...)
	bindings = append(bindings, &v1pb.Binding{Role: roleName, Members: []string{memberName}})
	setResp, err := iamClient.SetWorkspaceIamPolicy(ctx, withToken(adminToken, &v1pb.SetWorkspaceIamPolicyRequest{
		Policy: &v1pb.IamPolicy{Bindings: bindings},
		Etag:   original.Msg.GetEtag(),
	}))
	require.NoError(t, err)

	_, err = roleClient.ListRoles(ctx, withToken(memberToken, &v1pb.ListRolesRequest{}))
	require.NoError(t, err, "the custom role should grant roles.list")

	me, err := userClient.GetCurrentUser(ctx, withToken(memberToken, &emptypb.Empty{}))
	require.NoError(t, err)
	require.Contains(t, me.Msg.GetPermissions(), "metaxisdata.roles.list")
	require.Contains(t, me.Msg.GetPermissions(), "metaxisdata.instances.list", "the baseline stays")
	require.NotContains(t, me.Msg.GetPermissions(), "metaxisdata.users.delete")

	// A binding through a group grants the same way.
	groupName := fmt.Sprintf("groups/it-%s@example.com", suffix)
	_, err = groupClient.CreateGroup(ctx, withToken(adminToken, &v1pb.CreateGroupRequest{
		Group: &v1pb.Group{
			Name:    groupName,
			Title:   "Integration group",
			Members: []*v1pb.GroupMember{{Member: memberName, Role: v1pb.GroupMember_MEMBER}},
		},
	}))
	require.NoError(t, err)

	bindings = append(bindings, &v1pb.Binding{Role: listRolesRole, Members: []string{groupName}})
	setResp, err = iamClient.SetWorkspaceIamPolicy(ctx, withToken(adminToken, &v1pb.SetWorkspaceIamPolicyRequest{
		Policy: &v1pb.IamPolicy{Bindings: bindings},
		Etag:   setResp.Msg.GetEtag(),
	}))
	require.NoError(t, err)

	_, err = userClient.ListUsers(ctx, withToken(memberToken, &v1pb.ListUsersRequest{PageSize: 1}))
	require.NoError(t, err, "a group member inherits the bound role")

	// A stale etag is rejected so the caller re-reads instead of clobbering.
	_, err = iamClient.SetWorkspaceIamPolicy(ctx, withToken(adminToken, &v1pb.SetWorkspaceIamPolicyRequest{
		Policy: &v1pb.IamPolicy{Bindings: bindings},
		Etag:   original.Msg.GetEtag(),
	}))
	require.Equal(t, connect.CodeAborted, connect.CodeOf(err))

	// A policy without an admin must be refused rather than locking the
	// workspace out.
	_, err = iamClient.SetWorkspaceIamPolicy(ctx, withToken(adminToken, &v1pb.SetWorkspaceIamPolicyRequest{
		Policy: &v1pb.IamPolicy{},
		Etag:   setResp.Msg.GetEtag(),
	}))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// A role that is still bound cannot be deleted.
	_, err = roleClient.DeleteRole(ctx, withToken(adminToken, &v1pb.DeleteRoleRequest{Name: roleName}))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))

	// Deleting the group binding first unlocks both deletes.
	remaining := []*v1pb.Binding{}
	for _, binding := range bindings {
		remaining = append(remaining, &v1pb.Binding{Role: binding.GetRole(), Members: binding.GetMembers()})
	}
	_, err = iamClient.SetWorkspaceIamPolicy(ctx, withToken(adminToken, &v1pb.SetWorkspaceIamPolicyRequest{
		Policy: &v1pb.IamPolicy{Bindings: remaining[:len(remaining)-2]},
		Etag:   setResp.Msg.GetEtag(),
	}))
	require.NoError(t, err)
	_, err = roleClient.DeleteRole(ctx, withToken(adminToken, &v1pb.DeleteRoleRequest{Name: roleName}))
	require.NoError(t, err)
	_, err = roleClient.DeleteRole(ctx, withToken(adminToken, &v1pb.DeleteRoleRequest{Name: listRolesRole}))
	require.NoError(t, err)
	_, err = groupClient.DeleteGroup(ctx, withToken(adminToken, &v1pb.DeleteGroupRequest{Name: groupName}))
	require.NoError(t, err)
}

// withToken builds a request carrying a bearer token. The env package's helper
// of the same shape is unexported, so the runner owns its own.
func withToken[T any](token string, msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set("Authorization", "Bearer "+token)
	return req
}
