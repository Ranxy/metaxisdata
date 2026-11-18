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
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentcapability"
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
	current, err := s.store.GetQualityAgentConfig(ctx, agent.ID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the agent is not a built-in agent; it has no runtime configuration"))
	}
	databases, err := s.store.ListQualityAgentDatabases(ctx, agent.ID)
	if err != nil {
		return nil, err
	}
	next, nextDatabases, err := applyQualityAgentUpdate(req.Msg.GetQualityAgent(), current, databases, req.Msg.GetUpdateMask().GetPaths())
	if err != nil {
		return nil, err
	}
	if _, err := s.store.UpdateQualityAgentConfig(ctx, next); err != nil {
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
		name := agent.DisplayName
		if updateName {
			name = req.Msg.GetQualityAgent().GetDisplayName()
		}
		description := agent.Description
		if updateDescription {
			description = req.Msg.GetQualityAgent().GetDescription()
		}
		updated, err := s.store.UpdateQualityAgentIdentity(ctx, resourceID, name, description)
		if err != nil {
			return nil, err
		}
		if updated != nil {
			agent = updated
		}
	}
	view, err := s.qualityAgentView(ctx, workspaceID, agent)
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
	config, err := s.store.GetQualityAgentConfig(ctx, agent.ID)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the agent is not a built-in agent; it has no runtime configuration"))
	}
	if !agent.Enabled || config.State == store.QualityAgentStateDisabled {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the agent is disabled and cannot be started"))
	}
	next := store.QualityAgentStatePaused
	if start {
		next = store.QualityAgentStateActive
	}
	if config.State == next {
		view, err := s.qualityAgentView(ctx, workspaceID, agent)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(view), nil
	}
	if _, err := s.store.UpdateQualityAgentState(ctx, agent.ID, next); err != nil {
		return nil, err
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
	config, err := s.store.GetQualityAgentConfig(ctx, agent.ID)
	if err != nil || config == nil || config.OwnerPrincipalID == nil {
		return nil
	}
	if int32(currentUserID(ctx)) != *config.OwnerPrincipalID {
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
	config, err := s.store.GetQualityAgentConfig(ctx, agent.ID)
	if err != nil {
		return nil, err
	}
	var databases []string
	if config != nil {
		databases, err = s.store.ListQualityAgentDatabases(ctx, agent.ID)
		if err != nil {
			return nil, err
		}
	}
	view := convertQualityAgentToV1(workspaceID, agent, config, databases)
	if config != nil {
		conversation, err := s.store.EnsureQualityAgentConversation(ctx, workspaceID, agent.ID)
		if err != nil {
			return nil, err
		}
		view.Conversation = qualityAgentConversationName(workspaceID, conversation)
	}
	return view, nil
}

// parseQualityAgentConfig validates and converts the runtime fields of a create
// or update request. The identity fields are handled by the caller.
func parseQualityAgentConfig(agent *v1pb.QualityAgent) (*store.QualityAgentConfigMessage, []string, error) {
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
	config := &store.QualityAgentConfigMessage{
		OwnerPrincipalID:            principalIDPointer(owner),
		Capabilities:                capabilities,
		ProviderProfile:             strings.TrimSpace(agent.GetProviderProfile()),
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
		if budget.GetTokensPerHour() < 0 || budget.GetWakesPerHour() < 0 ||
			budget.GetMaxConcurrentSessions() < 0 || budget.GetMaxNewSessionsPerHour() < 0 ||
			budget.GetMaxActiveSessions() < 0 || budget.GetConversationMessagesPerHour() < 0 {
			return nil, nil, connectErrorInvalid("an agent budget cannot be negative")
		}
		config.TokensPerHour = budget.GetTokensPerHour()
		config.WakesPerHour = budget.GetWakesPerHour()
		config.MaxConcurrentSessions = budget.GetMaxConcurrentSessions()
		config.MaxNewSessionsPerHour = budget.GetMaxNewSessionsPerHour()
		config.MaxActiveSessions = budget.GetMaxActiveSessions()
		config.ConversationMessagesPerHour = budget.GetConversationMessagesPerHour()
	}
	return config, databases, nil
}

// applyQualityAgentUpdate merges the masked fields into the current config, then
// runs the same validation as create. The mask names a field of QualityAgent
// (snake_case proto field names).
func applyQualityAgentUpdate(patch *v1pb.QualityAgent, current *store.QualityAgentConfigMessage, databases []string, paths []string) (*store.QualityAgentConfigMessage, []string, error) {
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
			case "provider_profile":
				merged.ProviderProfile = patch.GetProviderProfile()
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
	return config, nextDatabases, nil
}

// qualityAgentConfigFields renders the editable fields of a stored config as the
// v1 shape parseQualityAgentConfig consumes.
func qualityAgentConfigFields(config *store.QualityAgentConfigMessage, databases []string) *v1pb.QualityAgent {
	if config == nil {
		return &v1pb.QualityAgent{}
	}
	return &v1pb.QualityAgent{
		Owner:           formatQualityAgentOwner(config.OwnerPrincipalID),
		Databases:       databases,
		Capabilities:    config.Capabilities,
		ProviderProfile: config.ProviderProfile,
		ReportSchedule: &v1pb.QualityAgentReportSchedule{
			Frequency: qualityAgentReportFrequencyToV1(config.ReportFrequency),
			Time:      config.ReportTime,
			Timezone:  config.ReportTimezone,
			DayOfWeek: v1pb.DayOfWeek(config.ReportWeekday),
		},
		Adoption: &v1pb.QualityAgentAdoptionSetting{
			Mode:                qualityAgentAdoptionModeToV1(config.AdoptionMode),
			RuleTypes:           qualityAgentRuleTypesToV1(config.AdoptionRuleTypes),
			ConfidenceThreshold: config.AdoptionConfidenceThreshold,
			MaxPerDay:           config.AdoptionMaxPerDay,
			AllowCritical:       config.AdoptionAllowCritical,
		},
		Budget: &v1pb.QualityAgentBudget{
			TokensPerHour:               config.TokensPerHour,
			WakesPerHour:                config.WakesPerHour,
			MaxConcurrentSessions:       config.MaxConcurrentSessions,
			MaxNewSessionsPerHour:       config.MaxNewSessionsPerHour,
			MaxActiveSessions:           config.MaxActiveSessions,
			ConversationMessagesPerHour: config.ConversationMessagesPerHour,
		},
	}
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

func validateQualityAgentReportSchedule(config *store.QualityAgentConfigMessage) error {
	switch config.ReportFrequency {
	case store.QualityAgentReportFrequencyUnspecified, store.QualityAgentReportFrequencyOff:
		return nil
	case store.QualityAgentReportFrequencyDaily, store.QualityAgentReportFrequencyWeekly:
	default:
		return connectErrorInvalid("unknown report frequency")
	}
	if _, err := time.Parse("15:04", config.ReportTime); err != nil {
		return connectErrorInvalid("report time must be HH:MM")
	}
	if config.ReportTimezone == "" {
		return connectErrorInvalid("report timezone is required while reporting is enabled")
	}
	if _, err := time.LoadLocation(config.ReportTimezone); err != nil {
		return connectErrorInvalid("invalid report timezone %q", config.ReportTimezone)
	}
	if config.ReportFrequency == store.QualityAgentReportFrequencyWeekly &&
		(config.ReportWeekday < 1 || config.ReportWeekday > 7) {
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

// convertQualityAgentToV1 renders one agent. The token is never part of this shape.
func convertQualityAgentToV1(workspaceID string, agent *store.QualityAgentMessage, config *store.QualityAgentConfigMessage, databases []string) *v1pb.QualityAgent {
	if agent == nil {
		return nil
	}
	view := &v1pb.QualityAgent{
		Name:        common.FormatWorkspace(workspaceID) + "/" + qualityAgentPrefix + agent.ResourceID,
		DisplayName: agent.DisplayName,
		Description: agent.Description,
		Enabled:     agent.Enabled,
		CreatedBy:   formatQualityContractPublisher(agent.CreatedBy),
		CreateTime:  timestamppb.New(agent.CreatedAt),
		UpdateTime:  timestamppb.New(agent.UpdatedAt),
		Databases:   databases,
	}
	if config == nil {
		return view
	}
	view.State = qualityAgentStateToV1(config.State)
	view.Owner = formatQualityAgentOwner(config.OwnerPrincipalID)
	view.Capabilities = config.Capabilities
	view.ProviderProfile = config.ProviderProfile
	view.ReportSchedule = &v1pb.QualityAgentReportSchedule{
		Frequency: qualityAgentReportFrequencyToV1(config.ReportFrequency),
		Time:      config.ReportTime,
		Timezone:  config.ReportTimezone,
		DayOfWeek: v1pb.DayOfWeek(config.ReportWeekday),
	}
	view.Adoption = &v1pb.QualityAgentAdoptionSetting{
		Mode:                qualityAgentAdoptionModeToV1(config.AdoptionMode),
		RuleTypes:           qualityAgentRuleTypesToV1(config.AdoptionRuleTypes),
		ConfidenceThreshold: config.AdoptionConfidenceThreshold,
		MaxPerDay:           config.AdoptionMaxPerDay,
		AllowCritical:       config.AdoptionAllowCritical,
	}
	view.Budget = &v1pb.QualityAgentBudget{
		TokensPerHour:               config.TokensPerHour,
		WakesPerHour:                config.WakesPerHour,
		MaxConcurrentSessions:       config.MaxConcurrentSessions,
		MaxNewSessionsPerHour:       config.MaxNewSessionsPerHour,
		MaxActiveSessions:           config.MaxActiveSessions,
		ConversationMessagesPerHour: config.ConversationMessagesPerHour,
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
