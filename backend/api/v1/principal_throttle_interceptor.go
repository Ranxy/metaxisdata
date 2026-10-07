package v1

import (
	"context"
	"strconv"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// principalLimitedProcedures are the methods that need a per-principal budget but
// carry no audit annotation, so the annotation alone cannot select them:
//
//   - ExplainSQL runs the LLM agent loop, the one path that spends an upstream
//     provider's tokens;
//   - FetchLLMModels asks the provider for its model list with the profile's key.
//
// Both are streaming-capable or cheap to call in a loop, and neither writes a
// ledger row, so the ledger is not what bounds them.
var principalLimitedProcedures = map[string]struct{}{
	v1connect.ExplainSQLServiceExplainSQLProcedure: {},
	v1connect.LLMServiceFetchLLMModelsProcedure:    {},
}

// auditedReadExemptProcedures are audited methods that only read. They are exempt
// because the budget covers ledger *writes*, and ListAuditLogs is the only audited
// method that changes nothing. It is not a narrow exemption: the audit interceptor
// runs before the ACL interceptor, so a call the caller is not allowed to make still
// writes a row, and an ordinary member holds no audit-log permission — every call
// from any signed-in account is refused and recorded, with no rate bound on the
// recording. That is the one unbounded ledger write left; docs/security-posture.md
// states it rather than quietly bounding reads here.
var auditedReadExemptProcedures = map[string]struct{}{
	v1connect.AuditLogServiceListAuditLogsProcedure: {},
}

// PrincipalThrottleInterceptor bounds what one signed-in principal may spend on
// the methods that write a permanent ledger row or reach an upstream LLM. The
// ledger is kept forever and an audited method is audited whether or not it
// changes anything, so a caller holding one account could otherwise grow it
// without limit; the LLM calls carry a cost rather than a row.
//
// It runs after auth, so the principal is known, and before audit, so a request
// it refuses never runs and leaves no ledger row. That is the whole backpressure
// semantic: the ledger still records every action that happened, and the caller
// is told to slow down instead of the record being dropped.
//
// The anonymous methods keep their own budgets (ThrottleInterceptor and the
// device login limiters): they have no principal, and their key is the resolved
// client address. This interceptor is a no-op for them.
type PrincipalThrottleInterceptor struct {
	stateCfg *state.State
}

// NewPrincipalThrottleInterceptor returns the interceptor reading its budget from
// stateCfg.
func NewPrincipalThrottleInterceptor(stateCfg *state.State) *PrincipalThrottleInterceptor {
	return &PrincipalThrottleInterceptor{stateCfg: stateCfg}
}

func (in *PrincipalThrottleInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if err := in.check(ctx, req.Spec().Procedure); err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

func (*PrincipalThrottleInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (in *PrincipalThrottleInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if err := in.check(ctx, conn.Spec().Procedure); err != nil {
			return err
		}
		return next(ctx, conn)
	}
}

// check applies the principal's budget for procedure.
func (in *PrincipalThrottleInterceptor) check(ctx context.Context, procedure string) error {
	if !isPrincipalBudgeted(ctx, procedure) {
		return nil
	}
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil
	}
	if !in.stateCfg.PrincipalRequestLimiter.Allow(principalLimiterKey(user.ID, procedure), time.Now()) {
		return connect.NewError(connect.CodeResourceExhausted, errors.New("too many requests to this method, try again later"))
	}
	return nil
}

// isPrincipalBudgeted reports whether procedure needs a per-principal budget. The
// audit annotation decides it, so a method that gains one later is budgeted
// without editing this file; the two lists are the deliberate exceptions, and
// principal_throttle_interceptor_test.go walks the protobuf registry to keep them
// honest.
func isPrincipalBudgeted(ctx context.Context, procedure string) bool {
	if _, ok := principalLimitedProcedures[procedure]; ok {
		return true
	}
	authCtx, ok := common.GetAuthContextFromContext(ctx)
	if !ok || !authCtx.Audit {
		return false
	}
	_, exempt := auditedReadExemptProcedures[procedure]
	return !exempt
}

// principalLimiterKey names one (principal, procedure) bucket. The procedure is
// part of the key so a noisy method cannot spend another method's quota, while a
// single limiter keeps one global counter across all of them.
func principalLimiterKey(userID int, procedure string) string {
	return "user:" + strconv.Itoa(userID) + "|" + procedure
}
