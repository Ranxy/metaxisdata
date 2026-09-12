package v1

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
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

			err := newACLInterceptorWithChecker(tc.checker).authorize(ctx)
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

// unannotatedMethods are the RPCs that intentionally carry no permission: the
// public auth endpoints and the self-service user paths whose access rule
// cannot be expressed as one permission. Every other method must be gated,
// reads included, so this allowlist is the only way to add an unguarded RPC.
var unannotatedMethods = map[string]bool{
	"metaxisdata.v1.AuthService.Login":          true,
	"metaxisdata.v1.AuthService.Logout":         true,
	"metaxisdata.v1.UserService.GetCurrentUser": true,
	"metaxisdata.v1.UserService.CreateUser":     true,
	"metaxisdata.v1.UserService.UpdateUser":     true,
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
