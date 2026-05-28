package v1

import (
	"context"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/cel-go/cel"
	celast "github.com/google/cel-go/common/ast"
	celoperators "github.com/google/cel-go/common/operators"
	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type AuditLogService struct {
	v1connect.UnimplementedAuditLogServiceHandler
	store *store.Store
}

func NewAuditLogService(store *store.Store) *AuditLogService {
	return &AuditLogService{store: store}
}

func (s *AuditLogService) ListAuditLogs(ctx context.Context, req *connect.Request[v1pb.ListAuditLogsRequest]) (*connect.Response[v1pb.ListAuditLogsResponse], error) {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authenticated user not found"))
	}
	isAdmin, err := isUserWorkspaceAdmin(ctx, s.store, user)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check workspace role"))
	}
	if !isAdmin {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only workspace admins can list audit logs"))
	}

	parent := strings.TrimSpace(req.Msg.GetParent())
	if parent == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("parent is required"))
	}
	if parent == "workspaces/-" {
		parent = ""
	}

	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.GetPageToken(),
		limit:   int(req.Msg.GetPageSize()),
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	find := &store.FindAuditLogMessage{
		Parent: &parent,
		Limit:  &limitPlusOne,
		Offset: &offset.offset,
	}
	filter, err := parseAuditLogFilter(req.Msg.GetFilter())
	if err != nil {
		return nil, err
	}
	find.Filter = filter

	auditLogs, err := s.store.SearchAuditLogs(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list audit logs"))
	}

	response := &v1pb.ListAuditLogsResponse{}
	if len(auditLogs) == limitPlusOne {
		auditLogs = auditLogs[:offset.limit]
		response.NextPageToken, err = offset.getNextPageToken()
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to marshal next page token"))
		}
	}
	for _, auditLog := range auditLogs {
		response.AuditLogs = append(response.AuditLogs, convertToV1AuditLog(auditLog))
	}

	return connect.NewResponse(response), nil
}

func parseAuditLogFilter(filter string) (*store.ListResourceFilter, error) {
	if strings.TrimSpace(filter) == "" {
		return nil, nil
	}

	env, err := cel.NewEnv()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create cel env"))
	}
	ast, iss := env.Parse(filter)
	if iss != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("failed to parse filter %q, error: %v", filter, iss.String()))
	}

	var positionalArgs []any
	parseToSQL := func(variable string, value any, operator OperatorType) (string, error) {
		switch variable {
		case "resource", "method", "user":
			stringValue, ok := value.(string)
			if !ok {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("expect string value for %q", variable))
			}
			if operator != ComparatorTypeEqual {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("only equality filter is supported for %q", variable))
			}
			positionalArgs = append(positionalArgs, stringValue)
			return fmt.Sprintf("payload->>'%s' = $%d", variable, len(positionalArgs)), nil
		case "severity":
			stringValue, ok := value.(string)
			if !ok {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.New("severity must be a string literal"))
			}
			if _, ok := v1pb.AuditLogSeverity_value[stringValue]; !ok {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid severity %q", stringValue))
			}
			if operator != ComparatorTypeEqual {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.New("severity only supports equality filter"))
			}
			positionalArgs = append(positionalArgs, stringValue)
			return fmt.Sprintf("payload->>'severity' = $%d", len(positionalArgs)), nil
		case "create_time":
			stringValue, ok := value.(string)
			if !ok {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.New("create_time must be a string literal in RFC3339 format"))
			}
			parsedTime, err := time.Parse(time.RFC3339, stringValue)
			if err != nil {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "invalid create_time filter"))
			}
			if operator != ComparatorTypeEqual && operator != ComparatorTypeGreaterEqual && operator != ComparatorTypeLessEqual {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.New("create_time only supports =, >=, <= operators"))
			}
			positionalArgs = append(positionalArgs, parsedTime)
			return fmt.Sprintf("created_at %s $%d", operator, len(positionalArgs)), nil
		default:
			return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported audit log filter field %q", variable))
		}
	}

	var getFilter func(expr celast.Expr) (string, error)
	getFilter = func(expr celast.Expr) (string, error) {
		switch expr.Kind() {
		case celast.CallKind:
			switch expr.AsCall().FunctionName() {
			case celoperators.LogicalOr:
				return getSubConditionFromExpr(expr, getFilter, "OR")
			case celoperators.LogicalAnd:
				return getSubConditionFromExpr(expr, getFilter, "AND")
			case celoperators.Equals:
				variable, value := getVariableAndValueFromExpr(expr)
				return parseToSQL(variable, value, ComparatorTypeEqual)
			case celoperators.GreaterEquals:
				variable, value := getVariableAndValueFromExpr(expr)
				return parseToSQL(variable, value, ComparatorTypeGreaterEqual)
			case celoperators.LessEquals:
				variable, value := getVariableAndValueFromExpr(expr)
				return parseToSQL(variable, value, ComparatorTypeLessEqual)
			default:
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported audit log filter operator %q", expr.AsCall().FunctionName()))
			}
		default:
			return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unexpected audit log filter expr kind %v", expr.Kind()))
		}
	}

	where, err := getFilter(ast.NativeRep().Expr())
	if err != nil {
		return nil, err
	}

	return &store.ListResourceFilter{Args: positionalArgs, Where: "(" + where + ")"}, nil
}

func convertToV1AuditLog(auditLog *storepb.AuditLog) *v1pb.AuditLog {
	if auditLog == nil {
		return nil
	}
	return &v1pb.AuditLog{
		Name:            fmt.Sprintf("%s/auditLogs/%d", auditLog.GetParent(), auditLog.GetId()),
		CreateTime:      auditLog.GetCreateTime(),
		Parent:          auditLog.GetParent(),
		Method:          auditLog.GetMethod(),
		Resource:        auditLog.GetResource(),
		User:            auditLog.GetUser(),
		Severity:        convertAuditSeverity(auditLog.GetSeverity()),
		Request:         auditLog.GetRequest(),
		Response:        auditLog.GetResponse(),
		Status:          convertAuditStatus(auditLog.GetStatus()),
		LatencyMs:       auditLog.GetLatencyMs(),
		ServiceData:     auditLog.GetServiceData(),
		RequestMetadata: convertAuditRequestMetadata(auditLog.GetRequestMetadata()),
	}
}

func convertAuditSeverity(severity storepb.AuditSeverity) v1pb.AuditLogSeverity {
	switch severity {
	case storepb.AuditSeverity_INFO:
		return v1pb.AuditLogSeverity_INFO
	case storepb.AuditSeverity_WARNING:
		return v1pb.AuditLogSeverity_WARNING
	case storepb.AuditSeverity_ERROR:
		return v1pb.AuditLogSeverity_ERROR
	default:
		return v1pb.AuditLogSeverity_AUDIT_LOG_SEVERITY_UNSPECIFIED
	}
}

func convertAuditStatus(status *storepb.AuditStatus) *v1pb.AuditLogStatus {
	if status == nil {
		return nil
	}
	return &v1pb.AuditLogStatus{Code: status.GetCode(), Message: status.GetMessage()}
}

func convertAuditRequestMetadata(metadata *storepb.RequestMetadata) *v1pb.AuditRequestMetadata {
	if metadata == nil {
		return nil
	}
	return &v1pb.AuditRequestMetadata{Ip: metadata.GetIp(), UserAgent: metadata.GetUserAgent()}
}
