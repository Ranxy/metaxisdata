package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func (*Store) listMetaRegistryResourceImpl(ctx context.Context, txn *sql.Tx, find *FindMetaRegistryResourceMessage, withMetadata bool) ([]*MetaRegistryResource, error) {
	where, args := buildMetaRegistryWhereClause("meta_registry_resource", find)

	var query string
	if withMetadata {
		query = `
		SELECT
			meta_registry_resource.id,
			meta_registry_resource.guid,
			meta_registry_resource.object_type,
			meta_registry_resource.metadata,
			meta_registry_resource.meta_hash
		FROM meta_registry_resource
		WHERE %s
		ORDER BY guid`
	} else {
		query = `
		SELECT
			meta_registry_resource.id,
			meta_registry_resource.guid,
			meta_registry_resource.object_type,
			NULL AS metadata,
			meta_registry_resource.meta_hash
		FROM meta_registry_resource
		WHERE %s
		ORDER BY guid`
	}

	query = fmt.Sprintf(query, strings.Join(where, " AND "))
	if v := find.Limit; v != nil {
		query += fmt.Sprintf(" LIMIT %d", *v)
	}
	if v := find.Offset; v != nil {
		query += fmt.Sprintf(" OFFSET %d", *v)
	}

	var metaRegistryMessages []*MetaRegistryResource
	rows, err := txn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var metadata []byte
		var metaRegistryMessage MetaRegistryResource
		if err := rows.Scan(
			&metaRegistryMessage.ID,
			&metaRegistryMessage.GUID,
			&metaRegistryMessage.ObjectType,
			&metadata,
			&metaRegistryMessage.MetaHash,
		); err != nil {
			return nil, err
		}
		if len(metadata) != 0 {
			m := &storepb.StoredMetadata{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(metadata, m); err != nil {
				return nil, errors.Wrap(err, " failed to unmarshal stored metadata")
			}
			metaRegistryMessage.Metadata = m
		}

		metaRegistryMessages = append(metaRegistryMessages, &metaRegistryMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return metaRegistryMessages, nil
}

func (*Store) listMetaRegistryResourceHistoryImpl(ctx context.Context, txn *sql.Tx, find *FindMetaRegistryResourceMessage, asOf time.Time) ([]*MetaRegistryResource, error) {
	where, args := buildMetaRegistryWhereClause("meta_registry_resource_history", find)
	args = append(args, asOf)
	asOfArg := len(args)
	where = append(where, fmt.Sprintf("meta_registry_resource_history.valid_from <= $%d", asOfArg))
	where = append(where, fmt.Sprintf("(meta_registry_resource_history.valid_to IS NULL OR meta_registry_resource_history.valid_to > $%d)", asOfArg))

	query := `
		SELECT
			meta_registry_resource_history.id,
			meta_registry_resource_history.guid,
			meta_registry_resource_history.object_type,
			meta_registry_resource_history.metadata,
			meta_registry_resource_history.meta_hash
		FROM meta_registry_resource_history
		WHERE %s
		ORDER BY guid`

	query = fmt.Sprintf(query, strings.Join(where, " AND "))
	if v := find.Limit; v != nil {
		query += fmt.Sprintf(" LIMIT %d", *v)
	}
	if v := find.Offset; v != nil {
		query += fmt.Sprintf(" OFFSET %d", *v)
	}

	var metaRegistryMessages []*MetaRegistryResource
	rows, err := txn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var metadata []byte
		var metaRegistryMessage MetaRegistryResource
		if err := rows.Scan(
			&metaRegistryMessage.ID,
			&metaRegistryMessage.GUID,
			&metaRegistryMessage.ObjectType,
			&metadata,
			&metaRegistryMessage.MetaHash,
		); err != nil {
			return nil, err
		}
		if len(metadata) != 0 {
			m := &storepb.StoredMetadata{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(metadata, m); err != nil {
				return nil, errors.Wrap(err, " failed to unmarshal stored metadata")
			}
			metaRegistryMessage.Metadata = m
		}

		metaRegistryMessages = append(metaRegistryMessages, &metaRegistryMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return metaRegistryMessages, nil
}

func (*Store) listMetaRegistryHistoryImpl(ctx context.Context, txn *sql.Tx, find *FindMetaRegistryHistoryMessage) ([]*MetaRegistryHistory, error) {
	where, args := []string{"TRUE"}, []any{}
	if v := find.GUID; v != nil {
		where, args = append(where, fmt.Sprintf("meta_registry_resource_history.guid = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.GUIDPrefix; v != nil {
		where, args = appendGUIDSubtreeCondition(where, args, "meta_registry_resource_history.guid", *v)
	}
	if v := find.ObjectType; v != nil {
		where, args = append(where, fmt.Sprintf("meta_registry_resource_history.object_type = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.ValidFrom; v != nil {
		where, args = append(where, fmt.Sprintf("meta_registry_resource_history.valid_from = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.TransitionTime; v != nil {
		args = append(args, *v)
		argIndex := len(args)
		where = append(where, fmt.Sprintf("(meta_registry_resource_history.valid_from = $%d OR meta_registry_resource_history.valid_to = $%d)", argIndex, argIndex))
	}

	query := `
		SELECT
			meta_registry_resource_history.id,
			meta_registry_resource_history.guid,
			meta_registry_resource_history.object_type,
			meta_registry_resource_history.metadata,
			meta_registry_resource_history.meta_hash,
			meta_registry_resource_history.valid_from,
			meta_registry_resource_history.valid_to
		FROM meta_registry_resource_history
		WHERE %s
		ORDER BY meta_registry_resource_history.valid_from %s, meta_registry_resource_history.guid`
	order := "ASC"
	if find.OrderDesc {
		order = "DESC"
	}
	query = fmt.Sprintf(query, strings.Join(where, " AND "), order)
	if v := find.Limit; v != nil {
		query += fmt.Sprintf(" LIMIT %d", *v)
	}
	if v := find.Offset; v != nil {
		query += fmt.Sprintf(" OFFSET %d", *v)
	}

	rows, err := txn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*MetaRegistryHistory
	for rows.Next() {
		var metadata []byte
		var history MetaRegistryHistory
		var validTo sql.NullTime
		if err := rows.Scan(
			&history.ID,
			&history.GUID,
			&history.ObjectType,
			&metadata,
			&history.MetaHash,
			&history.ValidFrom,
			&validTo,
		); err != nil {
			return nil, err
		}
		if validTo.Valid {
			value := validTo.Time
			history.ValidTo = &value
		}
		if len(metadata) != 0 {
			m := &storepb.StoredMetadata{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(metadata, m); err != nil {
				return nil, errors.Wrap(err, "failed to unmarshal stored metadata history")
			}
			history.Metadata = m
		}
		list = append(list, &history)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return list, nil
}

func (*Store) listSublevelMetaRegistryResourceImpl(ctx context.Context, txn *sql.Tx, parentGUID string, objectType storepb.MetaType, limitPreObjectType, offsetPreObjectType int) ([]*MetaRegistryResource, error) {
	// Whether using Lateral Join or Windows, for large datasets,
	// PG's query optimizer seems unable to select the correct index.
	// Therefore, we directly use the UNION ALL method here.

	nextTypes := getNextLevelObjectType(objectType)
	if len(nextTypes) == 0 {
		return []*MetaRegistryResource{}, nil
	}

	args := []any{}

	qb := strings.Builder{}

	for idx, nextType := range nextTypes {
		unionStr := ""
		if idx != 0 {
			unionStr = "UNION ALL "
		}
		nextQuery := fmt.Sprintf(`%s
		SELECT * FROM(
		SELECT
			meta_registry_resource.id,
			meta_registry_resource.guid,
			meta_registry_resource.object_type,
			meta_registry_resource.metadata,
			meta_registry_resource.meta_hash
		FROM meta_registry_resource
		WHERE (meta_registry_resource.guid = $%d OR meta_registry_resource.guid LIKE $%d ESCAPE E'\\') AND meta_registry_resource.object_type = $%d
		ORDER BY guid limit %d offset %d)
		`, unionStr, len(args)+1, len(args)+2, len(args)+3, limitPreObjectType, offsetPreObjectType)
		args = append(
			args,
			parentGUID,
			likePatternEscaper.Replace(parentGUID+common.MetaGUIDSplit)+"%",
			nextType,
		)
		//nolint:revive
		if _, err := qb.WriteString(nextQuery); err != nil {
			return nil, err
		}
	}
	var metaRegistryMessages []*MetaRegistryResource
	rows, err := txn.QueryContext(ctx, qb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var metadata []byte
		var metaRegistryMessage MetaRegistryResource
		if err := rows.Scan(
			&metaRegistryMessage.ID,
			&metaRegistryMessage.GUID,
			&metaRegistryMessage.ObjectType,
			&metadata,
			&metaRegistryMessage.MetaHash,
		); err != nil {
			return nil, err
		}
		if len(metadata) != 0 {
			m := &storepb.StoredMetadata{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(metadata, m); err != nil {
				return nil, errors.Wrap(err, " failed to unmarshal stored metadata")
			}
			metaRegistryMessage.Metadata = m
		}

		metaRegistryMessages = append(metaRegistryMessages, &metaRegistryMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return metaRegistryMessages, nil
}

func (*Store) listSublevelMetaRegistryResourceHistoryImpl(ctx context.Context, txn *sql.Tx, parentGUID string, objectType storepb.MetaType, limitPreObjectType, offsetPreObjectType int, asOf time.Time) ([]*MetaRegistryResource, error) {
	nextTypes := getNextLevelObjectType(objectType)
	if len(nextTypes) == 0 {
		return []*MetaRegistryResource{}, nil
	}

	args := []any{}
	qb := strings.Builder{}

	for idx, nextType := range nextTypes {
		unionStr := ""
		if idx != 0 {
			unionStr = "UNION ALL "
		}
		nextQuery := fmt.Sprintf(`%s
		SELECT * FROM(
		SELECT
			meta_registry_resource_history.id,
			meta_registry_resource_history.guid,
			meta_registry_resource_history.object_type,
			meta_registry_resource_history.metadata,
			meta_registry_resource_history.meta_hash
		FROM meta_registry_resource_history
		WHERE (meta_registry_resource_history.guid = $%d OR meta_registry_resource_history.guid LIKE $%d ESCAPE E'\\')
			AND meta_registry_resource_history.object_type = $%d
			AND meta_registry_resource_history.valid_from <= $%d
			AND (meta_registry_resource_history.valid_to IS NULL OR meta_registry_resource_history.valid_to > $%d)
		ORDER BY guid limit %d offset %d)
		`, unionStr, len(args)+1, len(args)+2, len(args)+3, len(args)+4, len(args)+4, limitPreObjectType, offsetPreObjectType)
		args = append(
			args,
			parentGUID,
			likePatternEscaper.Replace(parentGUID+common.MetaGUIDSplit)+"%",
			nextType,
			asOf,
		)
		if _, err := qb.WriteString(nextQuery); err != nil {
			return nil, err
		}
	}

	var metaRegistryMessages []*MetaRegistryResource
	rows, err := txn.QueryContext(ctx, qb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var metadata []byte
		var metaRegistryMessage MetaRegistryResource
		if err := rows.Scan(
			&metaRegistryMessage.ID,
			&metaRegistryMessage.GUID,
			&metaRegistryMessage.ObjectType,
			&metadata,
			&metaRegistryMessage.MetaHash,
		); err != nil {
			return nil, err
		}
		if len(metadata) != 0 {
			m := &storepb.StoredMetadata{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(metadata, m); err != nil {
				return nil, errors.Wrap(err, " failed to unmarshal stored metadata")
			}
			metaRegistryMessage.Metadata = m
		}

		metaRegistryMessages = append(metaRegistryMessages, &metaRegistryMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return metaRegistryMessages, nil
}
