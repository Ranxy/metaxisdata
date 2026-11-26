package v1

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/common/resource"
	"github.com/Ranxy/metaxisdata/backend/component/iam"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// evidenceAuthorizer allows exactly the targets it was given, so a test can tell a
// scoped grant from an unscoped one, and records what was asked.
type evidenceAuthorizer struct {
	allowed map[string]bool
	perm    permission.Permission
	ref     resource.Ref
	calls   int
	err     error
}

func (a *evidenceAuthorizer) Check(_ context.Context, _ *store.UserMessage, perm permission.Permission, ref resource.Ref) (bool, error) {
	a.calls++
	a.perm = perm
	a.ref = ref
	if a.err != nil {
		return false, a.err
	}
	return a.allowed[ref.ID], nil
}

func (*evidenceAuthorizer) Scopes(context.Context, *store.UserMessage, permission.Permission) (resource.ScopeSet, error) {
	return resource.ScopeSet{}, nil
}

var _ iam.Authorizer = (*evidenceAuthorizer)(nil)

// The agent may read the evidence of the databases it is delegated and nothing else.
// The permission must be rules.evidence: rules.list and executions.* are in the member
// baseline, which the scoped authorizer grants to every principal on every target, so
// neither can express this boundary.
func TestCheckEvidenceTargetIsScopedToTheAgentDatabases(t *testing.T) {
	t.Parallel()

	authorizer := &evidenceAuthorizer{allowed: map[string]bool{"inst1;app;public;orders": true}}
	service := &DataQualityService{authorizer: authorizer}
	actor := &store.UserMessage{ID: 110}

	require.NoError(t, service.checkEvidenceTarget(context.Background(), actor,
		"inst1;app;public;orders"))
	require.Equal(t, permission.DataQualityRulesEvidence, authorizer.perm)
	require.Equal(t, resource.NewRef(resource.TypeTable, "inst1;app;public;orders"), authorizer.ref)

	err := service.checkEvidenceTarget(context.Background(), actor,
		"inst2;app;public;orders")
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	require.Contains(t, err.Error(), string(permission.DataQualityRulesEvidence))
}

// A service built without an authorizer keeps working: focused unit tests and a
// deployment that has not wired IAM must not turn a read into an error.
func TestCheckEvidenceTargetWithoutAnAuthorizer(t *testing.T) {
	t.Parallel()

	service := &DataQualityService{}
	require.NoError(t, service.checkEvidenceTarget(context.Background(), &store.UserMessage{ID: 1}, "inst1;app;public;orders"))
}

// An authorizer failure is an internal error, never a denial: a broken grant read must
// not read as "you may not".
func TestCheckEvidenceTargetReportsAnAuthorizerFailure(t *testing.T) {
	t.Parallel()

	authorizer := &evidenceAuthorizer{err: errors.New("policy store unavailable")}
	service := &DataQualityService{authorizer: authorizer}
	err := service.checkEvidenceTarget(context.Background(), &store.UserMessage{ID: 1}, "inst1;app;public;orders")
	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}
