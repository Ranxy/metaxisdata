package dataquality

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	mssql "github.com/microsoft/go-mssqldb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
)

// ErrCodeExecution is recorded for an execution error that matches none of the
// specific classes: the statement was accepted and the target ran it, but it
// failed for a reason this build cannot name (a missing table after a schema
// change, a division by zero, a driver-specific refusal). It is separate from
// SQL_SYNTAX, which is a parse-time code, and from BAD_DATA, which means the
// result set did not match the compiled statement's shape. The frontend maps it
// to a generic localized message.
const ErrCodeExecution = "EXECUTION"

// maxErrorMessageBytes bounds the message a result stores, so one verbose driver
// error cannot grow the row without limit.
const maxErrorMessageBytes = 1024

// errBadData reports that a query result does not match the shape the compiled
// statement declared: a missing column, a value of the wrong type, or an
// aggregate that did not return exactly one row. It is a defect in the engine's
// contract or in the target's schema, never a rule the user can fix.
var errBadData = errors.New("the query result does not match the compiled statement's shape")

// badData builds a BAD_DATA error with a specific reason. ClassifyError maps it
// to ErrCodeBadData, and the reason stays in the server log.
func badData(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errBadData, fmt.Sprintf(format, args...))
}

var (
	// passwordAssignment finds key/value credential forms in a driver message.
	passwordAssignment = regexp.MustCompile(`(?i)\b(password|passwd|pwd)\s*=\s*[^\s;&]+`)
	// urlCredential finds the password of a DSN embedded in a message.
	urlCredential = regexp.MustCompile(`(?i)(://[^:/@\s]+):[^@\s]+@`)
)

// ClassifyError maps one execution error to the stable code a result records and
// a short message safe to show a user. The message never contains the driver's
// own text for a specific class (it can echo a connection string); an
// unclassified error keeps its text, sanitized and truncated, because it is the
// only thing that explains the failure.
//
// A nil error classifies to empty strings: there is nothing to record.
func ClassifyError(err error) (code string, message string) {
	if err == nil {
		return "", ""
	}
	if errors.Is(err, errBadData) {
		return ErrCodeBadData, "the query result did not match the rule's expected shape"
	}
	if errors.Is(err, dbfactory.ErrNoReadOnlyDataSource) {
		return ErrCodeNoReadOnly, "the instance has no read-only data source; add one in the instance settings"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrCodeTimeout, "the query timed out"
	}

	// The drivers' typed errors carry the engine's own code, which is the only
	// reliable classification: matching on message text guesses.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if classified := classifyPostgresCode(pgErr.Code); classified != "" {
			return classified, classMessage(classified)
		}
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		if classified := classifyMySQLNumber(mysqlErr.Number); classified != "" {
			return classified, classMessage(classified)
		}
	}
	// go-mssqldb returns Error values, but a caller may wrap a pointer, so both
	// forms are matched.
	var mssqlErr mssql.Error
	if errors.As(err, &mssqlErr) {
		if classified := classifyMSSQLNumber(mssqlErr.Number); classified != "" {
			return classified, classMessage(classified)
		}
	}
	var mssqlPtr *mssql.Error
	if errors.As(err, &mssqlPtr) && mssqlPtr != nil {
		if classified := classifyMSSQLNumber(mssqlPtr.Number); classified != "" {
			return classified, classMessage(classified)
		}
	}

	if classified := classifyErrorText(strings.ToLower(err.Error())); classified != "" {
		return classified, classMessage(classified)
	}
	return ErrCodeExecution, sanitizeErrorMessage(err.Error())
}

// classMessage is the user-facing description of a classified failure. The
// frontend localizes by code; this text is the fallback for an API consumer.
func classMessage(code string) string {
	switch code {
	case ErrCodeAuth:
		return "authentication to the target database failed"
	case ErrCodePermission:
		return "the target database denied the query"
	case ErrCodeConnection:
		return "the target database is unreachable"
	case ErrCodeTimeout:
		return "the query timed out"
	case ErrCodeLimitExceeded:
		return "the query exceeded its result limit"
	case ErrCodeNoReadOnly:
		return "the instance has no read-only data source; add one in the instance settings"
	case ErrCodeBadData:
		return "the query result did not match the rule's expected shape"
	case ErrCodeExecution, ErrCodeSQLSyntax, ErrCodeUnsupported, ErrCodeOrphaned, ErrCodeQuota:
		return "the query failed"
	}
	return "the query failed"
}

// classifyPostgresCode maps the PostgreSQL SQLSTATE classes a quality query can
// fail with. A code this build does not know stays unclassified rather than
// being guessed at.
func classifyPostgresCode(code string) string {
	switch code {
	case "28P01", "28000":
		// invalid_password, invalid_authorization_specification.
		return ErrCodeAuth
	case "42501":
		// insufficient_privilege.
		return ErrCodePermission
	case "3D000", "08000", "08001", "08003", "08004", "08006", "53300":
		// invalid_catalog_name, connection_exception and its subclasses,
		// too_many_connections.
		return ErrCodeConnection
	case "57014":
		// query_canceled: statement_timeout cancels the statement here rather
		// than surfacing a Go deadline.
		return ErrCodeTimeout
	case "53200", "53100":
		// out_of_memory, disk_full: the engine refused the work, not the query.
		return ErrCodeLimitExceeded
	default:
		return ""
	}
}

// classifyMySQLNumber maps the MySQL server error numbers a quality query can
// fail with.
func classifyMySQLNumber(number uint16) string {
	switch number {
	case 1045:
		// ER_ACCESS_DENIED_ERROR.
		return ErrCodeAuth
	case 1044, 1142, 1227:
		// ER_DBACCESS_DENIED_ERROR, ER_TABLEACCESS_DENIED_ERROR,
		// ER_SPECIFIC_ACCESS_DENIED_ERROR.
		return ErrCodePermission
	case 2003, 2006, 2013, 1049:
		// CR_CONN_HOST_ERROR, CR_SERVER_GONE_ERROR, CR_SERVER_LOST,
		// ER_BAD_DB_ERROR: the connection or the database it names is gone.
		return ErrCodeConnection
	case 3024, 1205, 1213:
		// ER_QUERY_TIMEOUT (max_execution_time), lock wait timeout,
		// deadlock: the work was interrupted, not rejected as invalid.
		return ErrCodeTimeout
	default:
		return ""
	}
}

// classifyMSSQLNumber maps the SQL Server error numbers a quality query can fail
// with.
func classifyMSSQLNumber(number int32) string {
	switch number {
	case 18456, 18452:
		// Login failed for user, login failed from untrusted domain.
		return ErrCodeAuth
	case 229, 230, 262:
		// Permission denied on object/database.
		return ErrCodePermission
	case 4060, 40613, 233, 10054, 10060:
		// Cannot open database, database unavailable, connection init
		// failure, connection reset, connection timeout.
		return ErrCodeConnection
	case 1222:
		// Lock request time out.
		return ErrCodeTimeout
	default:
		return ""
	}
}

// classifyErrorText is the last resort for a driver error that carries no
// structured code: a plain Go net error, or a driver that wraps one in text.
// The matching is deliberately narrow and ordered, so a generic word cannot
// mask a more specific class.
func classifyErrorText(lower string) string {
	for _, marker := range []string{"timed out", "timeout"} {
		if strings.Contains(lower, marker) {
			return ErrCodeTimeout
		}
	}
	for _, marker := range []string{"password authentication failed", "access denied for user", "authentication failed"} {
		if strings.Contains(lower, marker) {
			return ErrCodeAuth
		}
	}
	for _, marker := range []string{"permission denied", "insufficient privilege"} {
		if strings.Contains(lower, marker) {
			return ErrCodePermission
		}
	}
	for _, marker := range []string{
		"connection refused", "connection reset", "broken pipe", "no such host",
		"dial tcp", "server closed the connection", "bad connection",
		"failed to connect", "connection",
	} {
		if strings.Contains(lower, marker) {
			return ErrCodeConnection
		}
	}
	if strings.Contains(lower, "limit exceeded") {
		return ErrCodeLimitExceeded
	}
	return ""
}

// sanitizeErrorMessage makes an unclassified driver message safe to store: the
// password of a DSN and any password=… fragment are replaced, and the text is
// truncated on a rune boundary.
func sanitizeErrorMessage(message string) string {
	message = passwordAssignment.ReplaceAllString(message, "$1=***")
	message = urlCredential.ReplaceAllString(message, "$1:***@")
	return common.TruncateUTF8Bytes(message, maxErrorMessageBytes)
}
