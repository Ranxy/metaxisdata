package server

import (
	"context"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func (s *Server) initializeSetting(ctx context.Context) error {
	// secretLength is the length of the per-deployment AUTH_SECRET. It is the
	// seed for stored credentials and the JWT signing key.
	const secretLength = 32

	// initial branding
	_, firstTimeOnboarding, err := s.store.CreateSettingIfNotExist(ctx, &store.SettingMessage{
		Name:  storepb.SettingName_BRANDING_LOGO,
		Value: "",
	})
	if err != nil {
		return err
	}

	// initial JWT token
	secret, err := common.RandomString(secretLength)
	if err != nil {
		return errors.Wrap(err, "failed to generate random JWT secret")
	}
	if _, _, err := s.store.CreateSettingIfNotExist(ctx, &store.SettingMessage{
		Name:  storepb.SettingName_AUTH_SECRET,
		Value: secret,
	}); err != nil {
		return err
	}

	// initial workspace
	if _, _, err := s.store.CreateSettingIfNotExist(ctx, &store.SettingMessage{
		Name:  storepb.SettingName_WORKSPACE_ID,
		Value: uuid.New().String(),
	}); err != nil {
		return err
	}

	// Init password validation
	passwordSettingValue, err := protojson.Marshal(&storepb.PasswordRestrictionSetting{
		MinLength:                         8,
		RequireNumber:                     false,
		RequireLetter:                     false,
		RequireUppercaseLetter:            false,
		RequireSpecialCharacter:           false,
		RequireResetPasswordForFirstLogin: false,
	})
	if err != nil {
		return errors.Wrap(err, "failed to marshal initial password validation setting")
	}
	if _, _, err := s.store.CreateSettingIfNotExist(ctx, &store.SettingMessage{
		Name:  storepb.SettingName_PASSWORD_RESTRICTION,
		Value: string(passwordSettingValue),
	}); err != nil {
		return err
	}

	// initial workspace profile setting. It is admin-managed through the setting
	// API, so only create the row on a fresh install and never overwrite an
	// existing one on startup.
	workspaceProfileSetting, err := s.store.GetSetting(ctx, storepb.SettingName_WORKSPACE_PROFILE)
	if err != nil {
		return err
	}
	if workspaceProfileSetting == nil {
		bytes, err := protojson.Marshal(&storepb.WorkspaceProfileSetting{})
		if err != nil {
			return err
		}
		if _, err := s.store.UpsertSetting(ctx, &store.SetSettingMessage{
			Name:  storepb.SettingName_WORKSPACE_PROFILE,
			Value: string(bytes),
		}); err != nil {
			return err
		}
	}

	if firstTimeOnboarding {
		// Only grant workspace member role to allUsers at the first time.
		if _, err := s.store.PatchWorkspaceIamPolicy(ctx, &store.PatchIamPolicyMessage{
			Member: common.AllUsers,
			Roles: []string{
				common.FormatRole(common.WorkspaceMember),
			},
		}); err != nil {
			return err
		}
	}

	// Init workspace environment setting
	environmentSettingValue, err := protojson.Marshal(&storepb.EnvironmentSetting{
		Environments: []*storepb.EnvironmentSetting_Environment{
			{
				Title: "Test",
				Id:    "test",
			},
			{
				Title: "Prod",
				Id:    "prod",
			},
		},
	})
	if err != nil {
		return errors.Wrapf(err, "failed to marshal initial environment setting")
	}
	if _, _, err := s.store.CreateSettingIfNotExist(ctx, &store.SettingMessage{
		Name:  storepb.SettingName_ENVIRONMENT,
		Value: string(environmentSettingValue),
	}); err != nil {
		return err
	}

	return nil
}
