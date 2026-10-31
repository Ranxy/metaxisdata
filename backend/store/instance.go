package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/crypto"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// InstanceMessage is the message for instance.
type InstanceMessage struct {
	ResourceID    string
	EnvironmentID string
	Deleted       bool
	Metadata      *storepb.Instance
}

// UpdateInstanceMessage is the message for updating an instance.
type UpdateInstanceMessage struct {
	ResourceID string

	Deleted       *bool
	EnvironmentID *string
	Metadata      *storepb.Instance
	// MetadataUpdates applies field-level changes to the stored metadata in the
	// same transaction that locks the row. A caller that only owns a few fields
	// (the schema syncer writes last_sync_time and the engine version) must use
	// this instead of Metadata: a whole-metadata write built from an earlier read
	// silently overwrites every concurrent change to the other fields.
	MetadataUpdates []func(*storepb.Instance)
}

// FindInstanceMessage is the message for finding instances.
type FindInstanceMessage struct {
	ResourceID  *string
	ResourceIDs *[]string
	ShowDeleted bool
	Limit       *int
	Offset      *int
	Filter      *ListResourceFilter
}

// GetInstance gets an instance by the resource_id.
func (s *Store) GetInstance(ctx context.Context, find *FindInstanceMessage) (*InstanceMessage, error) {
	if find.ResourceID != nil {
		if v, ok := s.instanceCache.Get(getInstanceCacheKey(*find.ResourceID)); ok && !s.cacheDisabled {
			return v, nil
		}
	}

	// We will always return the resource regardless of its deleted state.
	find.ShowDeleted = true

	instances, err := s.ListInstances(ctx, find)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list instances with find instance message %+v", find)
	}
	if len(instances) == 0 {
		return nil, nil
	}
	if len(instances) > 1 {
		return nil, errors.Errorf("find %d instances with find instance message %+v, expected 1", len(instances), find)
	}

	instance := instances[0]
	s.instanceCache.Add(getInstanceCacheKey(instance.ResourceID), instance)
	return instance, nil
}

// ListInstances lists all instance.
func (s *Store) ListInstances(ctx context.Context, find *FindInstanceMessage) ([]*InstanceMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	instances, err := s.listInstanceImpl(ctx, tx, find)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	for _, instance := range instances {
		s.instanceCache.Add(getInstanceCacheKey(instance.ResourceID), instance)
	}
	return instances, nil
}

// mapCreateInstanceError turns a resource_id collision into the API's
// AlreadyExists contract. The instance id is chosen by the caller, so a duplicate
// is a client error ("pick another id") rather than an internal failure; without
// this the store returned the raw driver error and the RPC answered 500.
func mapCreateInstanceError(resourceID string, err error) error {
	if isUniqueViolation(err) {
		return common.Errorf(common.Conflict, "instance %q already exists", resourceID)
	}
	return err
}

// CreateInstance creates an instance.
func (s *Store) CreateInstance(ctx context.Context, instanceCreate *InstanceMessage) (*InstanceMessage, error) {
	if err := validateDataSources(instanceCreate.Metadata); err != nil {
		return nil, err
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	redacted, err := s.encryptInstance(instanceCreate.Metadata)
	if err != nil {
		return nil, err
	}
	metadataBytes, err := protojson.Marshal(redacted)
	if err != nil {
		return nil, err
	}
	var environment *string
	if instanceCreate.EnvironmentID != "" {
		environment = &instanceCreate.EnvironmentID
	}
	if _, err := tx.ExecContext(ctx, `
			INSERT INTO instance (
				resource_id,
				environment,
				metadata
			) VALUES ($1, $2, $3)
		`,
		instanceCreate.ResourceID,
		environment,
		metadataBytes,
	); err != nil {
		return nil, mapCreateInstanceError(instanceCreate.ResourceID, err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	instance := &InstanceMessage{
		EnvironmentID: instanceCreate.EnvironmentID,
		ResourceID:    instanceCreate.ResourceID,
		Metadata:      instanceCreate.Metadata,
	}
	s.instanceCache.Add(getInstanceCacheKey(instance.ResourceID), instance)
	return instance, nil
}

// UpdateInstance updates an instance.
func (s *Store) UpdateInstance(ctx context.Context, patch *UpdateInstanceMessage) (*InstanceMessage, error) {
	if patch.Metadata != nil && len(patch.MetadataUpdates) > 0 {
		return nil, errors.New("metadata and metadataUpdates are mutually exclusive")
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	set, args, where := []string{}, []any{}, []string{}
	if v := patch.EnvironmentID; v != nil {
		set, args = append(set, fmt.Sprintf("environment = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Deleted; v != nil {
		set, args = append(set, fmt.Sprintf(`deleted = $%d`, len(args)+1)), append(args, *v)
	}
	if v := patch.Metadata; v != nil {
		redacted, err := s.encryptInstance(v)
		if err != nil {
			return nil, err
		}
		metadata, err := protojson.Marshal(redacted)
		if err != nil {
			return nil, err
		}
		set, args = append(set, fmt.Sprintf("metadata = $%d", len(args)+1)), append(args, metadata)
	}
	if len(patch.MetadataUpdates) > 0 {
		metadata, err := s.patchInstanceMetadata(ctx, tx, patch.ResourceID, patch.MetadataUpdates)
		if err != nil {
			return nil, err
		}
		set, args = append(set, fmt.Sprintf("metadata = $%d", len(args)+1)), append(args, metadata)
	}
	where, args = append(where, fmt.Sprintf("resource_id = $%d", len(args)+1)), append(args, patch.ResourceID)

	if len(set) > 0 {
		query := fmt.Sprintf(`
			UPDATE instance
			SET `+strings.Join(set, ", ")+`
			WHERE %s
		`, strings.Join(where, " AND "))
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.instanceCache.Remove(getInstanceCacheKey(patch.ResourceID))
	return s.GetInstance(ctx, &FindInstanceMessage{ResourceID: &patch.ResourceID})
}

// patchInstanceMetadata reads the instance's metadata under a row lock, applies
// the field updates and returns the metadata bytes to write. Locking and reading
// in the same transaction as the write is what makes the patch a merge: a caller
// that built its update from an older read cannot overwrite a concurrent change
// to another field.
func (s *Store) patchInstanceMetadata(ctx context.Context, tx *sql.Tx, resourceID string, updates []func(*storepb.Instance)) ([]byte, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `
		SELECT metadata FROM instance WHERE resource_id = $1 FOR UPDATE
	`, resourceID).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.NotFound, "instance %q not found", resourceID)
		}
		return nil, errors.Wrapf(err, "failed to read the metadata of instance %q", resourceID)
	}

	cipher, err := s.credentialCipher()
	if err != nil {
		return nil, err
	}
	return applyInstanceMetadataUpdates(raw, cipher, updates)
}

// applyInstanceMetadataUpdates decrypts stored metadata, applies the updates and
// re-encrypts the result. It is the pure part of the patch path, kept separate so
// the merge semantics can be tested without a database.
func applyInstanceMetadataUpdates(raw []byte, cipher *crypto.Cipher, updates []func(*storepb.Instance)) ([]byte, error) {
	metadata := &storepb.Instance{}
	if err := common.ProtojsonUnmarshaler.Unmarshal(raw, metadata); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal the stored instance metadata")
	}
	if err := decryptInstance(metadata, cipher); err != nil {
		return nil, errors.Wrap(err, "failed to decrypt the stored instance credentials")
	}
	for _, update := range updates {
		update(metadata)
	}
	redacted, err := encryptInstanceMetadata(metadata, cipher)
	if err != nil {
		return nil, err
	}
	metadataBytes, err := protojson.Marshal(redacted)
	if err != nil {
		return nil, err
	}
	return metadataBytes, nil
}

func (s *Store) listInstanceImpl(ctx context.Context, txn *sql.Tx, find *FindInstanceMessage) ([]*InstanceMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	joinDSQuery := ""
	if filter := find.Filter; filter != nil {
		where = append(where, filter.Where)
		args = append(args, filter.Args...)
		if hasHostPortFilter(filter.Where) {
			joinDSQuery = "CROSS JOIN jsonb_array_elements(instance.metadata -> 'dataSources') AS ds"
		}
	}
	if v := find.ResourceID; v != nil {
		where, args = append(where, fmt.Sprintf("instance.resource_id = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.ResourceIDs; v != nil {
		where, args = append(where, fmt.Sprintf("instance.resource_id = ANY($%d)", len(args)+1)), append(args, *v)
	}
	if !find.ShowDeleted {
		where, args = append(where, fmt.Sprintf("instance.deleted = $%d", len(args)+1)), append(args, false)
	}

	query := fmt.Sprintf(`
		SELECT DISTINCT ON (resource_id)
			instance.resource_id,
			instance.environment,
			instance.deleted,
			instance.metadata
		FROM instance
		%s
		WHERE %s
		ORDER BY resource_id`, joinDSQuery, strings.Join(where, " AND "))
	if v := find.Limit; v != nil {
		query += fmt.Sprintf(" LIMIT %d", *v)
	}
	if v := find.Offset; v != nil {
		query += fmt.Sprintf(" OFFSET %d", *v)
	}

	// Resolve the cipher once for the whole page: the per-row lookup took the
	// cipher lock for every instance.
	cipher, err := s.credentialCipher()
	if err != nil {
		return nil, err
	}

	var instanceMessages []*InstanceMessage
	rows, err := txn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var instanceMessage InstanceMessage
		var environment sql.NullString
		var metadata []byte
		if err := rows.Scan(
			&instanceMessage.ResourceID,
			&environment,
			&instanceMessage.Deleted,
			&metadata,
		); err != nil {
			return nil, err
		}
		if environment.Valid {
			instanceMessage.EnvironmentID = environment.String
		}

		instanceMetadata := &storepb.Instance{}
		if err := common.ProtojsonUnmarshaler.Unmarshal(metadata, instanceMetadata); err != nil {
			return nil, err
		}
		if err := decryptInstance(instanceMetadata, cipher); err != nil {
			return nil, errors.Wrapf(err, "failed to decrypt the credentials of instance %s", instanceMessage.ResourceID)
		}
		instanceMessage.Metadata = instanceMetadata
		instanceMessages = append(instanceMessages, &instanceMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return instanceMessages, nil
}

func hasHostPortFilter(where string) bool {
	return strings.Contains(where, "ds ->> 'host'") || strings.Contains(where, "ds ->> 'port'")
}

var countActivateInstanceQuery = "SELECT COUNT(1) FROM instance WHERE (metadata ? 'activation') AND (metadata->>'activation')::boolean = TRUE AND deleted = FALSE"

// GetActivatedInstanceCount gets the number of activated instances.
func (s *Store) GetActivatedInstanceCount(ctx context.Context) (int, error) {
	var count int
	if err := s.GetDB().QueryRowContext(ctx, countActivateInstanceQuery).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func validateDataSources(metadata *storepb.Instance) error {
	dataSourceMap := map[string]bool{}
	adminCount := 0
	for _, dataSource := range metadata.GetDataSources() {
		if dataSourceMap[dataSource.GetId()] {
			return errors.Errorf("duplicate data source ID %s", dataSource.GetId())
		}
		dataSourceMap[dataSource.GetId()] = true
		if dataSource.GetType() == storepb.DataSourceType_ADMIN {
			adminCount++
		}
	}
	if adminCount != 1 {
		return errors.Errorf("require exactly one admin data source")
	}
	return nil
}

// IsObjectCaseSensitive returns true if the engine ignores database and table case sensitive.
func IsObjectCaseSensitive(instance *InstanceMessage) bool {
	switch instance.Metadata.GetEngine() {
	case storepb.Engine_TIDB:
		return false
	case storepb.Engine_MYSQL, storepb.Engine_MARIADB, storepb.Engine_OCEANBASE:
		return instance.Metadata == nil || instance.Metadata.MysqlLowerCaseTableNames == 0
	default:
		return true
	}
}

// encryptInstance returns a clone with every credential field replaced by its
// ciphertext and the plaintext cleared.
func (s *Store) encryptInstance(instance *storepb.Instance) (*storepb.Instance, error) {
	cipher, err := s.credentialCipher()
	if err != nil {
		return nil, err
	}
	return encryptInstanceMetadata(instance, cipher)
}

// encryptInstanceMetadata is encryptInstance with the cipher resolved by the
// caller, so a transaction that already resolved it can reuse it.
func encryptInstanceMetadata(instance *storepb.Instance, cipher *crypto.Cipher) (*storepb.Instance, error) {
	redacted, ok := proto.Clone(instance).(*storepb.Instance)
	if !ok {
		return nil, errors.Errorf("failed to clone instance")
	}
	for _, ds := range redacted.GetDataSources() {
		for _, field := range credentialFields(ds) {
			encrypted, err := cipher.Encrypt(*field.plaintext)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to encrypt the %s of data source %s", field.name, ds.GetId())
			}
			*field.ciphertext = encrypted
			*field.plaintext = ""
		}
	}
	return redacted, nil
}

// decryptInstance decrypts every credential field of an instance in place with
// the cipher the caller resolved once for the whole page.
func decryptInstance(instance *storepb.Instance, cipher *crypto.Cipher) error {
	for _, ds := range instance.GetDataSources() {
		for _, field := range credentialFields(ds) {
			plaintext, err := cipher.Decrypt(*field.ciphertext)
			if err != nil {
				return errors.Wrapf(err, "failed to decrypt the %s of data source %s", field.name, ds.GetId())
			}
			*field.plaintext = plaintext
		}
	}
	return nil
}

// credentialField pairs a plaintext data source field with the ciphertext field
// that stores it. The name labels the pair in errors, so a credential that fails
// to decrypt says which one it was.
type credentialField struct {
	name       string
	plaintext  *string
	ciphertext *string
}

func credentialFields(ds *storepb.DataSource) []credentialField {
	return []credentialField{
		{name: "password", plaintext: &ds.Password, ciphertext: &ds.PasswordCiphertext},
		{name: "ssl_ca", plaintext: &ds.SslCa, ciphertext: &ds.SslCaCiphertext},
		{name: "ssl_cert", plaintext: &ds.SslCert, ciphertext: &ds.SslCertCiphertext},
		{name: "ssl_key", plaintext: &ds.SslKey, ciphertext: &ds.SslKeyCiphertext},
		{name: "ssh_password", plaintext: &ds.SshPassword, ciphertext: &ds.SshPasswordCiphertext},
		{name: "ssh_private_key", plaintext: &ds.SshPrivateKey, ciphertext: &ds.SshPrivateKeyCiphertext},
	}
}
