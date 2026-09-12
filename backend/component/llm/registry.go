// Package llm provides internal components for LLM provider management.
package llm

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/Ranxy/metaxisdata/backend/config"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// registryCacheTTL bounds how long a resolved provider list is reused. Writes
// through LLMService invalidate the cache immediately; the TTL only covers
// changes made by another replica.
const registryCacheTTL = 30 * time.Second

// registryPageSize is the page size used to walk every profile.
const registryPageSize = 100

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

	mu       sync.Mutex
	loaded   bool
	cached   []ResolvedConfig
	cachedAt time.Time
}

// NewRegistry creates a new Registry.
func NewRegistry(st *store.Store, profile *config.Profile) *Registry {
	return &Registry{store: st, profile: profile}
}

// ListEnabled returns all (profile + enabled model) combinations.
//
// The result is cached briefly: every ExplainSQL request needs the provider and
// model for its cache key, and a miss costs a database round trip plus an
// AES-GCM decryption of every profile's API key.
func (r *Registry) ListEnabled(ctx context.Context) ([]ResolvedConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.loaded && time.Since(r.cachedAt) < registryCacheTTL {
		return slices.Clone(r.cached), nil
	}

	configs, err := r.loadEnabled(ctx)
	if err != nil {
		return nil, err
	}
	r.cached = configs
	r.cachedAt = time.Now()
	r.loaded = true
	return slices.Clone(configs), nil
}

// Invalidate drops the cached provider list. Profile writes must call it so a
// new or edited profile is visible immediately.
func (r *Registry) Invalidate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loaded = false
	r.cached = nil
}

// loadEnabled walks every profile page. The query used to run with the store's
// default limit, so profiles past the first 50 were invisible.
func (r *Registry) loadEnabled(ctx context.Context) ([]ResolvedConfig, error) {
	var configs []ResolvedConfig
	for offset := 0; ; offset += registryPageSize {
		limit := registryPageSize
		profiles, err := r.store.ListLLMProfiles(ctx, &store.FindLLMProfileMessage{
			Limit:  &limit,
			Offset: &offset,
		})
		if err != nil {
			return nil, err
		}
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
		if len(profiles) < registryPageSize {
			return configs, nil
		}
	}
}

// DebugEnabled returns whether the system is in debug mode.
func (r *Registry) DebugEnabled() bool {
	return r.profile.RuntimeDebug.Load()
}
