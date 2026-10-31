package v1

import (
	"context"
	"log/slog"
	"net/mail"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/notification/outbound"
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
	if value := dataQuality.GetDailyColumnAnalysisLimit(); value < 0 || value > dataquality.MaxDailyColumnAnalysisLimit {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("daily_column_analysis_limit must be between 0 and %d", dataquality.MaxDailyColumnAnalysisLimit))
	}
	if value := dataQuality.GetDailyLlmAnalysisLimit(); value < 0 || value > dataquality.MaxDailyLLMAnalysisLimit {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("daily_llm_analysis_limit must be between 0 and %d", dataquality.MaxDailyLLMAnalysisLimit))
	}
	if value := dataQuality.GetLlmConcurrencyLimit(); value < 0 || value > dataquality.MaxLLMConcurrencyLimit {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("llm_concurrency_limit must be between 0 and %d", dataquality.MaxLLMConcurrencyLimit))
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
		DailyLlmAnalysisLimit:    dataQuality.GetDailyLlmAnalysisLimit(),
		LlmConcurrencyLimit:      dataQuality.GetLlmConcurrencyLimit(),
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
		DailyLlmAnalysisLimit:    dataQuality.GetDailyLlmAnalysisLimit(),
		LlmConcurrencyLimit:      dataQuality.GetLlmConcurrencyLimit(),
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
		DailyLlmAnalysisLimit:    int32(resolved.DailyLLMAnalysisLimit),
		LlmConcurrencyLimit:      int32(resolved.LLMConcurrencyLimit),
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

// GetEmailSetting returns the workspace SMTP setting. An unset setting is
// returned empty rather than as a 404, so the settings page renders one form
// whether or not email delivery was ever configured.
func (s *SettingService) GetEmailSetting(ctx context.Context, _ *connect.Request[v1pb.GetEmailSettingRequest]) (*connect.Response[v1pb.EmailSetting], error) {
	setting, err := s.store.GetEmailSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get the email setting"))
	}
	return connect.NewResponse(convertToV1EmailSetting(setting)), nil
}

// UpdateEmailSetting rewrites the masked fields of the workspace SMTP setting.
// The password is write-only: an empty one keeps the stored password, and a read
// reports only whether one is stored. Clearing every field turns email delivery
// off.
func (s *SettingService) UpdateEmailSetting(ctx context.Context, request *connect.Request[v1pb.UpdateEmailSettingRequest]) (*connect.Response[v1pb.EmailSetting], error) {
	if request.Msg.Setting == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("setting must be set"))
	}
	if request.Msg.UpdateMask == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("update_mask must be set"))
	}
	existing, err := s.store.GetEmailSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get the email setting"))
	}
	setting, password, err := mergeEmailSettingUpdate(existing, request.Msg.Setting, request.Msg.UpdateMask.Paths)
	if err != nil {
		return nil, err
	}
	// Clearing the From address and the host is the explicit way to turn email
	// delivery off; the remaining SMTP fields are then irrelevant and are dropped
	// rather than stored as a half-configured server.
	if strings.TrimSpace(setting.GetFrom()) == "" && strings.TrimSpace(setting.GetSmtp().GetHost()) == "" {
		if err := s.store.DeleteEmailSetting(ctx); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to clear the email setting"))
		}
		return connect.NewResponse(&v1pb.EmailSetting{}), nil
	}
	if err := s.store.SetEmailSetting(ctx, setting, password); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update the email setting"))
	}
	stored, err := s.store.GetEmailSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get the email setting"))
	}
	return connect.NewResponse(convertToV1EmailSetting(stored)), nil
}

// SendTestEmail sends one message to the caller's own address through the
// configured SMTP server. It changes nothing, so it is a diagnostic probe the
// settings page can repeat while an administrator types the server in.
func (s *SettingService) SendTestEmail(ctx context.Context, _ *connect.Request[v1pb.SendTestEmailRequest]) (*connect.Response[v1pb.SendTestEmailResponse], error) {
	setting, err := s.store.GetEmailSettingForDelivery(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the email setting"))
	}
	if setting == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("email delivery is not configured"))
	}
	caller, ok := GetUserFromContext(ctx)
	if !ok || caller == nil || caller.Email == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("the caller has no email address"))
	}
	receiver, err := outbound.NewEmail(setting)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	title := "Metaxisdata test email"
	text := "This is a test email from Settings > Notifications. No data quality check failed."
	if normalizeLanguage(currentUserLanguage(ctx)) == "zh-CN" {
		title = "Metaxisdata 测试邮件"
		text = "这是来自「设置 → 通知」的测试邮件，没有数据质量检查失败。"
	}
	if _, err := receiver.Post(ctx, outbound.Message{
		Title:      title,
		Text:       text,
		Recipients: []string{caller.Email},
		OccurredAt: time.Now(),
	}); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&v1pb.SendTestEmailResponse{}), nil
}

// mergeEmailSettingUpdate applies the update mask to the stored setting and
// returns the merged value plus the password the store should seal. A nil
// password keeps the stored one; an empty mask is a full update. An empty
// password is never a request to clear a login password, because the request
// cannot tell "unchanged" from "cleared" — disabling authentication is how a
// password stops being required.
func mergeEmailSettingUpdate(existing *store.EmailSettingMessage, update *v1pb.EmailSetting, paths []string) (*storepb.EmailSetting, *string, error) {
	merged := &storepb.EmailSetting{Smtp: &storepb.EmailSMTPConfig{}}
	hasPassword := false
	if existing != nil && existing.Setting != nil {
		cloned, ok := proto.Clone(existing.Setting).(*storepb.EmailSetting)
		if !ok {
			return nil, nil, connect.NewError(connect.CodeInternal, errors.New("failed to clone the email setting"))
		}
		merged = cloned
		if merged.Smtp == nil {
			merged.Smtp = &storepb.EmailSMTPConfig{}
		}
		hasPassword = existing.HasPassword
	}
	var password *string
	if len(paths) == 0 {
		paths = []string{"from", "from_name", "smtp"}
	}
	for _, path := range paths {
		switch path {
		case "from":
			merged.From = strings.TrimSpace(update.GetFrom())
		case "from_name", "fromName":
			merged.FromName = strings.TrimSpace(update.GetFromName())
		case "smtp":
			config, value := emailSMTPConfigFromV1(update.GetSmtp())
			merged.Smtp = config
			if value != nil {
				password = value
				hasPassword = *value != ""
			}
		case "smtp.host":
			merged.Smtp.Host = strings.TrimSpace(update.GetSmtp().GetHost())
		case "smtp.port":
			merged.Smtp.Port = update.GetSmtp().GetPort()
		case "smtp.encryption":
			merged.Smtp.Encryption = storepb.EmailSMTPEncryption(update.GetSmtp().GetEncryption())
		case "smtp.authentication":
			merged.Smtp.Authentication = storepb.EmailSMTPAuthentication(update.GetSmtp().GetAuthentication())
		case "smtp.username":
			merged.Smtp.Username = strings.TrimSpace(update.GetSmtp().GetUsername())
		case "smtp.password":
			if value := update.GetSmtp().GetPassword(); value != "" {
				password = &value
				hasPassword = true
			}
		default:
			return nil, nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported update_mask %q", path))
		}
	}
	if err := validateEmailSetting(merged, hasPassword); err != nil {
		return nil, nil, err
	}
	return merged, password, nil
}

// emailSMTPConfigFromV1 converts the API's SMTP config, returning the password
// separately: the store owns sealing it, so the stored shape never carries it in
// the clear.
func emailSMTPConfigFromV1(config *v1pb.SMTPConfig) (*storepb.EmailSMTPConfig, *string) {
	if config == nil {
		return &storepb.EmailSMTPConfig{}, nil
	}
	converted := &storepb.EmailSMTPConfig{
		Host:           strings.TrimSpace(config.GetHost()),
		Port:           config.GetPort(),
		Encryption:     storepb.EmailSMTPEncryption(config.GetEncryption()),
		Authentication: storepb.EmailSMTPAuthentication(config.GetAuthentication()),
		Username:       strings.TrimSpace(config.GetUsername()),
	}
	var password *string
	if value := config.GetPassword(); value != "" {
		password = &value
	}
	return converted, password
}

// validateEmailSetting checks a complete configuration. Clearing the From
// address and the host is how an administrator turns email delivery off.
func validateEmailSetting(setting *storepb.EmailSetting, hasPassword bool) error {
	from := strings.TrimSpace(setting.GetFrom())
	host := strings.TrimSpace(setting.GetSmtp().GetHost())
	if from == "" && host == "" {
		return nil
	}
	if from == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("from is required"))
	}
	if parsed, err := mail.ParseAddress(from); err != nil || parsed.Address != from {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("from must be a valid email address"))
	}
	if strings.ContainsAny(setting.GetFromName(), "\r\n") {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("from_name must not contain line breaks"))
	}
	if len([]rune(setting.GetFromName())) > 100 {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("from_name is longer than 100 characters"))
	}
	if host == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("smtp.host is required"))
	}
	if len(host) > 255 || strings.ContainsAny(host, " \t\r\n/:") {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("smtp.host is not a valid host name"))
	}
	if port := setting.GetSmtp().GetPort(); port < 1 || port > 65535 {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("smtp.port must be between 1 and 65535"))
	}
	switch setting.GetSmtp().GetEncryption() {
	case storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_NONE,
		storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_STARTTLS,
		storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_SSL_TLS:
	case storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_UNSPECIFIED:
		return connect.NewError(connect.CodeInvalidArgument, errors.New("smtp.encryption is required"))
	default:
		return connect.NewError(connect.CodeInvalidArgument, errors.New("smtp.encryption is not supported"))
	}
	switch authentication := setting.GetSmtp().GetAuthentication(); authentication {
	case storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_NONE:
	case storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_PLAIN,
		storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_LOGIN,
		storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_CRAM_MD5:
		if setting.GetSmtp().GetUsername() == "" {
			return connect.NewError(connect.CodeInvalidArgument, errors.New("smtp.username is required for this authentication mechanism"))
		}
		if !hasPassword {
			return connect.NewError(connect.CodeInvalidArgument, errors.New("smtp.password is required for this authentication mechanism"))
		}
	case storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_UNSPECIFIED:
		return connect.NewError(connect.CodeInvalidArgument, errors.New("smtp.authentication is required"))
	default:
		return connect.NewError(connect.CodeInvalidArgument, errors.New("smtp.authentication is not supported"))
	}
	// A login over an unencrypted connection hands the password to anyone on the
	// path, so authentication requires transport encryption.
	if setting.GetSmtp().GetEncryption() == storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_NONE &&
		setting.GetSmtp().GetAuthentication() != storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_NONE {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("smtp.encryption must be STARTTLS or SSL/TLS when authentication is enabled"))
	}
	return nil
}

// convertToV1EmailSetting renders the stored setting. The password is never
// copied; a read reports only whether one is stored.
func convertToV1EmailSetting(message *store.EmailSettingMessage) *v1pb.EmailSetting {
	result := &v1pb.EmailSetting{}
	if message == nil || message.Setting == nil {
		return result
	}
	smtpConfig := message.Setting.GetSmtp()
	result.From = message.Setting.GetFrom()
	result.FromName = message.Setting.GetFromName()
	result.PasswordSet = message.HasPassword
	result.Smtp = &v1pb.SMTPConfig{
		Host:           smtpConfig.GetHost(),
		Port:           smtpConfig.GetPort(),
		Encryption:     v1pb.SMTPEncryption(smtpConfig.GetEncryption()),
		Authentication: v1pb.SMTPAuthentication(smtpConfig.GetAuthentication()),
		Username:       smtpConfig.GetUsername(),
	}
	return result
}
