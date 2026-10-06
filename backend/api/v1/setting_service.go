package v1

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/config"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// SettingService implements the setting service.
type SettingService struct {
	v1connect.UnimplementedSettingServiceHandler
	store   *store.Store
	profile *config.Profile
}

// NewSettingService creates a new SettingService.
func NewSettingService(store *store.Store, profile *config.Profile) *SettingService {
	return &SettingService{store: store, profile: profile}
}

// GetWorkspaceProfileSetting gets the workspace profile setting.
func (s *SettingService) GetWorkspaceProfileSetting(ctx context.Context, _ *connect.Request[v1pb.GetWorkspaceProfileSettingRequest]) (*connect.Response[v1pb.WorkspaceProfileSetting], error) {
	setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get workspace profile setting"))
	}
	return connect.NewResponse(convertToWorkspaceProfileSetting(setting)), nil
}

// UpdateWorkspaceProfileSetting updates the workspace profile setting.
func (s *SettingService) UpdateWorkspaceProfileSetting(ctx context.Context, request *connect.Request[v1pb.UpdateWorkspaceProfileSettingRequest]) (*connect.Response[v1pb.WorkspaceProfileSetting], error) {
	if request.Msg.Setting == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("setting must be set"))
	}
	if request.Msg.UpdateMask == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("update_mask must be set"))
	}

	setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get workspace profile setting"))
	}
	for _, path := range request.Msg.UpdateMask.Paths {
		switch path {
		case "external_url":
			setting.ExternalUrl = strings.TrimRight(request.Msg.Setting.ExternalUrl, "/")
		case "disallow_signup":
			setting.DisallowSignup = request.Msg.Setting.DisallowSignup
		case "disallow_password_signin":
			setting.DisallowPasswordSignin = request.Msg.Setting.DisallowPasswordSignin
		case "openlineage_retention_days":
			if request.Msg.Setting.OpenlineageRetentionDays < 0 {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("openlineage_retention_days must not be negative"))
			}
			setting.OpenlineageRetentionDays = request.Msg.Setting.OpenlineageRetentionDays
		case "domains":
			domains, err := normalizeIdentityDomains(request.Msg.Setting.Domains)
			if err != nil {
				return nil, err
			}
			setting.Domains = domains
		case "enforce_identity_domain":
			setting.EnforceIdentityDomain = request.Msg.Setting.EnforceIdentityDomain
		case "allowed_llm_provider_profiles":
			profiles, err := normalizeAllowedLLMProfiles(request.Msg.Setting.AllowedLlmProviderProfiles)
			if err != nil {
				return nil, err
			}
			setting.AllowedLlmProviderProfiles = profiles
		case "mcp_enabled":
			setting.McpEnabled = request.Msg.Setting.McpEnabled
		case "sso_email_identity_idps":
			idps, err := normalizeSSOEmailIdentityIdps(request.Msg.Setting.SsoEmailIdentityIdps)
			if err != nil {
				return nil, err
			}
			setting.SsoEmailIdentityIdps = idps
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported update_mask %q", path))
		}
	}

	// The MCP endpoint derives its issuer and resource identifiers from
	// external_url, so it cannot be enabled without one. Checking after the loop
	// makes the answer independent of the mask's field order.
	if setting.McpEnabled && setting.ExternalUrl == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("external_url must be configured before mcp_enabled can be turned on"))
	}

	payload, err := protojson.Marshal(setting)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to marshal workspace profile setting"))
	}
	if _, err := s.store.UpsertSetting(ctx, &store.SetSettingMessage{
		Name:  storepb.SettingName_WORKSPACE_PROFILE,
		Value: string(payload),
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update workspace profile setting"))
	}
	return connect.NewResponse(convertToWorkspaceProfileSetting(setting)), nil
}

func convertToWorkspaceProfileSetting(setting *storepb.WorkspaceProfileSetting) *v1pb.WorkspaceProfileSetting {
	return &v1pb.WorkspaceProfileSetting{
		ExternalUrl:                setting.GetExternalUrl(),
		DisallowSignup:             setting.GetDisallowSignup(),
		DisallowPasswordSignin:     setting.GetDisallowPasswordSignin(),
		OpenlineageRetentionDays:   setting.GetOpenlineageRetentionDays(),
		Domains:                    setting.GetDomains(),
		EnforceIdentityDomain:      setting.GetEnforceIdentityDomain(),
		AllowedLlmProviderProfiles: setting.GetAllowedLlmProviderProfiles(),
		McpEnabled:                 setting.GetMcpEnabled(),
		SsoEmailIdentityIdps:       formatIdentityProviderNames(setting.GetSsoEmailIdentityIdps()),
	}
}

// formatIdentityProviderNames turns the stored resource ids into the idps/{idp}
// names the API works in.
func formatIdentityProviderNames(idps []string) []string {
	names := make([]string, 0, len(idps))
	for _, idp := range idps {
		names = append(names, common.FormatIdentityProviderUID(idp))
	}
	return names
}

// normalizeSSOEmailIdentityIdps normalizes the identity providers allowed to
// stand in for a stable subject with their address claim. Entries are resource ids
// as idps/{idp} names them; a bare id is accepted too, because there is no API
// that hands them out and an operator reads them from the database.
func normalizeSSOEmailIdentityIdps(idps []string) ([]string, error) {
	result := make([]string, 0, len(idps))
	for _, idp := range idps {
		trimmed := strings.TrimSpace(idp)
		if trimmed == "" {
			continue
		}
		if id, err := common.GetIdentityProviderID(trimmed); err == nil {
			trimmed = id
		}
		if strings.ContainsAny(trimmed, "/:@ ") {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid identity provider %q", idp))
		}
		if !slices.Contains(result, trimmed) {
			result = append(result, trimmed)
		}
	}
	slices.Sort(result)
	return result, nil
}

// normalizeIdentityDomains trims and lowercases the entries, drops empties and
// rejects anything that is not a bare domain: validateEmailWithDomains matches
// "@"+entry, so an entry that already carries an "@" would never match.
func normalizeIdentityDomains(domains []string) ([]string, error) {
	result := make([]string, 0, len(domains))
	for _, domain := range domains {
		trimmed := strings.ToLower(strings.TrimSpace(domain))
		if trimmed == "" {
			continue
		}
		if strings.ContainsAny(trimmed, "@/: ") {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid domain %q", domain))
		}
		result = append(result, trimmed)
	}
	return result, nil
}

// normalizeAllowedLLMProfiles trims and de-duplicates the profile resource
// names, and rejects anything that is not one.
func normalizeAllowedLLMProfiles(profiles []string) ([]string, error) {
	const prefix = "llm-provider-profiles/"
	result := make([]string, 0, len(profiles))
	seen := make(map[string]struct{}, len(profiles))
	for _, profile := range profiles {
		trimmed := strings.TrimSpace(profile)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, prefix) || len(trimmed) == len(prefix) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid LLM provider profile %q", profile))
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result, nil
}

// GetDebugConfig gets the runtime debug config.
func (s *SettingService) GetDebugConfig(_ context.Context, _ *connect.Request[v1pb.GetDebugConfigRequest]) (*connect.Response[v1pb.GetDebugConfigResponse], error) {
	return connect.NewResponse(&v1pb.GetDebugConfigResponse{Enabled: s.profile.RuntimeDebug.Load()}), nil
}

// UpdateDebugConfig updates the runtime debug config.
//
// RuntimeDebug is the single switch the whole process reads: it selects the
// slog level, gates the debug interceptor's chatty logs and /debug/pprof, and
// decides whether panic handlers hand stack traces back to the caller. Keep it
// and the log level in lockstep so a toggle takes effect without a restart.
func (s *SettingService) UpdateDebugConfig(_ context.Context, request *connect.Request[v1pb.UpdateDebugConfigRequest]) (*connect.Response[v1pb.UpdateDebugConfigResponse], error) {
	enabled := request.Msg.GetEnabled()
	s.profile.RuntimeDebug.Store(enabled)
	if enabled {
		log.LogLevel.Set(slog.LevelDebug)
	} else {
		log.LogLevel.Set(slog.LevelInfo)
	}
	return connect.NewResponse(&v1pb.UpdateDebugConfigResponse{Enabled: enabled}), nil
}
