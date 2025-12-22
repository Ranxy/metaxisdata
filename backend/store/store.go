package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

type Store struct {
	dbConnManager *DBConnectionManager
	enableCache   bool

	// Cache
	Secret                string
	userIDCache           *lru.Cache[int, *UserMessage]
	userEmailCache        *lru.Cache[string, *UserMessage]
	groupCache            *lru.Cache[string, *GroupMessage]
	idpCache              *lru.Cache[string, *IdentityProviderMessage]
	instanceCache         *lru.Cache[string, *InstanceMessage]
	databaseCache         *lru.Cache[string, *DatabaseMessage]
	metaRegistryCache     *lru.Cache[int64, *MetaRegistryResource]
	metaRegistryGuidCache *lru.Cache[string, *MetaRegistryResource]
	policyCache           *lru.Cache[string, *PolicyMessage]
	projectCache          *lru.Cache[string, *ProjectMessage]
	rolesCache            *lru.Cache[string, *RoleMessage]
	settingCache          *lru.Cache[storepb.SettingName, *SettingMessage]
}

func New(ctx context.Context, pgURL string, enableCache bool) (*Store, error) {
	userIDCache, err := lru.New[int, *UserMessage](32768)
	if err != nil {
		return nil, err
	}
	userEmailCache, err := lru.New[string, *UserMessage](32768)
	if err != nil {
		return nil, err
	}
	groupCache, err := lru.New[string, *GroupMessage](1024)
	if err != nil {
		return nil, err
	}
	idpCache, err := lru.New[string, *IdentityProviderMessage](4)
	if err != nil {
		return nil, err
	}
	instanceCache, err := lru.New[string, *InstanceMessage](32768)
	if err != nil {
		return nil, err
	}
	databaseCache, err := lru.New[string, *DatabaseMessage](65536)
	if err != nil {
		return nil, err
	}
	metaRegistryCache, err := lru.New[int64, *MetaRegistryResource](65536)
	if err != nil {
		return nil, err
	}
	metaRegistryGuidCache, err := lru.New[string, *MetaRegistryResource](65536)
	if err != nil {
		return nil, err
	}
	policyCache, err := lru.New[string, *PolicyMessage](128)
	if err != nil {
		return nil, err
	}
	projectCache, err := lru.New[string, *ProjectMessage](32768)
	if err != nil {
		return nil, err
	}
	rolesCache, err := lru.New[string, *RoleMessage](64)
	if err != nil {
		return nil, err
	}
	settingCache, err := lru.New[storepb.SettingName, *SettingMessage](64)
	if err != nil {
		return nil, err
	}
	dbConnManager := NewDBConnectionManager(pgURL)
	if err := dbConnManager.Initialize(ctx); err != nil {
		return nil, err
	}
	s := &Store{
		dbConnManager:         dbConnManager,
		enableCache:           enableCache,
		userIDCache:           userIDCache,
		userEmailCache:        userEmailCache,
		rolesCache:            rolesCache,
		idpCache:              idpCache,
		instanceCache:         instanceCache,
		databaseCache:         databaseCache,
		metaRegistryCache:     metaRegistryCache,
		metaRegistryGuidCache: metaRegistryGuidCache,
		policyCache:           policyCache,
		groupCache:            groupCache,
		projectCache:          projectCache,
		settingCache:          settingCache,
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.dbConnManager.Close()
}

func (s *Store) GetDB() *sql.DB {
	return s.dbConnManager.GetDB()
}

func getPolicyCacheKey(resourceType storepb.Policy_Resource, resource string, policyType storepb.Policy_Type) string {
	return fmt.Sprintf("policies/%s/%s/%s", resourceType, resource, policyType)
}

func getInstanceCacheKey(instanceID string) string {
	return instanceID
}

func getDatabaseCacheKey(instanceID, databaseName string) string {
	return fmt.Sprintf("%s/%s", instanceID, databaseName)
}

func CalcStoreMetaHash(meta *storepb.StoredMetadata) (metadata []byte, metaHash []byte, err error) {
	metadataBytes, err := protojson.Marshal(meta)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to marshal table metadata")
	}

	h := sha256.Sum256(metadataBytes)
	return metadataBytes, h[:], nil
}
