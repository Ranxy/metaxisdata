package v1

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	"github.com/Ranxy/metaxisdata/backend/common"
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
	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate the agent id")
	}
	agent, err := s.store.CreateQualityAgent(ctx, resourceID, displayName, description,
		principalIDPointer(currentUserID(ctx)))
	if err != nil {
		return nil, err
	}
	token, err := s.mintQualityAgentToken(agent)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1pb.CreateQualityAgentResponse{
		Agent: convertQualityAgentToV1(workspaceID, agent),
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
	page, next, err := paginateInMemory(agents, offset)
	if err != nil {
		return nil, err
	}
	response := &v1pb.ListQualityAgentsResponse{NextPageToken: next}
	for _, agent := range page {
		response.QualityAgents = append(response.QualityAgents, convertQualityAgentToV1(workspaceID, agent))
	}
	return connect.NewResponse(response), nil
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
	return connect.NewResponse(convertQualityAgentToV1(workspaceID, agent)), nil
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
	return connect.NewResponse(&v1pb.RotateQualityAgentTokenResponse{
		Agent: convertQualityAgentToV1(workspaceID, agent),
		Token: token,
	}), nil
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
	if err := s.validateResourceWorkspace(ctx, workspace); err != nil {
		return "", "", err
	}
	return workspace, resourceID, nil
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

// convertQualityAgentToV1 renders one agent. The token is never part of this shape.
func convertQualityAgentToV1(workspaceID string, agent *store.QualityAgentMessage) *v1pb.QualityAgent {
	if agent == nil {
		return nil
	}
	return &v1pb.QualityAgent{
		Name:        common.FormatWorkspace(workspaceID) + "/" + qualityAgentPrefix + agent.ResourceID,
		DisplayName: agent.DisplayName,
		Description: agent.Description,
		Enabled:     agent.Enabled,
		CreatedBy:   formatQualityContractPublisher(agent.CreatedBy),
		CreateTime:  timestamppb.New(agent.CreatedAt),
	}
}
