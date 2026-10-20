package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeDriver is a minimal database/sql driver that serves one predefined result
// set. It exists so the scan and execution paths can be tested hermetically,
// including the transaction options that are handed to the driver and the
// fallback when a driver refuses a read-only transaction.
type fakeDriver struct {
	columns          []string
	columnTypeNames  []string
	values           [][]driver.Value
	nextErr          error
	refuseReadOnlyTx bool

	openedReadOnlyTx []bool
	queries          []string
	execs            []string
	queryDeadlines   []time.Time
	committed        atomic.Int32
	rolledBack       atomic.Int32
}

func (d *fakeDriver) Open(string) (driver.Conn, error) {
	return &fakeConn{driver: d}, nil
}

type fakeConn struct {
	driver *fakeDriver
}

func (*fakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not supported by the fake driver")
}

func (*fakeConn) Close() error {
	return nil
}

func (c *fakeConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *fakeConn) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.driver.openedReadOnlyTx = append(c.driver.openedReadOnlyTx, opts.ReadOnly)
	if opts.ReadOnly && c.driver.refuseReadOnlyTx {
		return nil, errors.New("read-only transactions are not supported")
	}
	return &fakeTx{driver: c.driver}, nil
}

func (c *fakeConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.driver.queries = append(c.driver.queries, query)
	if deadline, ok := ctx.Deadline(); ok {
		c.driver.queryDeadlines = append(c.driver.queryDeadlines, deadline)
	}
	return &fakeRows{
		columns:   c.driver.columns,
		typeNames: c.driver.columnTypeNames,
		values:    c.driver.values,
		nextErr:   c.driver.nextErr,
	}, nil
}

func (c *fakeConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.driver.execs = append(c.driver.execs, query)
	return driver.RowsAffected(0), nil
}

type fakeTx struct {
	driver *fakeDriver
}

func (t *fakeTx) Commit() error {
	t.driver.committed.Add(1)
	return nil
}

func (t *fakeTx) Rollback() error {
	t.driver.rolledBack.Add(1)
	return nil
}

type fakeRows struct {
	columns   []string
	typeNames []string
	values    [][]driver.Value
	nextErr   error
	index     int
}

func (r *fakeRows) Columns() []string {
	return r.columns
}

func (*fakeRows) Close() error {
	return nil
}

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		if r.nextErr != nil {
			return r.nextErr
		}
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func (r *fakeRows) ColumnTypeDatabaseTypeName(index int) string {
	if index < len(r.typeNames) {
		return r.typeNames[index]
	}
	return ""
}

var fakeDriverSequence atomic.Int64

// openFakeDB registers driver under a unique name and returns a pool over it.
// database/sql refuses to share a registration, so each test gets its own.
func openFakeDB(t *testing.T, driver *fakeDriver) *sql.DB {
	t.Helper()

	name := fmt.Sprintf("dbscan-%s-%d", t.Name(), fakeDriverSequence.Add(1))
	sql.Register(name, driver)
	db, err := sql.Open(name, "")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	return db
}

func scanFakeRows(t *testing.T, driver *fakeDriver, opts QueryOptions) (*QueryResult, error) {
	t.Helper()

	rows, err := openFakeDB(t, driver).Query("SELECT")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	return ScanRows(rows, opts)
}

func TestScanRowsNormalizesValues(t *testing.T) {
	t.Parallel()

	driver := &fakeDriver{
		columns:         []string{"id", "name", "score", "active", "created", "note"},
		columnTypeNames: []string{"BIGINT", "VARCHAR", "DOUBLE", "BOOLEAN", "TIMESTAMP", "VARCHAR"},
		values: [][]driver.Value{{
			[]byte("42"),
			[]byte("alice"),
			[]byte("1.5"),
			[]byte("1"),
			[]byte("2024-05-06 07:08:09"),
			nil,
		}},
	}

	result, err := scanFakeRows(t, driver, QueryOptions{})
	require.NoError(t, err)
	require.False(t, result.Truncated)
	require.Equal(t, []string{"id", "name", "score", "active", "created", "note"}, result.Columns)
	require.Len(t, result.Rows, 1)
	require.Equal(t, []any{
		int64(42),
		"alice",
		1.5,
		true,
		time.Date(2024, time.May, 6, 7, 8, 9, 0, time.UTC),
		nil,
	}, result.Rows[0])
}

func TestScanRowsKeepsNativeDriverValues(t *testing.T) {
	t.Parallel()

	created := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	driver := &fakeDriver{
		columns:         []string{"n", "score", "active", "name", "created", "missing"},
		columnTypeNames: []string{"INT", "FLOAT", "BOOLEAN", "TEXT", "TIMESTAMP", "TEXT"},
		values: [][]driver.Value{{
			int64(7),
			float64(2.5),
			true,
			"text",
			created,
			nil,
		}},
	}

	result, err := scanFakeRows(t, driver, QueryOptions{})
	require.NoError(t, err)
	require.Equal(t, []any{int64(7), float64(2.5), true, "text", created, nil}, result.Rows[0])
}

func TestScanRowsMaxRows(t *testing.T) {
	t.Parallel()

	newDriver := func(n int) *fakeDriver {
		values := make([][]driver.Value, 0, n)
		for i := 0; i < n; i++ {
			values = append(values, []driver.Value{int64(i)})
		}
		return &fakeDriver{columns: []string{"n"}, columnTypeNames: []string{"INT"}, values: values}
	}

	tests := []struct {
		name          string
		rows          int
		maxRows       int
		wantRows      int
		wantTruncated bool
	}{
		{name: "limit reached with more rows", rows: 3, maxRows: 2, wantRows: 2, wantTruncated: true},
		{name: "limit equals row count", rows: 2, maxRows: 2, wantRows: 2, wantTruncated: false},
		{name: "zero means unlimited", rows: 3, maxRows: 0, wantRows: 3, wantTruncated: false},
		{name: "limit above row count", rows: 1, maxRows: 10, wantRows: 1, wantTruncated: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, err := scanFakeRows(t, newDriver(test.rows), QueryOptions{MaxRows: test.maxRows})
			require.NoError(t, err)
			require.Len(t, result.Rows, test.wantRows)
			require.Equal(t, test.wantTruncated, result.Truncated)
		})
	}
}

func TestScanRowsMaxBytes(t *testing.T) {
	t.Parallel()

	newDriver := func(values ...string) *fakeDriver {
		rows := make([][]driver.Value, 0, len(values))
		for _, value := range values {
			rows = append(rows, []driver.Value{[]byte(value)})
		}
		return &fakeDriver{columns: []string{"s"}, columnTypeNames: []string{"VARCHAR"}, values: rows}
	}

	tests := []struct {
		name          string
		values        []string
		maxBytes      int
		wantRows      int
		wantTruncated bool
	}{
		{name: "second row exceeds the cap", values: []string{"aaaa", "bbbb"}, maxBytes: 5, wantRows: 1, wantTruncated: true},
		{name: "exactly at the cap", values: []string{"aaaa", "bbbb"}, maxBytes: 8, wantRows: 2, wantTruncated: false},
		{name: "first row exceeds the cap", values: []string{"0123456789"}, maxBytes: 3, wantRows: 0, wantTruncated: true},
		{name: "zero means unlimited", values: []string{"0123456789", "abcdefghij"}, maxBytes: 0, wantRows: 2, wantTruncated: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, err := scanFakeRows(t, newDriver(test.values...), QueryOptions{MaxBytes: test.maxBytes})
			require.NoError(t, err)
			require.Len(t, result.Rows, test.wantRows)
			require.Equal(t, test.wantTruncated, result.Truncated)
		})
	}
}

func TestScanRowsMaxBytesCountsNumbersAsEightBytes(t *testing.T) {
	t.Parallel()

	driver := &fakeDriver{
		columns:         []string{"n"},
		columnTypeNames: []string{"BIGINT"},
		values:          [][]driver.Value{{int64(1)}, {int64(2)}},
	}

	result, err := scanFakeRows(t, driver, QueryOptions{MaxBytes: 8})
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	require.True(t, result.Truncated)
}

func TestScanRowsReturnsRowError(t *testing.T) {
	t.Parallel()

	driver := &fakeDriver{
		columns:         []string{"n"},
		columnTypeNames: []string{"INT"},
		nextErr:         errors.New("connection reset"),
	}

	_, err := scanFakeRows(t, driver, QueryOptions{})
	require.EqualError(t, err, "connection reset")
}

func TestRunReadOnlyQueryCommitsAndAppliesTimeout(t *testing.T) {
	t.Parallel()

	driver := &fakeDriver{
		columns:         []string{"n"},
		columnTypeNames: []string{"BIGINT"},
		values:          [][]driver.Value{{int64(1)}},
	}
	db := openFakeDB(t, driver)

	var prepared, cleaned int
	hooks := ReadOnlyQueryHooks{
		Prepare: func(context.Context, *sql.Tx) error {
			prepared++
			return nil
		},
		Cleanup: func(context.Context, *sql.Tx) {
			cleaned++
		},
	}

	before := time.Now()
	result, err := RunReadOnlyQuery(context.Background(), db, "SELECT 1", QueryOptions{Timeout: 30 * time.Second}, hooks)
	require.NoError(t, err)
	require.Equal(t, [][]any{{int64(1)}}, result.Rows)
	require.Equal(t, []bool{true}, driver.openedReadOnlyTx)
	require.Equal(t, []string{"SELECT 1"}, driver.queries)
	require.Equal(t, int32(1), driver.committed.Load())
	require.Equal(t, int32(0), driver.rolledBack.Load())
	require.Equal(t, 1, prepared)
	require.Equal(t, 1, cleaned)
	require.Len(t, driver.queryDeadlines, 1)
	require.WithinDuration(t, before.Add(30*time.Second), driver.queryDeadlines[0], 5*time.Second)
}

func TestRunReadOnlyQueryFallsBackToPlainTransaction(t *testing.T) {
	t.Parallel()

	driver := &fakeDriver{
		columns:          []string{"n"},
		columnTypeNames:  []string{"BIGINT"},
		values:           [][]driver.Value{{int64(1)}},
		refuseReadOnlyTx: true,
	}
	db := openFakeDB(t, driver)

	result, err := RunReadOnlyQuery(context.Background(), db, "SELECT 1", QueryOptions{}, ReadOnlyQueryHooks{})
	require.NoError(t, err)
	require.Equal(t, [][]any{{int64(1)}}, result.Rows)
	require.Equal(t, []bool{true, false}, driver.openedReadOnlyTx)
	require.Equal(t, int32(1), driver.committed.Load())
}

func TestRunReadOnlyQueryReturnsScanErrorAndRollsBack(t *testing.T) {
	t.Parallel()

	driver := &fakeDriver{
		columns:         []string{"n"},
		columnTypeNames: []string{"INT"},
		nextErr:         errors.New("query aborted"),
	}
	db := openFakeDB(t, driver)

	var cleaned int
	_, err := RunReadOnlyQuery(context.Background(), db, "SELECT 1", QueryOptions{}, ReadOnlyQueryHooks{
		Cleanup: func(context.Context, *sql.Tx) {
			cleaned++
		},
	})
	require.EqualError(t, err, "query aborted")
	require.Equal(t, int32(0), driver.committed.Load())
	require.Equal(t, int32(1), driver.rolledBack.Load())
	require.Equal(t, 1, cleaned, "a best-effort cleanup still runs after a failed scan")
}

func TestRunReadOnlyQueryRejectsNilDatabase(t *testing.T) {
	t.Parallel()

	_, err := RunReadOnlyQuery(context.Background(), nil, "SELECT 1", QueryOptions{}, ReadOnlyQueryHooks{})
	require.EqualError(t, err, "cannot run a query on a nil database handle")
}
