package dbfactory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"

	// Register the drivers so GetReadOnlyDatabaseDriver can open them. Opening is
	// lazy, so none of these tests touches the network.
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/mssql"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/mysql"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/pg"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/starrocks"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func testDataSource(sourceType storepb.DataSourceType, host, port string) *storepb.DataSource {
	return &storepb.DataSource{
		Type:     sourceType,
		Username: "probe",
		Password: "probe",
		Host:     host,
		Port:     port,
		Database: "probe",
	}
}

func testInstance(resourceID string, engine storepb.Engine, dataSources ...*storepb.DataSource) *store.InstanceMessage {
	return &store.InstanceMessage{
		ResourceID: resourceID,
		Metadata: &storepb.Instance{
			Title:       resourceID,
			Engine:      engine,
			DataSources: dataSources,
		},
	}
}

func TestGetReadOnlyDatabaseDriverRejectsInstanceWithoutReadOnlyDataSource(t *testing.T) {
	t.Parallel()

	// The admin source is deliberately unusable (an invalid port would fail the
	// driver's DSN parsing). If the factory fell back to ADMIN, the test would
	// see that error instead of the sentinel.
	instance := testInstance("inst-admin-only", storepb.Engine_POSTGRES,
		testDataSource(storepb.DataSourceType_ADMIN, "127.0.0.1", "not-a-port"))

	driver, err := New(nil).GetReadOnlyDatabaseDriver(context.Background(), instance, &store.DatabaseMessage{DatabaseName: "probe"})
	require.Nil(t, driver)
	require.ErrorIs(t, err, ErrNoReadOnlyDataSource)
}

func TestGetReadOnlyDatabaseDriverDoesNotFallBackToAdminForAnyEngine(t *testing.T) {
	t.Parallel()

	engines := []storepb.Engine{
		storepb.Engine_MYSQL,
		storepb.Engine_POSTGRES,
		storepb.Engine_MSSQL,
		storepb.Engine_STARROCKS,
	}
	for _, engine := range engines {
		t.Run(engine.String(), func(t *testing.T) {
			t.Parallel()

			instance := testInstance("inst-"+engine.String(), engine,
				testDataSource(storepb.DataSourceType_ADMIN, "127.0.0.1", "not-a-port"))

			driver, err := New(nil).GetReadOnlyDatabaseDriver(context.Background(), instance, nil)
			require.Nil(t, driver)
			require.ErrorIs(t, err, ErrNoReadOnlyDataSource)
		})
	}
}

func TestGetReadOnlyDatabaseDriverUsesReadOnlyDataSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		engine storepb.Engine
		port   string
	}{
		{engine: storepb.Engine_MYSQL, port: "3306"},
		{engine: storepb.Engine_POSTGRES, port: "5432"},
		{engine: storepb.Engine_MSSQL, port: "1433"},
		{engine: storepb.Engine_STARROCKS, port: "9030"},
	}
	for _, test := range tests {
		t.Run(test.engine.String(), func(t *testing.T) {
			t.Parallel()

			// The admin source cannot be opened (empty host); only the READ_ONLY
			// source is usable, so a successful open proves which one was used.
			instance := testInstance("inst-"+test.engine.String(), test.engine,
				testDataSource(storepb.DataSourceType_ADMIN, "", ""),
				testDataSource(storepb.DataSourceType_READ_ONLY, "127.0.0.1", test.port))

			driver, err := New(nil).GetReadOnlyDatabaseDriver(context.Background(), instance, &store.DatabaseMessage{DatabaseName: "probe"})
			require.NoError(t, err)
			require.NotNil(t, driver)
			t.Cleanup(func() {
				require.NoError(t, driver.Close(context.Background()))
			})
		})
	}
}

func TestGetReadOnlyDatabaseDriverRejectsNilInstance(t *testing.T) {
	t.Parallel()

	driver, err := New(nil).GetReadOnlyDatabaseDriver(context.Background(), nil, nil)
	require.Nil(t, driver)
	require.Equal(t, common.Internal, common.ErrorCode(err))
}
