package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	"github.com/Ranxy/metaxisdata/backend/api/oauth"
	"github.com/Ranxy/metaxisdata/backend/common"
	clog "github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/component/audit"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The read services the tools call. The concrete ConnectRPC services in
// backend/api/v1 satisfy them, so a tool runs exactly the code the API runs —
// same validation, same store access, same conversion — and a test can put a
// fake in their place. The method signatures are the generated handler ones on
// purpose: that is what makes the substitution check happen at compile time.
type (
	// InstanceReader is the slice of InstanceService the tools use.
	InstanceReader interface {
		ListInstances(context.Context, *connect.Request[v1pb.ListInstancesRequest]) (*connect.Response[v1pb.ListInstancesResponse], error)
	}
	// DatabaseReader is the slice of DatabaseService the tools use.
	DatabaseReader interface {
		ListDatabases(context.Context, *connect.Request[v1pb.ListDatabasesRequest]) (*connect.Response[v1pb.ListDatabasesResponse], error)
		ListMetadata(context.Context, *connect.Request[v1pb.ListMetadataRequest]) (*connect.Response[v1pb.MetadataResponse], error)
		GetMetadata(context.Context, *connect.Request[v1pb.GetMetadataRequest]) (*connect.Response[v1pb.GetMetadataResponse], error)
		SearchMetadata(context.Context, *connect.Request[v1pb.SearchMetadataRequest]) (*connect.Response[v1pb.SearchMetadataResponse], error)
		GetSchemaString(context.Context, *connect.Request[v1pb.GetSchemaStringRequest]) (*connect.Response[v1pb.MetadataSchemaString], error)
	}
	// LineageReader is the slice of LineageService the tools use.
	LineageReader interface {
		AnalyzeSQL(context.Context, *connect.Request[v1pb.AnalyzeSQLRequest]) (*connect.Response[v1pb.AnalyzeSQLResponse], error)
		GetLineageGraph(context.Context, *connect.Request[v1pb.GetLineageGraphRequest]) (*connect.Response[v1pb.GetLineageGraphResponse], error)
	}
	// PrincipalReader is the slice of UserService the tools use.
	PrincipalReader interface {
		GetCurrentUser(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[v1pb.User], error)
	}
)

// PermissionChecker resolves whether a caller holds a permission. *iam.Manager is
// the production implementation; the interface lets a test decide the answer.
type PermissionChecker interface {
	CheckPermission(ctx context.Context, perm permission.Permission, user *store.UserMessage) (bool, error)
}

const (
	serverName = "metaxisdata"
	// maxRequestBytes bounds one tool call. The largest legitimate payload is a
	// SQL statement, which the AnalyzeSQL RPC itself caps at 1 MiB.
	maxRequestBytes = 2 << 20

	// tokenClockSkew tolerates the few seconds two replicas' clocks may differ by
	// before calling a token expired.
	tokenClockSkew = 30 * time.Second

	// instructions is what a client shows its model before the first call. It is
	// short on purpose: the tool descriptions carry the detail, and some clients
	// re-send this with every request.
	instructions = `metaxisdata is a data governance registry: it knows the schemas of the databases synced into it, and the column-level lineage between their tables. Every tool here reads; nothing writes.

Three rules decide whether a call works:

1. Address objects by name. An object is {instance, database, schema?, name}, and each value is either a name or the id a listing returned. A guid from an earlier result works too, but you never need to build one.
2. Analyze a SQL statement against an explicit scope. A statement's unqualified names cannot say which instance they belong to, so analyze_sql takes scopes: [{instance, database, schema?}]. Without one it answers with the databases that exist rather than guessing.
3. A failure is a question to rephrase, not a change to undo: the error envelope carries a code, a hint, and — when a name was unknown or ambiguous — the candidates it could have meant.

Which tool answers what: analyze_sql for a statement you have in hand; get_lineage_graph for an object already in the registry; search_metadata when you only have a name; get_metadata and get_ddl for its shape.`
)

// Config is everything the MCP server needs. Services are required; Stores and
// TrustedProxies are only used to write the per-call audit row, and Endpoints
// decides whether the surface is served at all.
type Config struct {
	Instances  InstanceReader
	Databases  DatabaseReader
	Lineage    LineageReader
	Principals PrincipalReader
	Checker    PermissionChecker
	Stores     *store.Store
	// TrustedProxies decides whether a forwarded address may be believed in the
	// audit row.
	TrustedProxies []string
	// Endpoints resolves the deployment's MCP resource identifier per request and
	// says whether the surface is enabled.
	Endpoints oauth.EndpointsFunc
	// Now lets a test pin the clock the audit rows measure against.
	Now func() time.Time
}

// Server is the MCP resource server: one stateless endpoint over the read
// services, with the tools in tool.go.
type Server struct {
	config Config
	sdk    *mcpsdk.Server
	// audit writes one ledger row per call. It is a field rather than a method call
	// so a test can observe the ledger without a database; NewServer points it at
	// the store-backed writer.
	audit func(ctx context.Context, request *mcpsdk.CallToolRequest, definition toolDefinition, user *store.UserMessage, failure error, started time.Time)
}

// NewServer builds the MCP server, registers its tools and its guide prompt.
func NewServer(config Config) *Server {
	if config.Now == nil {
		config.Now = time.Now
	}
	server := &Server{config: config}
	server.audit = server.auditToolCall
	server.sdk = mcpsdk.NewServer(
		// No version: this repository has no build version to report, and an empty
		// string would be a claim about one.
		&mcpsdk.Implementation{Name: serverName},
		&mcpsdk.ServerOptions{
			Instructions: instructions,
			// The default capability set advertises the legacy logging feature,
			// which the protocol retires; nothing here logs to the client.
			Capabilities: &mcpsdk.ServerCapabilities{},
		},
	)
	server.registerTools()
	server.registerGuide()
	return server
}

// Handler returns the fully wired MCP endpoint: the workspace gate, the bearer
// check and the stateless transport. The caller mounts it and wraps it with the
// standard library's cross-origin protection.
func (s *Server) Handler(tokens *auth.TokenAuthenticator) http.Handler {
	streamable := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return s.sdk },
		&mcpsdk.StreamableHTTPOptions{
			Stateless: true,
			// Plain JSON answers instead of an event stream: every tool is a
			// request-response, and a stream would only add a channel to test and
			// to time out.
			JSONResponse:        true,
			MaxRequestBodyBytes: maxRequestBytes,
		},
	)
	// The SDK reads the challenge's resource_metadata URL once, at construction,
	// while it is derived from a workspace setting that can change. So the
	// middleware adds only the scope, and challengeWriter adds the URL per
	// request.
	bearer := sdkauth.RequireBearerToken(NewTokenVerifier(s.config.Endpoints, tokens), &sdkauth.RequireBearerTokenOptions{
		Scopes:    []string{oauth.MCPReadScope},
		ClockSkew: tokenClockSkew,
	})(streamable)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoints, enabled, err := s.config.Endpoints(r.Context())
		if err != nil {
			http.Error(w, "the MCP endpoint is not available", http.StatusServiceUnavailable)
			return
		}
		if !enabled {
			// A switched-off surface must not advertise that it exists.
			http.NotFound(w, r)
			return
		}
		bearer.ServeHTTP(&challengeWriter{
			ResponseWriter:      w,
			resourceMetadataURL: endpoints.Issuer + oauth.ProtectedResourcePath,
		}, r)
	})
}

// challengeWriter completes the challenge RFC 9728 and the MCP specification ask
// for. The SDK's middleware writes the scope but neither the metadata URL (it
// cannot know the setting changed) nor the RFC 6750 error code, so both are added
// here, where the current deployment identifiers are known.
type challengeWriter struct {
	http.ResponseWriter
	resourceMetadataURL string
}

// challengeErrors maps the status a rejected request was answered with onto the
// RFC 6750 error code that belongs with it.
var challengeErrors = map[int]string{
	http.StatusUnauthorized: "invalid_token",
	http.StatusForbidden:    "insufficient_scope",
}

func (w *challengeWriter) WriteHeader(status int) {
	if errorCode := challengeErrors[status]; errorCode != "" {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(
			"Bearer resource_metadata=%q, scope=%q, error=%q",
			w.resourceMetadataURL, oauth.MCPReadScope, errorCode,
		))
	}
	w.ResponseWriter.WriteHeader(status)
}

// Unwrap lets http.ResponseController reach the writer underneath. Nothing on
// today's path needs it — the endpoint is stateless and answers JSON — but the SDK
// flushes through a controller, and a wrapper that hid the real writer would
// silently turn that into a no-op.
func (w *challengeWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// toolDefinition is one MCP tool: what a client sees, the permission it requires,
// and how it runs.
//
// RPC names the ConnectRPC method the tool wraps and Permission must equal that
// method's annotation; a guard test asserts both, so a tool cannot quietly widen
// what a caller may do, or wrap a method that writes. A tool that wraps no method
// (whoami answers from the verified principal) leaves RPC and Permission empty and
// is listed in that test.
type toolDefinition struct {
	Name        string
	Description string
	RPC         string
	Permission  string
	Schema      map[string]any
	Run         func(ctx context.Context, user *store.UserMessage, args json.RawMessage) (any, error)
}

func (s *Server) registerTools() {
	for _, definition := range s.toolDefinitions() {
		s.sdk.AddTool(&mcpsdk.Tool{
			Name:        definition.Name,
			Description: definition.Description,
			InputSchema: definition.Schema,
			Annotations: &mcpsdk.ToolAnnotations{
				// Every tool here reads; nothing writes, so nothing is
				// destructive, and nothing reaches outside this deployment.
				ReadOnlyHint:    true,
				IdempotentHint:  true,
				DestructiveHint: boolPointer(false),
				OpenWorldHint:   boolPointer(false),
			},
		}, s.toolHandler(definition))
	}
}

func (s *Server) toolHandler(definition toolDefinition) mcpsdk.ToolHandler {
	return func(ctx context.Context, request *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		result, err := s.dispatch(ctx, request, definition)
		if err != nil {
			return errorResult(err), nil
		}
		return result, nil
	}
}

// dispatch is the one place a tool runs. The audit row is written on **every**
// path, including the refusals: "who tried to read what" is exactly the event a
// ledger exists for, and a refused call is the one worth keeping.
func (s *Server) dispatch(ctx context.Context, request *mcpsdk.CallToolRequest, definition toolDefinition) (*mcpsdk.CallToolResult, error) {
	started := s.config.Now()
	user, _ := UserFromRequest(request)
	result, err := s.invoke(ctx, request, definition, user)
	s.audit(ctx, request, definition, user, err, started)
	return result, err
}

// invoke is the part of a dispatch that can refuse before anything has run.
func (s *Server) invoke(ctx context.Context, request *mcpsdk.CallToolRequest, definition toolDefinition, user *store.UserMessage) (*mcpsdk.CallToolResult, error) {
	if user == nil {
		return nil, newToolError(codeUnauthenticated, "the call carried no verified identity", "authorize the MCP client again")
	}
	if definition.Permission != "" {
		if s.config.Checker == nil {
			return nil, internalFailure(errNoPermissionChecker)
		}
		allowed, err := s.config.Checker.CheckPermission(ctx, definition.Permission, user)
		if err != nil {
			return nil, internalFailure(err)
		}
		if !allowed {
			return nil, permissionDenied(definition.Permission)
		}
	}
	// The handlers a tool calls are the ConnectRPC ones, and they read the caller
	// from the context (GetCurrentUser does). The interceptor puts it there on the
	// RPC path; this is the same step for the tool path, and without it a handler
	// answers as if nobody were signed in.
	ctx = context.WithValue(ctx, common.UserContextKey, user)
	payload, err := definition.Run(ctx, user, request.Params.Arguments)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, internalFailure(err)
	}
	// Structured content only. The SDK's generic tool path would append a text
	// copy of the same JSON; a metadata listing does not need to cross the wire
	// twice, and the Phase 0 contract test measures that difference.
	return &mcpsdk.CallToolResult{StructuredContent: json.RawMessage(encoded)}, nil
}

// auditToolCall records one tool call. It is best effort: a ledger that cannot be
// written must not fail a call the caller is entitled to make.
func (s *Server) auditToolCall(ctx context.Context, request *mcpsdk.CallToolRequest, definition toolDefinition, user *store.UserMessage, failure error, started time.Time) {
	if s.config.Stores == nil {
		return
	}
	auditCtx, cancel := audit.Context(ctx)
	defer cancel()

	workspaceID, err := s.config.Stores.GetWorkspaceID(auditCtx)
	if err != nil {
		slog.Error("failed to resolve the workspace for an MCP audit log", clog.WithError(err))
		return
	}
	// A call refused before the identity was resolved has no actor: the row still
	// says that someone reached the endpoint and was turned away.
	actor := ""
	if user != nil {
		actor = common.FormatUserUID(user.ID)
	}
	status, severity := auditStatus(failure)
	entry := &storepb.AuditLog{
		Parent:          common.FormatWorkspace(workspaceID),
		Method:          "mcp/tools/call:" + definition.Name,
		User:            actor,
		Severity:        severity,
		Status:          status,
		LatencyMs:       time.Since(started).Milliseconds(),
		RequestMetadata: audit.BuildRequestMetadata(requestHeaders(request), PeerAddress(request), s.config.TrustedProxies),
	}
	if args := auditArguments(request.Params.Arguments); args != nil {
		entry.Request = args
	}
	if _, err := s.config.Stores.CreateAuditLog(auditCtx, entry); err != nil {
		slog.Error("failed to persist an MCP audit log", clog.WithError(err))
	}
}

// auditStatus maps a tool failure onto the ledger's status and severity. The
// numeric code stays a ConnectRPC code so an audit reader keeps the vocabulary
// the API uses; the tool's finer-grained code leads the message.
func auditStatus(err error) (*storepb.AuditLogStatus, storepb.AuditLogSeverity) {
	if err == nil {
		return &storepb.AuditLogStatus{Message: "ok"}, storepb.AuditLogSeverity_INFO
	}
	var failure *toolError
	if !errors.As(err, &failure) {
		return &storepb.AuditLogStatus{Code: int32(connect.CodeUnknown), Message: err.Error()}, storepb.AuditLogSeverity_ERROR
	}
	code, severity := connect.CodeInternal, storepb.AuditLogSeverity_ERROR
	switch failure.Code {
	case codeNotFound:
		code, severity = connect.CodeNotFound, storepb.AuditLogSeverity_WARNING
	case codeInvalidArgument, codeAmbiguous, codeScopeRequired, codeResourceExhausted:
		code, severity = connect.CodeInvalidArgument, storepb.AuditLogSeverity_WARNING
	case codeUnsupported:
		code, severity = connect.CodeFailedPrecondition, storepb.AuditLogSeverity_WARNING
	case codeUnauthenticated:
		code, severity = connect.CodeUnauthenticated, storepb.AuditLogSeverity_WARNING
	case codePermissionDenied:
		code, severity = connect.CodePermissionDenied, storepb.AuditLogSeverity_WARNING
	case codeUnavailable:
		code = connect.CodeUnavailable
	case codeTimeout:
		code = connect.CodeDeadlineExceeded
	default:
		// Any other tool code is a fault on our own side, which is what the
		// initial values already say.
	}
	return &storepb.AuditLogStatus{Code: int32(code), Message: failure.Code + ": " + failure.Message}, severity
}

// auditArguments renders a tool's arguments for the ledger. Sensitive field names
// are redacted by the audit package; the arguments themselves are recorded because
// "which question was asked" is the part of a tool call worth keeping.
// maxAuditArgumentBytes bounds one string inside an audit row. A tool call may
// carry a megabyte of SQL and the ledger is kept forever, so the row records what
// was asked without becoming a second copy of the request.
const maxAuditArgumentBytes = 8 << 10

func auditArguments(raw json.RawMessage) *structpb.Struct {
	if len(raw) == 0 {
		return nil
	}
	var arguments any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil
	}
	payload, err := structpb.NewStruct(map[string]any{"arguments": truncateAuditValues(arguments)})
	if err != nil {
		return nil
	}
	return audit.SanitizeAuditStruct(payload)
}

// truncateAuditValues shortens every over-long string in place, wherever it sits
// in the argument tree.
func truncateAuditValues(value any) any {
	switch typed := value.(type) {
	case string:
		if len(typed) > maxAuditArgumentBytes {
			return typed[:maxAuditArgumentBytes] + "...(truncated)"
		}
		return typed
	case []any:
		for index := range typed {
			typed[index] = truncateAuditValues(typed[index])
		}
		return typed
	case map[string]any:
		for key, child := range typed {
			typed[key] = truncateAuditValues(child)
		}
		return typed
	default:
		return value
	}
}

func requestHeaders(request *mcpsdk.CallToolRequest) http.Header {
	if extra := request.GetExtra(); extra != nil {
		return extra.Header
	}
	return nil
}

// decodeArguments parses a tool's arguments. An unknown field is ignored so a
// client may send what a newer schema advertises; a malformed body is an
// invalid_argument failure rather than a protocol error.
func decodeArguments(raw json.RawMessage, destination any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, destination); err != nil {
		return invalidArgument("the arguments are not valid JSON for this tool: %v", err)
	}
	return nil
}

func boolPointer(value bool) *bool { return &value }
