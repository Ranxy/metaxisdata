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
	exprpb "google.golang.org/genproto/googleapis/type/expr"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// Binding conditions that can never, and always, hold at check time.
const (
	neverHolds  = `request.time > timestamp("2100-01-01T00:00:00Z")`
	alwaysHolds = `request.time > timestamp("2000-01-01T00:00:00Z")`
)

// TestWorkspaceIamPolicyRealServerIntegration exercises the IAM subsystem end to
// end against a real server: a member starts on the read baseline, gains a
// custom role through the workspace policy (directly and through a group), sees
// the change in GetCurrentUser, and the policy write path rejects a stale etag,
// a policy that would leave the workspace without a usable admin (no admin at
// all, one that is false at check time, or one that is merely time-boxed), and
// any attempt to move or drop the server-managed allUsers binding.
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
	require.NotNil(t, allUsersBaselineBinding(original.Msg.GetPolicy()),
		"first onboarding binds allUsers to the member baseline, and every full replace must carry it forward")

	// The caller is the seeded administrator, whose binding is what keeps the
	// workspace administrable below.
	admin, err := userClient.GetCurrentUser(ctx, withToken(adminToken, &emptypb.Empty{}))
	require.NoError(t, err)
	meName := admin.Msg.GetName()
	require.NotEmpty(t, meName)

	// One extra admin binding that is false at check time grants nothing, but
	// the policy still carries a real admin binding: the write is accepted
	// rather than refused for the wrong reason.
	withFalseExtra := append([]*v1pb.Binding{}, original.Msg.GetPolicy().GetBindings()...)
	withFalseExtra = append(withFalseExtra, &v1pb.Binding{
		Role:      "roles/workspaceAdmin",
		Members:   []string{meName},
		Condition: &exprpb.Expr{Expression: neverHolds},
	})
	_, err = iamClient.SetWorkspaceIamPolicy(ctx, withToken(adminToken, &v1pb.SetWorkspaceIamPolicyRequest{
		Policy: &v1pb.IamPolicy{Bindings: withFalseExtra},
		Etag:   original.Msg.GetEtag(),
	}))
	require.NoError(t, err, "a false extra binding does not invalidate the real admin binding")
	_, err = iamClient.SetWorkspaceIamPolicy(ctx, withToken(adminToken, &v1pb.SetWorkspaceIamPolicyRequest{
		Policy: original.Msg.GetPolicy(),
		Etag:   "",
	}))
	require.NoError(t, err)
	// The control above moved the etag; the run below starts from a fresh read.
	current, err := iamClient.GetWorkspaceIamPolicy(ctx, withToken(adminToken, &v1pb.GetWorkspaceIamPolicyRequest{}))
	require.NoError(t, err)
	require.True(t, proto.Equal(original.Msg.GetPolicy(), current.Msg.GetPolicy()), "the control writes restored the policy")

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
		Etag:   current.Msg.GetEtag(),
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

	// M9: allUsers matches everyone who ever signs up, so it may not be moved
	// onto another role — neither a predefined one nor a custom one that the
	// operator just created.
	for _, boundRole := range []string{"roles/workspaceAdmin", roleName} {
		forEveryone := append([]*v1pb.Binding{}, bindings...)
		forEveryone = append(forEveryone, &v1pb.Binding{Role: boundRole, Members: []string{"allUsers"}})
		_, err = iamClient.SetWorkspaceIamPolicy(ctx, withToken(adminToken, &v1pb.SetWorkspaceIamPolicyRequest{
			Policy: &v1pb.IamPolicy{Bindings: forEveryone},
			Etag:   setResp.Msg.GetEtag(),
		}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "allUsers onto %s must be refused", boundRole)
		require.Contains(t, err.Error(), "may only be bound to roles/workspaceMember")
	}

	// Dropping the server-managed binding is refused rather than stored.
	withoutSystem := make([]*v1pb.Binding, 0, len(bindings))
	for _, binding := range bindings {
		if binding.GetRole() == "roles/workspaceMember" {
			continue
		}
		withoutSystem = append(withoutSystem, &v1pb.Binding{Role: binding.GetRole(), Members: binding.GetMembers()})
	}
	_, err = iamClient.SetWorkspaceIamPolicy(ctx, withToken(adminToken, &v1pb.SetWorkspaceIamPolicyRequest{
		Policy: &v1pb.IamPolicy{Bindings: withoutSystem},
		Etag:   setResp.Msg.GetEtag(),
	}))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	require.Contains(t, err.Error(), "must keep its roles/workspaceMember binding")

	// The last-admin guard reads the conditions the permission check reads: a
	// binding that grants nothing at check time cannot be what keeps the
	// workspace administrable, and a merely time-boxed admin is not enough
	// either. Without this, one legal Set leaves the workspace with no usable
	// admin and no way back through the API.
	baseline := allUsersBaselineBinding(&v1pb.IamPolicy{Bindings: bindings})
	adminOnly := func(condition string) []*v1pb.Binding {
		binding := &v1pb.Binding{Role: "roles/workspaceAdmin", Members: []string{meName}}
		if condition != "" {
			binding.Condition = &exprpb.Expr{Expression: condition}
		}
		return []*v1pb.Binding{
			{Role: baseline.GetRole(), Members: baseline.GetMembers()},
			binding,
		}
	}
	for _, test := range []struct {
		reason  string
		binding []*v1pb.Binding
	}{
		{"no admin at all", []*v1pb.Binding{{Role: baseline.GetRole(), Members: baseline.GetMembers()}}},
		{"an admin binding that is false at check time", adminOnly(neverHolds)},
		{"an admin binding that is only time-boxed", adminOnly(alwaysHolds)},
	} {
		_, err = iamClient.SetWorkspaceIamPolicy(ctx, withToken(adminToken, &v1pb.SetWorkspaceIamPolicyRequest{
			Policy: &v1pb.IamPolicy{Bindings: test.binding},
			Etag:   setResp.Msg.GetEtag(),
		}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), test.reason)
		require.Contains(t, err.Error(), "whose binding has no condition", test.reason)
	}

	// Every refused write above left the stored policy exactly as it was. The
	// etag alone would not prove it: it is millisecond-precision, so two writes
	// in the same millisecond share it.
	afterRefusals, err := iamClient.GetWorkspaceIamPolicy(ctx, withToken(adminToken, &v1pb.GetWorkspaceIamPolicyRequest{}))
	require.NoError(t, err)
	require.True(t, proto.Equal(setResp.Msg.GetPolicy(), afterRefusals.Msg.GetPolicy()), "the refused writes left the stored policy untouched")

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

// allUsersBaselineBinding returns the server-managed allUsers binding, or nil
// when the policy does not carry it.
func allUsersBaselineBinding(policy *v1pb.IamPolicy) *v1pb.Binding {
	for _, binding := range policy.GetBindings() {
		if binding.GetRole() != "roles/workspaceMember" {
			continue
		}
		for _, member := range binding.GetMembers() {
			if member == "allUsers" {
				return binding
			}
		}
	}
	return nil
}
