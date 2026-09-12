package v1

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// ErrorMappingInterceptor translates the common.Code an error carries into the
// Connect status the client sees. Store and service code already return
// common.Errorf(common.NotFound, ...) and friends, but nothing consumed them,
// so a missing row or a duplicate key surfaced as CodeInternal/HTTP 500.
//
// It is installed closest to the handler so that outer interceptors (the audit
// interceptor in particular) observe the status the caller received.
type ErrorMappingInterceptor struct{}

// NewErrorMappingInterceptor returns a new error mapping interceptor.
func NewErrorMappingInterceptor() *ErrorMappingInterceptor {
	return &ErrorMappingInterceptor{}
}

func (*ErrorMappingInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		resp, err := next(ctx, req)
		return resp, mapCommonError(err)
	}
}

func (*ErrorMappingInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (*ErrorMappingInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		return mapCommonError(next(ctx, conn))
	}
}

// mapCommonError maps err to a Connect error. A handler that already picked a
// specific status keeps it; generic internal/unknown errors are upgraded when
// the chain still carries a common.Code. Everything else is internal, which is
// what an unhandled error deserves.
func mapCommonError(err error) error {
	if err == nil {
		return nil
	}

	var connectErr *connect.Error
	if errors.As(err, &connectErr) && connectErr.Code() != connect.CodeInternal && connectErr.Code() != connect.CodeUnknown {
		return err
	}

	return connect.NewError(connectCodeFor(common.ErrorCode(err)), err)
}

// connectCodeFor maps a common.Code onto the Connect status a client sees.
func connectCodeFor(code common.Code) connect.Code {
	switch code {
	case common.NotFound:
		return connect.CodeNotFound
	case common.Invalid:
		return connect.CodeInvalidArgument
	case common.Conflict:
		return connect.CodeAlreadyExists
	case common.NotAuthorized:
		return connect.CodePermissionDenied
	case common.NotImplemented:
		return connect.CodeUnimplemented
	case common.SizeExceeded:
		return connect.CodeResourceExhausted
	case common.Ok, common.Internal, common.DBExecutionError:
		return connect.CodeInternal
	default:
		return connect.CodeInternal
	}
}
