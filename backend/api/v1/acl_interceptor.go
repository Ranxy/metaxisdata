package v1

import (
	"context"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/component/iam"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// PermissionChecker resolves whether a caller holds a permission. *iam.Manager
// is the production implementation; the interface lets the interceptor be
// unit-tested with a fake checker.
type PermissionChecker interface {
	CheckPermission(ctx context.Context, perm permission.Permission, user *store.UserMessage) (bool, error)
}

// ACLInterceptor enforces the authorization requirement declared on an RPC
// through the (metaxisdata.v1.permission) method option. The permission is
// resolved by the IAM engine against the caller's roles and the workspace IAM
// policy: workspaceAdmin holds the whole catalog, workspaceMember the read
// baseline, and custom roles exactly what they were granted.
//
// A method without a permission annotation is not gated here (the handler owns
// its access control), and neither is a method declared
// allow_without_credential: those must stay reachable before login.
type ACLInterceptor struct {
	iam PermissionChecker
}

// NewACLInterceptor returns a new ACL interceptor backed by the IAM manager.
func NewACLInterceptor(iamManager *iam.Manager) *ACLInterceptor {
	return &ACLInterceptor{iam: iamManager}
}

// newACLInterceptorWithChecker builds an interceptor backed by a fake checker.
func newACLInterceptorWithChecker(checker PermissionChecker) *ACLInterceptor {
	return &ACLInterceptor{iam: checker}
}

func (in *ACLInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if err := in.authorize(ctx); err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

func (*ACLInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		return next(ctx, spec)
	}
}

func (in *ACLInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if err := in.authorize(ctx); err != nil {
			return err
		}
		return next(ctx, conn)
	}
}

func (in *ACLInterceptor) authorize(ctx context.Context) error {
	authCtx, ok := common.GetAuthContextFromContext(ctx)
	if !ok {
		return connect.NewError(connect.CodeInternal, errors.New("auth context not found"))
	}
	if authCtx.AllowWithoutCredential || authCtx.Permission == "" {
		return nil
	}

	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return connect.NewError(connect.CodeUnauthenticated, errors.Errorf("authentication required for permission %q", authCtx.Permission))
	}
	ok, err := in.iam.CheckPermission(ctx, permission.Permission(authCtx.Permission), user)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check permission"))
	}
	if !ok {
		return connect.NewError(connect.CodePermissionDenied, errors.Errorf("permission %q denied", authCtx.Permission))
	}
	return nil
}

// requirePermission returns a connect error unless the caller holds perm. It is
// the handler-side counterpart of the ACL interceptor, for RPCs whose access
// rule cannot be expressed as a single annotation (self-service paths and
// allow_missing creates).
func requirePermission(ctx context.Context, checker PermissionChecker, perm permission.Permission) error {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	ok, err := checker.CheckPermission(ctx, perm, user)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check permission"))
	}
	if !ok {
		return connect.NewError(connect.CodePermissionDenied, errors.Errorf("permission %q denied", perm))
	}
	return nil
}
