package v1

import (
	"context"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// ACLInterceptor enforces the authorization requirement declared on an RPC
// through the (metaxisdata.v1.permission) method option.
//
// Phase 0 only distinguishes "workspace admin only" (any non-empty permission)
// from "any authenticated caller" (empty permission). A finer-grained role to
// permission mapping will replace requireWorkspaceAdmin later.
type ACLInterceptor struct {
	store *store.Store
}

// NewACLInterceptor returns a new ACL interceptor.
func NewACLInterceptor(store *store.Store) *ACLInterceptor {
	return &ACLInterceptor{store: store}
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
	if authCtx.Permission == "" {
		return nil
	}

	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return connect.NewError(connect.CodePermissionDenied, errors.Errorf("permission %q is required", authCtx.Permission))
	}
	return requireWorkspaceAdmin(ctx, in.store, user)
}

// requireWorkspaceAdmin returns a PermissionDenied error unless the user is a
// workspace admin.
func requireWorkspaceAdmin(ctx context.Context, stores *store.Store, user *store.UserMessage) error {
	isAdmin, err := isUserWorkspaceAdmin(ctx, stores, user)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check permission"))
	}
	if !isAdmin {
		return connect.NewError(connect.CodePermissionDenied, errors.Errorf("user %q must be a workspace admin", user.Email))
	}
	return nil
}
