package server

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// TestConnectLongUnaryMethodsAreUnaryProcedures keeps the long-ceiling list from
// drifting out of the API it refers to: every entry must name a real unary method
// of this deployment's services. The keys are generated constants, so a rename
// fails to compile; this test catches an entry that names a streaming method (which
// the interceptor would never apply it to) or a procedure that no longer exists.
func TestConnectLongUnaryMethodsAreUnaryProcedures(t *testing.T) {
	t.Parallel()

	procedures := unaryProcedureNames(t)
	require.NotEmpty(t, connectLongUnaryMethods)

	for procedure := range connectLongUnaryMethods {
		require.True(t, procedures[procedure], "%q is not a unary procedure of this API", procedure)
		require.Equal(t, connectLongUnaryTimeout, connectLongUnaryMethods[procedure],
			"%q must use the long ceiling", procedure)
	}
}

// unaryProcedureNames builds the set of fully-qualified unary procedure names from
// the generated descriptors, the same way the ACL guard reads its method set.
func unaryProcedureNames(t *testing.T) map[string]bool {
	t.Helper()

	procedures := make(map[string]bool)
	protoregistry.GlobalFiles.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		if file.Package() != "metaxisdata.v1" {
			return true
		}
		services := file.Services()
		for i := range services.Len() {
			service := services.Get(i)
			methods := service.Methods()
			for j := range methods.Len() {
				method := methods.Get(j)
				if method.IsStreamingClient() || method.IsStreamingServer() {
					continue
				}
				procedures["/"+string(service.FullName())+"/"+string(method.Name())] = true
			}
		}
		return true
	})
	return procedures
}
