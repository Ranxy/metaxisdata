package v1

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/component/audit"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// ThrottleInterceptor bounds the anonymous request rate at the Connect entry for
// the methods whose first real work is a bcrypt comparison or hash. Without it an
// unauthenticated caller reaches Login with an unknown email (which deliberately
// runs a dummy bcrypt comparison to keep the timing flat) or calls CreateUser
// (which hashes a password), and roughly 100 requests a second exhausts the CPU
// for everyone. Login and CreateUser are each counted per source address with a
// global backstop, so one client cannot spend the deployment's CPU and a proxy
// that is not listed as trusted cannot let every client share one budget.
//
// CreateUser exempts a signed-in caller, because an administrator may create
// users in bulk and bounding an authenticated principal per user is the general
// rate-limit work (M24). Login does not: it is not a normal signed-in call, and
// every request still spends bcrypt on whoever the address names.
type ThrottleInterceptor struct {
	stateCfg       *state.State
	trustedProxies []string
}

// NewThrottleInterceptor returns an interceptor reading its budgets from stateCfg.
func NewThrottleInterceptor(stateCfg *state.State, trustedProxies []string) *ThrottleInterceptor {
	return &ThrottleInterceptor{stateCfg: stateCfg, trustedProxies: trustedProxies}
}

func (in *ThrottleInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if err := in.check(ctx, req.Spec().Procedure, req.Header(), req.Peer().Addr, time.Now()); err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

// check applies the budget of procedure. It returns a ConnectRPC error when the
// budget is exhausted.
func (in *ThrottleInterceptor) check(ctx context.Context, procedure string, header http.Header, peerAddr string, now time.Time) error {
	limiter, skipAuthenticated := in.limiterFor(procedure)
	if limiter == nil {
		return nil
	}
	if skipAuthenticated {
		if user, ok := GetUserFromContext(ctx); ok && user != nil {
			return nil
		}
	}
	// The source is the resolved client address, the same one the audit row
	// records, so a caller cannot reset its bucket with a forwarding header.
	source := audit.BuildRequestMetadata(header, peerAddr, in.trustedProxies).GetIp()
	if !limiter.Allow(source, now) {
		return connect.NewError(connect.CodeResourceExhausted, errors.New("too many requests, try again later"))
	}
	return nil
}

func (*ThrottleInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (*ThrottleInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

// limiterFor returns the budget for a procedure and whether a signed-in caller is
// exempt from it. A nil limiter means the method is not CPU-bound and anonymous.
func (in *ThrottleInterceptor) limiterFor(procedure string) (*state.WindowLimiter, bool) {
	switch procedure {
	case v1connect.AuthServiceLoginProcedure:
		return in.stateCfg.LoginRequestLimiter, false
	case v1connect.UserServiceCreateUserProcedure:
		return in.stateCfg.CreateUserRequestLimiter, true
	default:
		return nil, false
	}
}
