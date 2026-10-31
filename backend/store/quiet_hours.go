package store

import (
	"context"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// GetQuietHoursSetting reads the workspace quiet-hours schedule. A nil setting
// means quiet hours were never configured; an empty schedule means they are off.
func (s *Store) GetQuietHoursSetting(ctx context.Context) (*storepb.QuietHoursSetting, error) {
	stored, err := s.GetSetting(ctx, storepb.SettingName_QUIET_HOURS)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the quiet hours setting")
	}
	if stored == nil || stored.Value == "" {
		return nil, nil
	}
	setting := &storepb.QuietHoursSetting{}
	if err := common.ProtojsonUnmarshaler.Unmarshal([]byte(stored.Value), setting); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal the quiet hours setting")
	}
	return setting, nil
}

// SetQuietHoursSetting writes the workspace quiet-hours schedule.
func (s *Store) SetQuietHoursSetting(ctx context.Context, setting *storepb.QuietHoursSetting) error {
	if setting == nil {
		return common.Errorf(common.Invalid, "a quiet hours setting is required")
	}
	payload, err := protojson.Marshal(setting)
	if err != nil {
		return errors.Wrap(err, "failed to marshal the quiet hours setting")
	}
	update := &SetSettingMessage{
		Name:  storepb.SettingName_QUIET_HOURS,
		Value: string(payload),
	}
	if _, err := s.UpsertSetting(ctx, update); err != nil {
		return errors.Wrap(err, "failed to save the quiet hours setting")
	}
	return nil
}

// DeleteQuietHoursSetting removes the schedule, which turns quiet hours off.
func (s *Store) DeleteQuietHoursSetting(ctx context.Context) error {
	return s.DeleteSetting(ctx, storepb.SettingName_QUIET_HOURS)
}
