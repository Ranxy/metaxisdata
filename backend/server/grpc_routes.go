package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"

	"connectrpc.com/connect"
	"connectrpc.com/grpcreflect"
	grpcruntime "github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	"github.com/Ranxy/metaxisdata/backend/api/oauth"
	apiv1 "github.com/Ranxy/metaxisdata/backend/api/v1"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/common/stacktrace"
	"github.com/Ranxy/metaxisdata/backend/component/audit"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	"github.com/Ranxy/metaxisdata/backend/component/iam"
	llmcomp "github.com/Ranxy/metaxisdata/backend/component/llm"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/config"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/mcp"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/runner/schemasync"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// maxConnectRequestBytes caps what one ConnectRPC request may carry. Echo's
// transport-level body limit (100M) stays as a backstop, but every audited
// call is serialized into a ledger that is never pruned, so each handler needs a
// bound of its own — the general limit was far too generous to be one.
const maxConnectRequestBytes = 4 << 20

// maxAuthServiceRequestBytes caps what an AuthService request may carry. The
// service holds credentials and the short device login fields, and two of its
// endpoints are anonymous, so the general request limit is still far too
// generous here.
const maxAuthServiceRequestBytes = 64 << 10

// maxUserServiceRequestBytes caps what a UserService request may carry. It is
// generous for the single-user RPCs (an address, a display name, a password) and
// still bounds the anonymous, audited CreateUser together with BatchGetUsers,
// the one call the audit-log CSV export feeds a whole ledger's worth of names.
const maxUserServiceRequestBytes = 64 << 10

// mcpEndpointTimeout bounds one MCP request. A tool call queries the registry, so
// it gets more room than an OAuth protocol endpoint, but not an unbounded wait:
// the route sits outside the Connect interceptor chain.
const mcpEndpointTimeout = 60 * time.Second

func configureGrpcRouters(
	ctx context.Context,
	e *echo.Echo,
	stores *store.Store,
	profile *config.Profile,
	stateCfg *state.State,
	secret string,
	dbFactory *dbfactory.DBFactory,
	schemaSync *schemasync.Syncer,
	llmRegistry *llmcomp.Registry,
	lineageAnalyzer *lineage.Analyzer,
) error {
	// Note: the gateway response modifier takes the token duration on server startup. If the value is changed,
	// the user has to restart the server to take the latest value.
	gatewayModifier := auth.GatewayResponseModifier{}
	mux := grpcruntime.NewServeMux(
		grpcruntime.WithMarshalerOption(grpcruntime.MIMEWildcard, &grpcruntime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{},
			//nolint:forbidigo
			UnmarshalOptions: protojson.UnmarshalOptions{},
		}),
		grpcruntime.WithForwardResponseOption(gatewayModifier.Modify),
		grpcruntime.WithRoutingErrorHandler(func(ctx context.Context, sm *grpcruntime.ServeMux, m grpcruntime.Marshaler, w http.ResponseWriter, r *http.Request, httpStatus int) {
			err := &grpcruntime.HTTPStatusError{
				HTTPStatus: httpStatus,
				Err:        connect.NewError(connect.CodeNotFound, errors.Errorf("gateway routing error %d: request method %v, URI %v", httpStatus, r.Method, r.RequestURI)),
			}
			grpcruntime.DefaultHTTPErrorHandler(ctx, sm, m, w, r, err)
		}),
	)

	iamManager := iam.NewManager(stores)
	userService := apiv1.NewUserService(stores, iamManager, profile)
	authService := apiv1.NewAuthService(stores, secret, profile, stateCfg)
	auditLogService := apiv1.NewAuditLogService(stores)
	instanceService := apiv1.NewInstanceService(stores, dbFactory, schemaSync, stateCfg)
	databaseService := apiv1.NewDatabaseService(stores, schemaSync)
	lineageService := apiv1.NewLineageService(stores, lineageAnalyzer)
	openLineageService := apiv1.NewOpenLineageService(stores)
	llmService := apiv1.NewLLMService(stores, llmRegistry)
	explainSQLService := apiv1.NewExplainSQLService(stores, llmRegistry, lineageAnalyzer)
	settingService := apiv1.NewSettingService(stores, profile)
	environmentService := apiv1.NewEnvironmentService(stores)
	roleService := apiv1.NewRoleService(stores)
	groupService := apiv1.NewGroupService(stores)
	iamService := apiv1.NewIamService(stores)
	oauthService := apiv1.NewOAuthService(stores, profile, stateCfg)

	onPanic := func(_ context.Context, s connect.Spec, _ http.Header, p any) error {
		stack := stacktrace.TakeStacktrace(20 /* n */, 5 /* skip */)
		// keep a multiline stack
		slog.Error("v1 server panic error", "method", s.Procedure, log.WithError(errors.Errorf("error: %v\n%s", p, stack)))
		// Panic details (internal file paths, function names, line numbers) are
		// only safe to hand back in debug mode; otherwise they help a caller map
		// the server internals. The full stack is always logged above.
		if profile.RuntimeDebug.Load() {
			return connect.NewError(connect.CodeInternal, errors.Errorf("error: %v\n%s", p, stack))
		}
		return connect.NewError(connect.CodeInternal, errors.New("internal server error"))
	}

	handlerOpts := connect.WithHandlerOptions(
		connect.WithInterceptors(
			apiv1.NewDebugInterceptor(),
			auth.New(stores, secret, stateCfg, profile),
			// After auth so it can tell a signed-in caller from an anonymous one,
			// before audit so a request refused by the budget leaves no ledger row.
			apiv1.NewThrottleInterceptor(stateCfg, profile.TrustedProxies),
			apiv1.NewAuditInterceptor(stores, profile.TrustedProxies),
			apiv1.NewACLInterceptor(iamManager),
			// Innermost, so the audit interceptor records the status the client
			// actually received.
			apiv1.NewErrorMappingInterceptor(),
		),
		// A handler may narrow this further; a later option wins. The request is
		// read before the interceptor chain runs, so an oversized body answers
		// ResourceExhausted without reaching the handler or the audit ledger.
		connect.WithReadMaxBytes(maxConnectRequestBytes),
		connect.WithRecover(onPanic),
	)

	connectHandlers := make(map[string]http.Handler)

	userPath, userHandler := v1connect.NewUserServiceHandler(userService, handlerOpts, connect.WithReadMaxBytes(maxUserServiceRequestBytes))
	connectHandlers[userPath] = userHandler
	// AuthService carries only credentials and the short device login fields,
	// and two of its endpoints are anonymous. The general body limit is far too
	// generous for it: an unauthenticated caller could otherwise make the server
	// buffer a huge request per call.
	authPath, authHandler := v1connect.NewAuthServiceHandler(authService, handlerOpts, connect.WithReadMaxBytes(maxAuthServiceRequestBytes))
	connectHandlers[authPath] = authHandler
	auditLogPath, auditLogHandler := v1connect.NewAuditLogServiceHandler(auditLogService, handlerOpts)
	connectHandlers[auditLogPath] = auditLogHandler
	instancePath, instanceHandler := v1connect.NewInstanceServiceHandler(instanceService, handlerOpts)
	connectHandlers[instancePath] = instanceHandler
	databasePath, databaseHandler := v1connect.NewDatabaseServiceHandler(databaseService, handlerOpts)
	connectHandlers[databasePath] = databaseHandler
	lineagePath, lineageHandler := v1connect.NewLineageServiceHandler(lineageService, handlerOpts)
	connectHandlers[lineagePath] = lineageHandler
	openLineagePath, openLineageHandler := v1connect.NewOpenLineageServiceHandler(openLineageService, handlerOpts)
	connectHandlers[openLineagePath] = openLineageHandler
	llmPath, llmHandler := v1connect.NewLLMServiceHandler(llmService, handlerOpts)
	connectHandlers[llmPath] = llmHandler
	explainSQLPath, explainSQLHandler := v1connect.NewExplainSQLServiceHandler(explainSQLService, handlerOpts)
	connectHandlers[explainSQLPath] = explainSQLHandler
	settingPath, settingHandler := v1connect.NewSettingServiceHandler(settingService, handlerOpts)
	connectHandlers[settingPath] = settingHandler
	environmentPath, environmentHandler := v1connect.NewEnvironmentServiceHandler(environmentService, handlerOpts)
	connectHandlers[environmentPath] = environmentHandler
	rolePath, roleHandler := v1connect.NewRoleServiceHandler(roleService, handlerOpts)
	connectHandlers[rolePath] = roleHandler
	groupPath, groupHandler := v1connect.NewGroupServiceHandler(groupService, handlerOpts)
	connectHandlers[groupPath] = groupHandler
	iamPath, iamHandler := v1connect.NewIamServiceHandler(iamService, handlerOpts)
	connectHandlers[iamPath] = iamHandler
	oauthPath, oauthHandler := v1connect.NewOAuthServiceHandler(oauthService, handlerOpts)
	connectHandlers[oauthPath] = oauthHandler
	// grpc reflection handlers.
	reflector := grpcreflect.NewStaticReflector(
		v1connect.AuthServiceName,
		v1connect.AuditLogServiceName,
		v1connect.UserServiceName,
		v1connect.InstanceServiceName,
		v1connect.DatabaseServiceName,
		v1connect.LineageServiceName,
		v1connect.OpenLineageServiceName,
		v1connect.LLMServiceName,
		v1connect.ExplainSQLServiceName,
		v1connect.SettingServiceName,
		v1connect.EnvironmentServiceName,
		v1connect.RoleServiceName,
		v1connect.GroupServiceName,
		v1connect.IamServiceName,
		v1connect.OAuthServiceName,
	)
	reflectPath, reflectHandler := grpcreflect.NewHandlerV1(reflector, connect.WithReadMaxBytes(maxConnectRequestBytes))
	connectHandlers[reflectPath] = reflectHandler

	reflectAlphaPath, reflectAlphaHandler := grpcreflect.NewHandlerV1Alpha(reflector, connect.WithReadMaxBytes(maxConnectRequestBytes))
	connectHandlers[reflectAlphaPath] = reflectAlphaHandler

	// REST gateway proxy.
	grpcEndpoint := fmt.Sprintf(":%d", profile.Port)
	grpcConn, err := grpc.NewClient(
		grpcEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			// Bound both directions: the gateway used to accept 100MB while
			// sending without any matching ceiling.
			grpc.MaxCallRecvMsgSize(100*1024*1024),
			grpc.MaxCallSendMsgSize(100*1024*1024),
		),
	)
	if err != nil {
		return err
	}
	// The gateway connection is lazy, but it still owns a client channel. Close
	// it when the server context is cancelled (signal or post-shutdown).
	context.AfterFunc(ctx, func() { _ = grpcConn.Close() })

	if err := v1pb.RegisterAuthServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterAuditLogServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterUserServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterInstanceServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterDatabaseServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterLineageServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterOpenLineageServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterLLMServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterExplainSQLServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterSettingServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterEnvironmentServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterRoleServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterGroupServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterIamServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterOAuthServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}

	// Register OpenLineage event ingestion HTTP handler (plain REST, not ConnectRPC).
	olHandler := apiv1.NewOpenLineageHandler(stores, profile.TrustedProxies, lineageAnalyzer)
	olGroup := e.Group("/api/v1/lineage")
	// Ingestion skips the Connect interceptor chain, so it carries its own rate
	// limit and deadline: a valid key could otherwise drive unbounded concurrent
	// ingestion with no time bound. Key-less requests are budgeted per resolved
	// client address, so the same trusted-proxy rules as the audit record apply.
	olGroup.Use(openLineageIngestionMiddleware(profile.TrustedProxies))
	olHandler.RegisterRoutes(olGroup)

	// The OAuth 2.1 authorization server behind the MCP endpoint. Its routes are
	// plain HTTP because OAuth clients do not speak ConnectRPC, so they sit
	// outside the interceptor chain and carry their own authentication, rate
	// limits and error rendering. Everything they publish is disabled while
	// mcp_enabled is off, and unusable until external_url is configured.
	//
	// This authenticator is a second instance of the same rules the interceptor
	// uses, built from the same store, secret and state: it exists because the
	// interceptor owns its own instance, and the rules themselves live in one
	// place.
	tokenAuthenticator := auth.NewTokenAuthenticator(stores, secret, stateCfg)
	oauthServer := oauth.NewServer(oauth.ServerConfig{
		Stores:         stores,
		State:          stateCfg,
		Tokens:         tokenAuthenticator,
		Mode:           profile.Mode,
		Secret:         secret,
		TrustedProxies: profile.TrustedProxies,
		Endpoints:      oauth.WorkspaceEndpoints(stores),
	})
	e.GET("/.well-known/oauth-protected-resource", echo.WrapHandler(oauth.ProtectedResourceHandler(stores)))
	// RFC 9728 also defines the path-insertion form; clients try both.
	e.GET("/.well-known/oauth-protected-resource/*", echo.WrapHandler(oauth.ProtectedResourceHandler(stores)))
	e.GET("/.well-known/oauth-authorization-server", echo.WrapHandler(oauth.AuthServerMetadataHandler(stores)))
	// The browser flow is anonymous by design (the user may not be signed in yet)
	// and each request reads or writes the pending-request store, so it carries the
	// same ceiling as the endpoints that mint credentials. Each route carries its
	// own per-address budget: pointing all four at one limiter would tighten the
	// anonymous OAuth surface four-fold, which is the open I2 question in
	// docs/security-review-2026-10.md rather than a decision to make here.
	e.GET("/oauth/authorize", echo.WrapHandler(oauthServer.Audited(oauthServer.AuthorizeHandler())), oauthEndpointMiddleware(profile.TrustedProxies))
	e.GET("/oauth/authorize/complete", echo.WrapHandler(oauthServer.Audited(oauthServer.CompletionHandler())), oauthEndpointMiddleware(profile.TrustedProxies))
	e.POST("/oauth/token", echo.WrapHandler(oauthServer.Audited(oauthServer.TokenHandler())), oauthEndpointMiddleware(profile.TrustedProxies))
	e.POST("/oauth/register", echo.WrapHandler(oauthServer.Audited(oauth.RegisterHandler(stores))), oauthEndpointMiddleware(profile.TrustedProxies))

	// The MCP endpoint itself. Stateless, so it keeps no session and works behind
	// a load balancer, and wrapped in the standard library's cross-origin
	// protection: the SDK applies none by default, and a page that can post here
	// with a client's token would be a cross-site tool execution. The tools call
	// the same service instances the ConnectRPC API mounts, so a tool and its RPC
	// cannot answer differently.
	mcpServer := mcp.NewServer(mcp.Config{
		Instances:      instanceService,
		Databases:      databaseService,
		Lineage:        lineageService,
		Principals:     userService,
		Checker:        iamManager,
		CallLimiter:    stateCfg.MCPCallLimiter,
		Stores:         stores,
		TrustedProxies: profile.TrustedProxies,
		Endpoints:      oauth.WorkspaceEndpoints(stores),
	})
	mcpHandler := http.NewCrossOriginProtection().Handler(mcpServer.Handler(tokenAuthenticator))
	// This endpoint sits outside the Connect interceptor chain, so it carries its
	// own time bound: a hung store call must not hold a request open forever.
	mcpRoute := echo.WrapHandler(http.TimeoutHandler(mcpHandler, mcpEndpointTimeout, `{"error":"temporarily_unavailable","error_description":"the request timed out"}`))
	e.Any("/mcp", mcpRoute)
	e.Any("/mcp/*", mcpRoute)

	// The gateway reads and parses the whole body before it forwards anything, so
	// the per-handler ConnectRPC cap only rejects the message once it is already
	// buffered. Bound the body here too, so the REST form of an anonymous call
	// cannot be buffered at request size either.
	e.Any("/v1/*", echo.WrapHandler(gatewayPeerMiddleware(http.MaxBytesHandler(mux, maxConnectRequestBytes))))

	// Register Connect RPC handlers
	for path, handler := range connectHandlers {
		e.Any(path+"*", echo.WrapHandler(handler))
	}

	return nil
}

// gatewayPeerMiddleware stamps the outer request's peer address for the REST
// gateway. The gateway answers /v1/* by dialing the server's own port, so the
// Connect handler sees a loopback peer and would otherwise record every REST
// request as 127.0.0.1. The stamp carries a proof only this process can produce
// (audit.StampGatewayPeer), because a caller that reaches the Connect handler
// directly can send a stamp of its own; both headers are overwritten rather than
// appended, so even the gateway path keeps only the value written here.
func gatewayPeerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		audit.StampGatewayPeer(r.Header, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}
