package store

import (
	"context"
	"database/sql"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// CreateEnvironmentMessage is the message for creating an environment.
type CreateEnvironmentMessage struct {
	// Title is the user-facing name. The id is derived from it.
	Title string
	// Color is a preset palette key. Empty lets the server pick one.
	Color string
}

// EnvironmentPatch carries the fields to write for one environment. A nil
// pointer leaves the stored value untouched.
type EnvironmentPatch struct {
	Title *string
	Color *string
	Tags  map[string]string
}

// GetEnvironmentByID returns the environment with the given id, or nil when no
// environment carries it.
func (s *Store) GetEnvironmentByID(ctx context.Context, id string) (*storepb.EnvironmentSetting_Environment, error) {
	environments, err := s.GetEnvironmentSetting(ctx)
	if err != nil {
		return nil, err
	}
	for _, environment := range environments.GetEnvironments() {
		if environment.Id == id {
			return environment, nil
		}
	}
	return nil, nil
}

// ListEnvironments returns the configured environments in display order.
func (s *Store) ListEnvironments(ctx context.Context) ([]*storepb.EnvironmentSetting_Environment, error) {
	setting, err := s.GetEnvironmentSetting(ctx)
	if err != nil {
		return nil, err
	}
	return setting.GetEnvironments(), nil
}

// CountInstancesByEnvironment counts the live instances assigned to an
// environment. Deleted instances are excluded: they still carry the id but no
// longer need to keep the environment alive.
func (s *Store) CountInstancesByEnvironment(ctx context.Context, id string) (int, error) {
	var count int
	if err := s.GetDB().QueryRowContext(ctx,
		`SELECT count(*) FROM instance WHERE environment = $1 AND deleted = false`, id).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// CountInstancesByEnvironments returns the live instance count per environment
// id. It is one grouped query so listing environments does not issue a count
// query per row.
func (s *Store) CountInstancesByEnvironments(ctx context.Context) (map[string]int, error) {
	rows, err := s.GetDB().QueryContext(ctx,
		`SELECT environment, count(*) FROM instance WHERE environment IS NOT NULL AND deleted = false GROUP BY environment`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var id string
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			return nil, err
		}
		counts[id] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

// CreateEnvironment appends an environment to the workspace setting. The id is
// derived from the title and is immutable afterwards; the color falls back to
// the preset palette when empty.
func (s *Store) CreateEnvironment(ctx context.Context, create *CreateEnvironmentMessage) (*storepb.EnvironmentSetting_Environment, error) {
	return s.mutateEnvironments(ctx, func(setting *storepb.EnvironmentSetting) (*storepb.EnvironmentSetting_Environment, error) {
		return applyEnvironmentCreate(setting, create.Title, create.Color)
	})
}

// UpdateEnvironment rewrites the mutable fields of one environment. The id is
// never changed so existing instance references stay valid.
func (s *Store) UpdateEnvironment(ctx context.Context, id string, patch *EnvironmentPatch) (*storepb.EnvironmentSetting_Environment, error) {
	return s.mutateEnvironments(ctx, func(setting *storepb.EnvironmentSetting) (*storepb.EnvironmentSetting_Environment, error) {
		return applyEnvironmentUpdate(setting, id, patch)
	})
}

// DeleteEnvironment removes an environment from the setting. Callers must check
// CountInstancesByEnvironment first: a deleted environment leaves instances
// pointing at an id that no longer resolves.
func (s *Store) DeleteEnvironment(ctx context.Context, id string) error {
	_, err := s.mutateEnvironments(ctx, func(setting *storepb.EnvironmentSetting) (*storepb.EnvironmentSetting_Environment, error) {
		return nil, applyEnvironmentDelete(setting, id)
	})
	return err
}

// mutateEnvironments applies mutate to the environment setting under a row
// lock and persists the result. The lock serializes concurrent create/update/
// delete calls: without it two writers would each overwrite the whole JSONB
// blob and one addition would be lost.
func (s *Store) mutateEnvironments(ctx context.Context, mutate func(*storepb.EnvironmentSetting) (*storepb.EnvironmentSetting_Environment, error)) (*storepb.EnvironmentSetting_Environment, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	setting, err := readEnvironmentSettingForUpdate(ctx, tx)
	if err != nil {
		return nil, err
	}
	result, err := mutate(setting)
	if err != nil {
		return nil, err
	}
	payload, err := protojson.Marshal(setting)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal environment setting")
	}
	if _, err := tx.ExecContext(ctx, `
			INSERT INTO setting (name, value)
			VALUES ($1, $2)
			ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value
		`, storepb.SettingName_ENVIRONMENT.String(), string(payload)); err != nil {
		return nil, errors.Wrap(err, "failed to update environment setting")
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}

	s.settingCache.Add(storepb.SettingName_ENVIRONMENT, &SettingMessage{
		Name:  storepb.SettingName_ENVIRONMENT,
		Value: string(payload),
	})
	return result, nil
}

func readEnvironmentSettingForUpdate(ctx context.Context, txn *sql.Tx) (*storepb.EnvironmentSetting, error) {
	var value string
	err := txn.QueryRowContext(ctx,
		`SELECT value FROM setting WHERE name = $1 FOR UPDATE`,
		storepb.SettingName_ENVIRONMENT.String(),
	).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return &storepb.EnvironmentSetting{}, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to lock environment setting")
	}
	setting := &storepb.EnvironmentSetting{}
	if err := common.ProtojsonUnmarshaler.Unmarshal([]byte(value), setting); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal environment setting")
	}
	return setting, nil
}
