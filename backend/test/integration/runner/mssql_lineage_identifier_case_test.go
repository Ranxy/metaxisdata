//go:build integration

package runner

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
	integrationenv "github.com/Ranxy/metaxisdata/backend/test/integration/env"
)

// A T-SQL statement may spell a name in any case, while the sync registers the object under the
// spelling the instance reported. The two have to meet at one object: the registry is addressed
// by a meta GUID, compared byte for byte, so an analysis that recorded the statement's own
// spelling produced an edge into a GUID the registry does not have — the graph drew a floating
// node beside the view instead of the table it reads.
//
// This drives the real path against the real metadata database, which is the only place the
// folded lookup runs at all: a unit test with an in-memory catalog cannot tell whether
// `lower(guid COLLATE "default") = lower($1)` is a comparison PostgreSQL accepts, let alone one
// the index can serve.
//
// No SQL Server is involved. An analysis reads a definition and the registry; it never opens a
// connection to the instance it belongs to, which is what makes this runnable in CI.
func TestMSSQLLineageRecordsTheRegisteredRelationRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()

	// The instance is here for one reason: it is what tells the analyzer which engine's rules to
	// apply. Its data source points nowhere, and nothing in an analysis reads it.
	instanceID := mssqlCaseInstanceID(t)
	httpClient := &http.Client{Timeout: 10 * time.Second}
	instanceClient := v1connect.NewInstanceServiceClient(httpClient, env.BaseURL)
	createResp, err := instanceClient.CreateInstance(ctx, withToken(env.AdminToken(), &v1pb.CreateInstanceRequest{
		InstanceId: instanceID,
		Instance: &v1pb.Instance{
			Title:        instanceID,
			Engine:       v1pb.Engine_MSSQL,
			Environment:  common.FormatEnvironment("test"),
			Activation:   true,
			SyncInterval: durationpb.New(24 * time.Hour),
			DataSources: []*v1pb.DataSource{{
				Name:     common.FormatDataSource(instanceID, "admin"),
				Type:     v1pb.DataSourceType_ADMIN,
				Username: "sa",
				Password: "unused",
				Host:     "127.0.0.1",
				Port:     "1",
			}},
		},
	}))
	require.NoError(t, err)
	instance := createResp.Msg
	t.Cleanup(func() {
		_, _ = env.DeleteInstance(context.Background(), instance.GetName())
	})

	const (
		database    = "app"
		tableSchema = "dbo"
		// What the sync registered: SQL Server keeps the case the object was created with. What
		// the statement writes is the same object under another spelling.
		registeredTable = "Users"
	)
	registeredGUID := instanceID + ";" + database + ";" + tableSchema + ";" + registeredTable
	statementGUID := instanceID + ";" + database + ";" + tableSchema + ";users"

	registerMSSQLTable(ctx, t, env, registeredGUID, registeredTable)
	require.NotEqual(t, registeredGUID, statementGUID)

	// The manual SQL row carries a foreign key to the database it belongs to, and no sync can
	// create that row for an instance nothing can connect to.
	_, err = env.Store.CreateDatabaseDefault(ctx, &store.DatabaseMessage{InstanceID: instanceID, DatabaseName: database})
	require.NoError(t, err)

	manual := env.CreateManualSQL(ctx, t, common.FormatDatabase(instanceID, database), "case-folded-source", &v1pb.ManualSQL{
		Title:   "Case folded source",
		SqlText: "SELECT u.ID, u.Name FROM dbo.users AS u",
	})
	t.Cleanup(func() {
		env.DeleteManualSQL(context.Background(), t, manual.GetName())
	})

	relations := env.WaitForContextLineage(ctx, t, manual.GetGuid(), v1pb.MetaType_MANUAL_SQL, func(relations []*v1pb.LineageRelation) bool {
		return hasAPILineageEdge(relations, registeredGUID, "ID", manual.GetGuid(), "ID") &&
			hasAPILineageEdge(relations, registeredGUID, "Name", manual.GetGuid(), "Name")
	})
	for _, relation := range relations {
		require.NotEqual(t, statementGUID, relation.GetSourceGuid(),
			"the edge names the GUID the statement spelled, which the registry does not have")
	}
}

// mssqlCaseInstanceID names this test's instance: an instance resource ID is a path segment, and
// a test name is too long to carry one.
func mssqlCaseInstanceID(t *testing.T) string {
	t.Helper()

	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(t.Name()))
	return fmt.Sprintf("it-mssql-case-%08x", hasher.Sum32())
}

// registerMSSQLTable writes one table into the metadata registry exactly as a sync would: the
// GUID names the object, and the metadata carries the columns a wildcard expands to.
func registerMSSQLTable(ctx context.Context, t *testing.T, env *integrationenv.ServiceEnv, guid, name string) {
	t.Helper()

	metadata := &storepb.StoredMetadata{Type: &storepb.StoredMetadata_TableMetadata{TableMetadata: &storepb.TableMetadata{
		Name: name,
		Columns: []*storepb.ColumnMetadata{
			{Name: "ID", Type: "int", Nullable: false},
			{Name: "Name", Type: "nvarchar(100)", Nullable: true},
		},
	}}}
	bytes, err := protojson.Marshal(metadata)
	require.NoError(t, err)

	tx, err := env.Store.GetDB().BeginTx(ctx, &sql.TxOptions{})
	require.NoError(t, err)
	created, err := env.Store.BatchCreateMetaRegistryResource(ctx, tx, []*store.CreateMetaRegistryResourceMessage{{
		MetaRegistryResource: store.MetaRegistryResource{
			GUID:       guid,
			ObjectType: storepb.MetaType_TABLE,
			Metadata:   metadata,
		},
		MetadataBytes: bytes,
	}})
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	t.Cleanup(func() {
		cleanupTx, err := env.Store.GetDB().BeginTx(context.Background(), &sql.TxOptions{})
		if err != nil {
			return
		}
		defer cleanupTx.Rollback()
		_ = env.Store.BatchDeleteMetaRegistry(context.Background(), cleanupTx, created)
		_ = cleanupTx.Commit()
	})
}
