package v1

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	qualityAgentPrefix       = "qualityAgents/"
	maxQualityAgentNameBytes = 200
	maxQualityAgentDescBytes = 2000
)

// CreateQualityAgent creates the agent's internal principal, its configuration row
// and its database scope. There is no credential in the response: the agent is an
// internal runtime, not an API caller.
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
		Owner:          req.Msg.GetOwner(),
		Databases:      req.Msg.GetDatabases(),
		Capabilities:   req.Msg.GetCapabilities(),
		ReportSchedule: req.Msg.GetReportSchedule(),
		Adoption:       req.Msg.GetAdoption(),
		Budget:         req.Msg.GetBudget(),
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
	config.ID = agent.ID
	updated, err := s.store.UpdateQualityAgentConfig(ctx, config)
	if err != nil {
		// The identity already exists; disable it so a failed creation does not leave
		// a live agent behind. The row stays for the audit trail.
		_, _ = s.store.DisableQualityAgent(ctx, resourceID)
		return nil, err
	}
	if req.Msg.GetStart() {
		updated, err = s.store.UpdateQualityAgentState(ctx, agent.ID, store.QualityAgentStateActive)
		if err != nil {
			_, _ = s.store.DisableQualityAgent(ctx, resourceID)
			return nil, err
		}
	}
	if err := s.store.ReplaceQualityAgentDatabases(ctx, agent.ID, databases); err != nil {
		_, _ = s.store.DisableQualityAgent(ctx, resourceID)
		return nil, err
	}
	view, err := s.qualityAgentView(ctx, workspaceID, updated)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1pb.CreateQualityAgentResponse{Agent: view}), nil
}

// ListQualityAgents lists the workspace's agents. Non-administrators see only the
// agents they own.
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
	// The ACL grants every member the list permission so an owner can open their own
	// agents; this filter is the ownership check.
	visible := make([]*v1pb.QualityAgent, 0, len(agents))
	for _, agent := range agents {
		if !admin {
			if agent.OwnerPrincipalID == nil || int32(currentUserID(ctx)) != *agent.OwnerPrincipalID {
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

// DisableQualityAgent moves one agent to DISABLED and deactivates its principal.
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
	view, err := s.qualityAgentView(ctx, workspaceID, agent)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(view), nil
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
