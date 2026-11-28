package v1

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	qualityAgentRequestPrefix = "qualityAgentRequests/"
	qualityAgentAskPrefixV1   = "qualityAgentAsks/"
)

// ListQualityAgentRequests lists one agent's subagent decision requests. The owner
// and administrators may read; only the agent's tools write them.
func (s *DataQualityService) ListQualityAgentRequests(ctx context.Context, req *connect.Request[v1pb.ListQualityAgentRequestsRequest]) (*connect.Response[v1pb.ListQualityAgentRequestsResponse], error) {
	workspaceID, agent, err := s.qualityAgentRequestParent(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:        req.Msg.GetPageToken(),
		limit:        int(req.Msg.GetPageSize()),
		maximum:      qualityListMaximum,
		defaultLimit: qualityDefaultPageSize,
	})
	if err != nil {
		return nil, err
	}
	statuses, err := qualityAgentRequestStatuses(req.Msg.GetStatus())
	if err != nil {
		return nil, err
	}
	requests, err := s.store.ListQualityAgentRequestsPage(ctx, agent.ID, statuses,
		strings.TrimSpace(req.Msg.GetTargetGuid()), offset.limit+1, offset.offset)
	if err != nil {
		return nil, err
	}
	more := len(requests) > offset.limit
	if more {
		requests = requests[:offset.limit]
	}
	response := &v1pb.ListQualityAgentRequestsResponse{}
	if more {
		token, tokenErr := offset.getNextPageToken()
		if tokenErr != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(tokenErr, "failed to marshal next page token"))
		}
		response.NextPageToken = token
	}
	origins := s.qualityAgentSessionNames(ctx, workspaceID, qualityAgentRequestOriginIDs(requests))
	asks := s.qualityAgentAskNames(ctx, requests)
	for _, request := range requests {
		response.QualityAgentRequests = append(response.QualityAgentRequests,
			convertQualityAgentRequestToV1(workspaceID, agent.ResourceID, request, origins, asks))
	}
	return connect.NewResponse(response), nil
}

// GetQualityAgentDecisionRequest reads one request.
func (s *DataQualityService) GetQualityAgentDecisionRequest(ctx context.Context, req *connect.Request[v1pb.GetQualityAgentDecisionRequestRequest]) (*connect.Response[v1pb.QualityAgentRequest], error) {
	workspaceID, resourceID, err := s.qualityAgentRequestResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	request, err := s.store.GetQualityAgentRequestByResourceID(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if request == nil || !s.callerCanReadAgentID(ctx, request.AgentID) {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("quality agent request %q not found", resourceID))
	}
	agent, err := s.store.GetQualityAgentByID(ctx, request.AgentID)
	if err != nil || agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("quality agent request %q not found", resourceID))
	}
	origins := s.qualityAgentSessionNames(ctx, workspaceID, qualityAgentRequestOriginIDs([]*store.QualityAgentRequestMessage{request}))
	asks := s.qualityAgentAskNames(ctx, []*store.QualityAgentRequestMessage{request})
	return connect.NewResponse(convertQualityAgentRequestToV1(workspaceID, agent.ResourceID, request, origins, asks)), nil
}

// ListQualityAgentAsks lists one agent's owner-facing questions.
func (s *DataQualityService) ListQualityAgentAsks(ctx context.Context, req *connect.Request[v1pb.ListQualityAgentAsksRequest]) (*connect.Response[v1pb.ListQualityAgentAsksResponse], error) {
	workspaceID, agent, err := s.qualityAgentRequestParent(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:        req.Msg.GetPageToken(),
		limit:        int(req.Msg.GetPageSize()),
		maximum:      qualityListMaximum,
		defaultLimit: qualityDefaultPageSize,
	})
	if err != nil {
		return nil, err
	}
	statuses, err := qualityAgentAskStatuses(req.Msg.GetStatus())
	if err != nil {
		return nil, err
	}
	if len(req.Msg.GetStatuses()) > 0 {
		statuses = make([]store.QualityAgentAskStatus, 0, len(req.Msg.GetStatuses()))
		for _, status := range req.Msg.GetStatuses() {
			converted, statusErr := qualityAgentAskStatuses(status)
			if statusErr != nil {
				return nil, statusErr
			}
			statuses = append(statuses, converted...)
		}
	}
	asks, err := s.store.ListQualityAgentAsksPage(ctx, agent.ID, statuses, offset.limit+1, offset.offset)
	if err != nil {
		return nil, err
	}
	more := len(asks) > offset.limit
	if more {
		asks = asks[:offset.limit]
	}
	response := &v1pb.ListQualityAgentAsksResponse{}
	if more {
		token, tokenErr := offset.getNextPageToken()
		if tokenErr != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(tokenErr, "failed to marshal next page token"))
		}
		response.NextPageToken = token
	}
	conversations := s.qualityAgentSessionNames(ctx, workspaceID, qualityAgentAskConversationIDs(asks))
	coverage := s.qualityAgentAskCoverage(ctx, asks)
	for _, ask := range asks {
		response.QualityAgentAsks = append(response.QualityAgentAsks,
			convertQualityAgentAskToV1(workspaceID, agent.ResourceID, ask, conversations, coverage[ask.ID]))
	}
	return connect.NewResponse(response), nil
}

// GetQualityAgentAsk reads one owner-facing question.
func (s *DataQualityService) GetQualityAgentAsk(ctx context.Context, req *connect.Request[v1pb.GetQualityAgentAskRequest]) (*connect.Response[v1pb.QualityAgentAsk], error) {
	workspaceID, resourceID, err := s.qualityAgentAskResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	ask, err := s.store.GetQualityAgentAskByResourceID(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if ask == nil || !s.callerCanReadAgentID(ctx, ask.AgentID) {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("quality agent ask %q not found", resourceID))
	}
	agent, err := s.store.GetQualityAgentByID(ctx, ask.AgentID)
	if err != nil || agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("quality agent ask %q not found", resourceID))
	}
	conversations := s.qualityAgentSessionNames(ctx, workspaceID, qualityAgentAskConversationIDs([]*store.QualityAgentAskMessage{ask}))
	coverage := s.qualityAgentAskCoverage(ctx, []*store.QualityAgentAskMessage{ask})
	return connect.NewResponse(convertQualityAgentAskToV1(workspaceID, agent.ResourceID, ask, conversations, coverage[ask.ID])), nil
}

// qualityAgentRequestParent resolves the list parent to its agent and proves the
// caller may read that agent's coordination records.
func (s *DataQualityService) qualityAgentRequestParent(ctx context.Context, name string) (string, *store.QualityAgentMessage, error) {
	workspaceID, resourceID, err := s.qualityAgentResourceID(ctx, name)
	if err != nil {
		return "", nil, err
	}
	agent, err := s.qualityAgentConversationAccess(ctx, resourceID, false)
	if err != nil {
		return "", nil, err
	}
	return workspaceID, agent, nil
}

func (s *DataQualityService) qualityAgentRequestResourceID(ctx context.Context, name string) (string, string, error) {
	return s.qualityAgentCoordinationResourceID(ctx, name, qualityAgentRequestPrefix)
}

func (s *DataQualityService) qualityAgentAskResourceID(ctx context.Context, name string) (string, string, error) {
	return s.qualityAgentCoordinationResourceID(ctx, name, qualityAgentAskPrefixV1)
}

func (s *DataQualityService) qualityAgentCoordinationResourceID(ctx context.Context, name, prefix string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, prefix)
	if err != nil {
		return "", "", connectErrorInvalid("invalid %s name", strings.TrimSuffix(prefix, "/"))
	}
	workspace, err := s.canonicalAgentWorkspace(ctx, tokens[0])
	if err != nil {
		return "", "", err
	}
	if err := s.validateResourceWorkspace(ctx, workspace); err != nil {
		return "", "", err
	}
	return workspace, tokens[1], nil
}

// callerCanReadAgentID is the owner-or-administrator read check for a coordination
// row whose agent id is already known.
func (s *DataQualityService) callerCanReadAgentID(ctx context.Context, agentID int64) bool {
	allowed, err := s.callerCanReadQualityAgent(ctx, agentID)
	return err == nil && allowed
}

func qualityAgentRequestOriginIDs(requests []*store.QualityAgentRequestMessage) []int64 {
	ids := make([]int64, 0, len(requests))
	for _, request := range requests {
		if request.OriginSessionID != nil {
			ids = append(ids, *request.OriginSessionID)
		}
	}
	return ids
}

func qualityAgentAskConversationIDs(asks []*store.QualityAgentAskMessage) []int64 {
	ids := make([]int64, 0, len(asks))
	for _, ask := range asks {
		if ask.ConversationSessionID != nil {
			ids = append(ids, *ask.ConversationSessionID)
		}
	}
	return ids
}

// qualityAgentSessionNames maps session ids to resource ids in one read.
func (s *DataQualityService) qualityAgentSessionNames(ctx context.Context, workspaceID string, ids []int64) map[int64]string {
	names := map[int64]string{}
	if len(ids) == 0 {
		return names
	}
	stored, err := s.store.ListQualityAgentSessionsByIDs(ctx, workspaceID, ids)
	if err != nil {
		return names
	}
	for id, session := range stored {
		names[id] = session.ResourceID
	}
	return names
}

// qualityAgentAskCoverage maps each ask to the requests it carries in one read.
func (s *DataQualityService) qualityAgentAskCoverage(ctx context.Context, asks []*store.QualityAgentAskMessage) map[int64][]*store.QualityAgentRequestMessage {
	ids := make([]int64, 0, len(asks))
	for _, ask := range asks {
		ids = append(ids, ask.ID)
	}
	coverage := map[int64][]*store.QualityAgentRequestMessage{}
	if len(ids) == 0 {
		return coverage
	}
	stored, err := s.store.ListQualityAgentRequestsByAskIDs(ctx, ids)
	if err != nil {
		return coverage
	}
	return stored
}

func qualityAgentRequestStatuses(status v1pb.QualityAgentRequestStatus) ([]store.QualityAgentRequestStatus, error) {
	switch status {
	case v1pb.QualityAgentRequestStatus_QUALITY_AGENT_REQUEST_STATUS_UNSPECIFIED:
		return []store.QualityAgentRequestStatus{
			store.QualityAgentRequestStatusOpen, store.QualityAgentRequestStatusForwarded,
		}, nil
	case v1pb.QualityAgentRequestStatus_QUALITY_AGENT_REQUEST_STATUS_OPEN:
		return []store.QualityAgentRequestStatus{store.QualityAgentRequestStatusOpen}, nil
	case v1pb.QualityAgentRequestStatus_QUALITY_AGENT_REQUEST_STATUS_FORWARDED:
		return []store.QualityAgentRequestStatus{store.QualityAgentRequestStatusForwarded}, nil
	case v1pb.QualityAgentRequestStatus_QUALITY_AGENT_REQUEST_STATUS_ANSWERED:
		return []store.QualityAgentRequestStatus{store.QualityAgentRequestStatusAnswered}, nil
	case v1pb.QualityAgentRequestStatus_QUALITY_AGENT_REQUEST_STATUS_RESOLVED:
		return []store.QualityAgentRequestStatus{store.QualityAgentRequestStatusResolved}, nil
	case v1pb.QualityAgentRequestStatus_QUALITY_AGENT_REQUEST_STATUS_DISMISSED:
		return []store.QualityAgentRequestStatus{store.QualityAgentRequestStatusDismissed}, nil
	default:
		return nil, connectErrorInvalid("unknown request status %d", status)
	}
}

func qualityAgentAskStatuses(status v1pb.QualityAgentAskStatus) ([]store.QualityAgentAskStatus, error) {
	switch status {
	case v1pb.QualityAgentAskStatus_QUALITY_AGENT_ASK_STATUS_UNSPECIFIED:
		return []store.QualityAgentAskStatus{
			store.QualityAgentAskStatusOpen, store.QualityAgentAskStatusAnswered,
		}, nil
	case v1pb.QualityAgentAskStatus_QUALITY_AGENT_ASK_STATUS_OPEN:
		return []store.QualityAgentAskStatus{store.QualityAgentAskStatusOpen}, nil
	case v1pb.QualityAgentAskStatus_QUALITY_AGENT_ASK_STATUS_ANSWERED:
		return []store.QualityAgentAskStatus{store.QualityAgentAskStatusAnswered}, nil
	case v1pb.QualityAgentAskStatus_QUALITY_AGENT_ASK_STATUS_RESOLVED:
		return []store.QualityAgentAskStatus{store.QualityAgentAskStatusResolved}, nil
	case v1pb.QualityAgentAskStatus_QUALITY_AGENT_ASK_STATUS_DISMISSED:
		return []store.QualityAgentAskStatus{store.QualityAgentAskStatusDismissed}, nil
	default:
		return nil, connectErrorInvalid("unknown ask status %d", status)
	}
}

func convertQualityAgentQuestionsToV1(questions []*storepb.QualityAgentAskQuestion) []*v1pb.QualityAgentQuestion {
	converted := make([]*v1pb.QualityAgentQuestion, 0, len(questions))
	for _, question := range questions {
		if question == nil {
			continue
		}
		options := make([]*v1pb.QualityAgentQuestionOption, 0, len(question.Options))
		for _, option := range question.Options {
			if option == nil {
				continue
			}
			options = append(options, &v1pb.QualityAgentQuestionOption{
				Label:       option.Label,
				Description: option.Description,
			})
		}
		converted = append(converted, &v1pb.QualityAgentQuestion{
			Id:          question.Id,
			Header:      question.Header,
			Question:    question.Question,
			Detail:      question.Detail,
			Options:     options,
			MultiSelect: question.MultiSelect,
		})
	}
	return converted
}

func convertQualityAgentAnswersToV1(answers []*storepb.QualityAgentAskAnswer) []*v1pb.QualityAgentAnswer {
	converted := make([]*v1pb.QualityAgentAnswer, 0, len(answers))
	for _, answer := range answers {
		if answer == nil {
			continue
		}
		converted = append(converted, &v1pb.QualityAgentAnswer{
			Id:       answer.Id,
			Selected: append([]string(nil), answer.Selected...),
			Custom:   answer.Custom,
		})
	}
	return converted
}

func convertQualityAgentRequestToV1(workspaceID, agentResourceID string, request *store.QualityAgentRequestMessage, origins, asks map[int64]string) *v1pb.QualityAgentRequest {
	if request == nil {
		return nil
	}
	converted := &v1pb.QualityAgentRequest{
		Name:           common.FormatWorkspace(workspaceID) + "/" + qualityAgentRequestPrefix + request.ResourceID,
		Agent:          common.FormatWorkspace(workspaceID) + "/qualityAgents/" + agentResourceID,
		Target:         convertQualityTargetIdentityToV1(request.TargetGUID, request.TargetType),
		Kind:           v1pb.QualityAgentRequestKind(request.Kind),
		Status:         v1pb.QualityAgentRequestStatus(request.Status),
		Title:          request.Title,
		Question:       request.Question,
		Case:           qualityAgentCaseName(workspaceID, request.CaseResourceID),
		SuggestedPatch: convertQualityAgentFieldPatchToV1(request.SuggestedPatch),
		Resolution:     request.Resolution,
		CreateTime:     timestamppb.New(request.CreatedAt),
		UpdateTime:     timestamppb.New(request.UpdatedAt),
	}
	if request.OriginSessionID != nil {
		if name, ok := origins[*request.OriginSessionID]; ok {
			converted.OriginSession = common.FormatWorkspace(workspaceID) + "/qualityAgentSessions/" + name
		}
	}
	if request.AskID != nil {
		if name, ok := asks[*request.AskID]; ok {
			converted.Ask = common.FormatWorkspace(workspaceID) + "/" + qualityAgentAskPrefixV1 + name
		}
	}
	return converted
}

func convertQualityAgentAskToV1(workspaceID, agentResourceID string, ask *store.QualityAgentAskMessage, conversations map[int64]string, requests []*store.QualityAgentRequestMessage) *v1pb.QualityAgentAsk {
	if ask == nil {
		return nil
	}
	converted := &v1pb.QualityAgentAsk{
		Name:             common.FormatWorkspace(workspaceID) + "/" + qualityAgentAskPrefixV1 + ask.ResourceID,
		Agent:            common.FormatWorkspace(workspaceID) + "/qualityAgents/" + agentResourceID,
		Kind:             v1pb.QualityAgentAskKind(ask.Kind),
		Status:           v1pb.QualityAgentAskStatus(ask.Status),
		Title:            ask.Title,
		Question:         ask.Question,
		MessageSeq:       ask.MessageSeq,
		AnswerMessageSeq: ask.AnswerMessageSeq,
		ResolveTime:      timestampOrNil(ask.ResolvedAt),
		CreateTime:       timestamppb.New(ask.CreatedAt),
		UpdateTime:       timestamppb.New(ask.UpdatedAt),
		RequestCount:     int32(len(requests)),
		Questions:        convertQualityAgentQuestionsToV1(ask.Questions),
		Answers:          convertQualityAgentAnswersToV1(ask.Answers),
	}
	seenTargets := map[string]bool{}
	if ask.TargetGUID != "" {
		seenTargets[ask.TargetGUID] = true
		converted.Targets = append(converted.Targets,
			convertQualityTargetIdentityToV1(ask.TargetGUID, ask.TargetType))
	}
	for _, request := range requests {
		if request.TargetGUID == "" || seenTargets[request.TargetGUID] {
			continue
		}
		seenTargets[request.TargetGUID] = true
		converted.Targets = append(converted.Targets,
			convertQualityTargetIdentityToV1(request.TargetGUID, request.TargetType))
	}
	if ask.ConversationSessionID != nil {
		if name, ok := conversations[*ask.ConversationSessionID]; ok {
			converted.Conversation = common.FormatWorkspace(workspaceID) + "/qualityAgentSessions/" + name
		}
	}
	if ask.AnsweredBy != nil {
		converted.AnsweredBy = common.FormatUserUID(int(*ask.AnsweredBy))
	}
	if ask.AnsweredAt != nil {
		converted.AnswerTime = timestamppb.New(*ask.AnsweredAt)
	}
	return converted
}

func qualityAgentCaseName(workspaceID, resourceID string) string {
	if resourceID == "" {
		return ""
	}
	return common.FormatWorkspace(workspaceID) + "/qualityAgentCases/" + resourceID
}

// qualityAgentAskNames maps the ask ids named by a request page to their resource ids
// in one read.
func (s *DataQualityService) qualityAgentAskNames(ctx context.Context, requests []*store.QualityAgentRequestMessage) map[int64]string {
	ids := make([]int64, 0, len(requests))
	for _, request := range requests {
		if request.AskID != nil {
			ids = append(ids, *request.AskID)
		}
	}
	names := map[int64]string{}
	if len(ids) == 0 {
		return names
	}
	stored, err := s.store.ListQualityAgentAsksByIDs(ctx, ids)
	if err != nil {
		return names
	}
	for id, ask := range stored {
		names[id] = ask.ResourceID
	}
	return names
}

// convertQualityAgentFieldPatchToV1 renders the change a proposal asks for.
func convertQualityAgentFieldPatchToV1(patch *storepb.QualityAgentFieldPatch) *v1pb.QualityAgentFieldPatch {
	if patch == nil || patch.GetField() == "" {
		return nil
	}
	return &v1pb.QualityAgentFieldPatch{
		Field:        patch.GetField(),
		Value:        patch.GetValue(),
		BaseRevision: patch.GetBaseRevision(),
		Effect:       patch.GetEffect(),
		Rationale:    patch.GetRationale(),
	}
}
