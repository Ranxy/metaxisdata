package v1

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	qualityAgentPrefix = "qualityAgents/"
	// qualityAgentTokenDuration is how long one agent bearer token stays valid. An
	// agent is a long-running principal, not a browser session; rotation replaces
	// the token, and disabling the agent ends them all.
	qualityAgentTokenDuration = 90 * 24 * time.Hour
	maxQualityAgentNameBytes  = 200
	maxQualityAgentDescBytes  = 2000
)

// CreateQualityAgent creates an agent principal and returns its bearer token once.
// The token is an ordinary access token for the principal, so disabling the agent
// makes it unusable immediately.
func (s *DataQualityService) CreateQualityAgent(ctx context.Context, req *connect.Request[v1pb.CreateQualityAgentRequest]) (*connect.Response[v1pb.CreateQualityAgentResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	displayName := strings.TrimSpace(req.Msg.GetDisplayName())
	if displayName == "" || len(displayName) > maxQualityAgentNameBytes {
		return nil, connectErrorInvalid("an agent needs a display name (max %d bytes)", maxQualityAgentNameBytes)
	}
	description := strings.TrimSpace(req.Msg.GetDescription())
	if len(description) > maxQualityAgentDescBytes {
		return nil, connectErrorInvalid("an agent description is too long (max %d bytes)", maxQualityAgentDescBytes)
	}
	config, databases, err := parseQualityAgentConfig(&v1pb.QualityAgent{
		Owner:           req.Msg.GetOwner(),
		Databases:       req.Msg.GetDatabases(),
		Capabilities:    req.Msg.GetCapabilities(),
		ReportSchedule:  req.Msg.GetReportSchedule(),
		Adoption:        req.Msg.GetAdoption(),
		Budget:          req.Msg.GetBudget(),
		ProviderProfile: "",
	})
	if err != nil {
		return nil, err
	}
	if err := s.validateQualityAgentOwner(ctx, config.OwnerPrincipalID); err != nil {
		return nil, err
	}
	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate the agent id")
	}
	agent, err := s.store.CreateQualityAgent(ctx, resourceID, displayName, description,
		principalIDPointer(currentUserID(ctx)))
	if err != nil {
		return nil, err
	}
	config.AgentID = agent.ID
	config.State = store.QualityAgentStateDraft
	if req.Msg.GetStart() {
		config.State = store.QualityAgentStateActive
	}
	if _, err := s.store.CreateQualityAgentConfig(ctx, config); err != nil {
		// The identity already exists; deactivate it so a failed creation does not
		// leave a usable token behind. The row stays for the audit trail.
		_, _ = s.store.DisableQualityAgent(ctx, resourceID)
		return nil, err
	}
	if err := s.store.ReplaceQualityAgentDatabases(ctx, agent.ID, databases); err != nil {
		_, _ = s.store.DisableQualityAgent(ctx, resourceID)
		return nil, err
	}
	token, err := s.mintQualityAgentToken(agent)
	if err != nil {
		return nil, err
	}
	view, err := s.qualityAgentView(ctx, workspaceID, agent)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1pb.CreateQualityAgentResponse{
		Agent: view,
		Token: token,
	}), nil
}

// ListQualityAgents lists the workspace's agent principals. Tokens are never
// returned here: a token is shown once, when it is minted.
func (s *DataQualityService) ListQualityAgents(ctx context.Context, req *connect.Request[v1pb.ListQualityAgentsRequest]) (*connect.Response[v1pb.ListQualityAgentsResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:        req.Msg.GetPageToken(),
		limit:        int(req.Msg.GetPageSize()),
		maximum:      200,
		defaultLimit: 50,
	})
	if err != nil {
		return nil, err
	}
	agents, err := s.store.ListQualityAgents(ctx)
	if err != nil {
		return nil, err
	}
	admin, err := s.callerIsQualityAgentAdmin(ctx)
	if err != nil {
		return nil, err
	}
	// Non-administrators see only the agents they own. The ACL grants every member
	// the list permission so an owner can open their own agents; this filter is the
	// ownership check.
	visible := make([]*v1pb.QualityAgent, 0, len(agents))
	for _, agent := range agents {
		config, err := s.store.GetQualityAgentConfig(ctx, agent.ID)
		if err != nil {
			return nil, err
		}
		if !admin {
			if config == nil || config.OwnerPrincipalID == nil || int32(currentUserID(ctx)) != *config.OwnerPrincipalID {
				continue
			}
		}
		view, err := s.qualityAgentView(ctx, workspaceID, agent)
		if err != nil {
			return nil, err
		}
		visible = append(visible, view)
	}
	items, nextPage, err := paginateInMemory(visible, offset)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1pb.ListQualityAgentsResponse{
		QualityAgents: items,
		NextPageToken: nextPage,
	}), nil
}

// DisableQualityAgent disables one agent and deactivates its principal.
func (s *DataQualityService) DisableQualityAgent(ctx context.Context, req *connect.Request[v1pb.DisableQualityAgentRequest]) (*connect.Response[v1pb.QualityAgent], error) {
	workspaceID, resourceID, err := s.qualityAgentResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	agent, err := s.store.DisableQualityAgent(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("quality agent %q not found", resourceID))
	}
	if config, err := s.store.GetQualityAgentConfig(ctx, agent.ID); err != nil {
		return nil, err
	} else if config != nil {
		if _, err := s.store.UpdateQualityAgentState(ctx, agent.ID, store.QualityAgentStateDisabled); err != nil {
			return nil, err
		}
	}
	view, err := s.qualityAgentView(ctx, workspaceID, agent)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(view), nil
}

// RotateQualityAgentToken mints a new token and invalidates every token minted
// before this call.
func (s *DataQualityService) RotateQualityAgentToken(ctx context.Context, req *connect.Request[v1pb.RotateQualityAgentTokenRequest]) (*connect.Response[v1pb.RotateQualityAgentTokenResponse], error) {
	workspaceID, resourceID, err := s.qualityAgentResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	agent, err := s.fetchQualityAgent(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if !agent.Enabled {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the agent is disabled; create a new agent instead"))
	}
	if err := s.store.InvalidateQualityAgentTokens(ctx, agent.PrincipalID); err != nil {
		return nil, err
	}
	token, err := s.mintQualityAgentToken(agent)
	if err != nil {
		return nil, err
	}
	view, err := s.qualityAgentView(ctx, workspaceID, agent)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1pb.RotateQualityAgentTokenResponse{
		Agent: view,
		Token: token,
	}), nil
}

// qualityAgentActor returns the agent the request is made by, or nil for a human.
// It only identifies the actor: whether the action is allowed is the policy's call.
func (s *DataQualityService) qualityAgentActor(ctx context.Context) (*store.QualityAgentMessage, error) {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil || user.Type != storepb.PrincipalType_SERVICE_ACCOUNT {
		return nil, nil
	}
	agent, err := s.store.GetQualityAgentByPrincipalID(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return agent, nil
}

// authorizeAgentAction identifies the caller and, for an agent, runs the WS2 action
// policy before the handler reads or writes anything. A human caller returns nil and
// is never filtered by the agent switches. The returned agent is the write's
// attribution.
func (s *DataQualityService) authorizeAgentAction(ctx context.Context, action dataquality.ActionID, itemCount int, criticality dataquality.AssetCriticality) (*store.QualityAgentMessage, error) {
	agent, err := s.qualityAgentActor(ctx)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		// Human caller, or a service account the data-quality domain does not manage;
		// the ACL decides whether the RPC may run at all.
		return nil, nil
	}
	settings, err := dataquality.LoadSettings(ctx, s.store)
	if err != nil {
		return nil, errLoadQualitySettings(err)
	}
	decision := dataquality.DecideAction(dataquality.PolicyInput{
		Action:          action,
		Actor:           dataquality.ActorAgent,
		AutonomyEnabled: settings.AgentAutonomyEnabled,
		AgentEnabled:    agent.Enabled,
		ItemCount:       itemCount,
		Criticality:     criticality,
	})
	// RequiresApproval is not an execution: no handler may run an action a person has
	// not yet approved, so both a hard denial and an approval-pending verdict refuse.
	if !decision.Allowed || decision.RequiresApproval {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New(decision.Reason))
	}
	return agent, nil
}

// StageQualityPlan writes the target's plan declaration without touching the
// settings or the published version. It is the write an agent principal owns; the
// kill switch and the agent's state are checked before anything is read for a write.
func (s *DataQualityService) StageQualityPlan(ctx context.Context, req *connect.Request[v1pb.StageQualityPlanRequest]) (*connect.Response[v1pb.QualityContract], error) {
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	agent, err := s.authorizeAgentAction(ctx, dataquality.ActionProposePlanChange, 0, 0)
	if err != nil {
		return nil, err
	}
	targetGUID := strings.TrimSpace(req.Msg.GetTargetGuid())
	if targetGUID == "" {
		return nil, connectErrorInvalid("target_guid is required")
	}
	rules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &targetGUID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's quality rules")
	}
	metaType := storepb.MetaType(req.Msg.GetMetaType())
	if metaType == storepb.MetaType_UNSPECIFIED && len(rules) > 0 {
		metaType = rules[0].TargetType
	}
	if metaType == storepb.MetaType_UNSPECIFIED {
		return nil, connectErrorInvalid("meta_type is required when the target has no rules")
	}
	existing, err := s.store.GetQualityContract(ctx, targetGUID, metaType)
	if err != nil {
		return nil, err
	}
	// Re-evaluate with the target's real criticality now that it is known: the
	// amplifier may only fire after the read, still before the write.
	if agent != nil {
		_, authErr := s.authorizeAgentAction(ctx, dataquality.ActionProposePlanChange, 0,
			qualityChangeRequestCriticality(existing, rules))
		if authErr != nil {
			return nil, authErr
		}
	}
	if existing == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the target has no contract; an administrator must create one before a declaration can be staged"))
	}
	draft := &v1pb.QualityPlan{Coverage: req.Msg.GetCoverage()}
	plan, err := s.planQualityContractDraft(ctx, targetGUID, metaType, existing, draft, req.Msg.GetBaseVersion())
	if err != nil {
		return nil, err
	}
	if err := s.writeQualityContractDraft(ctx, targetGUID, metaType, existing, plan, req.Msg.GetBaseVersion()); err != nil {
		return nil, err
	}
	contract, err := s.qualityContractView(ctx, targetGUID, metaType)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(contract), nil
}

// mintQualityAgentToken mints the bearer token an agent authenticates with. It is
// minted as the agent's principal with the user API audience, so the ordinary auth
// interceptor resolves it and the ACL checks the agent role's permissions.
func (s *DataQualityService) mintQualityAgentToken(agent *store.QualityAgentMessage) (string, error) {
	if s.secret == "" || s.profile == nil {
		return "", connect.NewError(connect.CodeFailedPrecondition,
			errors.New("agent tokens are unavailable in this process"))
	}
	token, err := auth.GenerateAgentAccessToken(common.FormatUserUID(agent.PrincipalID),
		agent.PrincipalID, s.profile.Mode, s.secret, qualityAgentTokenDuration)
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to mint the agent token"))
	}
	return token, nil
}

// qualityAgentResourceID resolves an agent name to its workspace and resource id.
func (s *DataQualityService) qualityAgentResourceID(ctx context.Context, name string) (string, string, error) {
	workspace, resourceID, err := parseQualityAgentName(name)
	if err != nil {
		return "", "", connectErrorInvalid("%v", err)
	}
	workspace, err = s.canonicalAgentWorkspace(ctx, workspace)
	if err != nil {
		return "", "", err
	}
	if err := s.validateResourceWorkspace(ctx, workspace); err != nil {
		return "", "", err
	}
	return workspace, resourceID, nil
}

// canonicalAgentWorkspace resolves the AIP wildcard workspace on the agent family
// of RPCs. Unlike a list parent, a single-resource name may still be addressed with
// - by an older deep link; the rest of data quality canonicalizes the same way.
func (s *DataQualityService) canonicalAgentWorkspace(ctx context.Context, workspace string) (string, error) {
	if workspace != "-" {
		return workspace, nil
	}
	workspaceID, err := s.store.GetWorkspaceID(ctx)
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve the workspace"))
	}
	return workspaceID, nil
}

func parseQualityAgentName(name string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualityAgentPrefix)
	if err != nil {
		return "", "", errors.Wrap(err, "invalid quality agent name")
	}
	return tokens[0], tokens[1], nil
}

func (s *DataQualityService) fetchQualityAgent(ctx context.Context, resourceID string) (*store.QualityAgentMessage, error) {
	agent, err := s.store.GetQualityAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the quality agent")
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("quality agent %q not found", resourceID))
	}
	return agent, nil
}
