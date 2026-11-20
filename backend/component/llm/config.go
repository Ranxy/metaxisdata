package llm

import (
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
)

// This file owns the provider-selection rules a feature uses to turn the
// registry's enabled configurations into the one provider + model it runs on. It
// is a component-level rule, not a handler's.
//
// The model is the unit of choice everywhere: one profile can enable several
// models at different prices and capacities, so no rule infers a model from a
// profile. Explain SQL is the only feature that applies the workspace allowlist,
// and that allowlist names exact profile + model pairs. The data quality analyses
// choose their pair in the data quality settings, and a quality agent pins one
// pair on the agent, so neither applies the workspace allowlist a second time.
//
// The failures are Connect errors because every consumer is an RPC handler that
// returns them unchanged. A non-RPC caller can still read the status with
// connect.CodeOf(err).

// profileNamePrefix is the resource-name prefix the store gives every profile.
const profileNamePrefix = "llm-provider-profiles/"

// ModelRef names one exact model of one provider profile. Profile is a profile
// reference (compared through ProfileID, so a bare id is accepted too); Model is
// the name as the profile records it.
type ModelRef struct {
	Profile string
	Model   string
}

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

// FindConfig returns the first enabled config for the profile reference, or nil.
// The reference is a v1 resource name while the config may hold either form, so
// the comparison normalizes both sides.
func FindConfig(configs []ResolvedConfig, profile string) *ResolvedConfig {
	wantedID := ProfileID(profile)
	for i := range configs {
		if ProfileID(configs[i].ProfileName) == wantedID {
			return &configs[i]
		}
	}
	return nil
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

// AllowedModelConfigs keeps the configs whose exact profile + model pair is inside
// the allowlist. An empty allowlist means no restriction.
func AllowedModelConfigs(configs []ResolvedConfig, allowed []ModelRef) []ResolvedConfig {
	if len(allowed) == 0 {
		return configs
	}
	filtered := make([]ResolvedConfig, 0, len(configs))
	for _, config := range configs {
		for _, ref := range allowed {
			if ProfileID(config.ProfileName) != ProfileID(ref.Profile) {
				continue
			}
			if strings.TrimSpace(config.ModelName) == strings.TrimSpace(ref.Model) {
				filtered = append(filtered, config)
				break
			}
		}
	}
	return filtered
}

// ModelAllowed reports whether an exact profile + model pair is inside the
// allowlist; an empty allowlist allows everything.
func ModelAllowed(profile, model string, allowed []ModelRef) bool {
	if len(allowed) == 0 {
		return true
	}
	id := ProfileID(profile)
	model = strings.TrimSpace(model)
	if id == "" || model == "" {
		return false
	}
	for _, ref := range allowed {
		if ProfileID(ref.Profile) == id && strings.TrimSpace(ref.Model) == model {
			return true
		}
	}
	return false
}

// ResolveModelConfig picks one exact profile and model pair for a quality agent.
// There is no default: a missing half is InvalidArgument, and a profile that does
// not currently enable the model is NotFound. The caller decides what "no model"
// means.
func ResolveModelConfig(configs []ResolvedConfig, profile, model string) (*ResolvedConfig, error) {
	if strings.TrimSpace(profile) == "" || strings.TrimSpace(model) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("an LLM provider profile and a model are required"))
	}
	config := FindModelConfig(configs, profile, model)
	if config == nil {
		if FindConfig(configs, profile) == nil {
			return nil, connect.NewError(connect.CodeNotFound,
				errors.Errorf("LLM profile %q was not found or enables no model", profile))
		}
		return nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("LLM profile %q does not enable model %q", profile, model))
	}
	return config, nil
}

// ResolveAllowedModelConfig picks the pair Explain SQL runs on. An explicitly
// requested pair must be inside the workspace allowlist; with no request the first
// allowed config is the default. Each rejection carries its own status:
//
//   - InvalidArgument: the request names only half a pair, or the pair is outside
//     the workspace allowlist.
//   - NotFound: the pair is allowed but no enabled profile+model matches it.
//   - FailedPrecondition: no pair was requested and the allowlist admits none.
func ResolveAllowedModelConfig(configs []ResolvedConfig, allowed []ModelRef, profile, model string) (*ResolvedConfig, error) {
	profile = strings.TrimSpace(profile)
	model = strings.TrimSpace(model)
	if (profile == "") != (model == "") {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("an LLM provider profile and a model must be set together"))
	}
	if profile != "" {
		if !ModelAllowed(profile, model, allowed) {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				errors.Errorf("LLM provider model %q / %q is not allowed by the workspace settings", profile, model))
		}
		config := FindModelConfig(configs, profile, model)
		if config == nil {
			return nil, connect.NewError(connect.CodeNotFound,
				errors.Errorf("LLM provider profile %q does not enable model %q", profile, model))
		}
		return config, nil
	}
	allowedConfigs := AllowedModelConfigs(configs, allowed)
	if len(allowedConfigs) == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("no enabled LLM provider models are allowed"))
	}
	return &allowedConfigs[0], nil
}

// ResolveAnalysisConfig picks the pair a feature runs on when that feature's own
// settings name it: the exact pair when one is configured, otherwise the first
// enabled config. There is no workspace allowlist, so the data quality analyses
// are not restricted twice. Half a pair is never accepted: the model is not
// inferred from the profile.
func ResolveAnalysisConfig(configs []ResolvedConfig, profile, model string) (*ResolvedConfig, error) {
	if strings.TrimSpace(profile) == "" && strings.TrimSpace(model) == "" {
		if len(configs) == 0 {
			return nil, connect.NewError(connect.CodeFailedPrecondition,
				errors.New("no enabled LLM provider model is configured"))
		}
		return &configs[0], nil
	}
	return ResolveModelConfig(configs, profile, model)
}
