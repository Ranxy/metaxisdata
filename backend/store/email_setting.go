package store

import (
	"context"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// EmailSettingMessage is the workspace email setting as a read returns it: the
// password ciphertext is stripped, and only whether one is stored is reported.
type EmailSettingMessage struct {
	Setting     *storepb.EmailSetting
	HasPassword bool
}

// GetEmailSetting reads the workspace email setting. A nil setting means email
// delivery is not configured. The password is never part of the returned message.
func (s *Store) GetEmailSetting(ctx context.Context) (*EmailSettingMessage, error) {
	setting, err := s.getEmailSettingPayload(ctx)
	if err != nil || setting == nil {
		return nil, err
	}
	hasPassword := setting.GetSmtp().GetPassword() != ""
	setting.Smtp.Password = ""
	return &EmailSettingMessage{Setting: setting, HasPassword: hasPassword}, nil
}

// GetEmailSettingForDelivery reads the workspace email setting with its password
// decrypted, for the code that sends mail. A nil setting means email delivery is
// not configured.
func (s *Store) GetEmailSettingForDelivery(ctx context.Context) (*storepb.EmailSetting, error) {
	setting, err := s.getEmailSettingPayload(ctx)
	if err != nil || setting == nil {
		return nil, err
	}
	if setting.GetFrom() == "" || setting.GetSmtp().GetHost() == "" {
		return nil, nil
	}
	encrypted := setting.GetSmtp().GetPassword()
	if encrypted == "" {
		return setting, nil
	}
	cipher, err := s.credentialCipher()
	if err != nil {
		return nil, err
	}
	plaintext, err := cipher.Decrypt(encrypted)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decrypt the email password")
	}
	setting.Smtp.Password = plaintext
	return setting, nil
}

// SetEmailSetting writes the workspace email setting. A nil password keeps the
// stored one; a pointer replaces it, and an empty pointer clears it.
func (s *Store) SetEmailSetting(ctx context.Context, setting *storepb.EmailSetting, password *string) error {
	if setting == nil {
		return common.Errorf(common.Invalid, "an email setting is required")
	}
	if setting.Smtp == nil {
		setting.Smtp = &storepb.EmailSMTPConfig{}
	}
	stored := ""
	switch {
	case password != nil && *password != "":
		cipher, err := s.credentialCipher()
		if err != nil {
			return err
		}
		encrypted, err := cipher.Encrypt(*password)
		if err != nil {
			return errors.Wrap(err, "failed to encrypt the email password")
		}
		stored = encrypted
	case password == nil:
		// The update named no new password, so the stored ciphertext stays. It is
		// read back rather than trusting the request, because the request never
		// carries the stored password.
		existing, err := s.getEmailSettingPayload(ctx)
		if err != nil {
			return err
		}
		stored = existing.GetSmtp().GetPassword()
	default:
		// An empty password clears the stored ciphertext.
	}
	setting.Smtp.Password = stored

	payload, err := protojson.Marshal(setting)
	if err != nil {
		return errors.Wrap(err, "failed to marshal the email setting")
	}
	if _, err := s.UpsertSetting(ctx, &SetSettingMessage{
		Name:  storepb.SettingName_EMAIL,
		Value: string(payload),
	}); err != nil {
		return errors.Wrap(err, "failed to save the email setting")
	}
	return nil
}

// DeleteEmailSetting removes the workspace email setting, which turns email
// delivery off until an administrator configures it again.
func (s *Store) DeleteEmailSetting(ctx context.Context) error {
	return s.DeleteSetting(ctx, storepb.SettingName_EMAIL)
}

// getEmailSettingPayload returns the stored payload with its password still
// encrypted. A nil setting means the row is absent.
func (s *Store) getEmailSettingPayload(ctx context.Context) (*storepb.EmailSetting, error) {
	stored, err := s.GetSetting(ctx, storepb.SettingName_EMAIL)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the email setting")
	}
	if stored == nil || stored.Value == "" {
		return nil, nil
	}
	setting := &storepb.EmailSetting{}
	if err := common.ProtojsonUnmarshaler.Unmarshal([]byte(stored.Value), setting); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal the email setting")
	}
	if setting.Smtp == nil {
		setting.Smtp = &storepb.EmailSMTPConfig{}
	}
	return setting, nil
}
