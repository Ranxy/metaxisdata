package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

type Store struct {
	dbConnManager *DBConnectionManager

	// secret caches the AUTH_SECRET setting, the seed for stored credentials. It
	// is read on every obfuscated instance/LLM row, so it must not be written
	// lazily without a lock.
	secretMu sync.Mutex
	secret   string

	// cacheDisabled makes every cache read go to the database. It is meant for
	// short-lived observer stores (the integration harness) that read rows
	// written by another process and cannot invalidate this process's cache.
	cacheDisabled bool

	// Cache
	userIDCache           *lru.Cache[int, *UserMessage]
	userEmailCache        *lru.Cache[string, *UserMessage]
	groupCache            *lru.Cache[string, *GroupMessage]
	idpCache              *lru.Cache[string, *IdentityProviderMessage]
	instanceCache         *lru.Cache[string, *InstanceMessage]
	databaseCache         *lru.Cache[string, *DatabaseMessage]
	metaRegistryCache     *lru.Cache[int64, *MetaRegistryResource]
	metaRegistryGUIDCache *lru.Cache[MetaGUIDKey, *MetaRegistryResource]
	policyCache           *lru.Cache[string, *PolicyMessage]
	rolesCache            *lru.Cache[string, *RoleMessage]
	settingCache          *lru.Cache[storepb.SettingName, *SettingMessage]
}

// Option customises a Store.
type Option func(*Store)

// WithCacheDisabled makes cache reads bypass the cache and query the database.
// Use it for a store that observes rows written by another process: such a store
// never sees the writer's cache invalidation, so a cached row would stay visible
// after the writer deleted it.
func WithCacheDisabled() Option {
	return func(s *Store) {
		s.cacheDisabled = true
	}
}

func New(ctx context.Context, pgURL string, opts ...Option) (*Store, error) {
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
	metaRegistryGUIDCache, err := lru.New[MetaGUIDKey, *MetaRegistryResource](65536)
	if err != nil {
		return nil, err
	}
	policyCache, err := lru.New[string, *PolicyMessage](128)
	if err != nil {
		return nil, err
	}
	rolesCache, err := lru.New[string, *RoleMessage](256)
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
		userIDCache:           userIDCache,
		userEmailCache:        userEmailCache,
		idpCache:              idpCache,
		instanceCache:         instanceCache,
		databaseCache:         databaseCache,
		metaRegistryCache:     metaRegistryCache,
		metaRegistryGUIDCache: metaRegistryGUIDCache,
		policyCache:           policyCache,
		rolesCache:            rolesCache,
		groupCache:            groupCache,
		settingCache:          settingCache,
	}
	for _, opt := range opts {
		opt(s)
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

// CalcMetaHash computes only the SHA-256 hash of the given StoredMetadata,
// without returning the serialized JSON bytes.
func CalcMetaHash(meta *storepb.StoredMetadata) ([]byte, error) {
	_, hash, err := CalcStoreMetaHash(meta)
	return hash, err
}
