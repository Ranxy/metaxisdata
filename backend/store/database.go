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

// DatabaseMessage is the message for database.
type DatabaseMessage struct {
	InstanceID   string
	DatabaseName string

	EnvironmentID          string
	EffectiveEnvironmentID string

	Deleted  bool
	Metadata *storepb.DatabaseMetadata
}

func (d *DatabaseMessage) String() string {
	return common.FormatDatabase(d.InstanceID, d.DatabaseName)
}

// UpdateDatabaseMessage is the mssage for updating a database.
type UpdateDatabaseMessage struct {
	InstanceID   string
	DatabaseName string

	Deleted *bool
	// Empty string will unset the environment.
	EnvironmentID   *string
	MetadataUpdates []func(*storepb.DatabaseMetadata)
}

// FindDatabaseMessage is the message for finding databases.
type FindDatabaseMessage struct {
	EffectiveEnvironmentID *string
	InstanceID             *string
	DatabaseName           *string
	Engine                 *storepb.Engine
	// When this is used, we will return databases from archived instances or environments.
	// This is used for existing tasks with archived databases.
	ShowDeleted bool

	// IsCaseSensitive is used to ignore case sensitive when finding database.
	IsCaseSensitive bool

	Filter *ListResourceFilter
	Limit  *int
	Offset *int
}

// GetDatabase gets a database.
func (s *Store) GetDatabase(ctx context.Context, find *FindDatabaseMessage) (*DatabaseMessage, error) {
	if find.InstanceID != nil && find.DatabaseName != nil {
		if v, ok := s.databaseCache.Get(getDatabaseCacheKey(*find.InstanceID, *find.DatabaseName)); ok {
			return v, nil
		}
	}

	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	databases, err := s.listDatabaseImpl(ctx, tx, find)
	if err != nil {
		return nil, err
	}
	if len(databases) == 0 {
		return nil, nil
	}
	if len(databases) > 1 {
		return nil, &common.Error{Code: common.Conflict, Err: errors.Errorf("found %d database with filter %+v, expect 1", len(databases), find)}
	}
	database := databases[0]

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.databaseCache.Add(getDatabaseCacheKey(database.InstanceID, database.DatabaseName), database)
	return database, nil
}

// ListDatabases lists all databases.
func (s *Store) ListDatabases(ctx context.Context, find *FindDatabaseMessage) ([]*DatabaseMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	databases, err := s.listDatabaseImpl(ctx, tx, find)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	for _, database := range databases {
		s.databaseCache.Add(getDatabaseCacheKey(database.InstanceID, database.DatabaseName), database)
	}
	return databases, nil
}

// CreateDatabaseDefault creates a new database.
func (s *Store) CreateDatabaseDefault(ctx context.Context, create *DatabaseMessage) (*DatabaseMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := s.createDatabaseDefaultImpl(ctx, tx, create.InstanceID, create); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Invalidate an update the cache.
	s.databaseCache.Remove(getDatabaseCacheKey(create.InstanceID, create.DatabaseName))
	return s.GetDatabase(ctx, &FindDatabaseMessage{InstanceID: &create.InstanceID, DatabaseName: &create.DatabaseName, ShowDeleted: true})
}

// createDatabaseDefault only creates a default database with charset, collation.
func (*Store) createDatabaseDefaultImpl(ctx context.Context, txn *sql.Tx, instanceID string, create *DatabaseMessage) (int, error) {
	query := `
		INSERT INTO db (
			instance,
			name,
			deleted
		)
		VALUES ($1, $2, $3)
		ON CONFLICT (instance, name) DO UPDATE SET
			deleted = EXCLUDED.deleted
		RETURNING id`
	var databaseUID int
	if err := txn.QueryRowContext(ctx, query,
		instanceID,
		create.DatabaseName,
		false,
	).Scan(
		&databaseUID,
	); err != nil {
		return 0, err
	}
	return databaseUID, nil
}

// UpsertDatabase upserts a database.
func (s *Store) UpsertDatabase(ctx context.Context, create *DatabaseMessage) (*DatabaseMessage, error) {
	metadataString, err := protojson.Marshal(create.Metadata)
	if err != nil {
		return nil, err
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var environment *string
	if create.EnvironmentID != "" {
		environment = &create.EnvironmentID
	}
	query := `
		INSERT INTO db (
			instance,
			environment,
			name,
			deleted,
			metadata
		)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (instance, name) DO UPDATE SET
			environment = EXCLUDED.environment,
			name = EXCLUDED.name,
			metadata = EXCLUDED.metadata
		RETURNING id`
	var databaseUID int
	if err := tx.QueryRowContext(ctx, query,
		create.InstanceID,
		environment,
		create.DatabaseName,
		create.Deleted,
		metadataString,
	).Scan(
		&databaseUID,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Invalidate and update the cache.
	s.databaseCache.Remove(getDatabaseCacheKey(create.InstanceID, create.DatabaseName))
	return s.GetDatabase(ctx, &FindDatabaseMessage{InstanceID: &create.InstanceID, DatabaseName: &create.DatabaseName, ShowDeleted: true})
}

// UpdateDatabase updates a database.
func (s *Store) UpdateDatabase(ctx context.Context, patch *UpdateDatabaseMessage) (*DatabaseMessage, error) {
	set, args := []string{}, []any{}
	if v := patch.EnvironmentID; v != nil {
		if *v == "" {
			set = append(set, "environment = NULL")
		} else {
			set, args = append(set, fmt.Sprintf("environment = $%d", len(args)+1)), append(args, *v)
		}
	}
	if v := patch.Deleted; v != nil {
		set, args = append(set, fmt.Sprintf("deleted = $%d", len(args)+1)), append(args, *v)
	}
	if fs := patch.MetadataUpdates; len(fs) > 0 {
		database, err := s.GetDatabase(ctx, &FindDatabaseMessage{
			InstanceID:   &patch.InstanceID,
			DatabaseName: &patch.DatabaseName,
		})
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get database %q", common.FormatDatabase(patch.InstanceID, patch.DatabaseName))
		}
		// GetDatabase returns (nil, nil) when the row is gone; cloning its
		// metadata would panic the whole process.
		if database == nil {
			return nil, common.Errorf(common.NotFound, "database %q not found", common.FormatDatabase(patch.InstanceID, patch.DatabaseName))
		}
		md := proto.CloneOf(database.Metadata)
		for _, f := range fs {
			f(md)
		}
		metadataBytes, err := protojson.Marshal(md)
		if err != nil {
			return nil, err
		}
		set, args = append(set, fmt.Sprintf("metadata = $%d", len(args)+1)), append(args, metadataBytes)
	}
	args = append(args, patch.InstanceID, patch.DatabaseName)

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var databaseUID int
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`
		UPDATE db
		SET `+strings.Join(set, ", ")+`
		WHERE instance = $%d AND name = $%d
		RETURNING id
	`, len(args)-1, len(args)),
		args...,
	).Scan(
		&databaseUID,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Invalidate and update database cache.
	s.databaseCache.Remove(getDatabaseCacheKey(patch.InstanceID, patch.DatabaseName))
	return s.GetDatabase(ctx, &FindDatabaseMessage{InstanceID: &patch.InstanceID, DatabaseName: &patch.DatabaseName, ShowDeleted: true})
}

func (*Store) listDatabaseImpl(ctx context.Context, txn *sql.Tx, find *FindDatabaseMessage) ([]*DatabaseMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	if filter := find.Filter; filter != nil {
		where = append(where, filter.Where)
		args = append(args, filter.Args...)
	}
	if v := find.EffectiveEnvironmentID; v != nil {
		where, args = append(where, fmt.Sprintf(`
		COALESCE(
			db.environment,
			instance.environment
		) = $%d`, len(args)+1)), append(args, *v)
	}
	if v := find.InstanceID; v != nil {
		where, args = append(where, fmt.Sprintf("db.instance = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.DatabaseName; v != nil {
		if find.IsCaseSensitive {
			where, args = append(where, fmt.Sprintf("db.name = $%d", len(args)+1)), append(args, *v)
		} else {
			where, args = append(where, fmt.Sprintf("LOWER(db.name) = LOWER($%d)", len(args)+1)), append(args, *v)
		}
	}
	if v := find.Engine; v != nil {
		where, args = append(where, fmt.Sprintf("instance.metadata->>'engine' = $%d", len(args)+1)), append(args, *v)
	}
	if !find.ShowDeleted {
		where, args = append(where, fmt.Sprintf("instance.deleted = $%d", len(args)+1)), append(args, false)
		// We don't show databases that are deleted by users already.
		where, args = append(where, fmt.Sprintf("db.deleted = $%d", len(args)+1)), append(args, false)
	}

	query := fmt.Sprintf(`
		SELECT
			COALESCE(
				db.environment,
				instance.environment
			),
			db.environment,
			db.instance,
			db.name,
			db.deleted,
			db.metadata
		FROM db
		LEFT JOIN instance ON db.instance = instance.resource_id
		WHERE %s
		ORDER BY db.instance, db.name`, strings.Join(where, " AND "))
	if v := find.Limit; v != nil {
		query += fmt.Sprintf(" LIMIT %d", *v)
	}
	if v := find.Offset; v != nil {
		query += fmt.Sprintf(" OFFSET %d", *v)
	}

	var databaseMessages []*DatabaseMessage
	rows, err := txn.QueryContext(ctx, query,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		databaseMessage := &DatabaseMessage{}
		var metadataString string
		var effectiveEnvironment, environment sql.NullString
		if err := rows.Scan(
			&effectiveEnvironment,
			&environment,
			&databaseMessage.InstanceID,
			&databaseMessage.DatabaseName,
			&databaseMessage.Deleted,
			&metadataString,
		); err != nil {
			return nil, err
		}
		if effectiveEnvironment.Valid {
			databaseMessage.EffectiveEnvironmentID = effectiveEnvironment.String
		}
		if environment.Valid {
			databaseMessage.EnvironmentID = environment.String
		}

		var metadata storepb.DatabaseMetadata
		if err := common.ProtojsonUnmarshaler.Unmarshal([]byte(metadataString), &metadata); err != nil {
			return nil, err
		}
		databaseMessage.Metadata = &metadata

		databaseMessages = append(databaseMessages, databaseMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return databaseMessages, nil
}
