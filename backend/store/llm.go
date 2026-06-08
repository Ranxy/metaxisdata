package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// LLMProfileMessage is the message for an LLM provider profile.
type LLMProfileMessage struct {
	ID         int64
	ResourceID string
	Metadata   *storepb.LlmProviderProfile
}

// FindLLMProfileMessage is the message for finding LLM provider profiles.
type FindLLMProfileMessage struct {
	ResourceID *string
	Limit      *int
	Offset     *int
}

// UpdateLLMProfileMessage is the message for updating an LLM provider profile.
type UpdateLLMProfileMessage struct {
	ResourceID string
	Title      *string
	BaseURL    *string
	APIKey     *string
	Models     []*storepb.LlmProviderModel
}

func (s *Store) obfuscateLLMProfile(ctx context.Context, meta *storepb.LlmProviderProfile) error {
	secret, err := s.GetSecret(ctx)
	if err != nil {
		return err
	}
	meta.ApiKeyEncrypted = common.Obfuscate(meta.ApiKeyEncrypted, secret)
	return nil
}

func (s *Store) deobfuscateLLMProfile(ctx context.Context, meta *storepb.LlmProviderProfile) error {
	if meta.ApiKeyEncrypted == "" {
		return nil
	}
	secret, err := s.GetSecret(ctx)
	if err != nil {
		return err
	}
	key, err := common.Unobfuscate(meta.ApiKeyEncrypted, secret)
	if err != nil {
		return err
	}
	meta.ApiKeyEncrypted = key
	return nil
}

// CreateLLMProfile creates a new LLM provider profile.
func (s *Store) CreateLLMProfile(ctx context.Context, resourceID string, meta *storepb.LlmProviderProfile) (*LLMProfileMessage, error) {
	cloned, ok := proto.Clone(meta).(*storepb.LlmProviderProfile)
	if !ok {
		return nil, errors.New("failed to clone LLM profile")
	}
	if err := s.obfuscateLLMProfile(ctx, cloned); err != nil {
		return nil, err
	}

	metadataBytes, err := protojson.Marshal(cloned)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal LLM profile")
	}

	now := time.Now().UTC()

	var id int64
	err = s.GetDB().QueryRowContext(ctx, `
		INSERT INTO llm_provider_profile (resource_id, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, resourceID, metadataBytes, now, now).Scan(&id)
	if err != nil {
		return nil, errors.Wrap(err, "failed to insert LLM profile")
	}

	meta.CreateTime = timestamppb.New(now)
	meta.UpdateTime = timestamppb.New(now)
	meta.Name = "llm-provider-profiles/" + resourceID

	return &LLMProfileMessage{
		ID:         id,
		ResourceID: resourceID,
		Metadata:   meta,
	}, nil
}

// UpdateLLMProfile updates an existing LLM provider profile.
func (s *Store) UpdateLLMProfile(ctx context.Context, update *UpdateLLMProfileMessage) (*LLMProfileMessage, error) {
	existing, err := s.GetLLMProfile(ctx, &FindLLMProfileMessage{ResourceID: &update.ResourceID})
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, errors.Errorf("LLM profile %q not found", update.ResourceID)
	}

	meta := existing.Metadata
	if update.Title != nil {
		meta.Title = *update.Title
	}
	if update.BaseURL != nil {
		meta.BaseUrl = *update.BaseURL
	}
	if update.APIKey != nil {
		meta.ApiKeyEncrypted = *update.APIKey
	}
	if update.Models != nil {
		meta.Models = update.Models
	}

	cloned, ok := proto.Clone(meta).(*storepb.LlmProviderProfile)
	if !ok {
		return nil, errors.New("failed to clone LLM profile")
	}
	if err := s.obfuscateLLMProfile(ctx, cloned); err != nil {
		return nil, err
	}

	metadataBytes, err := protojson.Marshal(cloned)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal LLM profile")
	}

	now := time.Now().UTC()
	if _, err := s.GetDB().ExecContext(ctx, `
		UPDATE llm_provider_profile SET metadata = $2, updated_at = $3
		WHERE resource_id = $1
	`, update.ResourceID, metadataBytes, now); err != nil {
		return nil, errors.Wrap(err, "failed to update LLM profile")
	}

	meta.UpdateTime = timestamppb.New(now)
	return &LLMProfileMessage{
		ID:         existing.ID,
		ResourceID: update.ResourceID,
		Metadata:   meta,
	}, nil
}

// GetLLMProfile gets a single LLM profile by resource ID.
func (s *Store) GetLLMProfile(ctx context.Context, find *FindLLMProfileMessage) (*LLMProfileMessage, error) {
	profiles, err := s.ListLLMProfiles(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, nil
	}
	return profiles[0], nil
}

// ListLLMProfiles lists LLM provider profiles.
func (s *Store) ListLLMProfiles(ctx context.Context, find *FindLLMProfileMessage) ([]*LLMProfileMessage, error) {
	where := []string{"1 = 1"}
	args := []any{}

	if find.ResourceID != nil {
		args = append(args, *find.ResourceID)
		where = append(where, fmt.Sprintf("resource_id = $%d", len(args)))
	}

	limit := 50
	offset := 0
	if find.Limit != nil {
		limit = *find.Limit
	}
	if find.Offset != nil {
		offset = *find.Offset
	}

	args = append(args, limit, offset)
	query := fmt.Sprintf(`
		SELECT id, resource_id, metadata, created_at, updated_at
		FROM llm_provider_profile
		WHERE %s
		ORDER BY updated_at DESC
		LIMIT $%d OFFSET $%d
	`, strings.Join(where, " AND "), len(args)-1, len(args))

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query LLM profiles")
	}
	defer rows.Close()

	var profiles []*LLMProfileMessage
	for rows.Next() {
		var (
			id         int64
			resourceID string
			metadata   []byte
			createdAt  time.Time
			updatedAt  time.Time
		)
		if err := rows.Scan(&id, &resourceID, &metadata, &createdAt, &updatedAt); err != nil {
			return nil, errors.Wrap(err, "failed to scan LLM profile")
		}

		meta := &storepb.LlmProviderProfile{}
		if err := common.ProtojsonUnmarshaler.Unmarshal(metadata, meta); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal LLM profile")
		}
		if err := s.deobfuscateLLMProfile(ctx, meta); err != nil {
			return nil, err
		}

		meta.Name = "llm-provider-profiles/" + resourceID
		meta.CreateTime = timestamppb.New(createdAt)
		meta.UpdateTime = timestamppb.New(updatedAt)

		profiles = append(profiles, &LLMProfileMessage{
			ID:         id,
			ResourceID: resourceID,
			Metadata:   meta,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return profiles, nil
}

// DeleteLLMProfile deletes an LLM provider profile.
func (s *Store) DeleteLLMProfile(ctx context.Context, resourceID string) error {
	_, err := s.GetDB().ExecContext(ctx, `
		DELETE FROM llm_provider_profile WHERE resource_id = $1
	`, resourceID)
	return errors.Wrap(err, "failed to delete LLM profile")
}
