package v1

import (
	"context"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The budgets are the state package's constants; they are repeated here so a
// silent change to them shows up as a failing test rather than passing either
// way.
const (
	testPrincipalMethodBudget     = 3000
	testPrincipalGlobalBudget     = 30000
	testLogoutRequestSourceBudget = 120
)

// anonymousAuditedMethods are the audited methods reachable without a credential.
// They carry a per-source budget rather than a per-principal one, and this map is
// the only way to add another: TestEveryAuditedMethodIsBudgeted refuses an
// audited anonymous method that is not here, and each entry names the limiter
// that is supposed to bound it so the claim is checked rather than asserted.
var anonymousAuditedMethods = map[string]func(*ThrottleInterceptor) *state.WindowLimiter{
	v1connect.AuthServiceLoginProcedure: func(in *ThrottleInterceptor) *state.WindowLimiter {
		return in.stateCfg.LoginRequestLimiter
	},
	v1connect.AuthServiceLogoutProcedure: func(in *ThrottleInterceptor) *state.WindowLimiter {
		return in.stateCfg.LogoutRequestLimiter
	},
	v1connect.AuthServiceCreateDeviceLoginProcedure: func(in *ThrottleInterceptor) *state.WindowLimiter {
		// Counted in the handler, not on the interceptor chain: the request is
		// bounded before anything is allocated.
		return in.stateCfg.DeviceLoginLimiter
	},
	v1connect.UserServiceCreateUserProcedure: func(in *ThrottleInterceptor) *state.WindowLimiter {
		return in.stateCfg.CreateUserRequestLimiter
	},
}

// handlerBudgetedAnonymousMethods are the audited anonymous methods whose budget
// is applied inside the handler rather than by ThrottleInterceptor, so the guard
// does not require limiterFor to return their limiter.
var handlerBudgetedAnonymousMethods = map[string]bool{
	v1connect.AuthServiceCreateDeviceLoginProcedure: true,
}

// auditedReadMethods are the audited methods that only read, which is the only
// reason one may be exempt from the per-principal budget. This map and the
// runtime auditedReadExemptProcedures must match exactly, so exempting another
// method is a deliberate two-file change rather than an edit that passes.
var auditedReadMethods = map[string]bool{
	v1connect.AuditLogServiceListAuditLogsProcedure: true,
}

// extraPrincipalLimitedMethods are the expensive methods that carry no audit
// annotation. They must match principalLimitedProcedures exactly, for the same
// reason.
var extraPrincipalLimitedMethods = map[string]bool{
	v1connect.ExplainSQLServiceExplainSQLProcedure: true,
	v1connect.LLMServiceFetchLLMModelsProcedure:    true,
}

// TestEveryAuditedMethodIsBudgeted is the guard for the ledger's growth: a
// permanent audit row has to be bounded for the caller that writes it, whoever
// that caller is. Every audited method must therefore be bounded either per
// source (anonymous) or per principal, and the only exemptions are the audited
// read methods listed in auditedReadExemptProcedures.
//
// It walks the protobuf registry rather than a list, so a method that gains an
// audit annotation later is covered without editing the runtime code — and a
// method that gains one and is neither budgeted nor deliberately exempt turns
// this test red.
func TestEveryAuditedMethodIsBudgeted(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New()
	require.NoError(t, err)
	anonymous := NewThrottleInterceptor(stateCfg, nil)

	seen := map[string]bool{}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if fd.Package() != "metaxisdata.v1" {
			return true
		}
		for i := range fd.Services().Len() {
			service := fd.Services().Get(i)
			for j := range service.Methods().Len() {
				method := service.Methods().Get(j)
				procedure := connectProcedure(method)
				seen[procedure] = true

				audited := methodAudited(method)
				ctx := context.WithValue(context.Background(), common.AuthContextKey, &common.AuthContext{Audit: audited})
				budgeted := isPrincipalBudgeted(ctx, procedure)

				if !audited {
					_, extra := principalLimitedProcedures[procedure]
					require.Equalf(t, extra, budgeted,
						"method %s is not audited, so only an explicit entry may make it budgeted", procedure)
					continue
				}
				if methodAllowWithoutCredential(method) {
					limiter, ok := anonymousAuditedMethods[procedure]
					require.Truef(t, ok,
						"audited anonymous method %s has no per-source budget; add it to anonymousAuditedMethods", procedure)
					recorded := limiter(anonymous)
					require.NotNilf(t, recorded, "method %s names a nil limiter", procedure)
					if !handlerBudgetedAnonymousMethods[procedure] {
						got, _ := anonymous.limiterFor(procedure)
						require.Samef(t, recorded, got,
							"method %s records a limiter the interceptor does not apply", procedure)
					}
					continue
				}
				_, exempt := auditedReadExemptProcedures[procedure]
				require.Equalf(t, !exempt, budgeted,
					"audited authenticated method %s must be budgeted per principal, or listed as an exempt read", procedure)
			}
		}
		return true
	})
	require.NotEmpty(t, seen)

	// The two runtime lists and the test's copies of them must agree, both ways:
	// a new exemption or a new expensive method is a change to both files.
	for procedure := range principalLimitedProcedures {
		require.Truef(t, extraPrincipalLimitedMethods[procedure],
			"principalLimitedProcedures gained %s without the test allowlist", procedure)
	}
	for procedure := range extraPrincipalLimitedMethods {
		_, ok := principalLimitedProcedures[procedure]
		require.Truef(t, ok, "extraPrincipalLimitedMethods lists %s, which the runtime does not", procedure)
	}
	for procedure := range auditedReadExemptProcedures {
		require.Truef(t, auditedReadMethods[procedure],
			"auditedReadExemptProcedures gained %s without the test allowlist", procedure)
	}
	for procedure := range auditedReadMethods {
		_, ok := auditedReadExemptProcedures[procedure]
		require.Truef(t, ok, "auditedReadMethods lists %s, which the runtime does not exempt", procedure)
	}

	checkStale := func(kind string, procedures map[string]struct{}) {
		for procedure := range procedures {
			if !seen[procedure] {
				t.Errorf("%s lists %s, which no longer exists", kind, procedure)
			}
		}
	}
	checkStale("principalLimitedProcedures", principalLimitedProcedures)
	checkStale("auditedReadExemptProcedures", auditedReadExemptProcedures)
	for procedure := range anonymousAuditedMethods {
		if !seen[procedure] {
			t.Errorf("anonymousAuditedMethods lists %s, which no longer exists", procedure)
		}
	}
}

// TestPrincipalThrottleInterceptorBoundsOnePrincipalPerMethod pins the per-key
// dimension: the budget is spent by (principal, method), so one principal
// hammering one method is refused while another principal, and another method of
// the same principal, are unaffected.
func TestPrincipalThrottleInterceptorBoundsOnePrincipalPerMethod(t *testing.T) {
	t.Parallel()

	interceptor := newTestPrincipalThrottleInterceptor(t)
	create := v1connect.InstanceServiceCreateInstanceProcedure
	deleteMethod := v1connect.InstanceServiceDeleteInstanceProcedure
	ctx := principalBudgetContext(7)

	for range testPrincipalMethodBudget {
		require.NoError(t, interceptor.check(ctx, create))
	}
	requireResourceExhausted(t, interceptor.check(ctx, create))

	require.NoError(t, interceptor.check(principalBudgetContext(8), create),
		"another principal has its own budget")
	require.NoError(t, interceptor.check(ctx, deleteMethod),
		"another method of the same principal has its own budget")
}

// TestPrincipalThrottleInterceptorHasADeploymentBackstop pins the global
// dimension: it is shared across principals and methods, so principals each
// spending their per-method budget on the same method exhaust the deployment's.
func TestPrincipalThrottleInterceptorHasADeploymentBackstop(t *testing.T) {
	t.Parallel()

	interceptor := newTestPrincipalThrottleInterceptor(t)
	create := v1connect.InstanceServiceCreateInstanceProcedure
	principals := testPrincipalGlobalBudget / testPrincipalMethodBudget

	for id := 1; id <= principals; id++ {
		ctx := principalBudgetContext(id)
		for range testPrincipalMethodBudget {
			require.NoError(t, interceptor.check(ctx, create))
		}
	}
	// The per-principal bucket of this one is fresh, so only the deployment
	// budget can be what refuses it.
	requireResourceExhausted(t, interceptor.check(principalBudgetContext(principals+1), create))
}

// TestPrincipalThrottleInterceptorIgnoresWhatItDoesNotBound pins the two cases it
// must not touch: an anonymous caller (the entry limiter owns that address) and a
// method that writes no ledger row and reaches no LLM.
func TestPrincipalThrottleInterceptorIgnoresWhatItDoesNotBound(t *testing.T) {
	t.Parallel()

	interceptor := newTestPrincipalThrottleInterceptor(t)
	create := v1connect.InstanceServiceCreateInstanceProcedure

	anonymousCtx := context.WithValue(context.Background(), common.AuthContextKey, &common.AuthContext{Audit: true})
	for range testPrincipalMethodBudget * 2 {
		require.NoError(t, interceptor.check(anonymousCtx, create), "an anonymous caller is not counted here")
	}

	unbudgetedCtx := context.WithValue(context.Background(), common.AuthContextKey, &common.AuthContext{})
	for range testPrincipalMethodBudget * 2 {
		require.NoError(t, interceptor.check(unbudgetedCtx, v1connect.InstanceServiceListInstancesProcedure))
	}
}

// TestPrincipalThrottleInterceptorCoversStreamingMethods pins the streaming path,
// which is where ExplainSQL (the LLM call) arrives: a refused stream must not
// reach its handler, and a stream on a covered method must be counted.
func TestPrincipalThrottleInterceptorCoversStreamingMethods(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New()
	require.NoError(t, err)
	interceptor := NewPrincipalThrottleInterceptor(stateCfg)
	ctx := principalBudgetContext(42)
	procedure := v1connect.InstanceServiceCreateInstanceProcedure

	for range testPrincipalMethodBudget {
		require.NoError(t, interceptor.check(ctx, procedure))
	}

	called := false
	handler := interceptor.WrapStreamingHandler(func(context.Context, connect.StreamingHandlerConn) error {
		called = true
		return nil
	})
	err = handler(ctx, &fakeStreamingConn{procedure: procedure})
	requireResourceExhausted(t, err)
	require.False(t, called, "a refused stream must not reach the handler")

	// ExplainSQL is not audited, so its entry in principalLimitedProcedures is
	// what makes the interceptor count it.
	err = handler(ctx, &fakeStreamingConn{procedure: v1connect.ExplainSQLServiceExplainSQLProcedure})
	require.NoError(t, err)
	require.True(t, called)
}

// TestThrottleInterceptorLogoutBudget pins that Logout — audited, anonymous and
// replayable with one valid token — is bounded per source like Login is, and that
// it does not consume Login's budget.
func TestThrottleInterceptorLogoutBudget(t *testing.T) {
	t.Parallel()

	interceptor := newTestThrottleInterceptor(t, nil)
	now := time.Now()

	err := allow(t, interceptor, testLogoutRequestSourceBudget, v1connect.AuthServiceLogoutProcedure, http.Header{}, "203.0.113.5:4040", now)
	require.NoError(t, err)
	err = allow(t, interceptor, 1, v1connect.AuthServiceLogoutProcedure, http.Header{}, "203.0.113.5:4040", now)
	requireResourceExhausted(t, err)

	err = allow(t, interceptor, 1, v1connect.AuthServiceLogoutProcedure, http.Header{}, "203.0.113.6:4040", now)
	require.NoError(t, err, "another source keeps its own budget")

	err = allow(t, interceptor, 1, v1connect.AuthServiceLoginProcedure, http.Header{}, "203.0.113.5:4040", now)
	require.NoError(t, err, "Login has its own bucket")
}

func newTestPrincipalThrottleInterceptor(t *testing.T) *PrincipalThrottleInterceptor {
	t.Helper()
	stateCfg, err := state.New()
	require.NoError(t, err)
	return NewPrincipalThrottleInterceptor(stateCfg)
}

// principalBudgetContext is a signed-in caller whose method is audited, which is
// what makes the interceptor count it.
func principalBudgetContext(userID int) context.Context {
	user := &store.UserMessage{ID: userID, Email: "user@example.com"}
	ctx := context.WithValue(context.Background(), common.AuthContextKey, &common.AuthContext{Audit: true})
	return context.WithValue(ctx, common.UserContextKey, user)
}

// connectProcedure renders a descriptor as the procedure string the interceptors
// see.
func connectProcedure(method protoreflect.MethodDescriptor) string {
	return "/" + string(method.Parent().FullName()) + "/" + string(method.Name())
}

func methodAudited(method protoreflect.MethodDescriptor) bool {
	options, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok || options == nil {
		return false
	}
	audited, ok := proto.GetExtension(options, v1pb.E_Audit).(bool)
	return ok && audited
}

func methodAllowWithoutCredential(method protoreflect.MethodDescriptor) bool {
	options, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok || options == nil {
		return false
	}
	allowed, ok := proto.GetExtension(options, v1pb.E_AllowWithoutCredential).(bool)
	return ok && allowed
}

// fakeStreamingConn is the minimum a streaming interceptor reads: the procedure.
type fakeStreamingConn struct {
	procedure string
}

func (c *fakeStreamingConn) Spec() connect.Spec        { return connect.Spec{Procedure: c.procedure} }
func (*fakeStreamingConn) Peer() connect.Peer          { return connect.Peer{} }
func (*fakeStreamingConn) Receive(any) error           { return nil }
func (*fakeStreamingConn) RequestHeader() http.Header  { return http.Header{} }
func (*fakeStreamingConn) Send(any) error              { return nil }
func (*fakeStreamingConn) ResponseHeader() http.Header { return http.Header{} }
func (*fakeStreamingConn) ResponseTrailer() http.Header {
	return http.Header{}
}
