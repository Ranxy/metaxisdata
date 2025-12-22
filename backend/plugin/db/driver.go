// Package db provides the interfaces and libraries for database driver plugins.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// InstanceMetadata is the metadata for an instance.
type InstanceMetadata struct {
	Version string
	// Simplified database metadata.
	Databases []*storepb.DatabaseSchemaMetadata
	Metadata  *storepb.Instance
}

// TableKey is the map key for table metadata.
type TableKey struct {
	Schema string
	Table  string
}

type TableKeyWithColumns struct {
	Schema  string
	Table   string
	Columns []*storepb.ColumnMetadata
}

// ColumnKey is the map key for table metadata.
type ColumnKey struct {
	Schema string
	Table  string
	Column string
}

// IndexKey is the map key for table metadata.
type IndexKey struct {
	Schema string
	Table  string
	Index  string
}

type ConstraintKey struct {
	Schema     string
	Constraint string
}

type SequenceKey struct {
	Schema   string
	Sequence string
}

var (
	driversMu sync.RWMutex
	drivers   = make(map[storepb.Engine]driverFunc)
)

type driverFunc func() Driver

// MigrationStatus is the status of migration.
type MigrationStatus string

const (
	// Pending is the migration status for PENDING.
	Pending MigrationStatus = "PENDING"
	// Done is the migration status for DONE.
	Done MigrationStatus = "DONE"
	// Failed is the migration status for FAILED.
	Failed MigrationStatus = "FAILED"
)

// ConnectionConfig is the configuration for connections.
type ConnectionConfig struct {
	DataSource        *storepb.DataSource
	ConnectionContext ConnectionContext
	Password          string
}

// ConnectionContext is the context for connection.
// It's not used for establishing the db connection, but is useful for logging.
type ConnectionContext struct {
	EnvironmentID string
	InstanceID    string
	EngineVersion string
	// UseDatabaseOwner is used by Postgres for using role of database owner.
	UseDatabaseOwner bool
	DatabaseName     string
	// It's only set for Redshift datashare database.
	DataShare bool
	// ReadOnly is only supported for Postgres at the moment.
	ReadOnly bool
	// MessageBuffer is used for logging messages from the database server.
	// MessageBuffer []*v1pb.QueryResult_Message
}

// AppendMessage appends a message to the message buffer.
// func (c *ConnectionContext) AppendMessage(message *v1pb.QueryResult_Message) {
// 	c.MessageBuffer = append(c.MessageBuffer, message)
// }

// Driver is the interface for database driver.
type Driver interface {
	// General execution
	// A driver might support multiple engines (e.g. MySQL driver can support both MySQL and TiDB),
	// So we pass the dbType to tell the exact engine.
	Open(ctx context.Context, dbType storepb.Engine, config ConnectionConfig) (Driver, error)
	// Remember to call Close to avoid connection leak
	Close(ctx context.Context) error
	Ping(ctx context.Context) error
	GetDB() *sql.DB

	// Sync schema
	// SyncInstance syncs the instance metadata.
	SyncInstance(ctx context.Context) (*InstanceMetadata, error)
	// SyncDBSchema syncs a single database schema.
	SyncDBSchema(ctx context.Context) (*storepb.DatabaseSchemaMetadata, error)
}

// Register makes a database driver available by the provided type.
// If Register is called twice with the same name or if driver is nil,
// it panics.
func Register(dbType storepb.Engine, f driverFunc) {
	driversMu.Lock()
	defer driversMu.Unlock()
	if f == nil {
		panic("db: Register driver is nil")
	}
	if _, dup := drivers[dbType]; dup {
		panic(fmt.Sprintf("db: Register called twice for driver %s", dbType))
	}
	drivers[dbType] = f
}

// Open opens a database specified by its database driver type and connection config without verifying the connection.
func Open(ctx context.Context, dbType storepb.Engine, connectionConfig ConnectionConfig) (Driver, error) {
	driversMu.RLock()
	f, ok := drivers[dbType]
	driversMu.RUnlock()
	if !ok {
		return nil, errors.Errorf("db: unknown driver %v", dbType)
	}

	driver, err := f().Open(ctx, dbType, connectionConfig)
	if err != nil {
		return nil, err
	}

	return driver, nil
}

// ErrorWithPosition is the error with the position information.
type ErrorWithPosition struct {
	Err   error
	Start *storepb.Position
	End   *storepb.Position
}

func (e *ErrorWithPosition) Error() string {
	return e.Err.Error()
}

func (e *ErrorWithPosition) Unwrap() error {
	return e.Err
}
