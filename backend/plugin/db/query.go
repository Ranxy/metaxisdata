package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/pkg/errors"
)

// QueryOptions bounds one read-only query. The zero value means "no limit" in
// every field.
type QueryOptions struct {
	// MaxRows caps the number of rows returned. 0 means unlimited.
	MaxRows int
	// MaxBytes caps the accumulated size of the returned row values, in bytes.
	// The size is an estimate over the normalized values rather than an exact
	// wire size (see ScanRows). 0 means unlimited.
	MaxBytes int
	// Timeout bounds the whole execution, from beginning the transaction to
	// committing it. 0 means the caller's context is the only deadline.
	Timeout time.Duration
}

// QueryResult is the scanned outcome of one read-only query. Rows holds the
// values in column order, and a value is one of nil, int64, float64, bool,
// string or time.Time.
type QueryResult struct {
	Columns   []string
	Rows      [][]any
	Truncated bool
}

// ReadOnlyQueryer is an optional capability: a driver that implements it can run
// one query through a read-only transaction and return normalized rows. It is
// deliberately not part of Driver, so a driver without a safe read-only path
// simply does not implement it and the caller refuses the execution instead of
// running it with write credentials.
type ReadOnlyQueryer interface {
	QueryReadOnly(ctx context.Context, query string, opts QueryOptions) (*QueryResult, error)
}

// ReadOnlyQueryHooks lets an engine apply and undo session-level settings inside
// the read-only transaction. Both callbacks are optional.
type ReadOnlyQueryHooks struct {
	// Prepare runs inside the transaction before the query; returning an error
	// aborts the execution.
	Prepare func(ctx context.Context, tx *sql.Tx) error
	// Cleanup runs after the rows were scanned and before the commit. It undoes
	// a session-level setting so a pooled connection does not inherit it. Its
	// error is ignored: a best-effort reset must not turn a good result into an
	// error.
	Cleanup func(ctx context.Context, tx *sql.Tx)
}

// RunReadOnlyQuery is the shared implementation of ReadOnlyQueryer for the SQL
// drivers. It begins a read-only transaction when the driver supports one,
// applies opts.Timeout to the context, runs hooks.Prepare, executes exactly one
// statement, scans it through ScanRows, runs hooks.Cleanup and commits.
//
// Error classification is the caller's job: this function returns the driver's
// own error, so the upper layer can map context.DeadlineExceeded to a timeout
// code, connection/permission failures to their codes, and everything else to a
// SQL error without losing the cause.
func RunReadOnlyQuery(ctx context.Context, database *sql.DB, query string, opts QueryOptions, hooks ReadOnlyQueryHooks) (*QueryResult, error) {
	if database == nil {
		return nil, errors.New("cannot run a query on a nil database handle")
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	tx, err := beginReadOnlyTx(ctx, database)
	if err != nil {
		return nil, err
	}
	// Every path that does not commit rolls the transaction back; after a
	// successful commit, Rollback is a no-op.
	defer func() { _ = tx.Rollback() }()

	if hooks.Prepare != nil {
		if err := hooks.Prepare(ctx, tx); err != nil {
			return nil, err
		}
	}

	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	// The rows are closed before Commit because some drivers refuse a commit
	// while a result set is still open.
	result, scanErr := scanAndCloseRows(rows, opts)
	if hooks.Cleanup != nil {
		// The query is over, so a session-level setting can be undone even when
		// the scan failed.
		hooks.Cleanup(ctx, tx)
	}
	if scanErr != nil {
		return nil, scanErr
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// beginReadOnlyTx begins a read-only transaction, falling back to a plain one
// when the driver refuses the read-only option. MSSQL has no read-only
// transactions at all, and the MySQL-wire engines may not implement them, so on
// those engines the read-only guarantee is the SELECT-only statement produced by
// the compiler plus a read-only account; the transaction option is best-effort.
func beginReadOnlyTx(ctx context.Context, database *sql.DB) (*sql.Tx, error) {
	tx, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err == nil {
		return tx, nil
	}

	plainTx, plainErr := database.BeginTx(ctx, nil)
	if plainErr != nil {
		return nil, errors.Wrapf(plainErr, "failed to begin a transaction after the read-only transaction was refused: %v", err)
	}
	return plainTx, nil
}

// scanAndCloseRows scans rows and closes them before returning, so the
// transaction can be committed without an open result set.
func scanAndCloseRows(rows *sql.Rows, opts QueryOptions) (*QueryResult, error) {
	defer func() { _ = rows.Close() }()
	return ScanRows(rows, opts)
}
