package v1

import (
	"context"
	"strconv"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// qualityAgentCaseActionPageLimit is the default page the activity view reads.
	qualityAgentCaseActionPageLimit = 50
	// maxQualityAgentCaseActionPageLimit bounds one page: the ledger is a reading surface and the
	// activity view walks it forward.
	maxQualityAgentCaseActionPageLimit = 200
)

// ListQualityAgentCaseActions lists the writes one agent made to checks, newest first. The owner and
// administrators may read it: it is the record behind the write budget, and the answer to "what has
// this agent changed" that does not require reading a transcript.
func (s *DataQualityService) ListQualityAgentCaseActions(ctx context.Context, req *connect.Request[v1pb.ListQualityAgentCaseActionsRequest]) (*connect.Response[v1pb.ListQualityAgentCaseActionsResponse], error) {
	workspaceID, resourceID, err := s.qualityAgentResourceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	agent, err := s.qualityAgentConversationAccess(ctx, resourceID, false)
	if err != nil {
		return nil, err
	}
	caseID, err := s.caseActionCaseFilter(ctx, agent, req.Msg.GetCase())
	if err != nil {
		return nil, err
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token: req.Msg.GetPageToken(), limit: int(req.Msg.GetPageSize()),
		maximum: maxQualityAgentCaseActionPageLimit, defaultLimit: qualityAgentCaseActionPageLimit,
	})
	if err != nil {
		return nil, err
	}
	actions, err := s.store.ListQualityAgentCaseActions(ctx, &store.FindQualityAgentCaseActionList{
		AgentID: agent.ID,
		CaseID:  caseID,
		Limit:   offset.limit + 1,
		Offset:  offset.offset,
	})
	if err != nil {
		return nil, err
	}
	page, nextPageToken, err := paginate(actions, offset)
	if err != nil {
		return nil, err
	}
	response := &v1pb.ListQualityAgentCaseActionsResponse{NextPageToken: nextPageToken}
	for _, action := range page {
		response.QualityAgentCaseActions = append(response.QualityAgentCaseActions,
			convertQualityAgentCaseActionToV1(workspaceID, agent.ResourceID, action))
	}
	return connect.NewResponse(response), nil
}

// caseActionCaseFilter resolves the optional work-item filter. A case that is not this agent's is
// refused rather than answered with an empty page: "no history" and "not your case" are different
// facts, and only one of them is the caller's to learn.
func (s *DataQualityService) caseActionCaseFilter(ctx context.Context, agent *store.QualityAgentMessage, name string) (*int64, error) {
	resourceID := caseResourceID(name)
	if resourceID == "" {
		return nil, nil
	}
	openCase, err := s.store.GetQualityAgentCaseByResourceID(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if openCase == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("case %q not found", resourceID))
	}
	if openCase.AgentID != agent.ID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("the case belongs to another agent"))
	}
	caseID := openCase.ID
	return &caseID, nil
}

// caseResourceID takes the last segment of a case resource name, or the id itself.
func caseResourceID(name string) string {
	trimmed := strings.TrimSpace(name)
	if index := strings.LastIndex(trimmed, "/"); index >= 0 {
		return trimmed[index+1:]
	}
	return trimmed
}

// convertQualityAgentCaseActionToV1 renders one ledger row. The check and the execution are named by
// their resource ids so the activity view links to them without a second read.
func convertQualityAgentCaseActionToV1(workspaceID, agentResourceID string, action *store.QualityAgentCaseActionMessage) *v1pb.QualityAgentCaseAction {
	if action == nil {
		return nil
	}
	view := &v1pb.QualityAgentCaseAction{
		Name:         common.FormatWorkspace(workspaceID) + "/qualityAgentCaseActions/" + strconv.FormatInt(action.ID, 10),
		Agent:        common.FormatWorkspace(workspaceID) + "/qualityAgents/" + agentResourceID,
		Target:       convertQualityTargetIdentityToV1(action.TargetGUID, action.TargetType),
		Action:       qualityAgentCaseActionKindToV1(action.Action),
		Field:        action.Field,
		FromRevision: action.FromRevision,
		ToRevision:   action.ToRevision,
		Class:        action.Reason,
		Reason:       action.Reason,
		CreateTime:   timestamppb.New(action.CreatedAt),
	}
	if action.CaseID != nil {
		view.Case = common.FormatWorkspace(workspaceID) + "/qualityAgentCases/" + strconv.FormatInt(*action.CaseID, 10)
	}
	if action.RuleID != nil {
		view.Check = "qualityRules/" + strconv.FormatInt(*action.RuleID, 10)
	}
	if action.ExecutionID != nil {
		view.Execution = "qualityExecutions/" + strconv.FormatInt(*action.ExecutionID, 10)
	}
	return view
}

// qualityAgentCaseActionKindToV1 maps the stored action kind onto the wire.
func qualityAgentCaseActionKindToV1(kind store.QualityAgentCaseActionKind) v1pb.QualityAgentCaseActionKind {
	switch kind {
	case store.QualityAgentCaseActionRevise:
		return v1pb.QualityAgentCaseActionKind_QUALITY_AGENT_CASE_ACTION_KIND_REVISE
	case store.QualityAgentCaseActionControl:
		return v1pb.QualityAgentCaseActionKind_QUALITY_AGENT_CASE_ACTION_KIND_CONTROL
	case store.QualityAgentCaseActionRun:
		return v1pb.QualityAgentCaseActionKind_QUALITY_AGENT_CASE_ACTION_KIND_RUN
	case store.QualityAgentCaseActionPropose:
		return v1pb.QualityAgentCaseActionKind_QUALITY_AGENT_CASE_ACTION_KIND_PROPOSE
	default:
		return v1pb.QualityAgentCaseActionKind_QUALITY_AGENT_CASE_ACTION_KIND_UNSPECIFIED
	}
}
