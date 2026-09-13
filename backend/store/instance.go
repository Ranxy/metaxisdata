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

	redacted, err := s.obfuscateInstance(ctx, instanceCreate.Metadata)
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
		return nil, err
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
	set, args, where := []string{}, []any{}, []string{}
	if v := patch.EnvironmentID; v != nil {
		set, args = append(set, fmt.Sprintf("environment = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Deleted; v != nil {
		set, args = append(set, fmt.Sprintf(`deleted = $%d`, len(args)+1)), append(args, *v)
	}
	if v := patch.Metadata; v != nil {
		redacted, err := s.obfuscateInstance(ctx, v)
		if err != nil {
			return nil, err
		}
		metadata, err := protojson.Marshal(redacted)
		if err != nil {
			return nil, err
		}
		set, args = append(set, fmt.Sprintf("metadata = $%d", len(args)+1)), append(args, metadata)
	}
	where, args = append(where, fmt.Sprintf("resource_id = $%d", len(args)+1)), append(args, patch.ResourceID)

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

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

	// Resolve the secret once for the whole page: the per-row lookup took the
	// secret lock for every instance.
	secret, err := s.GetSecret(ctx)
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
		if err := unObfuscateInstanceWithSecret(instanceMetadata, secret); err != nil {
			return nil, err
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

// obfuscateInstance returns a clone with every credential field replaced by its
// obfuscated form and the plaintext cleared.
func (s *Store) obfuscateInstance(ctx context.Context, instance *storepb.Instance) (*storepb.Instance, error) {
	secret, err := s.GetSecret(ctx)
	if err != nil {
		return nil, err
	}

	redacted, ok := proto.Clone(instance).(*storepb.Instance)
	if !ok {
		return nil, errors.Errorf("failed to clone instance")
	}
	for _, ds := range redacted.GetDataSources() {
		for _, field := range secretFields(ds) {
			obfuscated, err := common.Obfuscate(*field.plaintext, secret)
			if err != nil {
				return nil, err
			}
			*field.obfuscated = obfuscated
			*field.plaintext = ""
		}
	}
	return redacted, nil
}

// unObfuscateInstanceWithSecret decrypts every credential field with a secret
// the caller already resolved, so reading a list of instances resolves it once
// instead of once per row.
func unObfuscateInstanceWithSecret(instance *storepb.Instance, secret string) error {
	for _, ds := range instance.GetDataSources() {
		for _, field := range secretFields(ds) {
			plaintext, err := common.Unobfuscate(*field.obfuscated, secret)
			if err != nil {
				return err
			}
			*field.plaintext = plaintext
		}
	}
	return nil
}

// secretField pairs a plaintext data source field with its obfuscated
// counterpart.
type secretField struct {
	plaintext  *string
	obfuscated *string
}

func secretFields(ds *storepb.DataSource) []secretField {
	return []secretField{
		{plaintext: &ds.Password, obfuscated: &ds.ObfuscatedPassword},
		{plaintext: &ds.SslCa, obfuscated: &ds.ObfuscatedSslCa},
		{plaintext: &ds.SslCert, obfuscated: &ds.ObfuscatedSslCert},
		{plaintext: &ds.SslKey, obfuscated: &ds.ObfuscatedSslKey},
		{plaintext: &ds.SshPassword, obfuscated: &ds.ObfuscatedSshPassword},
		{plaintext: &ds.SshPrivateKey, obfuscated: &ds.ObfuscatedSshPrivateKey},
	}
}
