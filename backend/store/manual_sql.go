package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

const manualSQLGUIDPrefix = "__manual_sql__/"

// ManualSQLMessage is the store representation of a user-maintained SQL entry.
type ManualSQLMessage struct {
	ID                 int64
	GUID               string
	ManualSQLID        string
	InstanceResourceID string
	DatabaseName       string
	SchemaName         string
	Name               string
	Title              string
	Comment            string
	SQLText            string
	Tags               []string
	Attributes         map[string]string
	ContentSearch      string
	CreatedBy          string
	UpdatedBy          string
	Deleted            bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// FindManualSQLMessage is the query filter for manual SQL entries.
type FindManualSQLMessage struct {
	GUID               *string
	ManualSQLID        *string
	InstanceResourceID *string
	DatabaseName       *string
	SchemaName         *string
	Tags               *[]string
	Query              *string
	ShowDeleted        bool
	Limit              *int
	Offset             *int
}

// UpdateManualSQLMessage contains the mutable fields for a manual SQL entry.
type UpdateManualSQLMessage struct {
	Name       *string
	Title      *string
	Comment    *string
	SQLText    *string
	SchemaName *string
	Tags       *[]string
	Attributes *map[string]string
	UpdatedBy  *string
	Deleted    *bool
}

func buildManualSQLGUID(instanceResourceID, databaseName, schemaName, manualSQLID string) string {
	return strings.Join([]string{instanceResourceID, databaseName, schemaName, manualSQLGUIDPrefix + manualSQLID}, common.MetaGUIDSplit)
}

func normalizeManualSQLTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}

	seen := make(map[string]string, len(tags))
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" {
			continue
		}
		normalized := strings.ToLower(trimmed)
		if _, ok := seen[normalized]; !ok {
			seen[normalized] = trimmed
		}
	}

	if len(seen) == 0 {
		return nil
	}

	normalizedKeys := make([]string, 0, len(seen))
	for key := range seen {
		normalizedKeys = append(normalizedKeys, key)
	}
	slices.Sort(normalizedKeys)

	result := make([]string, 0, len(normalizedKeys))
	for _, key := range normalizedKeys {
		result = append(result, seen[key])
	}
	return result
}

func normalizeManualSQLAttributes(attributes map[string]string) map[string]string {
	if len(attributes) == 0 {
		return nil
	}

	normalized := make(map[string]string, len(attributes))
	keys := make([]string, 0, len(attributes))
	for key := range attributes {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	for _, key := range keys {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		normalized[trimmedKey] = strings.TrimSpace(attributes[key])
	}

	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func buildManualSQLSearchDocument(msg *ManualSQLMessage) string {
	parts := []string{
		strings.TrimSpace(msg.ManualSQLID),
		strings.TrimSpace(msg.Name),
		strings.TrimSpace(msg.Title),
		strings.TrimSpace(msg.Comment),
		strings.TrimSpace(msg.SQLText),
	}

	tags := normalizeManualSQLTags(msg.Tags)
	parts = append(parts, tags...)

	attributes := normalizeManualSQLAttributes(msg.Attributes)
	if len(attributes) > 0 {
		keys := make([]string, 0, len(attributes))
		for key := range attributes {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			parts = append(parts, key)
			parts = append(parts, attributes[key])
			parts = append(parts, fmt.Sprintf("%s %s", key, attributes[key]))
		}
	}

	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		filtered = append(filtered, trimmed)
	}

	return strings.Join(filtered, "\n")
}

func buildManualSQLStoredMetadata(msg *ManualSQLMessage) *storepb.StoredMetadata {
	tags := normalizeManualSQLTags(msg.Tags)
	attributes := normalizeManualSQLAttributes(msg.Attributes)

	return &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_ManualSqlMetadata{
			ManualSqlMetadata: &storepb.ManualSQLMetadata{
				ManualSqlId:      msg.ManualSQLID,
				Name:             msg.Name,
				Title:            msg.Title,
				Comment:          msg.Comment,
				SqlText:          msg.SQLText,
				Tags:             tags,
				Attributes:       attributes,
				SchemaName:       msg.SchemaName,
				InstanceResource: common.FormatInstance(msg.InstanceResourceID),
				DatabaseName:     msg.DatabaseName,
			},
		},
	}
}

func prepareManualSQLMessage(msg *ManualSQLMessage) *ManualSQLMessage {
	prepared := *msg
	prepared.ManualSQLID = strings.TrimSpace(prepared.ManualSQLID)
	prepared.Name = strings.TrimSpace(prepared.Name)
	if prepared.ManualSQLID == "" {
		prepared.ManualSQLID = prepared.Name
	}
	if prepared.Name == "" {
		prepared.Name = prepared.ManualSQLID
	}
	prepared.InstanceResourceID = strings.TrimSpace(prepared.InstanceResourceID)
	prepared.DatabaseName = strings.TrimSpace(prepared.DatabaseName)
	prepared.SchemaName = strings.TrimSpace(prepared.SchemaName)
	prepared.Title = strings.TrimSpace(prepared.Title)
	prepared.Comment = strings.TrimSpace(prepared.Comment)
	prepared.SQLText = strings.TrimSpace(prepared.SQLText)
	prepared.CreatedBy = strings.TrimSpace(prepared.CreatedBy)
	prepared.UpdatedBy = strings.TrimSpace(prepared.UpdatedBy)
	prepared.Tags = normalizeManualSQLTags(prepared.Tags)
	prepared.Attributes = normalizeManualSQLAttributes(prepared.Attributes)
	if prepared.GUID == "" {
		prepared.GUID = buildManualSQLGUID(prepared.InstanceResourceID, prepared.DatabaseName, prepared.SchemaName, prepared.ManualSQLID)
	}
	prepared.ContentSearch = buildManualSQLSearchDocument(&prepared)
	return &prepared
}

// CreateManualSQL creates or revives a manual SQL entry and mirrors it into meta_registry_resource.
func (s *Store) CreateManualSQL(ctx context.Context, msg *ManualSQLMessage) (*ManualSQLMessage, error) {
	prepared := prepareManualSQLMessage(msg)
	observedAt := time.Now().UTC()

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	created, err := upsertManualSQLRow(ctx, tx, prepared)
	if err != nil {
		return nil, err
	}

	if err := replaceManualSQLTags(ctx, tx, created.ID, created.Tags); err != nil {
		return nil, err
	}
	if err := replaceManualSQLAttributes(ctx, tx, created.ID, created.Attributes); err != nil {
		return nil, err
	}
	if err := s.upsertManualSQLMetaRegistryAt(ctx, tx, created, observedAt); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}

	return s.GetManualSQL(ctx, &FindManualSQLMessage{GUID: &created.GUID, ShowDeleted: true})
}

// GetManualSQL returns one manual SQL entry or nil if none exists.
func (s *Store) GetManualSQL(ctx context.Context, find *FindManualSQLMessage) (*ManualSQLMessage, error) {
	list, err := s.ListManualSQL(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	if len(list) > 1 {
		return nil, errors.Errorf("found %d manual SQL records with filter %+v, expect 1", len(list), find)
	}
	return list[0], nil
}

// ListManualSQL lists manual SQL entries matching the filter.
func (s *Store) ListManualSQL(ctx context.Context, find *FindManualSQLMessage) ([]*ManualSQLMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	list, err := listManualSQLImpl(ctx, tx, find)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return list, nil
}

// UpdateManualSQL updates a manual SQL entry by GUID.
func (s *Store) UpdateManualSQL(ctx context.Context, guid string, patch *UpdateManualSQLMessage) (*ManualSQLMessage, error) {
	observedAt := time.Now().UTC()

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	currentList, err := listManualSQLImpl(ctx, tx, &FindManualSQLMessage{GUID: &guid, ShowDeleted: true})
	if err != nil {
		return nil, err
	}
	if len(currentList) == 0 {
		return nil, errors.Errorf("manual SQL %q not found", guid)
	}
	current := currentList[0]
	updated := *current

	if patch.Name != nil {
		updated.Name = *patch.Name
	}
	if patch.Title != nil {
		updated.Title = *patch.Title
	}
	if patch.Comment != nil {
		updated.Comment = *patch.Comment
	}
	if patch.SQLText != nil {
		updated.SQLText = *patch.SQLText
	}
	if patch.SchemaName != nil {
		updated.SchemaName = *patch.SchemaName
	}
	if patch.Tags != nil {
		updated.Tags = *patch.Tags
	}
	if patch.Attributes != nil {
		updated.Attributes = *patch.Attributes
	}
	if patch.UpdatedBy != nil {
		updated.UpdatedBy = *patch.UpdatedBy
	}
	if patch.Deleted != nil {
		updated.Deleted = *patch.Deleted
	}

	prepared := prepareManualSQLMessage(&updated)
	oldGUID := current.GUID

	row, err := updateManualSQLRow(ctx, tx, oldGUID, prepared)
	if err != nil {
		return nil, err
	}
	if err := replaceManualSQLTags(ctx, tx, row.ID, row.Tags); err != nil {
		return nil, err
	}
	if err := replaceManualSQLAttributes(ctx, tx, row.ID, row.Attributes); err != nil {
		return nil, err
	}

	if oldGUID != row.GUID || row.Deleted {
		if err := s.deleteManualSQLMetaRegistryTx(ctx, tx, oldGUID, observedAt); err != nil {
			return nil, err
		}
		if err := deleteColumnLineageByMetaTx(ctx, tx, oldGUID); err != nil {
			return nil, err
		}
	}
	if !row.Deleted {
		if err := s.upsertManualSQLMetaRegistryAt(ctx, tx, row, observedAt); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}

	return s.GetManualSQL(ctx, &FindManualSQLMessage{GUID: &row.GUID, ShowDeleted: true})
}

func buildDeleteManualSQLStatement(guid string, updatedBy *string) (string, []any) {
	sets := []string{"deleted = TRUE", "updated_at = NOW()"}
	args := make([]any, 0, 2)
	if updatedBy != nil {
		sets = append(sets, fmt.Sprintf("updated_by = $%d", len(args)+1))
		args = append(args, strings.TrimSpace(*updatedBy))
	}
	args = append(args, guid)

	return `
		UPDATE manual_sql
		SET ` + strings.Join(sets, ", ") + `
		WHERE guid = $` + fmt.Sprintf("%d", len(args)), args
}

func buildDeleteManualSQLMetaRegistryStatement(guid string) (string, []any) {
	where, args := appendGUIDSubtreeCondition(nil, nil, "guid", guid)
	return `DELETE FROM meta_registry_resource WHERE ` + where[0], args
}

func buildDeleteColumnLineageByGUIDStatement(tableName, guid string) (string, []any) {
	where, args := appendGUIDSubtreeCondition(nil, nil, "meta_guid", guid)
	return `DELETE FROM ` + tableName + ` WHERE ` + where[0], args
}

// DeleteManualSQL soft-deletes a manual SQL entry and removes its registry mirror and lineage.
func (s *Store) DeleteManualSQL(ctx context.Context, guid string, updatedBy *string) error {
	observedAt := time.Now().UTC()

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	query, args := buildDeleteManualSQLStatement(guid, updatedBy)
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return errors.Wrap(err, "failed to delete manual SQL")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected for manual SQL delete")
	}
	if affected == 0 {
		return errors.Errorf("manual SQL %q not found", guid)
	}

	if err := s.deleteManualSQLMetaRegistryTx(ctx, tx, guid, observedAt); err != nil {
		return err
	}
	if err := deleteColumnLineageByMetaTx(ctx, tx, guid); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit transaction")
	}
	return nil
}

func upsertManualSQLRow(ctx context.Context, tx *sql.Tx, msg *ManualSQLMessage) (*ManualSQLMessage, error) {
	row := &ManualSQLMessage{}
	err := tx.QueryRowContext(ctx, `
		INSERT INTO manual_sql (
			guid,
			deleted,
			instance_resource_id,
			database_name,
			schema_name,
			name,
			title,
			comment,
			sql_text,
			content_search,
			search_vector,
			created_by,
			updated_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, to_tsvector('simple', $10), $11, $12)
		ON CONFLICT (instance_resource_id, database_name, name) DO UPDATE SET
			guid = EXCLUDED.guid,
			deleted = EXCLUDED.deleted,
			schema_name = EXCLUDED.schema_name,
			title = EXCLUDED.title,
			comment = EXCLUDED.comment,
			sql_text = EXCLUDED.sql_text,
			content_search = EXCLUDED.content_search,
			search_vector = EXCLUDED.search_vector,
			updated_by = EXCLUDED.updated_by,
			updated_at = NOW()
		RETURNING id, guid, deleted, instance_resource_id, database_name, schema_name, name, title, comment, sql_text, content_search, created_by, updated_by, created_at, updated_at
	`, msg.GUID, msg.Deleted, msg.InstanceResourceID, msg.DatabaseName, msg.SchemaName, msg.Name, msg.Title, msg.Comment, msg.SQLText, msg.ContentSearch, msg.CreatedBy, msg.UpdatedBy).Scan(
		&row.ID,
		&row.GUID,
		&row.Deleted,
		&row.InstanceResourceID,
		&row.DatabaseName,
		&row.SchemaName,
		&row.Name,
		&row.Title,
		&row.Comment,
		&row.SQLText,
		&row.ContentSearch,
		&row.CreatedBy,
		&row.UpdatedBy,
		&row.CreatedAt,
		&row.UpdatedAt,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to upsert manual SQL row")
	}
	row.ManualSQLID = row.Name
	row.Tags = msg.Tags
	row.Attributes = msg.Attributes
	return row, nil
}

func updateManualSQLRow(ctx context.Context, tx *sql.Tx, currentGUID string, msg *ManualSQLMessage) (*ManualSQLMessage, error) {
	row := &ManualSQLMessage{}
	err := tx.QueryRowContext(ctx, `
		UPDATE manual_sql
		SET guid = $1,
			deleted = $2,
			schema_name = $3,
			name = $4,
			title = $5,
			comment = $6,
			sql_text = $7,
			content_search = $8,
			search_vector = to_tsvector('simple', $8),
			updated_by = $9,
			updated_at = NOW()
		WHERE guid = $10
		RETURNING id, guid, deleted, instance_resource_id, database_name, schema_name, name, title, comment, sql_text, content_search, created_by, updated_by, created_at, updated_at
	`, msg.GUID, msg.Deleted, msg.SchemaName, msg.Name, msg.Title, msg.Comment, msg.SQLText, msg.ContentSearch, msg.UpdatedBy, currentGUID).Scan(
		&row.ID,
		&row.GUID,
		&row.Deleted,
		&row.InstanceResourceID,
		&row.DatabaseName,
		&row.SchemaName,
		&row.Name,
		&row.Title,
		&row.Comment,
		&row.SQLText,
		&row.ContentSearch,
		&row.CreatedBy,
		&row.UpdatedBy,
		&row.CreatedAt,
		&row.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.Errorf("manual SQL %q not found", currentGUID)
		}
		return nil, errors.Wrap(err, "failed to update manual SQL row")
	}
	row.ManualSQLID = row.Name
	row.Tags = msg.Tags
	row.Attributes = msg.Attributes
	return row, nil
}

func listManualSQLImpl(ctx context.Context, tx *sql.Tx, find *FindManualSQLMessage) ([]*ManualSQLMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	if find == nil {
		find = &FindManualSQLMessage{}
	}
	if find.GUID != nil {
		where, args = append(where, fmt.Sprintf("guid = $%d", len(args)+1)), append(args, strings.TrimSpace(*find.GUID))
	}
	if find.ManualSQLID != nil {
		where, args = append(where, fmt.Sprintf("name = $%d", len(args)+1)), append(args, strings.TrimSpace(*find.ManualSQLID))
	}
	if find.InstanceResourceID != nil {
		where, args = append(where, fmt.Sprintf("instance_resource_id = $%d", len(args)+1)), append(args, strings.TrimSpace(*find.InstanceResourceID))
	}
	if find.DatabaseName != nil {
		where, args = append(where, fmt.Sprintf("database_name = $%d", len(args)+1)), append(args, strings.TrimSpace(*find.DatabaseName))
	}
	if find.SchemaName != nil {
		where, args = append(where, fmt.Sprintf("schema_name = $%d", len(args)+1)), append(args, strings.TrimSpace(*find.SchemaName))
	}
	if !find.ShowDeleted {
		where = append(where, "deleted = FALSE")
	}

	if find.Tags != nil {
		for _, tag := range normalizeManualSQLTags(*find.Tags) {
			where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM manual_sql_tag mst WHERE mst.manual_sql_id = manual_sql.id AND mst.tag_norm = $%d)", len(args)+1))
			args = append(args, strings.ToLower(tag))
		}
	}

	if find.Query != nil {
		queryText := strings.TrimSpace(*find.Query)
		if queryText != "" {
			where = append(where, fmt.Sprintf("search_vector @@ plainto_tsquery('simple', $%d)", len(args)+1))
			args = append(args, queryText)
		}
	}

	query := `
		SELECT id, guid, deleted, instance_resource_id, database_name, schema_name, name, title, comment, sql_text, content_search, created_by, updated_by, created_at, updated_at
		FROM manual_sql
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY updated_at DESC, id DESC`
	if find.Limit != nil {
		query += fmt.Sprintf(" LIMIT %d", *find.Limit)
	}
	if find.Offset != nil {
		query += fmt.Sprintf(" OFFSET %d", *find.Offset)
	}

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query manual SQL")
	}
	defer rows.Close()

	var result []*ManualSQLMessage
	var ids []int64
	for rows.Next() {
		var msg ManualSQLMessage
		if err := rows.Scan(
			&msg.ID,
			&msg.GUID,
			&msg.Deleted,
			&msg.InstanceResourceID,
			&msg.DatabaseName,
			&msg.SchemaName,
			&msg.Name,
			&msg.Title,
			&msg.Comment,
			&msg.SQLText,
			&msg.ContentSearch,
			&msg.CreatedBy,
			&msg.UpdatedBy,
			&msg.CreatedAt,
			&msg.UpdatedAt,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan manual SQL")
		}
		msg.ManualSQLID = msg.Name
		result = append(result, &msg)
		ids = append(ids, msg.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for manual SQL")
	}

	tagMap, err := listManualSQLTags(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	attributeMap, err := listManualSQLAttributes(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	for _, item := range result {
		item.Tags = tagMap[item.ID]
		item.Attributes = attributeMap[item.ID]
	}

	return result, nil
}

func listManualSQLTags(ctx context.Context, tx *sql.Tx, ids []int64) (map[int64][]string, error) {
	result := make(map[int64][]string)
	if len(ids) == 0 {
		return result, nil
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT manual_sql_id, tag
		FROM manual_sql_tag
		WHERE manual_sql_id = ANY($1)
		ORDER BY manual_sql_id, tag_norm, tag
	`, pq.Array(ids))
	if err != nil {
		return nil, errors.Wrap(err, "failed to query manual SQL tags")
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var tag string
		if err := rows.Scan(&id, &tag); err != nil {
			return nil, errors.Wrap(err, "failed to scan manual SQL tag")
		}
		result[id] = append(result[id], tag)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for manual SQL tags")
	}
	return result, nil
}

func listManualSQLAttributes(ctx context.Context, tx *sql.Tx, ids []int64) (map[int64]map[string]string, error) {
	result := make(map[int64]map[string]string)
	if len(ids) == 0 {
		return result, nil
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT manual_sql_id, attr_key, attr_value
		FROM manual_sql_attribute
		WHERE manual_sql_id = ANY($1)
		ORDER BY manual_sql_id, attr_key_norm
	`, pq.Array(ids))
	if err != nil {
		return nil, errors.Wrap(err, "failed to query manual SQL attributes")
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var key string
		var value string
		if err := rows.Scan(&id, &key, &value); err != nil {
			return nil, errors.Wrap(err, "failed to scan manual SQL attribute")
		}
		if _, ok := result[id]; !ok {
			result[id] = make(map[string]string)
		}
		result[id][key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for manual SQL attributes")
	}
	return result, nil
}

func replaceManualSQLTags(ctx context.Context, tx *sql.Tx, manualSQLID int64, tags []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM manual_sql_tag WHERE manual_sql_id = $1`, manualSQLID); err != nil {
		return errors.Wrap(err, "failed to delete manual SQL tags")
	}
	for _, tag := range normalizeManualSQLTags(tags) {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO manual_sql_tag (manual_sql_id, tag, tag_norm)
			VALUES ($1, $2, $3)
		`, manualSQLID, tag, strings.ToLower(tag)); err != nil {
			return errors.Wrap(err, "failed to insert manual SQL tag")
		}
	}
	return nil
}

func replaceManualSQLAttributes(ctx context.Context, tx *sql.Tx, manualSQLID int64, attributes map[string]string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM manual_sql_attribute WHERE manual_sql_id = $1`, manualSQLID); err != nil {
		return errors.Wrap(err, "failed to delete manual SQL attributes")
	}
	for key, value := range normalizeManualSQLAttributes(attributes) {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO manual_sql_attribute (manual_sql_id, attr_key, attr_value, attr_key_norm, attr_value_norm)
			VALUES ($1, $2, $3, $4, $5)
		`, manualSQLID, key, value, strings.ToLower(key), strings.ToLower(value)); err != nil {
			return errors.Wrap(err, "failed to insert manual SQL attribute")
		}
	}
	return nil
}

func (s *Store) upsertManualSQLMetaRegistryAt(ctx context.Context, tx *sql.Tx, msg *ManualSQLMessage, observedAt time.Time) error {
	storedMetadata := buildManualSQLStoredMetadata(msg)
	metadataBytes, metaHash, err := CalcStoreMetaHash(storedMetadata)
	if err != nil {
		return errors.Wrap(err, "failed to calculate manual SQL metadata hash")
	}
	_, err = s.BatchCreateMetaRegistryResourceAt(ctx, tx, []*CreateMetaRegistryResourceMessage{{
		MetaRegistryResource: MetaRegistryResource{
			GUID:       msg.GUID,
			ObjectType: storepb.MetaType_MANUAL_SQL,
			Metadata:   storedMetadata,
			MetaHash:   metaHash,
		},
		MetadataBytes: metadataBytes,
	}}, observedAt)
	if err != nil {
		return errors.Wrap(err, "failed to mirror manual SQL into meta registry")
	}
	return nil
}

func (s *Store) deleteManualSQLMetaRegistryTx(ctx context.Context, tx *sql.Tx, guid string, observedAt time.Time) error {
	list, err := s.listMetaRegistryResourceImpl(ctx, tx, &FindMetaRegistryResourceMessage{GUIDPrefix: &guid}, false)
	if err != nil {
		return errors.Wrap(err, "failed to list manual SQL meta registry entries")
	}
	if err := s.BatchDeleteMetaRegistryAt(ctx, tx, list, observedAt); err != nil {
		return errors.Wrap(err, "failed to delete manual SQL meta registry entry")
	}
	return nil
}

func deleteColumnLineageByMetaTx(ctx context.Context, tx *sql.Tx, metaGUID string) error {
	lineageQuery, lineageArgs := buildDeleteColumnLineageByGUIDStatement("column_lineage", metaGUID)
	if _, err := tx.ExecContext(ctx, lineageQuery, lineageArgs...); err != nil {
		return err
	}
	versionQuery, versionArgs := buildDeleteColumnLineageByGUIDStatement("column_lineage_version", metaGUID)
	if _, err := tx.ExecContext(ctx, versionQuery, versionArgs...); err != nil {
		return err
	}
	return nil
}
