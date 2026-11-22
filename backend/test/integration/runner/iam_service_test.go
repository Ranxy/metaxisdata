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

	"github.com/Ranxy/metaxisdata/backend/common"
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
	listGroupsRole := "roles/lg" + suffix

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
			Name:        listGroupsRole,
			Title:       "Integration group reader",
			Permissions: []string{"metaxisdata.groups.list"},
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

	// users.list is baseline, so the owner picker and owner filter work for a
	// non-admin; roles.list still is not.
	_, err = userClient.ListUsers(ctx, withToken(memberToken, &v1pb.ListUsersRequest{PageSize: 1}))
	require.NoError(t, err, "the member baseline grants the workspace member directory read")

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

	// Until the group binding exists, the member does not hold groups.list: the
	// group is only the vehicle for the role, not the grant itself.
	_, err = groupClient.ListGroups(ctx, withToken(memberToken, &v1pb.ListGroupsRequest{PageSize: 1}))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	bindings = append(bindings, &v1pb.Binding{Role: listGroupsRole, Members: []string{groupName}})
	setResp, err = iamClient.SetWorkspaceIamPolicy(ctx, withToken(adminToken, &v1pb.SetWorkspaceIamPolicyRequest{
		Policy: &v1pb.IamPolicy{Bindings: bindings},
		Etag:   setResp.Msg.GetEtag(),
	}))
	require.NoError(t, err)

	_, err = groupClient.ListGroups(ctx, withToken(memberToken, &v1pb.ListGroupsRequest{PageSize: 1}))
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
	_, err = roleClient.DeleteRole(ctx, withToken(adminToken, &v1pb.DeleteRoleRequest{Name: listGroupsRole}))
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

// TestIamGrantApiRealServerIntegration covers the scoped-grant management API:
// an admin creates a grant on one database, lists it, gets a conflict on a
// duplicate, and deletes it. A member cannot manage grants, allUsers is refused
// as a scoped grant, and a scope that names no live database is refused.
func TestIamGrantApiRealServerIntegration(t *testing.T) {
	t.Parallel()

	fixture := setupQualityMySQLFixture(t, true)
	httpClient := &http.Client{Timeout: 5 * time.Second}
	iamClient := v1connect.NewIamServiceClient(httpClient, fixture.env.BaseURL)
	adminToken := fixture.env.AdminToken()
	ownerName, ownerToken := createQualityAgentTestMember(t, fixture, "grant-api-owner")
	resource := common.FormatDatabase(fixture.instanceID, fixture.sourceDatabase)

	created, err := iamClient.CreateIamGrant(fixture.ctx, withToken(adminToken, &v1pb.CreateIamGrantRequest{
		Grant: &v1pb.IamGrant{Resource: resource, Role: "roles/qualityOwner", Member: ownerName},
	}))
	require.NoError(t, err)
	require.NotEmpty(t, created.Msg.GetName())
	require.Equal(t, resource, created.Msg.GetResource())

	listed, err := iamClient.ListIamGrants(fixture.ctx, withToken(adminToken, &v1pb.ListIamGrantsRequest{Resource: resource}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetGrants(), 1)
	require.Equal(t, created.Msg.GetName(), listed.Msg.GetGrants()[0].GetName())

	_, err = iamClient.CreateIamGrant(fixture.ctx, withToken(adminToken, &v1pb.CreateIamGrantRequest{
		Grant: &v1pb.IamGrant{Resource: resource, Role: "roles/qualityOwner", Member: ownerName},
	}))
	require.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(err))

	_, err = iamClient.CreateIamGrant(fixture.ctx, withToken(adminToken, &v1pb.CreateIamGrantRequest{
		Grant: &v1pb.IamGrant{Resource: resource, Role: "roles/qualityOwner", Member: common.AllUsers},
	}))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	_, err = iamClient.CreateIamGrant(fixture.ctx, withToken(adminToken, &v1pb.CreateIamGrantRequest{
		Grant: &v1pb.IamGrant{
			Resource: "instances/does-not-exist/databases/x",
			Role:     "roles/qualityOwner",
			Member:   ownerName,
		},
	}))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	_, err = iamClient.ListIamGrants(fixture.ctx, withToken(ownerToken, &v1pb.ListIamGrantsRequest{}))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	_, err = iamClient.CreateIamGrant(fixture.ctx, withToken(ownerToken, &v1pb.CreateIamGrantRequest{
		Grant: &v1pb.IamGrant{Resource: resource, Role: "roles/qualityOwner", Member: ownerName},
	}))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	_, err = iamClient.DeleteIamGrant(fixture.ctx, withToken(adminToken, &v1pb.DeleteIamGrantRequest{Name: created.Msg.GetName()}))
	require.NoError(t, err)

	listed, err = iamClient.ListIamGrants(fixture.ctx, withToken(adminToken, &v1pb.ListIamGrantsRequest{Resource: resource}))
	require.NoError(t, err)
	require.Empty(t, listed.Msg.GetGrants())
}

// TestCheckPermissionsRealServerIntegration covers the self-service check: the
// caller's own permissions on one resource, unannotated so a scoped member can
// ask without holding an admin permission.
func TestCheckPermissionsRealServerIntegration(t *testing.T) {
	t.Parallel()

	fixture := setupQualityMySQLFixture(t, true)
	adminToken := fixture.env.AdminToken()
	ownerName, ownerToken := createQualityAgentTestMember(t, fixture, "check-owner")
	_, strangerToken := createQualityAgentTestMember(t, fixture, "check-stranger")
	httpClient := &http.Client{Timeout: 5 * time.Second}
	iamClient := v1connect.NewIamServiceClient(httpClient, fixture.env.BaseURL)

	check := func(token, targetGUID string) []bool {
		t.Helper()
		response, err := iamClient.CheckPermissions(fixture.ctx, withToken(token, &v1pb.CheckPermissionsRequest{
			ResourceType: "table",
			ResourceId:   targetGUID,
			Permissions: []string{
				"metaxisdata.dataQuality.rules.get",
				"metaxisdata.dataQuality.rules.create",
				"metaxisdata.dataQuality.contracts.publish",
			},
		}))
		require.NoError(t, err)
		require.Len(t, response.Msg.GetAllowed(), 3)
		return response.Msg.GetAllowed()
	}

	require.Equal(t, []bool{true, true, true}, check(adminToken, fixture.usersGUID),
		"an administrator holds every target permission")
	require.Equal(t, []bool{true, false, false}, check(ownerToken, fixture.usersGUID),
		"a member holds the read baseline only")
	require.Equal(t, []bool{true, false, false}, check(strangerToken, fixture.usersGUID))

	resource := common.FormatDatabase(fixture.instanceID, fixture.sourceDatabase)
	_, err := iamClient.CreateIamGrant(fixture.ctx, withToken(adminToken, &v1pb.CreateIamGrantRequest{
		Grant: &v1pb.IamGrant{Resource: resource, Role: "roles/qualityOwner", Member: ownerName},
	}))
	require.NoError(t, err)

	require.Equal(t, []bool{true, true, true}, check(ownerToken, fixture.usersGUID),
		"the scoped grant adds create and publish on the granted database")
	// The database scope accepts the resource-name form of resource_id and maps it
	// to the same GUID the grant stores.
	byName, err := iamClient.CheckPermissions(fixture.ctx, withToken(ownerToken, &v1pb.CheckPermissionsRequest{
		ResourceType: "database",
		ResourceId:   resource,
		Permissions:  []string{"metaxisdata.dataQuality.rules.create"},
	}))
	require.NoError(t, err)
	require.Equal(t, []bool{true}, byName.Msg.GetAllowed())

	otherTarget := "another-instance;another_db;;users"
	require.Equal(t, []bool{true, false, false}, check(ownerToken, otherTarget),
		"the scoped grant does not reach another database")

	_, err = iamClient.CheckPermissions(fixture.ctx, withToken(adminToken, &v1pb.CheckPermissionsRequest{
		ResourceType: "not-a-type",
		ResourceId:   "x",
		Permissions:  []string{"metaxisdata.dataQuality.rules.get"},
	}))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	_, err = iamClient.CheckPermissions(fixture.ctx, withToken(adminToken, &v1pb.CheckPermissionsRequest{
		ResourceType: "table",
		ResourceId:   fixture.usersGUID,
		Permissions:  []string{"metaxisdata.notARealPermission"},
	}))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}
