package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// readPermissions are the permissions this surface may require. A tool needs to
// appear here deliberately, so a tool that writes — or that reaches a corner of
// the API nobody meant to expose — cannot slip in behind a plausible name.
var readPermissions = map[string]bool{
	permission.InstancesList: true,
	permission.DatabasesList: true,
	permission.DatabasesRead: true,
	permission.LineageGet:    true,
}

// methodsWithoutAPermission are the RPCs a tool may wrap without requiring a
// permission: whoami answers from the verified principal itself, and
// GetCurrentUser carries no annotation in the API either. It is the same kind of
// allowlist as backend/api/v1/acl_interceptor_test.go's, with one entry.
var methodsWithoutAPermission = map[string]bool{
	"metaxisdata.v1.UserService.GetCurrentUser": true,
}

// TestEveryToolMatchesItsRPC is the guard this surface needs: a tool is a second
// way into the same services, so its permission has to be the one the RPC
// declares — not a copy that can drift, and never a weaker one — and the method it
// wraps has to be a read.
func TestEveryToolMatchesItsRPC(t *testing.T) {
	t.Parallel()

	// The table is built per server because each tool closes over the configured
	// readers; an empty configuration is enough to read what the tools declare.
	server := NewServer(Config{})
	seen := map[string]bool{}
	for _, definition := range server.toolDefinitions() {
		require.NotEmpty(t, definition.Name)
		require.NotEmpty(t, definition.Description, "tool %s must describe what it answers", definition.Name)
		require.NotEmpty(t, definition.RPC, "tool %s must name the RPC it wraps", definition.Name)
		require.NotEmpty(t, definition.Schema, "tool %s must declare an input schema", definition.Name)
		require.NotNil(t, definition.Run, "tool %s must do something", definition.Name)

		method := lookupMethod(t, definition.RPC)
		declared := methodPermission(method)
		seen[definition.RPC] = true

		if methodsWithoutAPermission[definition.RPC] {
			require.Empty(t, declared, "%s is allowlisted as annotation-free but declares one", definition.RPC)
			require.Empty(t, definition.Permission,
				"tool %s wraps an annotation-free method and must not require a permission", definition.Name)
			continue
		}
		require.NotEmpty(t, declared, "%s carries no permission annotation, so it must not be exposed as a tool", definition.RPC)
		require.True(t, permission.Exist(declared), "%s declares unknown permission %q", definition.RPC, declared)
		require.Equal(t, declared, definition.Permission,
			"tool %s must require exactly what %s requires", definition.Name, definition.RPC)
		require.True(t, readPermissions[definition.Permission],
			"tool %s requires %q, which is not one of the read permissions this surface exposes",
			definition.Name, definition.Permission)
	}

	// An allowlist entry no tool uses any more is a guard that quietly stopped
	// guarding: the method could be wrapped tomorrow without anyone noticing.
	for method := range methodsWithoutAPermission {
		require.True(t, seen[method], "%s is allowlisted but no tool wraps it", method)
	}
}

func lookupMethod(t *testing.T, fullName string) protoreflect.MethodDescriptor {
	t.Helper()

	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(fullName))
	require.NoError(t, err, "tool names RPC %q, which is not in the registry", fullName)
	method, ok := descriptor.(protoreflect.MethodDescriptor)
	require.True(t, ok, "%q is not a method", fullName)
	return method
}

func methodPermission(method protoreflect.MethodDescriptor) string {
	options, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok {
		return ""
	}
	declared, ok := proto.GetExtension(options, v1pb.E_Permission).(string)
	if !ok {
		return ""
	}
	return declared
}
