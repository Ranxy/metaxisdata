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
