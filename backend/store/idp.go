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

// GetIdentityProvider gets an identity provider.
func (s *Store) GetIdentityProvider(ctx context.Context, find *FindIdentityProviderMessage) (*IdentityProviderMessage, error) {
	if find.ResourceID != nil {
		if v, ok := s.idpCache.Get(*find.ResourceID); ok && !s.cacheDisabled {
			return v, nil
		}
	}

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

	if len(identityProviders) == 0 {
		return nil, nil
	} else if len(identityProviders) > 1 {
		return nil, &common.Error{Code: common.Conflict, Err: errors.Errorf("found %d identity providers with filter %+v, expect 1", len(identityProviders), find)}
	}

	identityProvider := identityProviders[0]
	s.idpCache.Add(identityProvider.ResourceID, identityProvider)
	return identityProvider, nil
}

func (*Store) listIdentityProvidersImpl(ctx context.Context, txn *sql.Tx, find *FindIdentityProviderMessage) ([]*IdentityProviderMessage, error) {
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
		identityProviderMessage.Config = convertIdentityProviderConfigString(identityProviderMessage.Type, identityProviderConfig)
		identityProviderMessages = append(identityProviderMessages, &identityProviderMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return identityProviderMessages, nil
}

func convertIdentityProviderType(identityProviderType string) storepb.IdentityProviderType {
	switch identityProviderType {
	case "OAUTH2":
		return storepb.IdentityProviderType_OAUTH2
	default:
		return storepb.IdentityProviderType_IDENTITY_PROVIDER_TYPE_UNSPECIFIED
	}
}

func convertIdentityProviderConfigString(identityProviderType storepb.IdentityProviderType, config string) *storepb.IdentityProviderConfig {
	switch identityProviderType {
	case storepb.IdentityProviderType_OAUTH2:
		var formattedConfig storepb.OAuth2IdentityProviderConfig
		if err := common.ProtojsonUnmarshaler.Unmarshal([]byte(config), &formattedConfig); err != nil {
			return nil
		}
		return &storepb.IdentityProviderConfig{
			Config: &storepb.IdentityProviderConfig_Oauth2Config{
				Oauth2Config: &formattedConfig,
			},
		}
	default:
		// Unknown identity provider types have no configuration to decode.
		return nil
	}
}
