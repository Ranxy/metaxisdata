package schemasync

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/mysql"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/config"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func getTestSyncer(t *testing.T) *Syncer {
	ctx := context.TODO()
	pgURL := os.Getenv("PG_URL")
	s, err := store.New(ctx, pgURL, false)
	require.NoError(t, err)

	dbFactory := dbfactory.New(s)

	stateCfg, err := state.New()
	require.NoError(t, err)

	return NewSyncer(s, dbFactory, &config.Profile{}, stateCfg, nil)
}

func TestSyncInstance(t *testing.T) {
	ctx := context.Background()
	s := getTestSyncer(t)

	id := "mysqldev1"

	find := &store.FindInstanceMessage{
		ResourceID: &id,
	}
	instance, err := s.store.GetInstanceV2(ctx, find)
	require.NoError(t, err)

	im, schema, newSchema, err := s.SyncInstance(context.Background(), instance)
	require.NoError(t, err)
	require.NotNil(t, im)
	require.NotNil(t, schema)
	require.NotNil(t, newSchema)
}

func TestSyncDatabase(t *testing.T) {
	ctx := context.Background()
	s := getTestSyncer(t)

	id := "mysqldev"
	dbName := "ods_tmp"

	find := &store.FindDatabaseMessage{
		InstanceID:   &id,
		DatabaseName: &dbName,
	}
	database, err := s.store.GetDatabaseV2(ctx, find)
	require.NoError(t, err)

	err = s.SyncDatabaseSchema(context.Background(), database)

	require.NoError(t, err)

	guidPrefix := id + "." + dbName
	excludeObjectType := []storepb.MetaType{storepb.MetaType_COLUMN}
	list, err := s.store.ListMetaRegistryResource(ctx, &store.FindMetaRegistryResourceMessage{GUIDPrefix: &guidPrefix, ExcludeObjectType: &excludeObjectType})
	require.NoError(t, err)

	got, err := json.Marshal(list)
	require.NoError(t, err)
	err = os.WriteFile("./sync_database_got.json", got, 0644)
	require.NoError(t, err)
}
