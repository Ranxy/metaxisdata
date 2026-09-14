package v1

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"strings"

	"connectrpc.com/connect"
	celast "github.com/google/cel-go/common/ast"
	celoverloads "github.com/google/cel-go/common/overloads"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
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

// convertToEngine maps the store engine onto its public counterpart. The two
// enums are value-compatible, and everything the drivers cannot connect to was
// removed from both, so an unknown value can only be a stale row.
func convertToEngine(engine storepb.Engine) v1pb.Engine {
	switch engine {
	case storepb.Engine_MYSQL:
		return v1pb.Engine_MYSQL
	case storepb.Engine_POSTGRES:
		return v1pb.Engine_POSTGRES
	case storepb.Engine_TIDB:
		return v1pb.Engine_TIDB
	case storepb.Engine_MARIADB:
		return v1pb.Engine_MARIADB
	case storepb.Engine_OCEANBASE:
		return v1pb.Engine_OCEANBASE
	case storepb.Engine_STARROCKS:
		return v1pb.Engine_STARROCKS
	case storepb.Engine_DORIS:
		return v1pb.Engine_DORIS
	case storepb.Engine_MSSQL:
		return v1pb.Engine_MSSQL
	default:
	}
	return v1pb.Engine_ENGINE_UNSPECIFIED
}

func convertEngine(engine v1pb.Engine) storepb.Engine {
	switch engine {
	case v1pb.Engine_MYSQL:
		return storepb.Engine_MYSQL
	case v1pb.Engine_POSTGRES:
		return storepb.Engine_POSTGRES
	case v1pb.Engine_TIDB:
		return storepb.Engine_TIDB
	case v1pb.Engine_MARIADB:
		return storepb.Engine_MARIADB
	case v1pb.Engine_OCEANBASE:
		return storepb.Engine_OCEANBASE
	case v1pb.Engine_STARROCKS:
		return storepb.Engine_STARROCKS
	case v1pb.Engine_DORIS:
		return storepb.Engine_DORIS
	case v1pb.Engine_MSSQL:
		return storepb.Engine_MSSQL
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

// maxPageOffset bounds the offset a page token may carry. The token used to hold
// an int32 offset whose addition could wrap negative (falling back to the first
// page); the bound also keeps a forged token from asking the database for an
// absurd OFFSET.
const maxPageOffset = math.MaxInt32

func (p *pageOffset) getNextPageToken() (string, error) {
	next := int64(p.offset) + int64(p.limit)
	if next < 0 || next > maxPageOffset {
		// The caller walked past the representable range; there is no next page.
		return "", nil
	}
	return marshalPageToken(&storepb.PageToken{
		Limit:  int64(p.limit),
		Offset: next,
	})
}

// paginate cuts a limit+1 probe down to the requested page and derives the next
// page token. A probe shorter than limit+1 means the caller reached the end.
func paginate[T any](items []T, offset *pageOffset) ([]T, string, error) {
	if len(items) < offset.limit+1 {
		return items, "", nil
	}
	nextPageToken, err := offset.getNextPageToken()
	if err != nil {
		return nil, "", connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to marshal next page token"))
	}
	return items[:offset.limit], nextPageToken, nil
}

// paginateInMemory pages an already-materialized slice. It is for collections
// assembled after the query (predefined roles merged with custom ones), where
// the limit+1 probe paginate expects cannot be pushed into SQL.
func paginateInMemory[T any](items []T, offset *pageOffset) ([]T, string, error) {
	start := min(offset.offset, len(items))
	end := min(start+offset.limit, len(items))
	if end == len(items) {
		return items[start:end], "", nil
	}
	nextPageToken, err := offset.getNextPageToken()
	if err != nil {
		return nil, "", connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to marshal next page token"))
	}
	return items[start:end], nextPageToken, nil
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
		if token.Offset < 0 || token.Offset > maxPageOffset {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid page token offset"))
		}
		offset.limit = int(size.limit)
		if offset.limit <= 0 {
			// The follow-up request left page_size unset; keep the size the
			// token was issued with instead of silently re-defaulting it, which
			// used to overlap or skip rows.
			offset.limit = int(token.Limit)
		}
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
	if offset.offset < 0 {
		offset.offset = 0
	}
	return offset, nil
}

// getDatabaseMessage retrieves a database by parsing the database resource name.
// This is a common utility function to avoid code duplication across services.
func getDatabaseMessage(ctx context.Context, s *store.Store, databaseResourceName string) (*store.DatabaseMessage, error) {
	instanceID, databaseName, err := common.GetInstanceDatabaseID(databaseResourceName)
	if err != nil {
		return nil, common.Errorf(common.Invalid, "invalid database resource name %q", databaseResourceName)
	}

	instance, err := s.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &instanceID})
	if err != nil {
		return nil, common.Errorf(common.Internal, "failed to get instance %s: %v", instanceID, err)
	}
	if instance == nil {
		return nil, common.Errorf(common.NotFound, "instance %q not found", instanceID)
	}

	find := &store.FindDatabaseMessage{
		InstanceID:      &instanceID,
		DatabaseName:    &databaseName,
		IsCaseSensitive: store.IsObjectCaseSensitive(instance),
		ShowDeleted:     true,
	}
	database, err := s.GetDatabase(ctx, find)
	if err != nil {
		return nil, common.Errorf(common.Internal, "failed to get database %q: %v", databaseResourceName, err)
	}
	if database == nil {
		return nil, common.Errorf(common.NotFound, "database %q not found", databaseResourceName)
	}
	return database, nil
}

func GetUserFromContext(ctx context.Context) (*store.UserMessage, bool) {
	user, ok := ctx.Value(common.UserContextKey).(*store.UserMessage)
	return user, ok
}

// GetTokenRestrictionFromContext returns the restriction carried by the access
// token that authenticated the request. The second result is false for a
// full-access token.
func GetTokenRestrictionFromContext(ctx context.Context) (auth.TokenRestriction, bool) {
	restriction, ok := ctx.Value(common.TokenRestrictionContextKey).(auth.TokenRestriction)
	return restriction, ok
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
