package v1

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// mutatingMethodPrefixes are the verb prefixes a write RPC uses. A method that
// changes durable state belongs in the permanent ledger — especially when the
// change redirects data or rotates a credential — so the annotation is checked
// rather than remembered.
var mutatingMethodPrefixes = []string{"Create", "Update", "Delete", "Set", "Revoke", "Batch"}

// unauditedMutatingMethods is the complete allowlist of writes that deliberately
// stay out of the ledger. Each entry states why, so adding one is a decision
// about the permanent record rather than a convenient omission.
var unauditedMutatingMethods = map[string]string{
	"metaxisdata.v1.AuthService.CreateSSOState":                     "a pre-login flow that keeps process-local pending state; nothing durable is written",
	"metaxisdata.v1.UserService.BatchGetUsers":                      "a read whose Batch prefix is not a write",
	"metaxisdata.v1.NotificationService.BatchMarkNotificationsRead": "a self-service inbox operation scoped to the caller, not an administrative action (see .agents/docs/notifications.md)",
	"metaxisdata.v1.NotificationService.DeleteNotification":         "a self-service inbox operation scoped to the caller, not an administrative action (see .agents/docs/notifications.md)",
}

// TestMutatingMethodsAreAudited walks the protobuf registry instead of a list,
// so a new write RPC cannot reach production without either the audit annotation
// or an entry here. It also checks the allowlist for stale entries: a method that
// disappears must be removed from it, and an allowlisted method that gains the
// annotation must lose the exemption.
func TestMutatingMethodsAreAudited(t *testing.T) {
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

				if !isMutatingMethod(fullName) {
					continue
				}
				audited := methodAudited(method)
				reason, allowlisted := unauditedMutatingMethods[fullName]
				if allowlisted {
					require.NotEmptyf(t, reason, "allowlist entry for %s must state why it is not audited", fullName)
					require.Falsef(t, audited, "%s now carries the audit annotation; remove it from the allowlist", fullName)
					continue
				}
				require.Truef(t, audited,
					"write RPC %s is not audited; add (metaxisdata.v1.audit) = true or allowlist it with a reason", fullName)
			}
		}
		return true
	})
	require.NotEmpty(t, seen)

	for fullName := range unauditedMutatingMethods {
		require.Truef(t, seen[fullName], "allowlist entry %s no longer exists", fullName)
	}
}

// isMutatingMethod reports whether a method name uses one of the write verbs.
func isMutatingMethod(fullName string) bool {
	name := fullName[strings.LastIndex(fullName, ".")+1:]
	for _, prefix := range mutatingMethodPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
