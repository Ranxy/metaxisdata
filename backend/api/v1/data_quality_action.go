package v1

import (
	"context"

	"connectrpc.com/connect"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// GetQualityActionPolicy reads the WS2 action catalog with the policy verdict a
// live agent would get for every action. It is read-only: the catalog is code, and
// the verdict reflects the workspace kill switch and the caller agent's state.
func (s *DataQualityService) GetQualityActionPolicy(ctx context.Context, req *connect.Request[v1pb.GetQualityActionPolicyRequest]) (*connect.Response[v1pb.QualityActionPolicy], error) {
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	settings, err := dataquality.LoadSettings(ctx, s.store)
	if err != nil {
		return nil, errLoadQualitySettings(err)
	}
	// A human reading the policy sees the verdict an enabled agent would get; an
	// agent sees its own state, so a disabled agent reads "autonomy off" rather than
	// a policy it would not actually be allowed to use.
	agentEnabled := true
	if agent, err := s.qualityAgentActor(ctx); err != nil {
		return nil, err
	} else if agent != nil {
		agentEnabled = agent.Enabled
	}
	response := &v1pb.QualityActionPolicy{AgentAutonomyEnabled: settings.AgentAutonomyEnabled}
	for _, card := range dataquality.ActionCatalog() {
		decision := dataquality.DecideAction(dataquality.PolicyInput{
			Action:          card.ID,
			Actor:           dataquality.ActorAgent,
			AutonomyEnabled: settings.AgentAutonomyEnabled,
			AgentEnabled:    agentEnabled,
		})
		response.Actions = append(response.Actions, &v1pb.QualityAction{
			Id:               string(card.ID),
			Title:            card.Title,
			AutonomyLevel:    dataquality.AutonomyLevelName(card.Level),
			Actor:            dataquality.ActorKindName(card.Actor),
			RedLine:          card.RedLine,
			Implemented:      card.Implemented,
			AgentReachable:   card.AgentReachable,
			AgentAllowed:     decision.Allowed,
			RequiresApproval: decision.RequiresApproval,
			DecisionCode:     qualityActionDecisionCode(decision.Code),
			Reason:           decision.Reason,
		})
	}
	return connect.NewResponse(response), nil
}

// qualityActionDecisionCode renders the policy verdict for the API.
func qualityActionDecisionCode(code dataquality.DecisionCode) string {
	switch code {
	case dataquality.DecisionAllowed:
		return "ALLOWED"
	case dataquality.DecisionDeniedAutonomyOff:
		return "DENIED_AUTONOMY_OFF"
	case dataquality.DecisionDeniedHumanOnly:
		return "DENIED_HUMAN_ONLY"
	case dataquality.DecisionDeniedRedLine:
		return "DENIED_RED_LINE"
	case dataquality.DecisionDeniedNotImplemented:
		return "DENIED_NOT_IMPLEMENTED"
	case dataquality.DecisionDeniedNotExposed:
		return "DENIED_NOT_EXPOSED"
	case dataquality.DecisionDeniedCritical:
		return "DENIED_CRITICAL"
	case dataquality.DecisionDeniedUnknownActor:
		return "DENIED_UNKNOWN_ACTOR"
	case dataquality.DecisionRequiresApproval:
		return "REQUIRES_APPROVAL"
	default:
		return "UNKNOWN"
	}
}
