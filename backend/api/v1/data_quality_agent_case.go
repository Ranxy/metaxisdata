package v1

import (
	"context"
	"strconv"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// qualityAgentCasePageLimit is the default page a case board reads.
	qualityAgentCasePageLimit = 50
	// maxQualityAgentCasePageLimit bounds one page: the board is a reading surface, not an export.
	maxQualityAgentCasePageLimit = 200
)

// ListQualityAgentCases lists one agent's work items. The owner and administrators may read: a
// case is the agent's own to-do, and the point of showing it is that a person can see what the
// agent could not settle, which a table only the agent reads cannot provide.
func (s *DataQualityService) ListQualityAgentCases(ctx context.Context, req *connect.Request[v1pb.ListQualityAgentCasesRequest]) (*connect.Response[v1pb.ListQualityAgentCasesResponse], error) {
	workspaceID, resourceID, err := s.qualityAgentResourceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	agent, err := s.qualityAgentConversationAccess(ctx, resourceID, false)
	if err != nil {
		return nil, err
	}
	states, err := qualityAgentCaseStates(req.Msg.GetStates())
	if err != nil {
		return nil, err
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token: req.Msg.GetPageToken(), limit: int(req.Msg.GetPageSize()),
		maximum: maxQualityAgentCasePageLimit, defaultLimit: qualityAgentCasePageLimit,
	})
	if err != nil {
		return nil, err
	}
	// One row beyond the page is the probe paginate cuts: it tells the caller whether there is a
	// next page without a second count query.
	cases, err := s.store.ListQualityAgentCases(ctx, &store.FindQualityAgentCaseMessage{
		AgentID: &agent.ID,
		States:  states,
		Limit:   offset.limit + 1,
		Offset:  offset.offset,
	})
	if err != nil {
		return nil, err
	}
	page, nextPageToken, err := paginate(cases, offset)
	if err != nil {
		return nil, err
	}
	response := &v1pb.ListQualityAgentCasesResponse{NextPageToken: nextPageToken}
	for _, openCase := range page {
		response.QualityAgentCases = append(response.QualityAgentCases,
			convertQualityAgentCaseToV1(workspaceID, agent.ResourceID, openCase))
	}
	return connect.NewResponse(response), nil
}

// qualityAgentCaseStates maps the requested states onto the stored lifecycle. An unknown value is
// refused rather than ignored: a board that silently lists more than it was asked for is worse than
// an error.
func qualityAgentCaseStates(states []v1pb.QualityAgentCaseState) ([]store.QualityAgentCaseState, error) {
	if len(states) == 0 {
		return nil, nil
	}
	converted := make([]store.QualityAgentCaseState, 0, len(states))
	for _, state := range states {
		switch state {
		case v1pb.QualityAgentCaseState_QUALITY_AGENT_CASE_STATE_OPEN:
			converted = append(converted, store.QualityAgentCaseStateOpen)
		case v1pb.QualityAgentCaseState_QUALITY_AGENT_CASE_STATE_DIAGNOSED:
			converted = append(converted, store.QualityAgentCaseStateDiagnosed)
		case v1pb.QualityAgentCaseState_QUALITY_AGENT_CASE_STATE_ACTED:
			converted = append(converted, store.QualityAgentCaseStateActed)
		case v1pb.QualityAgentCaseState_QUALITY_AGENT_CASE_STATE_AWAITING_VERIFICATION:
			converted = append(converted, store.QualityAgentCaseStateAwaiting)
		case v1pb.QualityAgentCaseState_QUALITY_AGENT_CASE_STATE_RESOLVED:
			converted = append(converted, store.QualityAgentCaseStateResolved)
		case v1pb.QualityAgentCaseState_QUALITY_AGENT_CASE_STATE_FROZEN:
			converted = append(converted, store.QualityAgentCaseStateFrozen)
		case v1pb.QualityAgentCaseState_QUALITY_AGENT_CASE_STATE_ESCALATED:
			converted = append(converted, store.QualityAgentCaseStateEscalated)
		case v1pb.QualityAgentCaseState_QUALITY_AGENT_CASE_STATE_UNSPECIFIED:
			return nil, connectErrorInvalid("an empty case state filter lists every state; omit the field instead")
		default:
			return nil, connectErrorInvalid("unknown case state %d", state)
		}
	}
	return converted, nil
}

// convertQualityAgentCaseToV1 renders one work item. The check is named by its resource id and the
// summary carries the prose, so the board needs no second read per row.
func convertQualityAgentCaseToV1(workspaceID, agentResourceID string, openCase *store.QualityAgentCaseMessage) *v1pb.QualityAgentCase {
	if openCase == nil {
		return nil
	}
	view := &v1pb.QualityAgentCase{
		Name:        common.FormatWorkspace(workspaceID) + "/" + qualityAgentCasePrefix + openCase.ResourceID,
		Agent:       common.FormatWorkspace(workspaceID) + "/qualityAgents/" + agentResourceID,
		Target:      convertQualityTargetIdentityToV1(openCase.TargetGUID, openCase.TargetType),
		Class:       openCase.FailureClass,
		State:       qualityAgentCaseStateToV1(openCase.State),
		Attempts:    openCase.Attempts,
		MaxAttempts: openCase.MaxAttempts,
		Field:       openCase.Field,
		Summary:     openCase.Summary,
		Resolution:  openCase.Resolution,
		CreateTime:  timestamppb.New(openCase.CreatedAt),
		UpdateTime:  timestamppb.New(openCase.UpdatedAt),
	}
	if openCase.RuleID != nil {
		view.Check = "qualityRules/" + resourceIDOf(*openCase.RuleID)
	}
	if openCase.ExecutionID != nil {
		view.Execution = "qualityExecutions/" + resourceIDOf(*openCase.ExecutionID)
	}
	if openCase.NextActionAt != nil {
		view.NextActionAt = timestamppb.New(*openCase.NextActionAt)
	}
	return view
}

// resourceIDOf renders a stored id as the resource name's last segment.
func resourceIDOf(id int64) string {
	return strconv.FormatInt(id, 10)
}

// qualityAgentCaseStateToV1 maps the stored lifecycle onto the wire.
func qualityAgentCaseStateToV1(state store.QualityAgentCaseState) v1pb.QualityAgentCaseState {
	switch state {
	case store.QualityAgentCaseStateOpen:
		return v1pb.QualityAgentCaseState_QUALITY_AGENT_CASE_STATE_OPEN
	case store.QualityAgentCaseStateDiagnosed:
		return v1pb.QualityAgentCaseState_QUALITY_AGENT_CASE_STATE_DIAGNOSED
	case store.QualityAgentCaseStateActed:
		return v1pb.QualityAgentCaseState_QUALITY_AGENT_CASE_STATE_ACTED
	case store.QualityAgentCaseStateAwaiting:
		return v1pb.QualityAgentCaseState_QUALITY_AGENT_CASE_STATE_AWAITING_VERIFICATION
	case store.QualityAgentCaseStateResolved:
		return v1pb.QualityAgentCaseState_QUALITY_AGENT_CASE_STATE_RESOLVED
	case store.QualityAgentCaseStateFrozen:
		return v1pb.QualityAgentCaseState_QUALITY_AGENT_CASE_STATE_FROZEN
	case store.QualityAgentCaseStateEscalated:
		return v1pb.QualityAgentCaseState_QUALITY_AGENT_CASE_STATE_ESCALATED
	default:
		return v1pb.QualityAgentCaseState_QUALITY_AGENT_CASE_STATE_UNSPECIFIED
	}
}
