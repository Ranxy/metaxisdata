// Package llm provides internal components for LLM provider management.
package llm

import (
	"context"

	"github.com/Ranxy/metaxisdata/backend/config"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// ResolvedConfig is a ready-to-use provider + model combination.
type ResolvedConfig struct {
	ProfileName  string
	ProfileTitle string
	Type         storepb.LLMProviderType
	BaseURL      string
	APIKey       string // decrypted
	ModelName    string
}

// Registry provides access to configured LLM providers for internal consumers.
type Registry struct {
	store   *store.Store
	profile *config.Profile
}

// NewRegistry creates a new Registry.
func NewRegistry(st *store.Store, profile *config.Profile) *Registry {
	return &Registry{store: st, profile: profile}
}

// ListEnabled returns all (profile + enabled model) combinations.
func (r *Registry) ListEnabled(ctx context.Context) ([]ResolvedConfig, error) {
	profiles, err := r.store.ListLLMProfiles(ctx, &store.FindLLMProfileMessage{})
	if err != nil {
		return nil, err
	}

	var configs []ResolvedConfig
	for _, p := range profiles {
		for _, m := range p.Metadata.Models {
			if !m.Enabled {
				continue
			}
			title := p.Metadata.Title
			if title == "" {
				title = p.Metadata.Name
			}
			configs = append(configs, ResolvedConfig{
				ProfileName:  p.Metadata.Name,
				ProfileTitle: title,
				Type:         p.Metadata.Type,
				BaseURL:      p.Metadata.BaseUrl,
				APIKey:       p.Metadata.ApiKeyEncrypted,
				ModelName:    m.Name,
			})
		}
	}
	return configs, nil
}

// DebugEnabled returns whether the system is in debug mode.
func (r *Registry) DebugEnabled() bool {
	return r.profile.RuntimeDebug.Load()
}
