package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// FindSettingMessage is the message for finding setting.
type FindSettingMessage struct {
	Name *storepb.SettingName
}

// SetSettingMessage is the message for updating setting.
type SetSettingMessage struct {
	Name  storepb.SettingName
	Value string
}

// SettingMessage is the message of setting.
type SettingMessage struct {
	Name  storepb.SettingName
	Value string
}

func (s *Store) GetPasswordRestrictionSetting(ctx context.Context) (*storepb.PasswordRestrictionSetting, error) {
	passwordRestriction := &storepb.PasswordRestrictionSetting{
		MinLength: 8,
	}
	setting, err := s.GetSetting(ctx, storepb.SettingName_PASSWORD_RESTRICTION)
	if err != nil {
		return nil, err
	}
	if setting == nil {
		return passwordRestriction, nil
	}

	if err := common.ProtojsonUnmarshaler.Unmarshal([]byte(setting.Value), passwordRestriction); err != nil {
		return nil, err
	}
	return passwordRestriction, nil
}

// GetWorkspaceGeneralSetting gets the workspace general setting payload.
func (s *Store) GetWorkspaceGeneralSetting(ctx context.Context) (*storepb.WorkspaceProfileSetting, error) {
	setting, err := s.GetSetting(ctx, storepb.SettingName_WORKSPACE_PROFILE)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get setting %v", storepb.SettingName_WORKSPACE_PROFILE)
	}
	if setting == nil {
		return nil, errors.Errorf("cannot find setting %v", storepb.SettingName_WORKSPACE_PROFILE)
	}

	payload := new(storepb.WorkspaceProfileSetting)
	if err := common.ProtojsonUnmarshaler.Unmarshal([]byte(setting.Value), payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// GetWorkspaceID finds the workspace id in setting mt.workspace.id.
func (s *Store) GetWorkspaceID(ctx context.Context) (string, error) {
	setting, err := s.GetSetting(ctx, storepb.SettingName_WORKSPACE_ID)
	if err != nil {
		return "", errors.Wrapf(err, "failed to get setting %v", storepb.SettingName_WORKSPACE_ID)
	}
	if setting == nil {
		return "", errors.Errorf("cannot find setting %v", storepb.SettingName_WORKSPACE_ID)
	}
	return setting.Value, nil
}

func (s *Store) GetEnvironmentSetting(ctx context.Context) (*storepb.EnvironmentSetting, error) {
	envSetting := &storepb.EnvironmentSetting{}
	setting, err := s.GetSetting(ctx, storepb.SettingName_ENVIRONMENT)
	if err != nil {
		return nil, err
	}
	if setting == nil {
		return envSetting, nil
	}

	if err := common.ProtojsonUnmarshaler.Unmarshal([]byte(setting.Value), envSetting); err != nil {
		return nil, err
	}
	return envSetting, nil
}

// DeleteCache deletes the cache.
func (s *Store) DeleteCache() {
	s.settingCache.Purge()
	s.policyCache.Purge()
	s.userEmailCache.Purge()
	s.userIDCache.Purge()
}

// GetSetting returns the setting by name.
func (s *Store) GetSetting(ctx context.Context, name storepb.SettingName) (*SettingMessage, error) {
	if v, ok := s.settingCache.Get(name); ok && !s.cacheDisabled {
		return v, nil
	}

	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	settings, err := listSettingImpl(ctx, tx, &FindSettingMessage{
		Name: &name,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list setting")
	}
	if len(settings) == 0 {
		return nil, nil
	}
	if len(settings) > 1 {
		return nil, errors.Errorf("found multiple settings: %v", name)
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}

	s.settingCache.Add(settings[0].Name, settings[0])
	return settings[0], nil
}

// ListSetting returns a list of settings.
func (s *Store) ListSetting(ctx context.Context, find *FindSettingMessage) ([]*SettingMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()
	settings, err := listSettingImpl(ctx, tx, find)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list setting")
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}

	for _, setting := range settings {
		s.settingCache.Add(setting.Name, setting)
	}
	return settings, nil
}

// GetSecret returns the per-deployment AUTH_SECRET setting, the seed used to
// obfuscate stored credentials. The server generates it once and keeps it in
// the database, so it survives restarts. An absent or empty value is an error
// rather than an empty seed.
func (s *Store) GetSecret(ctx context.Context) (string, error) {
	s.secretMu.Lock()
	defer s.secretMu.Unlock()

	if s.secret != "" {
		return s.secret, nil
	}
	setting, err := s.GetSetting(ctx, storepb.SettingName_AUTH_SECRET)
	if err != nil {
		return "", err
	}
	if setting == nil || setting.Value == "" {
		return "", errors.New("auth secret not found")
	}
	s.secret = setting.Value
	return s.secret, nil
}

// UpsertSetting upserts the setting by name.
func (s *Store) UpsertSetting(ctx context.Context, update *SetSettingMessage) (*SettingMessage, error) {
	fields := []string{"name", "value"}
	updateFields := []string{"value = EXCLUDED.value"}
	valuePlaceholders, args := []string{"$1", "$2"}, []any{update.Name.String(), update.Value}

	query := `INSERT INTO setting (` + strings.Join(fields, ", ") + `) 
		VALUES (` + strings.Join(valuePlaceholders, ", ") + `) 
		ON CONFLICT (name) DO UPDATE SET ` + strings.Join(updateFields, ", ") + `
		RETURNING name, value`

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	var setting SettingMessage
	var nameString string
	if err := tx.QueryRowContext(ctx, query, args...).Scan(
		&nameString,
		&setting.Value,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, &common.Error{Code: common.NotFound, Err: errors.Errorf("setting not found: %s", update.Name)}
		}
		return nil, err
	}
	value, ok := storepb.SettingName_value[nameString]
	if !ok {
		return nil, errors.Errorf("invalid setting name string: %s", nameString)
	}
	setting.Name = storepb.SettingName(value)

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}
	s.settingCache.Add(setting.Name, &setting)
	return &setting, nil
}

// CreateSettingIfNotExist creates a new setting only if the named setting doesn't exist.
func (s *Store) CreateSettingIfNotExist(ctx context.Context, create *SettingMessage) (*SettingMessage, bool, error) {
	if v, ok := s.settingCache.Get(create.Name); ok && !s.cacheDisabled {
		return v, false, nil
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()
	settings, err := listSettingImpl(ctx, tx, &FindSettingMessage{Name: &create.Name})
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to list settings")
	}
	if len(settings) > 1 {
		return nil, false, errors.Errorf("found settings for setting name: %v", create.Name)
	}
	if len(settings) == 1 {
		// Don't create setting if the named setting already exists.
		return settings[0], false, nil
	}

	fields := []string{"name", "value"}
	valuesPlaceholders, args := []string{"$1", "$2"}, []any{create.Name.String(), create.Value}

	query := `INSERT INTO setting (` + strings.Join(fields, ",") + `)
		VALUES (` + strings.Join(valuesPlaceholders, ",") + `)
		RETURNING name, value`
	var setting SettingMessage
	var nameString string
	if err := tx.QueryRowContext(ctx, query, args...).Scan(
		&nameString,
		&setting.Value,
	); err != nil {
		return nil, false, err
	}
	value, ok := storepb.SettingName_value[nameString]
	if !ok {
		return nil, false, errors.Errorf("invalid setting name string: %s", nameString)
	}
	setting.Name = storepb.SettingName(value)

	if err := tx.Commit(); err != nil {
		return nil, false, errors.Wrap(err, "failed to commit transaction")
	}
	s.settingCache.Add(setting.Name, &setting)
	return &setting, true, nil
}

// DeleteSetting deletes a setting by the name.
func (s *Store) DeleteSetting(ctx context.Context, name storepb.SettingName) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM setting WHERE name = $1`, name.String()); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit transaction")
	}

	s.settingCache.Remove(name)
	return nil
}

func listSettingImpl(ctx context.Context, txn *sql.Tx, find *FindSettingMessage) ([]*SettingMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	if v := find.Name; v != nil {
		where, args = append(where, fmt.Sprintf("name = $%d", len(args)+1)), append(args, v.String())
	}
	rows, err := txn.QueryContext(ctx, `
		SELECT
			name,
			value
		FROM setting
		WHERE `+strings.Join(where, " AND "), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var settingMessages []*SettingMessage
	for rows.Next() {
		var settingMessage SettingMessage
		var nameString string
		if err := rows.Scan(
			&nameString,
			&settingMessage.Value,
		); err != nil {
			return nil, err
		}
		value, ok := storepb.SettingName_value[nameString]
		if !ok {
			return nil, errors.Errorf("invalid setting name string: %s", nameString)
		}
		settingMessage.Name = storepb.SettingName(value)
		settingMessages = append(settingMessages, &settingMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return settingMessages, nil
}
