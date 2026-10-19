package dataquality

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	mssql "github.com/microsoft/go-mssqldb"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
)

// A nil error is not a failure: there is nothing to record.
func TestClassifyErrorIgnoresNil(t *testing.T) {
	t.Parallel()

	code, message := ClassifyError(nil)
	require.Empty(t, code)
	require.Empty(t, message)
}

// The codes are persisted and mapped to localized text, so each classification is
// pinned independently of the driver that produced it.
func TestClassifyErrorCodes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{
			name: "bad data keeps its own code",
			err:  badData("the query result has no %q column", "total_rows"),
			want: ErrCodeBadData,
		},
		{
			name: "no read-only data source",
			err:  fmt.Errorf("instance %q: %w", "instances/ins1", dbfactory.ErrNoReadOnlyDataSource),
			want: ErrCodeNoReadOnly,
		},
		{
			name: "deadline exceeded",
			err:  fmt.Errorf("query failed: %w", context.DeadlineExceeded),
			want: ErrCodeTimeout,
		},
		{
			name: "context canceled",
			err:  context.Canceled,
			want: ErrCodeTimeout,
		},
		{
			name: "postgres invalid password",
			err:  &pgconn.PgError{Code: "28P01", Message: "password authentication failed"},
			want: ErrCodeAuth,
		},
		{
			name: "postgres invalid authorization",
			err:  &pgconn.PgError{Code: "28000"},
			want: ErrCodeAuth,
		},
		{
			name: "postgres insufficient privilege",
			err:  &pgconn.PgError{Code: "42501"},
			want: ErrCodePermission,
		},
		{
			name: "postgres missing database",
			err:  &pgconn.PgError{Code: "3D000"},
			want: ErrCodeConnection,
		},
		{
			name: "postgres connection failure",
			err:  &pgconn.PgError{Code: "08006"},
			want: ErrCodeConnection,
		},
		{
			name: "postgres too many connections",
			err:  &pgconn.PgError{Code: "53300"},
			want: ErrCodeConnection,
		},
		{
			name: "postgres statement timeout",
			err:  &pgconn.PgError{Code: "57014"},
			want: ErrCodeTimeout,
		},
		{
			name: "postgres out of memory",
			err:  &pgconn.PgError{Code: "53200"},
			want: ErrCodeLimitExceeded,
		},
		{
			name: "mysql access denied",
			err:  &mysql.MySQLError{Number: 1045, Message: "Access denied"},
			want: ErrCodeAuth,
		},
		{
			name: "mysql database access denied",
			err:  &mysql.MySQLError{Number: 1044},
			want: ErrCodePermission,
		},
		{
			name: "mysql table access denied",
			err:  &mysql.MySQLError{Number: 1142},
			want: ErrCodePermission,
		},
		{
			name: "mysql cannot connect",
			err:  &mysql.MySQLError{Number: 2003},
			want: ErrCodeConnection,
		},
		{
			name: "mysql server gone",
			err:  &mysql.MySQLError{Number: 2006},
			want: ErrCodeConnection,
		},
		{
			name: "mysql max execution time",
			err:  &mysql.MySQLError{Number: 3024},
			want: ErrCodeTimeout,
		},
		{
			name: "sql server login failed",
			err:  mssql.Error{Number: 18456, Message: "Login failed for user"},
			want: ErrCodeAuth,
		},
		{
			name: "sql server permission denied",
			err:  mssql.Error{Number: 229},
			want: ErrCodePermission,
		},
		{
			name: "sql server cannot open database",
			err:  mssql.Error{Number: 4060},
			want: ErrCodeConnection,
		},
		{
			name: "sql server lock timeout",
			err:  mssql.Error{Number: 1222},
			want: ErrCodeTimeout,
		},
		{
			name: "dial failure in text",
			err:  errors.New("dial tcp 10.0.0.1:3306: connect: connection refused"),
			want: ErrCodeConnection,
		},
		{
			name: "io timeout in text",
			err:  errors.New("read tcp 10.0.0.1:3306: i/o timeout"),
			want: ErrCodeTimeout,
		},
		{
			name: "access denied in text",
			err:  errors.New("Access denied for user 'dq'@'%'"),
			want: ErrCodeAuth,
		},
		{
			name: "permission denied in text",
			err:  errors.New("permission denied for table orders"),
			want: ErrCodePermission,
		},
		{
			name: "unclassified driver error",
			err:  errors.New("column \"amount\" does not exist"),
			want: ErrCodeExecution,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			code, message := ClassifyError(test.err)
			require.Equal(t, test.want, code)
			require.NotEmpty(t, message)
		})
	}
}

// A classification survives wrapping: the driver error reaches the classifier
// through the layers that add context.
func TestClassifyErrorUnwraps(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("execute rule 7: %w", fmt.Errorf("query: %w", &pgconn.PgError{Code: "42501"}))
	code, _ := ClassifyError(wrapped)
	require.Equal(t, ErrCodePermission, code)
}

// A classified failure reports a fixed message: the driver's text can echo a
// connection string, and a classified code is enough for the UI.
func TestClassifyErrorDoesNotEchoClassifiedDriverText(t *testing.T) {
	t.Parallel()

	code, message := ClassifyError(&pgconn.PgError{
		Code:    "28P01",
		Message: "password authentication failed for user \"dq\" (password=hunter2)",
	})
	require.Equal(t, ErrCodeAuth, code)
	require.NotContains(t, message, "hunter2")
	require.NotContains(t, message, "password=")
	require.Equal(t, classMessage(ErrCodeAuth), message)
}

// An unclassified error keeps its text because it is the only explanation, but a
// DSN password and a password=… fragment are replaced first.
func TestClassifyErrorSanitizesUnclassifiedText(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		err     error
		secrets []string
		want    string
	}{
		{
			name:    "password assignment",
			err:     errors.New(`driver rejected the statement: host=db user=dq password=hunter2 database=shop`),
			secrets: []string{"hunter2"},
			want:    "password=***",
		},
		{
			name:    "dsn credentials",
			err:     errors.New("parse failed for postgres://dq:hunter2@db:5432/shop?sslmode=disable"),
			secrets: []string{"hunter2"},
			want:    "dq:***@",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			code, message := ClassifyError(test.err)
			require.Equal(t, ErrCodeExecution, code)
			for _, secret := range test.secrets {
				require.NotContains(t, message, secret)
			}
			require.Contains(t, message, test.want)
		})
	}
}

// The stored message is bounded so one verbose driver error cannot grow the row.
func TestClassifyErrorTruncatesTheMessage(t *testing.T) {
	t.Parallel()

	_, message := ClassifyError(errors.New(strings.Repeat("é", maxErrorMessageBytes)))
	require.LessOrEqual(t, len(message), maxErrorMessageBytes)
	require.True(t, utf8.ValidString(message))
	require.NotEmpty(t, message)
}

// classMessage has a fallback for every code this build can return, so a stored
// result never carries an empty localized message.
func TestClassMessageCoversEveryCode(t *testing.T) {
	t.Parallel()

	codes := []string{
		ErrCodeConnection, ErrCodeAuth, ErrCodeTimeout, ErrCodePermission,
		ErrCodeUnsupported, ErrCodeSQLSyntax, ErrCodeBadData, ErrCodeLimitExceeded,
		ErrCodeOrphaned, ErrCodeQuota, ErrCodeNoReadOnly, ErrCodeExecution,
	}
	for _, code := range codes {
		require.NotEmptyf(t, classMessage(code), "code %s has no message", code)
	}
}
