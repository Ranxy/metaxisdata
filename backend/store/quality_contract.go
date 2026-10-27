package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualityContractMessage is the target-level contract row: its settings and the
// newest published version. A target with no row has never been configured.
type QualityContractMessage struct {
	ID               int64
	TargetGUID       string
	TargetType       storepb.MetaType
	Settings         *storepb.QualityContractSettings
	PublishedVersion int32
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// QualityContractRevisionMessage is one published contract version. The snapshot
// is what the version promised; the counts are its diff against the previous
// version, kept as columns so the history list does not parse every snapshot.
type QualityContractRevisionMessage struct {
	ID            int64
	ContractID    int64
	Version       int32
	Snapshot      *storepb.QualityContractSnapshot
	CheckCount    int32
	AddedCount    int32
	ModifiedCount int32
	RemovedCount  int32
	Note          string
	PublishedBy   *int32
	PublishedAt   time.Time
}

const qualityContractColumns = `id, target_guid, target_type, settings, published_version, created_at, updated_at`

const qualityContractRevisionColumns = `id, contract_id, version, snapshot, check_count, added_count, modified_count, removed_count, note, published_by, published_at`

// GetQualityContract reads one target's contract. A nil message means the target
// has no contract row yet; callers treat that as the zero settings.
func (s *Store) GetQualityContract(ctx context.Context, targetGUID string, targetType storepb.MetaType) (*QualityContractMessage, error) {
	if targetGUID == "" {
		return nil, common.Errorf(common.Invalid, "a target GUID is required")
	}
	row := s.GetDB().QueryRowContext(ctx, `SELECT `+qualityContractColumns+`
		FROM quality_contract WHERE target_guid = $1 AND target_type = $2`, targetGUID, int16(targetType))
	message, err := scanQualityContract(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality contract")
	}
	return message, nil
}

// UpsertQualityContractSettings writes the target's settings, creating the
// contract row on first use, and returns the stored row.
func (s *Store) UpsertQualityContractSettings(ctx context.Context, targetGUID string, targetType storepb.MetaType, settings *storepb.QualityContractSettings) (*QualityContractMessage, error) {
	if targetGUID == "" {
		return nil, common.Errorf(common.Invalid, "a target GUID is required")
	}
	if settings == nil {
		settings = &storepb.QualityContractSettings{}
	}
	payload, err := protojson.Marshal(settings)
	if err != nil {
		return nil, err
	}
	row := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_contract (target_guid, target_type, settings)
		VALUES ($1, $2, $3)
		ON CONFLICT (target_guid, target_type) DO UPDATE
		SET settings = EXCLUDED.settings, updated_at = NOW()
		RETURNING `+qualityContractColumns,
		targetGUID, int16(targetType), payload)
	message, err := scanQualityContract(row)
	if err != nil {
		return nil, errors.Wrap(err, "failed to save the quality contract settings")
	}
	return message, nil
}

// PublishQualityContractInput is one publish attempt. Snapshot is built by the
// caller from the live rules; this method owns the version number and the
// no-op-on-identical-fingerprint rule.
type PublishQualityContractInput struct {
	TargetGUID    string
	TargetType    storepb.MetaType
	Settings      *storepb.QualityContractSettings
	Snapshot      *storepb.QualityContractSnapshot
	CheckCount    int32
	AddedCount    int32
	ModifiedCount int32
	RemovedCount  int32
	Note          string
	PublishedBy   *int32
}

// PublishQualityContractRevision records the snapshot as the next version.
//
// It runs in one transaction: the contract row is created if needed and locked,
// the latest revision's fingerprint is compared under that lock, and only then is
// a new version inserted and the contract's published_version advanced. Two
// concurrent publishes of the same state therefore produce one version.
// created=false means the live state already matched the newest version.
func (s *Store) PublishQualityContractRevision(ctx context.Context, input *PublishQualityContractInput) (*QualityContractRevisionMessage, *QualityContractMessage, bool, error) {
	if input == nil || input.TargetGUID == "" {
		return nil, nil, false, common.Errorf(common.Invalid, "a target GUID is required")
	}
	if input.Snapshot == nil {
		return nil, nil, false, common.Errorf(common.Invalid, "a contract snapshot is required")
	}
	settings := input.Settings
	if settings == nil {
		settings = &storepb.QualityContractSettings{}
	}
	settingsPayload, err := protojson.Marshal(settings)
	if err != nil {
		return nil, nil, false, err
	}
	snapshotPayload, err := protojson.Marshal(input.Snapshot)
	if err != nil {
		return nil, nil, false, err
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, false, errors.Wrap(err, "failed to begin the contract publish transaction")
	}
	defer tx.Rollback()

	var contractID int64
	var publishedVersion int32
	locked := tx.QueryRowContext(ctx, `
		INSERT INTO quality_contract (target_guid, target_type, settings)
		VALUES ($1, $2, $3)
		ON CONFLICT (target_guid, target_type) DO UPDATE SET settings = EXCLUDED.settings
		RETURNING id, published_version
	`, input.TargetGUID, int16(input.TargetType), settingsPayload)
	if err := locked.Scan(&contractID, &publishedVersion); err != nil {
		return nil, nil, false, errors.Wrap(err, "failed to lock the quality contract")
	}

	// Re-read under the row lock: the upsert above already locked the row, but the
	// latest revision is what decides whether this publish changes anything.
	var (
		latestID          int64
		latestVersion     int32
		latestFingerprint sql.NullString
		latestSnapshot    []byte
	)
	latestErr := tx.QueryRowContext(ctx, `
		SELECT id, version, snapshot->>'fingerprint', snapshot
		FROM quality_contract_revision
		WHERE contract_id = $1
		ORDER BY version DESC
		LIMIT 1
	`, contractID).Scan(&latestID, &latestVersion, &latestFingerprint, &latestSnapshot)
	switch {
	case latestErr == nil:
		if latestFingerprint.Valid && input.Snapshot.GetFingerprint() != "" &&
			latestFingerprint.String == input.Snapshot.GetFingerprint() {
			// The live state is what the newest version already recorded. Return it
			// instead of creating a duplicate version.
			revision, scanErr := scanQualityContractRevision(tx.QueryRowContext(ctx,
				`SELECT `+qualityContractRevisionColumns+` FROM quality_contract_revision WHERE id = $1`, latestID))
			if scanErr != nil {
				return nil, nil, false, errors.Wrap(scanErr, "failed to read the unchanged contract revision")
			}
			contract, scanErr := scanQualityContract(tx.QueryRowContext(ctx,
				`SELECT `+qualityContractColumns+` FROM quality_contract WHERE id = $1`, contractID))
			if scanErr != nil {
				return nil, nil, false, errors.Wrap(scanErr, "failed to read the quality contract")
			}
			if commitErr := tx.Commit(); commitErr != nil {
				return nil, nil, false, errors.Wrap(commitErr, "failed to commit the contract publish transaction")
			}
			return revision, contract, false, nil
		}
	case errors.Is(latestErr, sql.ErrNoRows):
	default:
		return nil, nil, false, errors.Wrap(latestErr, "failed to read the latest contract revision")
	}

	nextVersion := publishedVersion + 1
	revision, err := scanQualityContractRevision(tx.QueryRowContext(ctx, `
		INSERT INTO quality_contract_revision
			(contract_id, version, snapshot, check_count, added_count, modified_count, removed_count, note, published_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+qualityContractRevisionColumns,
		contractID, nextVersion, snapshotPayload, input.CheckCount, input.AddedCount,
		input.ModifiedCount, input.RemovedCount, input.Note, input.PublishedBy))
	if err != nil {
		return nil, nil, false, errors.Wrap(err, "failed to insert the contract revision")
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE quality_contract SET published_version = $2, updated_at = NOW() WHERE id = $1
	`, contractID, nextVersion); err != nil {
		return nil, nil, false, errors.Wrap(err, "failed to advance the contract version")
	}
	contract, err := scanQualityContract(tx.QueryRowContext(ctx,
		`SELECT `+qualityContractColumns+` FROM quality_contract WHERE id = $1`, contractID))
	if err != nil {
		return nil, nil, false, errors.Wrap(err, "failed to read the quality contract")
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, false, errors.Wrap(err, "failed to commit the contract publish transaction")
	}
	return revision, contract, true, nil
}

// GetLatestQualityContractRevision returns the newest published version, or nil.
func (s *Store) GetLatestQualityContractRevision(ctx context.Context, contractID int64) (*QualityContractRevisionMessage, error) {
	revision, err := scanQualityContractRevision(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityContractRevisionColumns+`
		FROM quality_contract_revision WHERE contract_id = $1 ORDER BY version DESC LIMIT 1`, contractID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the latest contract revision")
	}
	return revision, nil
}

// GetQualityContractRevisionByVersion reads one published version of a contract.
func (s *Store) GetQualityContractRevisionByVersion(ctx context.Context, contractID int64, version int32) (*QualityContractRevisionMessage, error) {
	revision, err := scanQualityContractRevision(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityContractRevisionColumns+`
		FROM quality_contract_revision WHERE contract_id = $1 AND version = $2`, contractID, version))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the contract revision")
	}
	return revision, nil
}

// ListQualityContractRevisions lists a contract's versions newest first.
func (s *Store) ListQualityContractRevisions(ctx context.Context, contractID int64, limit, offset int) ([]*QualityContractRevisionMessage, error) {
	if limit <= 0 {
		return nil, common.Errorf(common.Invalid, "a positive limit is required")
	}
	rows, err := s.GetDB().QueryContext(ctx, `SELECT `+qualityContractRevisionColumns+`
		FROM quality_contract_revision
		WHERE contract_id = $1
		ORDER BY version DESC
		LIMIT $2 OFFSET $3`, contractID, limit, max(offset, 0))
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the contract revisions")
	}
	defer rows.Close()

	revisions := []*QualityContractRevisionMessage{}
	for rows.Next() {
		revision, err := scanQualityContractRevision(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a contract revision")
		}
		revisions = append(revisions, revision)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the contract revisions")
	}
	return revisions, nil
}

// scanQualityContract reads one quality_contract row.
func scanQualityContract(scanner interface{ Scan(dest ...any) error }) (*QualityContractMessage, error) {
	var (
		message      QualityContractMessage
		targetType   int16
		settingsJSON []byte
	)
	if err := scanner.Scan(
		&message.ID, &message.TargetGUID, &targetType, &settingsJSON,
		&message.PublishedVersion, &message.CreatedAt, &message.UpdatedAt,
	); err != nil {
		return nil, err
	}
	message.TargetType = storepb.MetaType(targetType)
	settings := &storepb.QualityContractSettings{}
	if len(settingsJSON) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(settingsJSON, settings); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal the quality contract settings")
		}
	}
	message.Settings = settings
	return &message, nil
}

// scanQualityContractRevision reads one quality_contract_revision row.
func scanQualityContractRevision(scanner interface{ Scan(dest ...any) error }) (*QualityContractRevisionMessage, error) {
	var (
		message      QualityContractRevisionMessage
		snapshotJSON []byte
		publishedBy  sql.NullInt32
	)
	if err := scanner.Scan(
		&message.ID, &message.ContractID, &message.Version, &snapshotJSON,
		&message.CheckCount, &message.AddedCount, &message.ModifiedCount, &message.RemovedCount,
		&message.Note, &publishedBy, &message.PublishedAt,
	); err != nil {
		return nil, err
	}
	snapshot := &storepb.QualityContractSnapshot{}
	if len(snapshotJSON) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(snapshotJSON, snapshot); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal the quality contract snapshot")
		}
	}
	message.Snapshot = snapshot
	if publishedBy.Valid {
		id := publishedBy.Int32
		message.PublishedBy = &id
	}
	return &message, nil
}
