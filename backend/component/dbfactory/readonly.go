package dbfactory

import (
	"context"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
	"github.com/Ranxy/metaxisdata/backend/store"
	"github.com/Ranxy/metaxisdata/backend/utils"
)

// ErrNoReadOnlyDataSource reports that an instance has no READ_ONLY data source.
// The caller maps it to FailedPrecondition and tells the user to add one in the
// instance's data source settings. It exists as a sentinel so the API can tell
// this misconfiguration apart from a failing connection.
var ErrNoReadOnlyDataSource = errors.New("the instance has no READ_ONLY data source; add a read-only data source for it before running data quality rules")

// GetReadOnlyDatabaseDriver builds the driver used to execute a quality rule.
// It only accepts the instance's READ_ONLY data source and never falls back to
// ADMIN: running a check with write-capable credentials would defeat the reason
// the read-only path exists. The returned driver advertises ReadOnly in its
// connection context, which PostgreSQL turns into
// default_transaction_read_only=true.
//
// Upon successful return, the caller must call driver.Close().
func (d *DBFactory) GetReadOnlyDatabaseDriver(ctx context.Context, instance *store.InstanceMessage, database *store.DatabaseMessage) (db.Driver, error) {
	if instance == nil || instance.Metadata == nil {
		return nil, common.Errorf(common.Internal, "cannot build a read-only database driver without an instance")
	}
	dataSource := utils.DataSourceFromInstanceWithType(instance, storepb.DataSourceType_READ_ONLY)
	if dataSource == nil {
		return nil, errors.Wrapf(ErrNoReadOnlyDataSource, "instance %q", instance.ResourceID)
	}

	connectionContext := db.ConnectionContext{ReadOnly: true}
	if database != nil {
		connectionContext.DatabaseName = database.DatabaseName
	}
	return d.GetDataSourceDriver(ctx, instance, dataSource, connectionContext)
}
