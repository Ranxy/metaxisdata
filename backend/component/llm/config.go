package llm

import (
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
)

// This file owns the provider-selection rules a feature uses to turn the
// registry's enabled profiles into the one provider + model it runs on. It is a
// component-level rule, not a handler's: Explain SQL and the data quality
// analysis resolve their provider the same way, so the allowlist and the default
// cannot drift between them.
//
// The failures are Connect errors because every consumer is an RPC handler that
// returns them unchanged. A non-RPC caller can still read the status with
// connect.CodeOf(err).

// profileNamePrefix is the resource-name prefix the store gives every profile.
const profileNamePrefix = "llm-provider-profiles/"

// ProfileID normalizes a profile reference to its bare id. A profile's stored
// name is "llm-provider-profiles/{id}", while a request or a workspace setting may
// hold either form, so every comparison normalizes both sides.
func ProfileID(profile string) string {
	trimmed := strings.TrimSpace(profile)
	if id, ok := strings.CutPrefix(trimmed, profileNamePrefix); ok {
		return id
	}
	return trimmed
}

// NormalizeProfileName returns the canonical resource-name form of a profile
// reference. A bare id is accepted because an older workspace setting may hold
// one; an empty or malformed value is an error rather than a silent pass.
func NormalizeProfileName(profile string) (string, error) {
	id := ProfileID(profile)
	if id == "" {
		return "", errors.New("an LLM provider profile is required")
	}
	if strings.Contains(id, "/") {
		return "", errors.Errorf("LLM profile %q is not a profile resource name", profile)
	}
	return profileNamePrefix + id, nil
}

// FindModelConfig returns the enabled config for an exact profile and model pair,
// or nil. The model is compared exactly: a profile can enable several models at
// different prices, so the model is never inferred.
func FindModelConfig(configs []ResolvedConfig, profile, model string) *ResolvedConfig {
	wantedID := ProfileID(profile)
	model = strings.TrimSpace(model)
	if wantedID == "" || model == "" {
		return nil
	}
	for i := range configs {
		// Both sides are trimmed because the agent's value is stored trimmed while a
		// profile may hold whatever its provider returned.
		if ProfileID(configs[i].ProfileName) == wantedID && strings.TrimSpace(configs[i].ModelName) == model {
			return &configs[i]
		}
	}
	return nil
}

// FindConfig returns the enabled config the profile reference names, or nil. The
// reference is a v1 resource name while the config may hold either form, so the
// comparison normalizes both sides.
func FindConfig(configs []ResolvedConfig, profile string) *ResolvedConfig {
	wantedID := ProfileID(profile)
	for i := range configs {
		if ProfileID(configs[i].ProfileName) == wantedID {
			return &configs[i]
		}
	}
	return nil
}

// AllowedConfigs keeps the configs whose profile is inside the allowlist. An empty
// allowlist means no restriction.
func AllowedConfigs(configs []ResolvedConfig, allowed []string) []ResolvedConfig {
	if len(allowed) == 0 {
		return configs
	}
	allowedIDs := make(map[string]struct{}, len(allowed))
	for _, profile := range allowed {
		allowedIDs[ProfileID(profile)] = struct{}{}
	}
	filtered := make([]ResolvedConfig, 0, len(configs))
	for _, config := range configs {
		if _, ok := allowedIDs[ProfileID(config.ProfileName)]; ok {
			filtered = append(filtered, config)
		}
	}
	return filtered
}

// ProfileAllowed reports whether a requested profile is inside the allowlist; an
// empty allowlist allows everything.
func ProfileAllowed(profile string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	id := ProfileID(profile)
	for _, candidate := range allowed {
		if ProfileID(candidate) == id {
			return true
		}
	}
	return false
}

// ResolveConfig picks the provider and model a feature runs on. An explicitly
// requested profile must be allowed by the workspace setting; otherwise the first
// allowed one is the default. Each rejection carries its own status:
//
//   - InvalidArgument: the requested profile is outside the workspace allowlist.
//   - NotFound: the requested profile is allowed but no enabled profile matches.
//   - FailedPrecondition: no profile was requested and the allowlist admits none.
func ResolveConfig(configs []ResolvedConfig, allowed []string, requested string) (*ResolvedConfig, error) {
	if requested != "" {
		if !ProfileAllowed(requested, allowed) {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				errors.Errorf("LLM profile %q is not allowed by the workspace settings", requested))
		}
		config := FindConfig(configs, requested)
		if config == nil {
			return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("LLM profile %q not found", requested))
		}
		return config, nil
	}
	allowedConfigs := AllowedConfigs(configs, allowed)
	if len(allowedConfigs) == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("no enabled LLM provider profiles are allowed"))
	}
	return &allowedConfigs[0], nil
}

// ResolveModelConfig picks one exact profile and model pair. Unlike ResolveConfig
// there is no default: a missing half is InvalidArgument, a profile outside the
// allowlist is InvalidArgument, and a profile that does not currently enable the
// model is NotFound. The caller decides what "no model" means.
func ResolveModelConfig(configs []ResolvedConfig, allowed []string, profile, model string) (*ResolvedConfig, error) {
	if strings.TrimSpace(profile) == "" || strings.TrimSpace(model) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("an LLM provider profile and a model are required"))
	}
	if !ProfileAllowed(profile, allowed) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.Errorf("LLM profile %q is not allowed by the workspace settings", profile))
	}
	config := FindModelConfig(configs, profile, model)
	if config == nil {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("LLM profile %q does not enable model %q", profile, model))
	}
	return config, nil
}
