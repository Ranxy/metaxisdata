package v1

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
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
	if err := applyWorkspaceProfileUpdate(setting, request.Msg.Setting, request.Msg.UpdateMask.Paths); err != nil {
		return nil, err
	}

	// The MCP endpoint derives its issuer and resource identifiers from
	// external_url, so it cannot be enabled without one. Checking after the
	// masked fields are merged makes the answer independent of the mask's field
	// order.
	if setting.McpEnabled && setting.ExternalUrl == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("external_url must be configured before mcp_enabled can be turned on"))
	}

	payload, err := protojson.Marshal(setting)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to marshal workspace profile setting"))
	}
	_, err = s.store.UpsertSetting(ctx, &store.SetSettingMessage{
		Name:  storepb.SettingName_WORKSPACE_PROFILE,
		Value: string(payload),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update workspace profile setting"))
	}
	return connect.NewResponse(convertToWorkspaceProfileSetting(setting)), nil
}

// applyWorkspaceProfileUpdate merges the fields named by paths from update into
// setting. Validation failures are returned as Connect errors so the RPC can pass
// them through unchanged.
//
// Checks that span several fields run after the loop, which makes the result
// independent of the mask's field order: the data quality provider is validated
// against the allowed list as it stands once every masked field was applied. (The
// MCP check stays in the RPC, next to the other cross-setting invariant about
// external_url.)
func applyWorkspaceProfileUpdate(setting *storepb.WorkspaceProfileSetting, update *v1pb.WorkspaceProfileSetting, paths []string) error {
	var (
		dataQuality        *storepb.DataQualitySetting
		dataQualityUpdated bool
	)
	for _, path := range paths {
		switch path {
		case "external_url":
			setting.ExternalUrl = strings.TrimRight(update.GetExternalUrl(), "/")
		case "disallow_signup":
			setting.DisallowSignup = update.GetDisallowSignup()
		case "disallow_password_signin":
			setting.DisallowPasswordSignin = update.GetDisallowPasswordSignin()
		case "openlineage_retention_days":
			if update.GetOpenlineageRetentionDays() < 0 {
				return connect.NewError(connect.CodeInvalidArgument, errors.New("openlineage_retention_days must not be negative"))
			}
			setting.OpenlineageRetentionDays = update.GetOpenlineageRetentionDays()
		case "domains":
			domains, err := normalizeIdentityDomains(update.GetDomains())
			if err != nil {
				return err
			}
			setting.Domains = domains
		case "enforce_identity_domain":
			setting.EnforceIdentityDomain = update.GetEnforceIdentityDomain()
		case "allowed_llm_provider_profiles":
			profiles, err := normalizeAllowedLLMProfiles(update.GetAllowedLlmProviderProfiles())
			if err != nil {
				return err
			}
			setting.AllowedLlmProviderProfiles = profiles
		case "mcp_enabled":
			setting.McpEnabled = update.GetMcpEnabled()
		case "sso_email_identity_idps":
			idps, err := normalizeSSOEmailIdentityIdps(update.GetSsoEmailIdentityIdps())
			if err != nil {
				return err
			}
			setting.SsoEmailIdentityIdps = idps
		case "data_quality":
			normalized, err := normalizeDataQualitySetting(update.GetDataQuality())
			if err != nil {
				return err
			}
			dataQuality = normalized
			dataQualityUpdated = true
		default:
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported update_mask %q", path))
		}
	}

	if dataQualityUpdated {
		// A cleared data_quality message (nil) resets the workspace to the
		// defaults; a named provider still has to be allowed.
		if err := validateDataQualityProviderProfile(dataQuality.GetLlmProviderProfile(), setting.AllowedLlmProviderProfiles); err != nil {
			return err
		}
		setting.DataQuality = dataQuality
	}
	return nil
}

// normalizeDataQualitySetting validates the data quality settings of an update
// and converts them to the stored form. A nil message means the masked field was
// cleared, which resets the workspace to the defaults.
func normalizeDataQualitySetting(dataQuality *v1pb.DataQualitySetting) (*storepb.DataQualitySetting, error) {
	if dataQuality == nil {
		return nil, nil
	}

	if limit := dataQuality.GetFailureSampleLimit(); limit < 0 || limit > dataquality.MaxFailureSampleLimit {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("failure_sample_limit must be between 0 and %d", dataquality.MaxFailureSampleLimit))
	}
	interval := dataQuality.GetDefaultScheduleInterval()
	if interval != nil {
		if err := interval.CheckValid(); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("default_schedule_interval is not a valid duration: %v", err))
		}
	}
	intervalDuration := interval.AsDuration()
	if intervalDuration != 0 && (intervalDuration < dataquality.MinScheduleInterval || intervalDuration > dataquality.MaxScheduleInterval) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("default_schedule_interval must be between %s and %s, or zero for the default",
			dataquality.MinScheduleInterval, dataquality.MaxScheduleInterval))
	}
	if value := dataQuality.GetDailyExecutionLimit(); value < 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("daily_execution_limit must not be negative"))
	}
	if value := dataQuality.GetDailyColumnAnalysisLimit(); value < 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("daily_column_analysis_limit must not be negative"))
	}
	if value := dataQuality.GetMaxColumnsPerAnalysis(); value < 0 || value > dataquality.MaxColumnsPerAnalysisLimit {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("max_columns_per_analysis must be between 0 and %d", dataquality.MaxColumnsPerAnalysisLimit))
	}
	provider := strings.TrimSpace(dataQuality.GetLlmProviderProfile())
	if provider != "" && !validLLMProviderProfileName(provider) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid LLM provider profile %q", dataQuality.GetLlmProviderProfile()))
	}

	result := &storepb.DataQualitySetting{
		EnableLlmAnalysis:        dataQuality.GetEnableLlmAnalysis(),
		LlmProviderProfile:       provider,
		StoreFailureSamples:      dataQuality.GetStoreFailureSamples(),
		FailureSampleLimit:       dataQuality.GetFailureSampleLimit(),
		DailyExecutionLimit:      dataQuality.GetDailyExecutionLimit(),
		DailyColumnAnalysisLimit: dataQuality.GetDailyColumnAnalysisLimit(),
		MaxColumnsPerAnalysis:    dataQuality.GetMaxColumnsPerAnalysis(),
		SendSampleValuesToLlm:    dataQuality.GetSendSampleValuesToLlm(),
	}
	if interval != nil {
		result.DefaultScheduleInterval = durationpb.New(intervalDuration)
	}
	return result, nil
}

// validateDataQualityProviderProfile checks the provider a data quality update
// names. Empty means "the first allowed profile"; otherwise it must be a profile
// resource name and, when the workspace restricts its allowed list, a member of
// it.
func validateDataQualityProviderProfile(profile string, allowed []string) error {
	if profile == "" {
		return nil
	}
	if !validLLMProviderProfileName(profile) {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid LLM provider profile %q", profile))
	}
	if len(allowed) > 0 && !slices.Contains(allowed, profile) {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("LLM provider profile %q is not in allowed_llm_provider_profiles", profile))
	}
	return nil
}

// validLLMProviderProfileName reports whether profile is a profile resource name.
func validLLMProviderProfileName(profile string) bool {
	const prefix = "llm-provider-profiles/"
	return strings.HasPrefix(profile, prefix) && len(profile) > len(prefix)
}

// convertDataQualitySetting copies the stored data quality settings into the API
// form. Nil stays nil: the workspace has not saved them, and every reader
// resolves the defaults.
func convertDataQualitySetting(dataQuality *storepb.DataQualitySetting) *v1pb.DataQualitySetting {
	if dataQuality == nil {
		return nil
	}
	return &v1pb.DataQualitySetting{
		EnableLlmAnalysis:        dataQuality.GetEnableLlmAnalysis(),
		LlmProviderProfile:       dataQuality.GetLlmProviderProfile(),
		StoreFailureSamples:      dataQuality.GetStoreFailureSamples(),
		FailureSampleLimit:       dataQuality.GetFailureSampleLimit(),
		DefaultScheduleInterval:  dataQuality.GetDefaultScheduleInterval(),
		DailyExecutionLimit:      dataQuality.GetDailyExecutionLimit(),
		DailyColumnAnalysisLimit: dataQuality.GetDailyColumnAnalysisLimit(),
		MaxColumnsPerAnalysis:    dataQuality.GetMaxColumnsPerAnalysis(),
		SendSampleValuesToLlm:    dataQuality.GetSendSampleValuesToLlm(),
	}
}

// resolveDataQualitySetting returns the effective data quality settings as an
// explicit message: unset zeros are filled with the server defaults and the
// booleans are resolved from the message's presence. The stored message keeps the
// "zero means default" contract; the API returns the resolved form so the
// settings page and the LLM switch read one source instead of re-deriving the
// defaults.
func resolveDataQualitySetting(setting *storepb.WorkspaceProfileSetting) *storepb.DataQualitySetting {
	resolved := dataquality.SettingsFromProfile(setting)
	return &storepb.DataQualitySetting{
		EnableLlmAnalysis:        resolved.EnableLLMAnalysis,
		LlmProviderProfile:       resolved.LLMProviderProfile,
		StoreFailureSamples:      resolved.StoreFailureSamples,
		FailureSampleLimit:       int32(resolved.FailureSampleLimit),
		DefaultScheduleInterval:  durationpb.New(resolved.DefaultScheduleInterval),
		DailyExecutionLimit:      int32(resolved.DailyExecutionLimit),
		DailyColumnAnalysisLimit: int32(resolved.DailyColumnAnalysisLimit),
		MaxColumnsPerAnalysis:    int32(resolved.MaxColumnsPerAnalysis),
		SendSampleValuesToLlm:    resolved.SendSampleValuesToLLM,
	}
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
		DataQuality:                convertDataQualitySetting(resolveDataQualitySetting(setting)),
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
// as idps/{idp} names them; a bare id is accepted too, because that is the form a
// client may hold from before the provider was renamed into a resource name.
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
	result := make([]string, 0, len(profiles))
	seen := make(map[string]struct{}, len(profiles))
	for _, profile := range profiles {
		trimmed := strings.TrimSpace(profile)
		if trimmed == "" {
			continue
		}
		if !validLLMProviderProfileName(trimmed) {
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
