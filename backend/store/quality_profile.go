package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// columnProfileColumns lists the columns every read of column_profile returns,
// in the order scanColumnProfile expects them.
const columnProfileColumns = `id, table_guid, table_type, column_name, profile,
	sample_method, sample_rows, truncated, engine_version, profiled_at`

// upsertColumnProfileQuery writes one column's latest deterministic profile. The
// table holds one row per column, so the conflict target is the unique index
// over the column's identity; the update replaces every measured field because a
// profile is a snapshot, never a history.
const upsertColumnProfileQuery = `
	INSERT INTO column_profile (
		table_guid, table_type, column_name, profile,
		sample_method, sample_rows, truncated, engine_version, profiled_at
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	ON CONFLICT (table_guid, table_type, column_name) DO UPDATE SET
		profile = EXCLUDED.profile,
		sample_method = EXCLUDED.sample_method,
		sample_rows = EXCLUDED.sample_rows,
		truncated = EXCLUDED.truncated,
		engine_version = EXCLUDED.engine_version,
		profiled_at = EXCLUDED.profiled_at
	RETURNING id`

// deleteExpiredColumnProfilesQuery prunes profiles past their retention window.
// The maintenance runner calls it; the cutoff is the caller's, so the retention
// constant stays with the runner.
const deleteExpiredColumnProfilesQuery = `DELETE FROM column_profile WHERE profiled_at < $1`

// ColumnProfileMessage is one row of column_profile: the latest deterministic
// statistics measured for one column, plus how they were measured. Profiling is
// deterministic and never involves a model.
type ColumnProfileMessage struct {
	ID         int64
	TableGUID  string
	TableType  storepb.MetaType
	ColumnName string
	// Profile is the measured statistics; never nil on a row that was read.
	Profile       *storepb.ColumnProfile
	SampleMethod  string
	SampleRows    int64
	Truncated     bool
	EngineVersion string
	ProfiledAt    time.Time
}

// FindColumnProfileMessage filters column profiles. Every field is optional; an
// empty find lists everything, newest profiling first.
type FindColumnProfileMessage struct {
	TableGUID   *string
	TableType   *storepb.MetaType
	ColumnNames []string
	// ProfiledBefore keeps profiles measured before the given instant, which is
	// how a caller selects the stale ones.
	ProfiledBefore *time.Time
	Limit          *int
	Offset         *int
}

// columnProfileWhere builds the predicate and positional arguments for a find.
// The column names are a whitelist of the table's own columns, and every value
// is bound, so nothing from a caller reaches the statement text.
func columnProfileWhere(find *FindColumnProfileMessage) (string, []any) {
	where := []string{"TRUE"}
	args := []any{}
	if find == nil {
		return strings.Join(where, " AND "), args
	}
	if find.TableGUID != nil {
		args = append(args, *find.TableGUID)
		where = append(where, fmt.Sprintf("table_guid = $%d", len(args)))
	}
	if find.TableType != nil {
		args = append(args, int32(*find.TableType))
		where = append(where, fmt.Sprintf("table_type = $%d", len(args)))
	}
	if len(find.ColumnNames) > 0 {
		args = append(args, pq.Array(find.ColumnNames))
		where = append(where, fmt.Sprintf("column_name = ANY($%d)", len(args)))
	}
	if find.ProfiledBefore != nil {
		args = append(args, find.ProfiledBefore.UTC())
		where = append(where, fmt.Sprintf("profiled_at < $%d", len(args)))
	}
	return strings.Join(where, " AND "), args
}

// columnProfileQuery assembles one filtered, ordered column_profile read.
func columnProfileQuery(find *FindColumnProfileMessage) (string, []any) {
	where, args := columnProfileWhere(find)
	query := `SELECT ` + columnProfileColumns + ` FROM column_profile WHERE ` + where +
		` ORDER BY profiled_at DESC, id DESC`
	if find != nil && find.Limit != nil {
		query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, *find.Limit)
	}
	if find != nil && find.Offset != nil {
		query += fmt.Sprintf(" OFFSET $%d", len(args)+1)
		args = append(args, *find.Offset)
	}
	return query, args
}

// scanColumnProfile maps one column_profile row. The table type is read as the
// small integer it is stored as and converted to the enum, so a row whose value
// this build does not know still reads as that numeric value rather than as an
// unrelated type.
func scanColumnProfile(row rowScanner) (*ColumnProfileMessage, error) {
	var (
		message     ColumnProfileMessage
		tableType   int16
		profileJSON []byte
	)
	if err := row.Scan(
		&message.ID, &message.TableGUID, &tableType, &message.ColumnName, &profileJSON,
		&message.SampleMethod, &message.SampleRows, &message.Truncated, &message.EngineVersion, &message.ProfiledAt,
	); err != nil {
		return nil, err
	}
	message.TableType = storepb.MetaType(tableType)
	profile := &storepb.ColumnProfile{}
	if len(profileJSON) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(profileJSON, profile); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal a column profile")
		}
	}
	message.Profile = profile
	return &message, nil
}

// validateColumnProfile checks the identity a profile cannot be stored without.
// A profile without a column is not addressable, and the identity is exactly the
// unique index the upsert conflicts on.
func validateColumnProfile(message *ColumnProfileMessage) error {
	if message == nil {
		return common.Errorf(common.Invalid, "column profile is required")
	}
	if strings.TrimSpace(message.TableGUID) == "" {
		return common.Errorf(common.Invalid, "column profile table guid is required")
	}
	if message.TableType == storepb.MetaType_UNSPECIFIED {
		return common.Errorf(common.Invalid, "column profile table type is required")
	}
	if strings.TrimSpace(message.ColumnName) == "" {
		return common.Errorf(common.Invalid, "column profile column name is required")
	}
	return nil
}

// UpsertColumnProfile writes the latest profile of one column and returns it
// with the row id the store assigned. ProfiledAt is stamped when the caller did
// not set it, so a row can never claim to be measured at the zero time.
func (s *Store) UpsertColumnProfile(ctx context.Context, message *ColumnProfileMessage) (*ColumnProfileMessage, error) {
	if err := validateColumnProfile(message); err != nil {
		return nil, err
	}

	profile := message.Profile
	if profile == nil {
		profile = &storepb.ColumnProfile{}
	}
	profileJSON, err := protojson.Marshal(profile)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal a column profile")
	}

	profiledAt := message.ProfiledAt
	if profiledAt.IsZero() {
		profiledAt = time.Now().UTC()
	} else {
		profiledAt = profiledAt.UTC()
	}

	var id int64
	if err := s.GetDB().QueryRowContext(ctx, upsertColumnProfileQuery,
		message.TableGUID, int32(message.TableType), message.ColumnName, profileJSON,
		message.SampleMethod, message.SampleRows, message.Truncated, message.EngineVersion, profiledAt,
	).Scan(&id); err != nil {
		return nil, errors.Wrap(err, "failed to upsert a column profile")
	}

	stored := *message
	stored.ID = id
	stored.Profile = profile
	stored.ProfiledAt = profiledAt
	return &stored, nil
}

// GetColumnProfile returns one column's profile, or (nil, nil) when the column
// has never been profiled.
func (s *Store) GetColumnProfile(ctx context.Context, tableGUID string, tableType storepb.MetaType, columnName string) (*ColumnProfileMessage, error) {
	if strings.TrimSpace(tableGUID) == "" || strings.TrimSpace(columnName) == "" {
		return nil, common.Errorf(common.Invalid, "column profile table guid and column name are required")
	}
	list, err := s.ListColumnProfiles(ctx, &FindColumnProfileMessage{
		TableGUID:   &tableGUID,
		TableType:   &tableType,
		ColumnNames: []string{columnName},
	})
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	if len(list) > 1 {
		return nil, common.Errorf(common.Internal, "found %d column profiles for one column, expect 1", len(list))
	}
	return list[0], nil
}

// ListColumnProfiles lists profiles matching the filter, newest profiling first.
// The read runs in a read-only transaction like the other list paths, so a
// caller never sees a half-written batch.
func (s *Store) ListColumnProfiles(ctx context.Context, find *FindColumnProfileMessage) ([]*ColumnProfileMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin a column profile transaction")
	}
	defer tx.Rollback()

	query, args := columnProfileQuery(find)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query column profiles")
	}
	defer rows.Close()

	var profiles []*ColumnProfileMessage
	for rows.Next() {
		profile, err := scanColumnProfile(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a column profile")
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate column profiles")
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit a column profile transaction")
	}
	return profiles, nil
}

// DeleteExpiredColumnProfiles removes profiles measured before the cutoff and
// reports how many rows were dropped. Profiles are a cached snapshot, so
// expiring them loses nothing that cannot be measured again.
func (s *Store) DeleteExpiredColumnProfiles(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := s.GetDB().ExecContext(ctx, deleteExpiredColumnProfilesQuery, cutoff.UTC())
	if err != nil {
		return 0, errors.Wrap(err, "failed to delete expired column profiles")
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count deleted column profiles")
	}
	return deleted, nil
}
