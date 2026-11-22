package v1

import (
	"context"
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/common/resource"
	"github.com/Ranxy/metaxisdata/backend/component/iam"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type fakeChecker struct {
	granted map[permission.Permission]bool
	err     error
}

func (f *fakeChecker) CheckPermission(_ context.Context, perm permission.Permission, _ *store.UserMessage) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.granted[perm], nil
}

func TestACLInterceptorAuthorize(t *testing.T) {
	t.Parallel()

	user := &store.UserMessage{ID: 101, Email: "a@example.com"}
	tests := []struct {
		name     string
		authCtx  *common.AuthContext
		hasUser  bool
		checker  PermissionChecker
		wantCode connect.Code
	}{
		{
			name:    "unannotated method is not gated",
			authCtx: &common.AuthContext{},
			hasUser: true,
			checker: &fakeChecker{},
		},
		{
			name:    "public method stays reachable without a credential",
			authCtx: &common.AuthContext{AllowWithoutCredential: true, Permission: permission.SettingsGet},
			checker: &fakeChecker{},
		},
		{
			name:    "granted permission passes",
			authCtx: &common.AuthContext{Permission: permission.UsersDelete},
			hasUser: true,
			checker: &fakeChecker{granted: map[permission.Permission]bool{permission.UsersDelete: true}},
		},
		{
			name:     "denied permission is PermissionDenied",
			authCtx:  &common.AuthContext{Permission: permission.UsersDelete},
			hasUser:  true,
			checker:  &fakeChecker{},
			wantCode: connect.CodePermissionDenied,
		},
		{
			name:     "annotated method without a caller is Unauthenticated",
			authCtx:  &common.AuthContext{Permission: permission.UsersDelete},
			checker:  &fakeChecker{granted: map[permission.Permission]bool{permission.UsersDelete: true}},
			wantCode: connect.CodeUnauthenticated,
		},
		{
			name:     "a checker failure is Internal",
			authCtx:  &common.AuthContext{Permission: permission.UsersDelete},
			hasUser:  true,
			checker:  &fakeChecker{err: errors.New("db down")},
			wantCode: connect.CodeInternal,
		},
		{
			name:     "a missing auth context is Internal",
			checker:  &fakeChecker{},
			wantCode: connect.CodeInternal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			if tc.authCtx != nil {
				ctx = context.WithValue(ctx, common.AuthContextKey, tc.authCtx)
			}
			if tc.hasUser {
				ctx = context.WithValue(ctx, common.UserContextKey, user)
			}

			err := newACLInterceptorWithChecker(tc.checker).authorize(ctx, "")
			if tc.wantCode == 0 {
				require.NoError(t, err)
				return
			}
			var connectErr *connect.Error
			require.ErrorAs(t, err, &connectErr)
			require.Equal(t, tc.wantCode, connectErr.Code())
		})
	}
}

type fakeScopedAuthorizer struct {
	scopes resource.ScopeSet
	err    error
	calls  int
}

func (f *fakeScopedAuthorizer) Check(context.Context, *store.UserMessage, permission.Permission, resource.Ref) (bool, error) {
	f.calls++
	return !f.scopes.IsEmpty(), f.err
}

func (f *fakeScopedAuthorizer) Scopes(context.Context, *store.UserMessage, permission.Permission) (resource.ScopeSet, error) {
	f.calls++
	return f.scopes, f.err
}

var _ iam.Authorizer = (*fakeScopedAuthorizer)(nil)

// A scoped method asks the resource authorizer for the caller's scopes: an empty
// set is a denial, a non-empty set passes the coarse gate, and the handler then
// checks the specific target.
func TestACLInterceptorScopedMethod(t *testing.T) {
	t.Parallel()

	const procedure = "/metaxisdata.v1.DataQualityService/CreateQualityRule"
	user := &store.UserMessage{ID: 101}
	ctx := context.WithValue(context.Background(), common.AuthContextKey,
		&common.AuthContext{Permission: permission.DataQualityRulesCreate})
	ctx = context.WithValue(ctx, common.UserContextKey, user)

	scoped := map[string]bool{procedure: true}

	t.Run("an empty scope set is denied", func(t *testing.T) {
		t.Parallel()
		authorizer := &fakeScopedAuthorizer{}
		err := newACLInterceptorWithScoped(&fakeChecker{}, authorizer, scoped).authorize(ctx, procedure)
		require.Error(t, err)
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	})

	t.Run("a scoped grant passes even when the workspace denies", func(t *testing.T) {
		t.Parallel()
		authorizer := &fakeScopedAuthorizer{scopes: resource.ScopeSet{
			Scopes: []resource.Scope{resource.NewScope(resource.TypeDatabase, "i;db")},
		}}
		err := newACLInterceptorWithScoped(&fakeChecker{}, authorizer, scoped).authorize(ctx, procedure)
		require.NoError(t, err)
		require.Equal(t, 1, authorizer.calls)
	})

	t.Run("a missing authorizer is internal", func(t *testing.T) {
		t.Parallel()
		err := newACLInterceptorWithScoped(&fakeChecker{}, nil, scoped).authorize(ctx, procedure)
		require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	})

	t.Run("an authorizer error is internal", func(t *testing.T) {
		t.Parallel()
		authorizer := &fakeScopedAuthorizer{err: errors.New("grant read failed")}
		err := newACLInterceptorWithScoped(&fakeChecker{}, authorizer, scoped).authorize(ctx, procedure)
		require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	})

	t.Run("a method outside the scoped set keeps the workspace check", func(t *testing.T) {
		t.Parallel()
		authorizer := &fakeScopedAuthorizer{scopes: resource.ScopeSet{All: true}}
		err := newACLInterceptorWithScoped(&fakeChecker{}, authorizer, scoped).authorize(ctx, "/other")
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "the workspace checker denied")
		require.Zero(t, authorizer.calls)
	})
}

// unannotatedMethods are the RPCs that intentionally carry no permission: the
// public auth endpoints and the self-service user paths whose access rule
// cannot be expressed as one permission. Every other method must be gated,
// reads included, so this allowlist is the only way to add an unguarded RPC.
var unannotatedMethods = map[string]bool{
	// The self-service permission check names no workspace action: the handler
	// authenticates and answers only about the caller's own permissions.
	"metaxisdata.v1.IamService.CheckPermissions": true,
	"metaxisdata.v1.AuthService.Login":           true,
	"metaxisdata.v1.AuthService.Logout":          true,
	"metaxisdata.v1.AuthService.Refresh":         true,
	// The CLI's refresh path is anonymous like Refresh: the opaque refresh token in
	// the body is the credential, and the handler scopes everything to the session
	// that token names. A permission cannot express that.
	"metaxisdata.v1.AuthService.ExchangeRefreshToken": true,
	"metaxisdata.v1.AuthService.CreateSSOState":       true,
	"metaxisdata.v1.AuthService.CreateDeviceLogin":    true,
	"metaxisdata.v1.AuthService.GetDeviceLogin":       true,
	"metaxisdata.v1.AuthService.ApproveDeviceLogin":   true,
	"metaxisdata.v1.AuthService.ExchangeDeviceLogin":  true,
	// The consent page is a self-service path: a signed-in user decides whether
	// to grant a client access to their own identity, so no permission expresses
	// it. The protocol endpoints (/oauth/*) are plain HTTP and never reach this
	// interceptor at all.
	"metaxisdata.v1.OAuthService.GetOAuthAuthorizationRequest":     true,
	"metaxisdata.v1.OAuthService.ApproveOAuthAuthorizationRequest": true,
	"metaxisdata.v1.UserService.GetCurrentUser":                    true,
	"metaxisdata.v1.UserService.CreateUser":                        true,
	"metaxisdata.v1.UserService.UpdateUser":                        true,
	// An inbox belongs to one principal. The workspace-scoped catalog cannot say
	// "only mine", so these methods scope every query by the authenticated
	// caller's id instead of naming a permission: gating them would lock a custom
	// role out of its own messages.
	"metaxisdata.v1.NotificationService.ListNotifications":          true,
	"metaxisdata.v1.NotificationService.GetUnreadNotificationCount": true,
	"metaxisdata.v1.NotificationService.MarkNotificationsSeen":      true,
	"metaxisdata.v1.NotificationService.BatchMarkNotificationsRead": true,
	"metaxisdata.v1.NotificationService.MarkAllNotificationsRead":   true,
	"metaxisdata.v1.NotificationService.DeleteNotification":         true,
	// The live stream subscribes to that same inbox, under the same rule: the
	// subscription is registered for the authenticated caller's own id, and no
	// permission in the catalog says "only mine".
	"metaxisdata.v1.NotificationService.SubscribeNotifications": true,
	// Connected apps have two audiences on the same call: an administrator sees
	// the whole workspace and an ordinary user sees their own. The handler narrows
	// first and only widens when connectedApps.list / connectedApps.revoke resolves,
	// so no single annotation can express the rule. The scoping matrix is pinned by
	// connected_app_test.go, which is what keeps the widen from silently
	// becoming the default.
	"metaxisdata.v1.ConnectedAppService.ListConnectedApps":  true,
	"metaxisdata.v1.ConnectedAppService.RevokeConnectedApp": true,
}

// TestEveryMethodIsPermissionGated is the read-path guard the authorization
// review called for: an RPC without a permission annotation is reachable by any
// authenticated caller, so the only acceptable ones are on the allowlist above.
// It also fails on annotation drift (a permission string that is not in the
// catalog) and on stale allowlist entries.
func TestEveryMethodIsPermissionGated(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if fd.Package() != "metaxisdata.v1" {
			return true
		}
		for i := range fd.Services().Len() {
			service := fd.Services().Get(i)
			for j := range service.Methods().Len() {
				method := service.Methods().Get(j)
				fullName := string(method.FullName())
				seen[fullName] = true

				declared := methodPermission(method)
				if declared == "" {
					if !unannotatedMethods[fullName] {
						t.Errorf("method %s has no permission annotation; gate it or add it to unannotatedMethods", fullName)
					}
					continue
				}
				if !permission.Exist(declared) {
					t.Errorf("method %s declares permission %q which is not in the catalog", fullName, declared)
				}
				if unannotatedMethods[fullName] {
					t.Errorf("method %s is allowlisted as unannotated but declares %q", fullName, declared)
				}
			}
		}
		return true
	})
	require.NotEmpty(t, seen)

	for fullName := range unannotatedMethods {
		if !seen[fullName] {
			t.Errorf("unannotatedMethods lists %s, which no longer exists", fullName)
		}
	}
}

// TestPublicMethodsThatAlsoDeclareAPermission pins the one annotation combination
// the interceptor cannot honor. allow_without_credential short-circuits before the
// permission check, so a permission beside it is catalog documentation at best and
// a false assurance at worst: a reader sees a gated method that is actually
// anonymous. Two reads carry both on purpose (their proto comments say so); they
// are listed here so a third cannot join them by accident.
func TestPublicMethodsThatAlsoDeclareAPermission(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"metaxisdata.v1.SettingService.GetWorkspaceProfileSetting":     permission.SettingsGet,
		"metaxisdata.v1.IdentityProviderService.ListIdentityProviders": permission.IdentityProvidersList,
	}
	got := map[string]string{}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if fd.Package() != "metaxisdata.v1" {
			return true
		}
		for i := range fd.Services().Len() {
			service := fd.Services().Get(i)
			for j := range service.Methods().Len() {
				method := service.Methods().Get(j)
				if !methodAllowsWithoutCredential(method) {
					continue
				}
				if declared := methodPermission(method); declared != "" {
					got[string(method.FullName())] = declared
				}
			}
		}
		return true
	})
	require.Equal(t, want, got,
		"a public method must not also claim a permission unless this table says so deliberately")
}

// methodAllowsWithoutCredential reads the allow_without_credential method option.
func methodAllowsWithoutCredential(method protoreflect.MethodDescriptor) bool {
	options, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok || options == nil {
		return false
	}
	allow, ok := proto.GetExtension(options, v1pb.E_AllowWithoutCredential).(bool)
	return ok && allow
}

// TestReadPathsAreGated pins the specific concern from the review: the read
// RPCs that used to be "any authenticated caller" now carry permissions.
func TestReadPathsAreGated(t *testing.T) {
	t.Parallel()

	gated := map[string]string{
		"metaxisdata.v1.UserService.GetUser":                     permission.UsersGet,
		"metaxisdata.v1.UserService.ListUsers":                   permission.UsersList,
		"metaxisdata.v1.UserService.BatchGetUsers":               permission.UsersGet,
		"metaxisdata.v1.InstanceService.GetInstance":             permission.InstancesGet,
		"metaxisdata.v1.InstanceService.ListInstances":           permission.InstancesList,
		"metaxisdata.v1.EnvironmentService.ListEnvironments":     permission.EnvironmentsList,
		"metaxisdata.v1.DatabaseService.ListDatabases":           permission.DatabasesList,
		"metaxisdata.v1.DatabaseService.GetMetadata":             permission.DatabasesRead,
		"metaxisdata.v1.DatabaseService.ListMetadata":            permission.DatabasesRead,
		"metaxisdata.v1.LineageService.GetLineage":               permission.LineageGet,
		"metaxisdata.v1.OpenLineageService.ListOpenLineageTasks": permission.OpenLineageRead,
		"metaxisdata.v1.AuditLogService.ListAuditLogs":           permission.AuditLogsSearch,
	}
	for fullName, want := range gated {
		got := permissionForMethod(t, fullName)
		require.Equalf(t, want, got, "method %s", fullName)
	}
}

func permissionForMethod(t *testing.T, fullName string) string {
	t.Helper()
	desc, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(fullName))
	require.NoError(t, err)
	method, ok := desc.(protoreflect.MethodDescriptor)
	require.True(t, ok)
	return methodPermission(method)
}

func methodPermission(method protoreflect.MethodDescriptor) string {
	options, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok || options == nil {
		return ""
	}
	declared, ok := proto.GetExtension(options, v1pb.E_Permission).(string)
	if !ok {
		return ""
	}
	return declared
}

// TestScopedMethodsDeclareScopeablePermissions keeps the scoped-method gate
// and the catalog in step: the gate computes a scope set for the annotated
// permission, so a permission a scope cannot satisfy would make the gate deny
// every caller (or, if it were non-scopeable, never narrow).
func TestScopedMethodsDeclareScopeablePermissions(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, scopedMethods)
	for procedure := range scopedMethods {
		fullName := strings.Replace(strings.TrimPrefix(procedure, "/"), "/", ".", 1)
		declared := permissionForMethod(t, fullName)
		require.NotEmptyf(t, declared, "scoped method %s declares no permission", procedure)
		require.Truef(t, permission.Scopeable(declared),
			"scoped method %s declares permission %q, which a scope cannot satisfy", procedure, declared)
	}
	for perm := range targetScopedPermissions {
		require.Truef(t, permission.Scopeable(perm),
			"target-scoped permission %q is not scopeable", perm)
	}
}

// targetScopedPermissions are the permissions whose RPCs must be checked per
// target. A new method carrying one of them without a scopedMethods entry would
// silently fall back to the workspace check.
// workspaceScopedWithTargetPermission are the RPCs that carry a target-scoped
// permission only because it is the nearest analogue; the method itself is a
// workspace-level report and has no target to check.
var workspaceScopedWithTargetPermission = map[string]bool{
	"/metaxisdata.v1.DataQualityService/GetQualityLLMUsage": true,
}

var targetScopedPermissions = map[string]bool{
	"metaxisdata.dataQuality.rules.create":      true,
	"metaxisdata.dataQuality.rules.update":      true,
	"metaxisdata.dataQuality.rules.delete":      true,
	"metaxisdata.dataQuality.rules.run":         true,
	"metaxisdata.dataQuality.issues.update":     true,
	"metaxisdata.dataQuality.contracts.publish": true,
	"metaxisdata.dataQuality.suggest":           true,
	"metaxisdata.dataQuality.profile.run":       true,
	"metaxisdata.dataQuality.labels.update":     true,
}

// TestEveryTargetScopedMethodIsInScopedMethods guards the hand-maintained map:
// every DataQualityService method annotated with a target-scoped permission has
// to be in scopedMethods (and therefore have its handler check the target).
func TestEveryTargetScopedMethodIsInScopedMethods(t *testing.T) {
	t.Parallel()

	seen := 0
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if fd.Package() != "metaxisdata.v1" {
			return true
		}
		for i := range fd.Services().Len() {
			service := fd.Services().Get(i)
			if string(service.FullName()) != "metaxisdata.v1.DataQualityService" {
				continue
			}
			for j := range service.Methods().Len() {
				method := service.Methods().Get(j)
				declared := methodPermission(method)
				if !targetScopedPermissions[declared] {
					continue
				}
				procedure := "/" + string(service.FullName()) + "/" + string(method.Name())
				if workspaceScopedWithTargetPermission[procedure] {
					continue
				}
				seen++
				require.Truef(t, scopedMethods[procedure],
					"method %s declares target-scoped permission %q but is not in scopedMethods", procedure, declared)
			}
		}
		return true
	})
	require.NotZero(t, seen)
}
