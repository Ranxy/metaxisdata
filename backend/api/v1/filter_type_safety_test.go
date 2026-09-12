package v1

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/store"
)

// CEL literals are untyped, so `name == 123` yields an int64, and both
// Expr.AsLiteral() and CallExpr.Target() return nil for shapes such as a bare
// identifier. Asserting on those values without checking panicked the handler,
// which reached the client as an internal error. Every malformed filter must be
// rejected as InvalidArgument instead.
func TestFilterParsersRejectMistypedOperands(t *testing.T) {
	t.Parallel()

	parseUser := func(filter string) error {
		return parseListUserFilter(&store.FindUserMessage{}, filter)
	}
	parseInstance := func(filter string) error {
		_, err := parseListInstanceFilter(filter)
		return err
	}
	parseDatabase := func(filter string) error {
		_, err := getListDatabaseFilter(filter)
		return err
	}
	parseAudit := func(filter string) error {
		_, err := parseAuditLogFilter(filter)
		return err
	}

	tests := []struct {
		name   string
		parse  func(string) error
		filter string
	}{
		{"user int literal for string field", parseUser, `email == 123`},
		{"user identifier as matches argument", parseUser, `name.matches(email)`},
		{"user matches without receiver", parseUser, `matches("x")`},
		{"user int element in type list", parseUser, `user_type in [1]`},
		{"instance int literal for string field", parseInstance, `name == 123`},
		{"instance bool literal for port", parseInstance, `port == true`},
		{"instance identifier as matches argument", parseInstance, `name.matches(title)`},
		{"instance int element in engine list", parseInstance, `engine in [1]`},
		{"database int literal for name", parseDatabase, `name == 123`},
		{"database string literal for bool field", parseDatabase, `exclude_unassigned == "true"`},
		{"database matches without receiver", parseDatabase, `matches("x")`},
		{"audit int literal for string field", parseAudit, `resource == 123`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.parse(tc.filter)
			require.Error(t, err, "filter %q must be rejected", tc.filter)
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "filter %q", tc.filter)
		})
	}
}
