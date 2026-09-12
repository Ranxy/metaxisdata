package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"

	"connectrpc.com/connect"
	"connectrpc.com/grpcreflect"
	grpcruntime "github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	apiv1 "github.com/Ranxy/metaxisdata/backend/api/v1"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/common/stacktrace"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	"github.com/Ranxy/metaxisdata/backend/component/iam"
	llmcomp "github.com/Ranxy/metaxisdata/backend/component/llm"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/config"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/runner/schemasync"
	"github.com/Ranxy/metaxisdata/backend/store"
)

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
	instanceService := apiv1.NewInstanceService(stores, dbFactory, schemaSync)
	databaseService := apiv1.NewDatabaseService(stores, schemaSync)
	lineageService := apiv1.NewLineageService(stores)
	openLineageService := apiv1.NewOpenLineageService(stores)
	llmService := apiv1.NewLLMService(stores, llmRegistry)
	explainSQLService := apiv1.NewExplainSQLService(stores, llmRegistry)
	settingService := apiv1.NewSettingService(stores, profile)
	roleService := apiv1.NewRoleService(stores)
	groupService := apiv1.NewGroupService(stores)
	iamService := apiv1.NewIamService(stores)

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
			apiv1.NewAuditInterceptor(stores, profile.TrustedProxies),
			apiv1.NewACLInterceptor(iamManager),
			// Innermost, so the audit interceptor records the status the client
			// actually received.
			apiv1.NewErrorMappingInterceptor(),
		),
		connect.WithRecover(onPanic),
	)

	connectHandlers := make(map[string]http.Handler)

	userPath, userHandler := v1connect.NewUserServiceHandler(userService, handlerOpts)
	connectHandlers[userPath] = userHandler
	authPath, authHandler := v1connect.NewAuthServiceHandler(authService, handlerOpts)
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
	rolePath, roleHandler := v1connect.NewRoleServiceHandler(roleService, handlerOpts)
	connectHandlers[rolePath] = roleHandler
	groupPath, groupHandler := v1connect.NewGroupServiceHandler(groupService, handlerOpts)
	connectHandlers[groupPath] = groupHandler
	iamPath, iamHandler := v1connect.NewIamServiceHandler(iamService, handlerOpts)
	connectHandlers[iamPath] = iamHandler
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
		v1connect.RoleServiceName,
		v1connect.GroupServiceName,
		v1connect.IamServiceName,
	)
	reflectPath, reflectHandler := grpcreflect.NewHandlerV1(reflector)
	connectHandlers[reflectPath] = reflectHandler

	reflectAlphaPath, reflectAlphaHandler := grpcreflect.NewHandlerV1Alpha(reflector)
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
	if err := v1pb.RegisterRoleServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterGroupServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterIamServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}

	// Register OpenLineage event ingestion HTTP handler (plain REST, not ConnectRPC).
	olHandler := apiv1.NewOpenLineageHandler(stores)
	olGroup := e.Group("/api/v1/lineage")
	olHandler.RegisterRoutes(olGroup)

	e.Any("/v1/*", echo.WrapHandler(mux))

	// Register Connect RPC handlers
	for path, handler := range connectHandlers {
		e.Any(path+"*", echo.WrapHandler(handler))
	}

	return nil
}
