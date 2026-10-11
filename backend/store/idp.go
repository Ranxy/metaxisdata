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

// IdentityProviderMessage is the message for identity provider.
type IdentityProviderMessage struct {
	ResourceID string
	Title      string
	Domain     string
	Type       storepb.IdentityProviderType
	Config     *storepb.IdentityProviderConfig
}

// FindIdentityProviderMessage is the message for finding identity providers.
type FindIdentityProviderMessage struct {
	ResourceID *string
}

// UpdateIdentityProviderMessage is the message for updating an identity
// provider. A nil field is left alone.
type UpdateIdentityProviderMessage struct {
	ResourceID string
	Title      *string
	Domain     *string
	Config     *storepb.IdentityProviderConfig
}

// idpCredentialFields names the identity provider config fields stored
// encrypted with the deployment data key. The guard test
// TestEveryIdentityProviderCredentialIsEncrypted walks the stored message and
// fails when a credential-like field is missing here.
//
// Unlike an instance credential, the ciphertext replaces the field in place
// rather than moving to a sibling `_ciphertext` field: the whole config is one
// JSONB column, the proto message has no second field to write, and no caller
// reads the raw row.
var idpCredentialFields = []string{"client_secret"}

// GetIdentityProvider gets an identity provider, with its client secret
// decrypted.
func (s *Store) GetIdentityProvider(ctx context.Context, find *FindIdentityProviderMessage) (*IdentityProviderMessage, error) {
	if find.ResourceID != nil {
		if v, ok := s.idpCache.Get(*find.ResourceID); ok && !s.cacheDisabled {
			return v, nil
		}
	}

	identityProviders, err := s.readIdentityProviders(ctx, find)
	if err != nil {
		return nil, err
	}

	if len(identityProviders) == 0 {
		return nil, nil
	} else if len(identityProviders) > 1 {
		return nil, &common.Error{Code: common.Conflict, Err: errors.Errorf("found %d identity providers with filter %+v, expect 1", len(identityProviders), find)}
	}

	identityProvider := identityProviders[0]
	s.idpCache.Add(identityProvider.ResourceID, identityProvider)
	return identityProvider, nil
}

// ListIdentityProviders lists identity providers, with their client secrets
// decrypted.
func (s *Store) ListIdentityProviders(ctx context.Context, find *FindIdentityProviderMessage) ([]*IdentityProviderMessage, error) {
	if find == nil {
		find = &FindIdentityProviderMessage{}
	}
	identityProviders, err := s.readIdentityProviders(ctx, find)
	if err != nil {
		return nil, err
	}
	for _, identityProvider := range identityProviders {
		s.idpCache.Add(identityProvider.ResourceID, identityProvider)
	}
	return identityProviders, nil
}

func (s *Store) readIdentityProviders(ctx context.Context, find *FindIdentityProviderMessage) ([]*IdentityProviderMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	identityProviders, err := s.listIdentityProvidersImpl(ctx, tx, find)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return identityProviders, nil
}

func (s *Store) listIdentityProvidersImpl(ctx context.Context, txn *sql.Tx, find *FindIdentityProviderMessage) ([]*IdentityProviderMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	if v := find.ResourceID; v != nil {
		where, args = append(where, fmt.Sprintf("resource_id = $%d", len(args)+1)), append(args, *v)
	}

	rows, err := txn.QueryContext(ctx, `
		SELECT
			resource_id,
			name,
			domain,
			type,
			config
		FROM idp
		WHERE `+strings.Join(where, " AND ")+` ORDER BY id ASC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var identityProviderMessages []*IdentityProviderMessage
	for rows.Next() {
		var identityProviderMessage IdentityProviderMessage
		var identityProviderType string
		var identityProviderConfig string
		if err := rows.Scan(
			&identityProviderMessage.ResourceID,
			&identityProviderMessage.Title,
			&identityProviderMessage.Domain,
			&identityProviderType,
			&identityProviderConfig,
		); err != nil {
			return nil, err
		}
		identityProviderMessage.Type = convertIdentityProviderType(identityProviderType)
		config, err := s.convertIdentityProviderConfigString(identityProviderMessage.Type, identityProviderConfig)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read the config of identity provider %q", identityProviderMessage.ResourceID)
		}
		identityProviderMessage.Config = config
		identityProviderMessages = append(identityProviderMessages, &identityProviderMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return identityProviderMessages, nil
}

// CreateIdentityProvider creates an identity provider. The caller supplies the
// client secret in the clear; it is encrypted before it reaches the database.
func (s *Store) CreateIdentityProvider(ctx context.Context, create *IdentityProviderMessage) (*IdentityProviderMessage, error) {
	config, err := s.encryptIdentityProviderConfig(create.Config)
	if err != nil {
		return nil, err
	}
	configBytes, err := protojson.Marshal(config)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal the identity provider config")
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO idp (resource_id, name, domain, type, config)
		VALUES ($1, $2, $3, $4, $5)`,
		create.ResourceID, create.Title, create.Domain, identityProviderTypeString(create.Type), configBytes,
	); err != nil {
		if isUniqueViolation(err) {
			return nil, common.Errorf(common.Conflict, "identity provider %q already exists", create.ResourceID)
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// The stored row holds the ciphertext; the caller already has the plaintext
	// message it passed in, so return that rather than reading the row back.
	s.idpCache.Add(create.ResourceID, create)
	return create, nil
}

// UpdateIdentityProvider updates an identity provider. A nil field is left
// alone.
func (s *Store) UpdateIdentityProvider(ctx context.Context, patch *UpdateIdentityProviderMessage) (*IdentityProviderMessage, error) {
	set, args := []string{}, []any{}
	if v := patch.Title; v != nil {
		args = append(args, *v)
		set = append(set, fmt.Sprintf("name = $%d", len(args)))
	}
	if v := patch.Domain; v != nil {
		args = append(args, *v)
		set = append(set, fmt.Sprintf("domain = $%d", len(args)))
	}
	if v := patch.Config; v != nil {
		config, err := s.encryptIdentityProviderConfig(v)
		if err != nil {
			return nil, err
		}
		configBytes, err := protojson.Marshal(config)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal the identity provider config")
		}
		args = append(args, configBytes)
		set = append(set, fmt.Sprintf("config = $%d", len(args)))
	}
	if len(set) == 0 {
		return nil, errors.New("no field to update")
	}
	args = append(args, patch.ResourceID)
	resourceIDArg := fmt.Sprintf("$%d", len(args))

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var (
		resourceID   string
		title        string
		domain       string
		idpType      string
		configString string
	)
	if err := tx.QueryRowContext(ctx, `
		UPDATE idp SET `+strings.Join(set, ", ")+`
		WHERE resource_id = `+resourceIDArg+`
		RETURNING resource_id, name, domain, type, config`,
		args...,
	).Scan(&resourceID, &title, &domain, &idpType, &configString); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.NotFound, "identity provider %q not found", patch.ResourceID)
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// The RETURNING row carries the ciphertext; decrypt it so the response is
	// shaped like a read.
	config, err := s.convertIdentityProviderConfigString(convertIdentityProviderType(idpType), configString)
	if err != nil {
		return nil, err
	}
	updated := &IdentityProviderMessage{
		ResourceID: resourceID,
		Title:      title,
		Domain:     domain,
		Type:       convertIdentityProviderType(idpType),
		Config:     config,
	}
	s.idpCache.Add(updated.ResourceID, updated)
	return updated, nil
}

// DeleteIdentityProvider deletes an identity provider. Accounts bound to it keep
// their binding rows; those bindings stop resolving until a provider with the
// same id exists again.
func (s *Store) DeleteIdentityProvider(ctx context.Context, resourceID string) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `DELETE FROM idp WHERE resource_id = $1`, resourceID)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return common.Errorf(common.NotFound, "identity provider %q not found", resourceID)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.idpCache.Remove(resourceID)
	return nil
}

// encryptIdentityProviderConfig returns a copy of config whose client secret is
// encrypted. An empty secret stays empty, and a config with no OAuth2 half is
// returned unchanged.
func (s *Store) encryptIdentityProviderConfig(config *storepb.IdentityProviderConfig) (*storepb.IdentityProviderConfig, error) {
	oauth2Config := config.GetOauth2Config()
	if oauth2Config == nil || oauth2Config.GetClientSecret() == "" {
		return config, nil
	}
	cipher, err := s.credentialCipher()
	if err != nil {
		return nil, err
	}
	sealed, err := cipher.Encrypt(oauth2Config.GetClientSecret())
	if err != nil {
		return nil, errors.Wrap(err, "failed to encrypt the identity provider client secret")
	}
	encrypted, ok := proto.Clone(config).(*storepb.IdentityProviderConfig)
	if !ok {
		return nil, errors.New("failed to clone the identity provider config")
	}
	encrypted.GetOauth2Config().ClientSecret = sealed
	return encrypted, nil
}

// decryptIdentityProviderConfig returns a copy of config whose client secret is
// decrypted.
//
// A value that is not a ciphertext this deployment can open is an error, never a
// silent pass-through: a provider whose secret cannot be read is one nobody can
// sign in through, and handing the stored bytes to the provider as if they were
// the credential would turn a key mistake into an authentication failure that
// looks like a wrong secret.
func (s *Store) decryptIdentityProviderConfig(config *storepb.IdentityProviderConfig) (*storepb.IdentityProviderConfig, error) {
	oauth2Config := config.GetOauth2Config()
	if oauth2Config == nil || oauth2Config.GetClientSecret() == "" {
		return config, nil
	}
	cipher, err := s.credentialCipher()
	if err != nil {
		return nil, err
	}
	plaintext, err := cipher.Decrypt(oauth2Config.GetClientSecret())
	if err != nil {
		return nil, errors.Wrap(err, "the client secret is not readable with this deployment's data key; re-create the identity provider through the admin API")
	}
	decrypted, ok := proto.Clone(config).(*storepb.IdentityProviderConfig)
	if !ok {
		return nil, errors.New("failed to clone the identity provider config")
	}
	decrypted.GetOauth2Config().ClientSecret = plaintext
	return decrypted, nil
}

func convertIdentityProviderType(identityProviderType string) storepb.IdentityProviderType {
	switch identityProviderType {
	case "OAUTH2":
		return storepb.IdentityProviderType_OAUTH2
	default:
		return storepb.IdentityProviderType_IDENTITY_PROVIDER_TYPE_UNSPECIFIED
	}
}

// identityProviderTypeString is the inverse of convertIdentityProviderType: the
// value the idp_type_check constraint accepts.
func identityProviderTypeString(identityProviderType storepb.IdentityProviderType) string {
	switch identityProviderType {
	case storepb.IdentityProviderType_OAUTH2:
		return "OAUTH2"
	default:
		return ""
	}
}

// convertIdentityProviderConfigString decodes the config JSONB.
//
// The column holds the marshalled **outer** `IdentityProviderConfig` — the
// message the column comment names — so the oneof wrapper is part of the JSON
// and has to be part of the decode. Decoding it as the inner
// `OAuth2IdentityProviderConfig` instead, as this once did, drops every field as
// unknown under `DiscardUnknown` and yields an empty config: reads succeed and
// the login then fails with a mapping that is silently blank.
func (s *Store) convertIdentityProviderConfigString(identityProviderType storepb.IdentityProviderType, config string) (*storepb.IdentityProviderConfig, error) {
	if identityProviderType != storepb.IdentityProviderType_OAUTH2 {
		// Unknown identity provider types have no configuration to decode. The
		// type check constraint keeps them out of the table.
		return nil, nil
	}
	var formattedConfig storepb.IdentityProviderConfig
	if err := common.ProtojsonUnmarshaler.Unmarshal([]byte(config), &formattedConfig); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal the identity provider config")
	}
	// A row whose oneof names another protocol is a row this build cannot serve.
	// Reporting it beats returning an empty config that turns into an
	// inexplicable sign-in failure.
	if formattedConfig.GetOauth2Config() == nil {
		return nil, errors.New("the stored config of an OAUTH2 identity provider carries no oauth2Config")
	}
	return s.decryptIdentityProviderConfig(&formattedConfig)
}
