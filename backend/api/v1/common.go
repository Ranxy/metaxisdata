package v1

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	celast "github.com/google/cel-go/common/ast"
	celoverloads "github.com/google/cel-go/common/overloads"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type OperatorType string

const (
	ComparatorTypeEqual        OperatorType = "="
	ComparatorTypeLessEqual    OperatorType = "<="
	ComparatorTypeGreaterEqual OperatorType = ">="
)

var (
	deletePatch   = true
	undeletePatch = false
)

// likePatternEscaper escapes LIKE wildcards in a literal so that it only
// matches itself.
var likePatternEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// likePattern wraps a literal in LIKE wildcards, escaping any wildcard it
// contains. Use it together with a parameterized "LIKE $n" predicate.
func likePattern(value string) string {
	return "%" + likePatternEscaper.Replace(value) + "%"
}

func convertDeletedToState(deleted bool) v1pb.State {
	if deleted {
		return v1pb.State_DELETED
	}
	return v1pb.State_ACTIVE
}

func isValidResourceID(resourceID string) bool {
	return common.IsValidResourceID(resourceID)
}

func convertToEngine(engine storepb.Engine) v1pb.Engine {
	switch engine {
	case storepb.Engine_CLICKHOUSE:
		return v1pb.Engine_CLICKHOUSE
	case storepb.Engine_MYSQL:
		return v1pb.Engine_MYSQL
	case storepb.Engine_POSTGRES:
		return v1pb.Engine_POSTGRES
	case storepb.Engine_SNOWFLAKE:
		return v1pb.Engine_SNOWFLAKE
	case storepb.Engine_SQLITE:
		return v1pb.Engine_SQLITE
	case storepb.Engine_TIDB:
		return v1pb.Engine_TIDB
	case storepb.Engine_MONGODB:
		return v1pb.Engine_MONGODB
	case storepb.Engine_REDIS:
		return v1pb.Engine_REDIS
	case storepb.Engine_ORACLE:
		return v1pb.Engine_ORACLE
	case storepb.Engine_SPANNER:
		return v1pb.Engine_SPANNER
	case storepb.Engine_MSSQL:
		return v1pb.Engine_MSSQL
	case storepb.Engine_REDSHIFT:
		return v1pb.Engine_REDSHIFT
	case storepb.Engine_MARIADB:
		return v1pb.Engine_MARIADB
	case storepb.Engine_OCEANBASE:
		return v1pb.Engine_OCEANBASE
	case storepb.Engine_STARROCKS:
		return v1pb.Engine_STARROCKS
	case storepb.Engine_DORIS:
		return v1pb.Engine_DORIS
	case storepb.Engine_HIVE:
		return v1pb.Engine_HIVE
	case storepb.Engine_ELASTICSEARCH:
		return v1pb.Engine_ELASTICSEARCH
	case storepb.Engine_BIGQUERY:
		return v1pb.Engine_BIGQUERY
	case storepb.Engine_DYNAMODB:
		return v1pb.Engine_DYNAMODB
	case storepb.Engine_DATABRICKS:
		return v1pb.Engine_DATABRICKS
	case storepb.Engine_COCKROACHDB:
		return v1pb.Engine_COCKROACHDB
	case storepb.Engine_COSMOSDB:
		return v1pb.Engine_COSMOSDB
	case storepb.Engine_CASSANDRA:
		return v1pb.Engine_CASSANDRA
	case storepb.Engine_TRINO:
		return v1pb.Engine_TRINO
	default:
	}
	return v1pb.Engine_ENGINE_UNSPECIFIED
}

func convertEngine(engine v1pb.Engine) storepb.Engine {
	switch engine {
	case v1pb.Engine_CLICKHOUSE:
		return storepb.Engine_CLICKHOUSE
	case v1pb.Engine_MYSQL:
		return storepb.Engine_MYSQL
	case v1pb.Engine_POSTGRES:
		return storepb.Engine_POSTGRES
	case v1pb.Engine_SNOWFLAKE:
		return storepb.Engine_SNOWFLAKE
	case v1pb.Engine_SQLITE:
		return storepb.Engine_SQLITE
	case v1pb.Engine_TIDB:
		return storepb.Engine_TIDB
	case v1pb.Engine_MONGODB:
		return storepb.Engine_MONGODB
	case v1pb.Engine_REDIS:
		return storepb.Engine_REDIS
	case v1pb.Engine_ORACLE:
		return storepb.Engine_ORACLE
	case v1pb.Engine_SPANNER:
		return storepb.Engine_SPANNER
	case v1pb.Engine_MSSQL:
		return storepb.Engine_MSSQL
	case v1pb.Engine_REDSHIFT:
		return storepb.Engine_REDSHIFT
	case v1pb.Engine_MARIADB:
		return storepb.Engine_MARIADB
	case v1pb.Engine_OCEANBASE:
		return storepb.Engine_OCEANBASE
	case v1pb.Engine_STARROCKS:
		return storepb.Engine_STARROCKS
	case v1pb.Engine_DORIS:
		return storepb.Engine_DORIS
	case v1pb.Engine_HIVE:
		return storepb.Engine_HIVE
	case v1pb.Engine_ELASTICSEARCH:
		return storepb.Engine_ELASTICSEARCH
	case v1pb.Engine_BIGQUERY:
		return storepb.Engine_BIGQUERY
	case v1pb.Engine_DYNAMODB:
		return storepb.Engine_DYNAMODB
	case v1pb.Engine_DATABRICKS:
		return storepb.Engine_DATABRICKS
	case v1pb.Engine_COCKROACHDB:
		return storepb.Engine_COCKROACHDB
	case v1pb.Engine_COSMOSDB:
		return storepb.Engine_COSMOSDB
	case v1pb.Engine_CASSANDRA:
		return storepb.Engine_CASSANDRA
	case v1pb.Engine_TRINO:
		return storepb.Engine_TRINO
	default:
	}
	return storepb.Engine_ENGINE_UNSPECIFIED
}

func marshalPageToken(pageToken *storepb.PageToken) (string, error) {
	b, err := proto.Marshal(pageToken)
	if err != nil {
		return "", errors.Wrapf(err, "failed to marshal page token")
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func unmarshalPageToken(s string, pageToken *storepb.PageToken) error {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return errors.Wrapf(err, "failed to decode page token")
	}
	if err := proto.Unmarshal(b, pageToken); err != nil {
		return errors.Wrapf(err, "failed to unmarshal page token")
	}
	return nil
}

type pageSize struct {
	token   string
	limit   int
	maximum int
}

type pageOffset struct {
	limit  int
	offset int
}

func (p *pageOffset) getNextPageToken() (string, error) {
	return marshalPageToken(&storepb.PageToken{
		Limit:  int32(p.limit),
		Offset: int32(p.offset + p.limit),
	})
}

func parseLimitAndOffset(size *pageSize) (*pageOffset, error) {
	offset := &pageOffset{}
	if size.token != "" {
		var token storepb.PageToken
		if err := unmarshalPageToken(size.token, &token); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid page token"))
		}
		if token.Limit < 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("page size cannot be negative"))
		}
		offset.limit = int(size.limit)
		offset.offset = int(token.Offset)
	} else {
		offset.limit = int(size.limit)
	}
	if offset.limit <= 0 {
		offset.limit = 10
	}
	if offset.limit > size.maximum {
		offset.limit = size.maximum
	}
	return offset, nil
}

// connectErrorForWrite maps a store write error to a Connect error. A duplicate
// key (a store common.Conflict) is reported as CodeAlreadyExists so a concurrent
// duplicate registration surfaces as a conflict instead of a 500; every other
// store error stays internal. A general common.Code -> Connect mapping does not
// exist yet, so this covers the one case callers act on.
func connectErrorForWrite(err error, message string) error {
	if common.ErrorCode(err) == common.Conflict {
		return connect.NewError(connect.CodeAlreadyExists, errors.Wrap(err, message))
	}
	return connect.NewError(connect.CodeInternal, errors.Wrap(err, message))
}

// getDatabaseMessage retrieves a database by parsing the database resource name.
// This is a common utility function to avoid code duplication across services.
func getDatabaseMessage(ctx context.Context, s *store.Store, databaseResourceName string) (*store.DatabaseMessage, error) {
	instanceID, databaseName, err := common.GetInstanceDatabaseID(databaseResourceName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse %q", databaseResourceName)
	}

	instance, err := s.GetInstanceV2(ctx, &store.FindInstanceMessage{ResourceID: &instanceID})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get instance %s", instanceID)
	}
	if instance == nil {
		return nil, errors.Errorf("instance not found")
	}

	find := &store.FindDatabaseMessage{
		InstanceID:      &instanceID,
		DatabaseName:    &databaseName,
		IsCaseSensitive: store.IsObjectCaseSensitive(instance),
		ShowDeleted:     true,
	}
	database, err := s.GetDatabaseV2(ctx, find)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get database")
	}
	if database == nil {
		return nil, errors.Errorf("database %q not found", databaseResourceName)
	}
	return database, nil
}

func GetUserFromContext(ctx context.Context) (*store.UserMessage, bool) {
	user, ok := ctx.Value(common.UserContextKey).(*store.UserMessage)
	return user, ok
}

func getSubConditionFromExpr(expr celast.Expr, getFilter func(expr celast.Expr) (string, error), join string) (string, error) {
	var args []string
	for _, arg := range expr.AsCall().Args() {
		s, err := getFilter(arg)
		if err != nil {
			return "", err
		}
		args = append(args, "("+s+")")
	}
	return strings.Join(args, fmt.Sprintf(" %s ", join)), nil
}

// getVariableAndValueFromExpr extracts the variable and the literal operand of
// a simple filter comparison such as `name == "x"` or `engine in ["MYSQL"]`.
// Any other shape is rejected as InvalidArgument.
func getVariableAndValueFromExpr(expr celast.Expr) (string, any, error) {
	var variable string
	var value any
	for _, arg := range expr.AsCall().Args() {
		switch arg.Kind() {
		case celast.IdentKind:
			variable = arg.AsIdent()
		case celast.SelectKind:
			// Handle member selection like "labels.environment"
			sel := arg.AsSelect()
			if sel.Operand().Kind() == celast.IdentKind {
				variable = fmt.Sprintf("%s.%s", sel.Operand().AsIdent(), sel.FieldName())
			}
		case celast.LiteralKind:
			value = arg.AsLiteral().Value()
		case celast.ListKind:
			list := []any{}
			for _, e := range arg.AsList().Elements() {
				if e.Kind() == celast.LiteralKind {
					list = append(list, e.AsLiteral().Value())
				}
			}
			value = list
		default:
		}
	}
	if variable == "" {
		return "", nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("expect a filter variable"))
	}
	if value == nil {
		return "", nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("expect a literal value for %q", variable))
	}
	return variable, value, nil
}

// filterString extracts a string literal operand. CEL literals are returned as
// untyped any, so asserting without checking panics the whole request.
func filterString(variable string, value any) (string, error) {
	v, ok := value.(string)
	if !ok {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid %s filter: expect a string literal, got %T", variable, value))
	}
	return v, nil
}

// filterBool extracts a bool literal operand.
func filterBool(variable string, value any) (bool, error) {
	v, ok := value.(bool)
	if !ok {
		return false, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid %s filter: expect a bool literal, got %T", variable, value))
	}
	return v, nil
}

// filterStringList extracts a non-empty list of string literal operands, as
// used by `engine in ["MYSQL", "POSTGRES"]`.
func filterStringList(variable string, value any) ([]string, error) {
	raw, ok := value.([]any)
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid %s filter: expect a list literal, got %T", variable, value))
	}
	if len(raw) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("empty %s filter", variable))
	}
	list := make([]string, 0, len(raw))
	for _, item := range raw {
		v, ok := item.(string)
		if !ok {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid %s filter: expect string elements, got %T", variable, item))
		}
		list = append(list, v)
	}
	return list, nil
}

// matchArgs extracts the identifier and the literal operand of a
// `x.matches("y")` call. CallExpr.Target() is nil for a non-receiver call and
// Expr.AsLiteral() returns nil for a non-literal, so neither may be
// dereferenced unguarded.
func matchArgs(expr celast.Expr) (string, any, error) {
	target := expr.AsCall().Target()
	if target == nil || target.Kind() != celast.IdentKind {
		return "", nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("expect an identifier before %q", celoverloads.Matches))
	}
	args := expr.AsCall().Args()
	if len(args) != 1 {
		return "", nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid args for %q", target.AsIdent()))
	}
	if args[0].Kind() != celast.LiteralKind {
		return "", nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("expect a literal argument for %q", celoverloads.Matches))
	}
	return target.AsIdent(), args[0].AsLiteral().Value(), nil
}
