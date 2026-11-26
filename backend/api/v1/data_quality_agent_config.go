package v1

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentcapability"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentlang"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	maxQualityAgentDatabases    = 500
	maxQualityAgentCapabilities = 16
)

// GetQualityAgent reads one agent with its runtime configuration. An owner may
// read the agents they own; an administrator may read every agent.
func (s *DataQualityService) GetQualityAgent(ctx context.Context, req *connect.Request[v1pb.GetQualityAgentRequest]) (*connect.Response[v1pb.QualityAgent], error) {
	workspaceID, resourceID, err := s.qualityAgentResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	agent := s.fetchQualityAgentForCaller(ctx, resourceID)
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("quality agent %q not found", resourceID))
	}
	view, err := s.qualityAgentView(ctx, workspaceID, agent)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(view), nil
}

// UpdateQualityAgent replaces the masked configuration fields of one agent.
func (s *DataQualityService) UpdateQualityAgent(ctx context.Context, req *connect.Request[v1pb.UpdateQualityAgentRequest]) (*connect.Response[v1pb.QualityAgent], error) {
	workspaceID, resourceID, err := s.qualityAgentResourceID(ctx, req.Msg.GetQualityAgent().GetName())
	if err != nil {
		return nil, err
	}
	agent, err := s.fetchQualityAgent(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	databases, err := s.store.ListQualityAgentDatabases(ctx, agent.ID)
	if err != nil {
		return nil, err
	}
	next, nextDatabases, err := applyQualityAgentUpdate(req.Msg.GetQualityAgent(), agent, databases, req.Msg.GetUpdateMask().GetPaths())
	if err != nil {
		return nil, err
	}
	if err := s.validateQualityAgentOwner(ctx, next.OwnerPrincipalID); err != nil {
		return nil, err
	}
	// A stored pair that no longer resolves is tolerated by the runtime, so an edit
	// that leaves it alone (a rename, a budget change) must not be refused; only a
	// changed pair is checked against the registry.
	if next.ProviderProfile != agent.ProviderProfile || next.ProviderModel != agent.ProviderModel {
		if err := s.validateQualityAgentProvider(ctx, next.ProviderProfile, next.ProviderModel); err != nil {
			return nil, err
		}
	}
	updated, err := s.store.UpdateQualityAgentConfig(ctx, next)
	if err != nil {
		return nil, err
	}
	if err := s.store.ReplaceQualityAgentDatabases(ctx, agent.ID, nextDatabases); err != nil {
		return nil, err
	}
	paths := req.Msg.GetUpdateMask().GetPaths()
	updateName, updateDescription := len(paths) == 0, len(paths) == 0
	for _, path := range paths {
		switch path {
		case "display_name":
			updateName = true
		case "description":
			updateDescription = true
		default:
		}
	}
	if updateName || updateDescription {
		name := updated.DisplayName
		if updateName {
			name = req.Msg.GetQualityAgent().GetDisplayName()
		}
		description := updated.Description
		if updateDescription {
			description = req.Msg.GetQualityAgent().GetDescription()
		}
		identityUpdated, err := s.store.UpdateQualityAgentIdentity(ctx, resourceID, name, description)
		if err != nil {
			return nil, err
		}
		if identityUpdated != nil {
			updated = identityUpdated
		}
	}
	view, err := s.qualityAgentView(ctx, workspaceID, updated)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(view), nil
}

// StartQualityAgent activates a DRAFT or PAUSED agent.
func (s *DataQualityService) StartQualityAgent(ctx context.Context, req *connect.Request[v1pb.StartQualityAgentRequest]) (*connect.Response[v1pb.QualityAgent], error) {
	return s.transitionQualityAgentState(ctx, req.Msg.GetName(), true)
}

// PauseQualityAgent pauses an ACTIVE agent without disabling its identity.
func (s *DataQualityService) PauseQualityAgent(ctx context.Context, req *connect.Request[v1pb.PauseQualityAgentRequest]) (*connect.Response[v1pb.QualityAgent], error) {
	return s.transitionQualityAgentState(ctx, req.Msg.GetName(), false)
}

func (s *DataQualityService) transitionQualityAgentState(ctx context.Context, name string, start bool) (*connect.Response[v1pb.QualityAgent], error) {
	workspaceID, resourceID, err := s.qualityAgentResourceID(ctx, name)
	if err != nil {
		return nil, err
	}
	agent, err := s.fetchQualityAgent(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if !s.qualityAgentOwnerOrAdmin(ctx, agent) {
		return nil, connect.NewError(connect.CodePermissionDenied,
			errors.New("only the agent's owner or an administrator may start or pause it"))
	}
	if start {
		switch agent.State {
		case store.QualityAgentStateDraft, store.QualityAgentStatePaused:
			updated, err := s.store.UpdateQualityAgentState(ctx, agent.ID, store.QualityAgentStateActive)
			if err != nil {
				return nil, err
			}
			agent = updated
		case store.QualityAgentStateActive:
			// Idempotent: an already active agent stays active.
		case store.QualityAgentStateDisabled:
			return nil, connect.NewError(connect.CodeFailedPrecondition,
				errors.New("the agent is disabled and cannot be started"))
		default:
			return nil, connect.NewError(connect.CodeFailedPrecondition,
				errors.New("the agent state does not allow starting"))
		}
	} else {
		switch agent.State {
		case store.QualityAgentStateActive:
			updated, err := s.store.UpdateQualityAgentState(ctx, agent.ID, store.QualityAgentStatePaused)
			if err != nil {
				return nil, err
			}
			agent = updated
		case store.QualityAgentStatePaused:
			// Idempotent: an already paused agent stays paused.
		case store.QualityAgentStateDraft:
			return nil, connect.NewError(connect.CodeFailedPrecondition,
				errors.New("a draft agent is not running; nothing to pause"))
		case store.QualityAgentStateDisabled:
			return nil, connect.NewError(connect.CodeFailedPrecondition,
				errors.New("the agent is disabled and cannot be paused"))
		default:
			return nil, connect.NewError(connect.CodeFailedPrecondition,
				errors.New("the agent state does not allow pausing"))
		}
	}
	view, err := s.qualityAgentView(ctx, workspaceID, agent)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(view), nil
}

// fetchQualityAgentForCaller returns the agent only when the caller may read it:
// an administrator, or the configured owner.
func (s *DataQualityService) fetchQualityAgentForCaller(ctx context.Context, resourceID string) *store.QualityAgentMessage {
	agent, err := s.store.GetQualityAgentByResourceID(ctx, resourceID)
	if err != nil || agent == nil {
		return nil
	}
	admin, err := s.callerIsQualityAgentAdmin(ctx)
	if err != nil {
		return nil
	}
	if admin {
		return agent
	}
	if agent.OwnerPrincipalID == nil || int32(currentUserID(ctx)) != *agent.OwnerPrincipalID {
		return nil
	}
	return agent
}

// callerIsQualityAgentAdmin reports whether the caller may manage and read every
// agent.
func (s *DataQualityService) callerIsQualityAgentAdmin(ctx context.Context) (bool, error) {
	if s.iam == nil {
		return true, nil
	}
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return false, nil
	}
	return s.iam.CheckPermission(ctx, permission.DataQualityAgentsManage, user)
}

// qualityAgentView renders the identity plus configuration and scope.
func (s *DataQualityService) qualityAgentView(ctx context.Context, workspaceID string, agent *store.QualityAgentMessage) (*v1pb.QualityAgent, error) {
	databases, err := s.store.ListQualityAgentDatabases(ctx, agent.ID)
	if err != nil {
		return nil, err
	}
	view := convertQualityAgentToV1(workspaceID, agent, databases)
	// The configured guardrails are not the outcome. Surface what auto-adoption
	// actually did, so an owner can tell an empty queue from a broken write.
	status, err := s.store.GetQualityAgentAdoptionStatus(ctx, agent.ID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	view.AdoptionStatus = convertQualityAgentAdoptionStatusToV1(status)
	conversation, err := s.store.EnsureQualityAgentConversation(ctx, workspaceID, agent.ID)
	if err != nil {
		return nil, err
	}
	view.Conversation = qualityAgentConversationName(workspaceID, conversation)
	return view, nil
}

// parseQualityAgentConfig validates and converts the runtime fields of a create
// or update request. The identity fields are handled by the caller.
func parseQualityAgentConfig(agent *v1pb.QualityAgent) (*store.QualityAgentMessage, []string, error) {
	if agent == nil {
		return nil, nil, connectErrorInvalid("an agent is required")
	}
	owner, err := parseUserResourceID(agent.GetOwner())
	if err != nil {
		return nil, nil, connectErrorInvalid("owner must be a user resource name: %v", err)
	}
	databases, err := normalizeQualityAgentDatabases(agent.GetDatabases())
	if err != nil {
		return nil, nil, err
	}
	capabilities := normalizeQualityAgentCapabilities(agent.GetCapabilities())
	if err := agentcapability.Validate(capabilities); err != nil {
		return nil, nil, connectErrorInvalid("%v", err)
	}
	language := strings.TrimSpace(agent.GetLanguage())
	if !agentlang.Supported(language) {
		return nil, nil, connectErrorInvalid("language must be empty, en-US or zh-CN")
	}
	providerProfile, providerModel, err := normalizeQualityAgentProvider(agent.GetProviderProfile(), agent.GetProviderModel())
	if err != nil {
		return nil, nil, err
	}
	config := &store.QualityAgentMessage{
		OwnerPrincipalID: principalIDPointer(owner),
		Language:         language,
		ProviderProfile:  providerProfile,
		ProviderModel:    providerModel,
		Capabilities:     capabilities,
		// Adoption is a write, so an omitted setting means off; PAUSED is only
		// meaningful for an agent that actually holds the adopt capability.
		AdoptionMode:                store.QualityAgentAdoptionModeOff,
		AdoptionConfidenceThreshold: 0.7,
		AdoptionMaxPerDay:           10,
		ConversationMessagesPerHour: 60,
	}
	if schedule := agent.GetReportSchedule(); schedule != nil {
		frequency, err := qualityAgentReportFrequencyFromV1(schedule.GetFrequency())
		if err != nil {
			return nil, nil, err
		}
		config.ReportFrequency = frequency
		config.ReportTime = strings.TrimSpace(schedule.GetTime())
		config.ReportTimezone = strings.TrimSpace(schedule.GetTimezone())
		config.ReportWeekday = int16(schedule.GetDayOfWeek())
		if err := validateQualityAgentReportSchedule(config); err != nil {
			return nil, nil, err
		}
	} else if containsCapability(capabilities, agentcapability.Patrol) {
		// The design default is one daily report at 09:00; UTC is the fallback when
		// the caller did not name a timezone.
		config.ReportFrequency = store.QualityAgentReportFrequencyDaily
		config.ReportTime = "09:00"
		config.ReportTimezone = "UTC"
	}
	if adoption := agent.GetAdoption(); adoption != nil {
		mode, err := qualityAgentAdoptionModeFromV1(adoption.GetMode())
		if err != nil {
			return nil, nil, err
		}
		if mode != store.QualityAgentAdoptionModeOff && !containsCapability(capabilities, agentcapability.Adopt) {
			return nil, nil, connectErrorInvalid("automatic adoption requires the %q capability", agentcapability.Adopt)
		}
		ruleTypes := qualityAgentRuleTypesFromV1(adoption.GetRuleTypes())
		threshold := adoption.GetConfidenceThreshold()
		if threshold == 0 {
			threshold = 0.7
		}
		if threshold < 0 || threshold > 1 {
			return nil, nil, connectErrorInvalid("the adoption confidence threshold must be between 0 and 1")
		}
		maxPerDay := adoption.GetMaxPerDay()
		if maxPerDay == 0 {
			maxPerDay = 10
		}
		if maxPerDay < 0 || maxPerDay > 100 {
			return nil, nil, connectErrorInvalid("the daily adoption limit must be between 0 and 100")
		}
		config.AdoptionMode = mode
		config.AdoptionRuleTypes = ruleTypes
		config.AdoptionConfidenceThreshold = threshold
		config.AdoptionMaxPerDay = maxPerDay
		config.AdoptionAllowCritical = adoption.GetAllowCritical()
	}
	if budget := agent.GetBudget(); budget != nil {
		if err := validateQualityAgentBudget(budget); err != nil {
			return nil, nil, err
		}
		config.TokensPerHour = budget.GetTokensPerHour()
		config.WakesPerHour = budget.GetWakesPerHour()
		config.MaxConcurrentSessions = budget.GetMaxConcurrentSessions()
		config.MaxNewSessionsPerHour = budget.GetMaxNewSessionsPerHour()
		config.MaxActiveSessions = budget.GetMaxActiveSessions()
		config.ConversationMessagesPerHour = budget.GetConversationMessagesPerHour()
		config.RepairMaxPerDay = budget.GetRepairMaxPerDay()
		config.RunMaxPerRulePerDay = budget.GetRunMaxPerRulePerDay()
	}
	return config, databases, nil
}

// validateQualityAgentBudget keeps a per-agent budget inside the same bounds the
// former workspace settings enforced; zero keeps meaning "use the server default".
func validateQualityAgentBudget(budget *v1pb.QualityAgentBudget) error {
	if budget.GetTokensPerHour() < 0 || budget.GetTokensPerHour() > dataquality.MaxAgentRunnerTokensPerHour {
		return connectErrorInvalid("the hourly token budget must be between 0 and %d", dataquality.MaxAgentRunnerTokensPerHour)
	}
	if budget.GetWakesPerHour() < 0 || budget.GetWakesPerHour() > dataquality.MaxAgentRunnerWakesPerHour {
		return connectErrorInvalid("the hourly wake budget must be between 0 and %d", dataquality.MaxAgentRunnerWakesPerHour)
	}
	if budget.GetMaxConcurrentSessions() < 0 || budget.GetMaxConcurrentSessions() > dataquality.MaxAgentRunnerConcurrentSessions {
		return connectErrorInvalid("the concurrent session budget must be between 0 and %d", dataquality.MaxAgentRunnerConcurrentSessions)
	}
	if budget.GetMaxNewSessionsPerHour() < 0 || budget.GetMaxNewSessionsPerHour() > dataquality.MaxAgentRunnerNewSessionsPerHour {
		return connectErrorInvalid("the new sessions per hour budget must be between 0 and %d", dataquality.MaxAgentRunnerNewSessionsPerHour)
	}
	if budget.GetMaxActiveSessions() < 0 || budget.GetMaxActiveSessions() > dataquality.MaxAgentRunnerActiveSessions {
		return connectErrorInvalid("the active sessions budget must be between 0 and %d", dataquality.MaxAgentRunnerActiveSessions)
	}
	if budget.GetConversationMessagesPerHour() < 0 || budget.GetConversationMessagesPerHour() > dataquality.MaxAgentRunnerConversationMessagesPerHour {
		return connectErrorInvalid("the conversation messages per hour budget must be between 0 and %d", dataquality.MaxAgentRunnerConversationMessagesPerHour)
	}
	if budget.GetRepairMaxPerDay() < 0 || budget.GetRepairMaxPerDay() > dataquality.MaxAgentRepairsPerDay {
		return connectErrorInvalid("the daily repair budget must be between 0 and %d", dataquality.MaxAgentRepairsPerDay)
	}
	if budget.GetRunMaxPerRulePerDay() < 0 || budget.GetRunMaxPerRulePerDay() > dataquality.MaxAgentRunsPerRulePerDay {
		return connectErrorInvalid("the daily runs per check budget must be between 0 and %d", dataquality.MaxAgentRunsPerRulePerDay)
	}
	return nil
}

// applyQualityAgentUpdate merges the masked fields into the current agent, then
// runs the same validation as create. The mask names a field of QualityAgent
// (snake_case proto field names).
func applyQualityAgentUpdate(patch *v1pb.QualityAgent, current *store.QualityAgentMessage, databases []string, paths []string) (*store.QualityAgentMessage, []string, error) {
	if patch == nil {
		return nil, nil, connectErrorInvalid("an agent is required")
	}
	merged := qualityAgentConfigFields(current, databases)
	if len(paths) == 0 {
		merged = patch
	} else {
		for _, path := range paths {
			switch path {
			case "display_name", "description":
				// Identity fields are handled by UpdateQualityAgent.
			case "owner":
				merged.Owner = patch.GetOwner()
			case "databases":
				merged.Databases = patch.GetDatabases()
			case "capabilities":
				merged.Capabilities = patch.GetCapabilities()
			case "language":
				merged.Language = patch.GetLanguage()
			case "provider_profile":
				merged.ProviderProfile = patch.GetProviderProfile()
			case "provider_model":
				merged.ProviderModel = patch.GetProviderModel()
			case "report_schedule":
				merged.ReportSchedule = patch.GetReportSchedule()
			case "adoption":
				merged.Adoption = patch.GetAdoption()
			case "budget":
				merged.Budget = patch.GetBudget()
			default:
				return nil, nil, connectErrorInvalid("unsupported update mask path %q", path)
			}
		}
	}
	config, nextDatabases, err := parseQualityAgentConfig(merged)
	if err != nil {
		return nil, nil, err
	}
	config.ID = current.ID
	return config, nextDatabases, nil
}

// qualityAgentOwnerOrAdmin is the caller check shared by the owner-manageable
// operations (start/pause). ACL alone cannot express "owner of this row".
func (s *DataQualityService) qualityAgentOwnerOrAdmin(ctx context.Context, agent *store.QualityAgentMessage) bool {
	if agent == nil {
		return false
	}
	if agent.OwnerPrincipalID != nil && int32(currentUserID(ctx)) == *agent.OwnerPrincipalID {
		return true
	}
	admin, err := s.callerIsQualityAgentAdmin(ctx)
	return err == nil && admin
}

// validateQualityAgentOwner requires the owner to be a live human member. Parsing
// only proves users/{id}; a service account or a deleted member cannot own an agent
// whose owner-only RPCs would then be reachable by a machine credential.
func (s *DataQualityService) validateQualityAgentOwner(ctx context.Context, owner *int32) error {
	if owner == nil {
		return connectErrorInvalid("the agent needs an owner")
	}
	user, err := s.store.GetUserByID(ctx, int(*owner))
	if err != nil {
		return err
	}
	if user == nil || user.MemberDeleted || user.Type != storepb.PrincipalType_END_USER {
		return connectErrorInvalid("the agent owner must be an active workspace member")
	}
	return nil
}

// qualityAgentConfigFields renders the editable fields of a stored agent as the
// v1 shape parseQualityAgentConfig consumes.
func qualityAgentConfigFields(agent *store.QualityAgentMessage, databases []string) *v1pb.QualityAgent {
	if agent == nil {
		return &v1pb.QualityAgent{}
	}
	adoptionMode := agent.AdoptionMode
	if !containsCapability(agent.Capabilities, agentcapability.Adopt) {
		// An agent without the adopt capability cannot auto-adopt, so presenting a
		// stored guardrail as an active mode is misleading and would make an unrelated
		// masked update fail the adoption prerequisite.
		adoptionMode = store.QualityAgentAdoptionModeOff
	}
	return &v1pb.QualityAgent{
		Owner:           formatQualityAgentOwner(agent.OwnerPrincipalID),
		Language:        agent.Language,
		ProviderProfile: agent.ProviderProfile,
		ProviderModel:   agent.ProviderModel,
		Databases:       databases,
		Capabilities:    agent.Capabilities,
		ReportSchedule: &v1pb.QualityAgentReportSchedule{
			Frequency: qualityAgentReportFrequencyToV1(agent.ReportFrequency),
			Time:      agent.ReportTime,
			Timezone:  agent.ReportTimezone,
			DayOfWeek: v1pb.DayOfWeek(agent.ReportWeekday),
		},
		Adoption: &v1pb.QualityAgentAdoptionSetting{
			Mode:                qualityAgentAdoptionModeToV1(adoptionMode),
			RuleTypes:           qualityAgentRuleTypesToV1(agent.AdoptionRuleTypes),
			ConfidenceThreshold: agent.AdoptionConfidenceThreshold,
			MaxPerDay:           agent.AdoptionMaxPerDay,
			AllowCritical:       agent.AdoptionAllowCritical,
		},
		Budget: &v1pb.QualityAgentBudget{
			TokensPerHour:               agent.TokensPerHour,
			WakesPerHour:                agent.WakesPerHour,
			MaxConcurrentSessions:       agent.MaxConcurrentSessions,
			MaxNewSessionsPerHour:       agent.MaxNewSessionsPerHour,
			MaxActiveSessions:           agent.MaxActiveSessions,
			ConversationMessagesPerHour: agent.ConversationMessagesPerHour,
			RepairMaxPerDay:             agent.RepairMaxPerDay,
			RunMaxPerRulePerDay:         agent.RunMaxPerRulePerDay,
		},
	}
}

// normalizeQualityAgentProvider validates the shape of the agent's model selection
// and stores it in canonical form. The pair is required: the model is never
// inferred from the profile, because one profile can enable several models at
// different prices. Whether the profile currently enables the model is a
// service-level check against the registry. A bare profile id is accepted for the
// same reason `llm.FindModelConfig` accepts one (older references may hold it); the
// stored value is always the resource name.
func normalizeQualityAgentProvider(profile, model string) (string, string, error) {
	profile = strings.TrimSpace(profile)
	model = strings.TrimSpace(model)
	if profile == "" && model == "" {
		return "", "", connectErrorInvalid("an agent needs an LLM provider profile and a model")
	}
	if profile == "" || model == "" {
		return "", "", connectErrorInvalid("provider_profile and provider_model must be set together")
	}
	normalized, err := llm.NormalizeProfileName(profile)
	if err != nil {
		return "", "", connectErrorInvalid("%v", err)
	}
	return normalized, model, nil
}

// validateQualityAgentProvider refuses a selection the registry cannot currently
// run, so a typo or a disabled model does not leave a configured agent without a
// model at its first wake.
func (s *DataQualityService) validateQualityAgentProvider(ctx context.Context, profile, model string) error {
	if s.suggester == nil {
		return connect.NewError(connect.CodeFailedPrecondition,
			errors.New("no LLM provider registry is configured"))
	}
	return s.suggester.ValidateAgentProvider(ctx, profile, model)
}

func normalizeQualityAgentDatabases(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, connectErrorInvalid("an agent needs at least one database in scope")
	}
	if len(values) > maxQualityAgentDatabases {
		return nil, connectErrorInvalid("an agent may name at most %d databases", maxQualityAgentDatabases)
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, _, err := common.GetInstanceDatabaseID(value); err != nil {
			return nil, connectErrorInvalid("database %q must be a database resource name", value)
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil, connectErrorInvalid("an agent needs at least one database in scope")
	}
	return out, nil
}

func normalizeQualityAgentCapabilities(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	if len(out) > maxQualityAgentCapabilities {
		out = out[:maxQualityAgentCapabilities]
	}
	return out
}

func containsCapability(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validateQualityAgentReportSchedule(agent *store.QualityAgentMessage) error {
	switch agent.ReportFrequency {
	case store.QualityAgentReportFrequencyUnspecified, store.QualityAgentReportFrequencyOff:
		return nil
	case store.QualityAgentReportFrequencyDaily, store.QualityAgentReportFrequencyWeekly:
	default:
		return connectErrorInvalid("unknown report frequency")
	}
	if _, err := time.Parse("15:04", agent.ReportTime); err != nil {
		return connectErrorInvalid("report time must be HH:MM")
	}
	if agent.ReportTimezone == "" {
		return connectErrorInvalid("report timezone is required while reporting is enabled")
	}
	if _, err := time.LoadLocation(agent.ReportTimezone); err != nil {
		return connectErrorInvalid("invalid report timezone %q", agent.ReportTimezone)
	}
	if agent.ReportFrequency == store.QualityAgentReportFrequencyWeekly &&
		(agent.ReportWeekday < 1 || agent.ReportWeekday > 7) {
		return connectErrorInvalid("a weekly report needs a weekday")
	}
	return nil
}

func qualityAgentReportFrequencyFromV1(value v1pb.QualityAgentReportFrequency) (store.QualityAgentReportFrequency, error) {
	switch value {
	case v1pb.QualityAgentReportFrequency_QUALITY_AGENT_REPORT_FREQUENCY_UNSPECIFIED:
		return store.QualityAgentReportFrequencyUnspecified, nil
	case v1pb.QualityAgentReportFrequency_QUALITY_AGENT_REPORT_FREQUENCY_OFF:
		return store.QualityAgentReportFrequencyOff, nil
	case v1pb.QualityAgentReportFrequency_QUALITY_AGENT_REPORT_FREQUENCY_DAILY:
		return store.QualityAgentReportFrequencyDaily, nil
	case v1pb.QualityAgentReportFrequency_QUALITY_AGENT_REPORT_FREQUENCY_WEEKLY:
		return store.QualityAgentReportFrequencyWeekly, nil
	default:
		return 0, connectErrorInvalid("unknown report frequency")
	}
}

func qualityAgentAdoptionModeFromV1(value v1pb.QualityAgentAdoptionMode) (store.QualityAgentAdoptionMode, error) {
	switch value {
	case v1pb.QualityAgentAdoptionMode_QUALITY_AGENT_ADOPTION_MODE_UNSPECIFIED,
		v1pb.QualityAgentAdoptionMode_QUALITY_AGENT_ADOPTION_MODE_OFF:
		return store.QualityAgentAdoptionModeOff, nil
	case v1pb.QualityAgentAdoptionMode_QUALITY_AGENT_ADOPTION_MODE_PAUSED:
		return store.QualityAgentAdoptionModePaused, nil
	case v1pb.QualityAgentAdoptionMode_QUALITY_AGENT_ADOPTION_MODE_ACTIVE:
		return store.QualityAgentAdoptionModeActive, nil
	default:
		return 0, connectErrorInvalid("unknown adoption mode")
	}
}

func qualityAgentRuleTypesFromV1(values []v1pb.QualityRuleType) []int16 {
	out := make([]int16, 0, len(values))
	for _, value := range values {
		if value == v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED {
			continue
		}
		out = append(out, int16(value))
	}
	return out
}

func formatQualityAgentOwner(owner *int32) string {
	if owner == nil || *owner <= 0 {
		return ""
	}
	return common.FormatUserUID(int(*owner))
}

// convertQualityAgentToV1 renders one agent. State is the only lifecycle truth.
func convertQualityAgentToV1(workspaceID string, agent *store.QualityAgentMessage, databases []string) *v1pb.QualityAgent {
	if agent == nil {
		return nil
	}
	// An agent without the adopt capability cannot auto-adopt; reading a stored
	// guardrail as an armed mode would misdescribe what it can do.
	adoptionMode := agent.AdoptionMode
	if !containsCapability(agent.Capabilities, agentcapability.Adopt) {
		adoptionMode = store.QualityAgentAdoptionModeOff
	}
	return &v1pb.QualityAgent{
		Name:            common.FormatWorkspace(workspaceID) + "/" + qualityAgentPrefix + agent.ResourceID,
		DisplayName:     agent.DisplayName,
		Description:     agent.Description,
		Language:        agent.Language,
		ProviderProfile: agent.ProviderProfile,
		ProviderModel:   agent.ProviderModel,
		CreatedBy:       formatQualityContractPublisher(agent.CreatedBy),
		CreateTime:      timestamppb.New(agent.CreatedAt),
		UpdateTime:      timestamppb.New(agent.UpdatedAt),
		State:           qualityAgentStateToV1(agent.State),
		Owner:           formatQualityAgentOwner(agent.OwnerPrincipalID),
		Databases:       databases,
		Capabilities:    agent.Capabilities,
		ReportSchedule: &v1pb.QualityAgentReportSchedule{
			Frequency: qualityAgentReportFrequencyToV1(agent.ReportFrequency),
			Time:      agent.ReportTime,
			Timezone:  agent.ReportTimezone,
			DayOfWeek: v1pb.DayOfWeek(agent.ReportWeekday),
		},
		Adoption: &v1pb.QualityAgentAdoptionSetting{
			Mode:                qualityAgentAdoptionModeToV1(adoptionMode),
			RuleTypes:           qualityAgentRuleTypesToV1(agent.AdoptionRuleTypes),
			ConfidenceThreshold: agent.AdoptionConfidenceThreshold,
			MaxPerDay:           agent.AdoptionMaxPerDay,
			AllowCritical:       agent.AdoptionAllowCritical,
		},
		Budget: &v1pb.QualityAgentBudget{
			TokensPerHour:               agent.TokensPerHour,
			WakesPerHour:                agent.WakesPerHour,
			MaxConcurrentSessions:       agent.MaxConcurrentSessions,
			MaxNewSessionsPerHour:       agent.MaxNewSessionsPerHour,
			MaxActiveSessions:           agent.MaxActiveSessions,
			ConversationMessagesPerHour: agent.ConversationMessagesPerHour,
			RepairMaxPerDay:             agent.RepairMaxPerDay,
			RunMaxPerRulePerDay:         agent.RunMaxPerRulePerDay,
		},
	}
}

// convertQualityAgentAdoptionStatusToV1 renders the read-only adoption outcome.
func convertQualityAgentAdoptionStatusToV1(status *store.QualityAgentAdoptionStatusMessage) *v1pb.QualityAgentAdoptionStatus {
	if status == nil {
		return nil
	}
	view := &v1pb.QualityAgentAdoptionStatus{
		AdoptedLast_24H:   status.AdoptedLast24h,
		ApprovedLast_24H:  status.ApprovedLast24h,
		FailedLast_24H:    status.FailedLast24h,
		PendingDraftCount: status.PendingDraftCount,
		LastFailureReason: status.LastFailureReason,
	}
	if status.LastFailureTime != nil {
		view.LastFailureTime = timestamppb.New(*status.LastFailureTime)
	}
	return view
}

func qualityAgentStateToV1(state store.QualityAgentState) v1pb.QualityAgentState {
	switch state {
	case store.QualityAgentStateDraft:
		return v1pb.QualityAgentState_QUALITY_AGENT_STATE_DRAFT
	case store.QualityAgentStateActive:
		return v1pb.QualityAgentState_QUALITY_AGENT_STATE_ACTIVE
	case store.QualityAgentStatePaused:
		return v1pb.QualityAgentState_QUALITY_AGENT_STATE_PAUSED
	case store.QualityAgentStateDisabled:
		return v1pb.QualityAgentState_QUALITY_AGENT_STATE_DISABLED
	default:
		return v1pb.QualityAgentState_QUALITY_AGENT_STATE_UNSPECIFIED
	}
}

func qualityAgentReportFrequencyToV1(frequency store.QualityAgentReportFrequency) v1pb.QualityAgentReportFrequency {
	switch frequency {
	case store.QualityAgentReportFrequencyOff:
		return v1pb.QualityAgentReportFrequency_QUALITY_AGENT_REPORT_FREQUENCY_OFF
	case store.QualityAgentReportFrequencyDaily:
		return v1pb.QualityAgentReportFrequency_QUALITY_AGENT_REPORT_FREQUENCY_DAILY
	case store.QualityAgentReportFrequencyWeekly:
		return v1pb.QualityAgentReportFrequency_QUALITY_AGENT_REPORT_FREQUENCY_WEEKLY
	default:
		return v1pb.QualityAgentReportFrequency_QUALITY_AGENT_REPORT_FREQUENCY_UNSPECIFIED
	}
}

func qualityAgentAdoptionModeToV1(mode store.QualityAgentAdoptionMode) v1pb.QualityAgentAdoptionMode {
	switch mode {
	case store.QualityAgentAdoptionModeOff:
		return v1pb.QualityAgentAdoptionMode_QUALITY_AGENT_ADOPTION_MODE_OFF
	case store.QualityAgentAdoptionModePaused:
		return v1pb.QualityAgentAdoptionMode_QUALITY_AGENT_ADOPTION_MODE_PAUSED
	case store.QualityAgentAdoptionModeActive:
		return v1pb.QualityAgentAdoptionMode_QUALITY_AGENT_ADOPTION_MODE_ACTIVE
	default:
		return v1pb.QualityAgentAdoptionMode_QUALITY_AGENT_ADOPTION_MODE_UNSPECIFIED
	}
}

func qualityAgentRuleTypesToV1(values []int16) []v1pb.QualityRuleType {
	out := make([]v1pb.QualityRuleType, 0, len(values))
	for _, value := range values {
		out = append(out, v1pb.QualityRuleType(value))
	}
	return out
}
