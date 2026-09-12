//go:build integration

package runner

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// TestDataSourceResourceLifecycleRealServerIntegration drives the AIP-shaped
// data source resource through the real server: create, masked update and
// delete, with the instance's data_sources projection checked after each step.
func TestDataSourceResourceLifecycleRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := t.Context()

	instanceID := "ds-lifecycle"
	instance, err := env.CreatePostgresInstance(ctx, instanceID)
	require.NoError(t, err)
	defer func() {
		_, _ = env.DeleteInstance(ctx, instance.GetName())
	}()

	adminName := instance.GetDataSources()[0].GetName()
	require.Equal(t, "instances/"+instanceID+"/dataSources/admin", adminName)

	reader := &v1pb.DataSource{
		Type:     v1pb.DataSourceType_READ_ONLY,
		Username: "postgres",
		Password: "postgres",
		Host:     env.PostgresHost,
		Port:     env.PostgresPort,
		Database: "postgres",
	}

	created, err := env.CreateDataSource(ctx, instance.GetName(), reader, "reader", false)
	require.NoError(t, err)
	require.Equal(t, "instances/"+instanceID+"/dataSources/reader", created.GetName())
	require.Equal(t, v1pb.DataSourceType_READ_ONLY, created.GetType())
	// Reads never return the credentials that were written.
	require.Empty(t, created.GetPassword())

	// A duplicate ID is a conflict, not a silent second entry.
	_, err = env.CreateDataSource(ctx, instance.GetName(), reader, "reader", false)
	require.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(err))

	// The ID becomes the last segment of the resource name, so it must be a
	// valid resource ID.
	_, err = env.CreateDataSource(ctx, instance.GetName(), reader, "Not_An_Id", false)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// Only read-only data sources can be created here.
	_, err = env.CreateDataSource(ctx, instance.GetName(), &v1pb.DataSource{
		Type:     v1pb.DataSourceType_ADMIN,
		Username: "postgres",
		Password: "postgres",
		Host:     env.PostgresHost,
		Port:     env.PostgresPort,
	}, "second-admin", false)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// Only the masked fields are written.
	updated, err := env.UpdateDataSource(ctx, created.GetName(), &v1pb.DataSource{Database: "it_app"}, []string{"database"}, false)
	require.NoError(t, err)
	require.Equal(t, "it_app", updated.GetDatabase())
	require.Equal(t, env.PostgresHost, updated.GetHost(), "an unmasked field keeps its stored value")
	require.Equal(t, "postgres", updated.GetUsername())

	instance, err = env.GetInstance(ctx, instance.GetName())
	require.NoError(t, err)
	require.Len(t, instance.GetDataSources(), 2)
	require.Equal(t, "instances/"+instanceID+"/dataSources/admin", instance.GetDataSources()[0].GetName())
	require.Equal(t, "it_app", instance.GetDataSources()[1].GetDatabase())

	// The admin connection is part of the instance and cannot be deleted.
	err = env.DeleteDataSource(ctx, adminName)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	require.NoError(t, env.DeleteDataSource(ctx, created.GetName()))
	instance, err = env.GetInstance(ctx, instance.GetName())
	require.NoError(t, err)
	require.Len(t, instance.GetDataSources(), 1)

	// A data source of another instance cannot be addressed through this one.
	_, err = env.UpdateDataSource(ctx, "instances/"+instanceID+"/dataSources/missing", &v1pb.DataSource{Database: "x"}, []string{"database"}, false)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}
