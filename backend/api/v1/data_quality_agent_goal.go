package v1

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	qualityAgentGoalPrefix = "qualityAgentGoals/"
	maxAgentGoalObjective  = 4000
	maxAgentGoalRounds     = 10000
	defaultAgentGoalRounds = 256
	// goalCreationWindow is the window the per-workspace goal budget counts over.
	goalCreationWindow = time.Hour
)

// CreateQualityAgentGoal records one durable objective for the built-in runner. The
// runner must be enabled, the target's database must be in the workspace allowlist,
// and the goal must name an enabled agent; every check happens before the write.
func (s *DataQualityService) CreateQualityAgentGoal(ctx context.Context, req *connect.Request[v1pb.CreateQualityAgentGoalRequest]) (*connect.Response[v1pb.QualityAgentGoal], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	settings, err := dataquality.LoadSettings(ctx, s.store)
	if err != nil {
		return nil, errLoadQualitySettings(err)
	}
	if !settings.AgentRunnerEnabled {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the quality agent runner is disabled in this workspace"))
	}
	kind := storepb.QualityAgentGoalKind(req.Msg.GetKind())
	targetGUID := strings.TrimSpace(req.Msg.GetTargetGuid())
	triggerRef := strings.TrimSpace(req.Msg.GetTriggerRef())
	objective := strings.TrimSpace(req.Msg.GetObjective())
	switch kind {
	case storepb.QualityAgentGoalKind_QUALITY_AGENT_GOAL_KIND_MAINTAIN_TARGET:
		if targetGUID == "" {
			return nil, connectErrorInvalid("a maintain goal needs a target")
		}
	case storepb.QualityAgentGoalKind_QUALITY_AGENT_GOAL_KIND_INVESTIGATE_EVENT:
		if req.Msg.GetTrigger() == v1pb.QualityAgentGoalTrigger_QUALITY_AGENT_GOAL_TRIGGER_UNSPECIFIED {
			return nil, connectErrorInvalid("an event goal needs a trigger")
		}
		if triggerRef == "" {
			return nil, connectErrorInvalid("an event goal needs a trigger reference")
		}
	case storepb.QualityAgentGoalKind_QUALITY_AGENT_GOAL_KIND_CUSTOM:
		if objective == "" {
			return nil, connectErrorInvalid("a custom goal needs an objective")
		}
	default:
		return nil, connectErrorInvalid("unknown goal kind %d", req.Msg.GetKind())
	}
	if len(objective) > maxAgentGoalObjective {
		return nil, connectErrorInvalid("the objective is too long (max %d bytes)", maxAgentGoalObjective)
	}
	metaType := storepb.MetaType(req.Msg.GetMetaType())
	databaseRef := ""
	if targetGUID != "" {
		if metaType == storepb.MetaType_UNSPECIFIED {
			rules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &targetGUID})
			if err != nil {
				return nil, errors.Wrap(err, "failed to read the target's rules")
			}
			if len(rules) == 0 {
				return nil, connectErrorInvalid("meta_type is required when the target has no rules")
			}
			metaType = rules[0].TargetType
		}
		databaseRef, err = databaseRefOfTarget(targetGUID)
		if err != nil {
			return nil, err
		}
		if !settings.AgentRunnerDatabaseEnabled(databaseRef) {
			return nil, connect.NewError(connect.CodeFailedPrecondition,
				errors.Errorf("database %q is not in the runner's allowlist", databaseRef))
		}
	}
	agent, err := s.resolveGoalAgent(ctx, req.Msg.GetAgent())
	if err != nil {
		return nil, err
	}
	created, err := s.store.CountQualityAgentGoalsSince(ctx, workspaceID, time.Now().Add(-goalCreationWindow))
	if err != nil {
		return nil, err
	}
	if created >= int64(settings.AgentRunnerGoalsPerHour) {
		return nil, connect.NewError(connect.CodeResourceExhausted,
			errors.Errorf("the workspace already created %d goals in the last hour", created))
	}
	maxRounds := req.Msg.GetMaxRounds()
	if maxRounds <= 0 {
		maxRounds = defaultAgentGoalRounds
	}
	if maxRounds > maxAgentGoalRounds {
		return nil, connectErrorInvalid("max_rounds is too large (max %d)", maxAgentGoalRounds)
	}
	input := &store.CreateQualityAgentGoalInput{
		WorkspaceID: workspaceID,
		Kind:        kind,
		Trigger:     storepb.QualityAgentGoalTrigger(req.Msg.GetTrigger()),
		TriggerRef:  triggerRef,
		TargetGUID:  targetGUID,
		TargetType:  metaType,
		DatabaseRef: databaseRef,
		AgentID:     agent.ID,
		Objective:   objective,
		MaxRounds:   maxRounds,
		CreatedBy:   principalIDPointer(currentUserID(ctx)),
	}
	message, err := s.store.CreateQualityAgentGoal(ctx, input)
	if err != nil {
		if common.ErrorCode(err) == common.Conflict {
			return nil, connect.NewError(connect.CodeAlreadyExists, err)
		}
		return nil, err
	}
	return connect.NewResponse(convertQualityAgentGoalToV1(workspaceID, message)), nil
}

// databaseRefOfTarget derives the database resource name from a target GUID
// (instance;database;schema;object), the value the allowlist holds.
func databaseRefOfTarget(targetGUID string) (string, error) {
	segments := common.SplitMetaGUID(targetGUID)
	if len(segments) < 2 || segments[0] == "" || segments[1] == "" {
		return "", connectErrorInvalid("target GUID %q does not name a database object", targetGUID)
	}
	return common.FormatDatabase(segments[0], segments[1]), nil
}

// resolveGoalAgent picks the agent a goal runs as: the requested one, or the first
// enabled quality agent of the workspace.
func (s *DataQualityService) resolveGoalAgent(ctx context.Context, name string) (*store.QualityAgentMessage, error) {
	if name = strings.TrimSpace(name); name != "" {
		principalID, err := parseUserResourceID(name)
		if err != nil {
			return nil, connectErrorInvalid("%v", err)
		}
		agent, err := s.store.GetQualityAgentByPrincipalID(ctx, principalID)
		if err != nil {
			return nil, err
		}
		if agent == nil || !agent.Enabled {
			return nil, connect.NewError(connect.CodeFailedPrecondition,
				errors.Errorf("agent %q is not an enabled quality agent", name))
		}
		return agent, nil
	}
	agents, err := s.store.ListQualityAgents(ctx)
	if err != nil {
		return nil, err
	}
	for _, agent := range agents {
		if agent.Enabled {
			return agent, nil
		}
	}
	return nil, connect.NewError(connect.CodeFailedPrecondition,
		errors.New("create an enabled quality agent before creating a goal"))
}

// parseUserResourceID parses users/{principal-id}.
func parseUserResourceID(name string) (int, error) {
	id, err := common.GetUserID(name)
	if err != nil || id <= 0 {
		return 0, errors.New("the agent must be a user resource name")
	}
	return id, nil
}

// ListQualityAgentGoals lists the workspace's goals newest first.
func (s *DataQualityService) ListQualityAgentGoals(ctx context.Context, req *connect.Request[v1pb.ListQualityAgentGoalsRequest]) (*connect.Response[v1pb.ListQualityAgentGoalsResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
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
	limit := offset.limit + 1
	find := &store.FindQualityAgentGoalMessage{WorkspaceID: &workspaceID, Limit: &limit, Offset: &offset.offset}
	if guid := strings.TrimSpace(req.Msg.GetTargetGuid()); guid != "" {
		find.TargetGUID = &guid
	}
	if database := strings.TrimSpace(req.Msg.GetDatabase()); database != "" {
		find.DatabaseRef = &database
	}
	switch phase := req.Msg.GetPhase(); phase {
	case v1pb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_UNSPECIFIED:
	case v1pb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_ACTIVE,
		v1pb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_PAUSED,
		v1pb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_BLOCKED,
		v1pb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_COMPLETE:
		find.Phases = []storepb.QualityAgentGoalPhase{storepb.QualityAgentGoalPhase(phase)}
	default:
		return nil, connectErrorInvalid("unknown goal phase %d", phase)
	}
	messages, err := s.store.ListQualityAgentGoals(ctx, find)
	if err != nil {
		return nil, err
	}
	messages, nextPageToken, err := paginate(messages, offset)
	if err != nil {
		return nil, err
	}
	response := &v1pb.ListQualityAgentGoalsResponse{NextPageToken: nextPageToken}
	for _, message := range messages {
		response.QualityAgentGoals = append(response.QualityAgentGoals, convertQualityAgentGoalToV1(workspaceID, message))
	}
	return connect.NewResponse(response), nil
}

// GetQualityAgentGoal reads one goal.
func (s *DataQualityService) GetQualityAgentGoal(ctx context.Context, req *connect.Request[v1pb.GetQualityAgentGoalRequest]) (*connect.Response[v1pb.QualityAgentGoal], error) {
	workspaceID, resourceID, err := s.qualityAgentGoalResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	message, err := s.fetchQualityAgentGoal(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertQualityAgentGoalToV1(workspaceID, message)), nil
}

// PauseQualityAgentGoal stops automatic continuation; the state and journal stay.
func (s *DataQualityService) PauseQualityAgentGoal(ctx context.Context, req *connect.Request[v1pb.PauseQualityAgentGoalRequest]) (*connect.Response[v1pb.QualityAgentGoal], error) {
	return s.transitionGoal(ctx, req.Msg.GetName(), req.Msg.GetExpectedRevision(),
		storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_PAUSED)
}

// ResumeQualityAgentGoal puts a paused or blocked goal back to active.
func (s *DataQualityService) ResumeQualityAgentGoal(ctx context.Context, req *connect.Request[v1pb.ResumeQualityAgentGoalRequest]) (*connect.Response[v1pb.QualityAgentGoal], error) {
	return s.transitionGoal(ctx, req.Msg.GetName(), req.Msg.GetExpectedRevision(),
		storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_ACTIVE)
}

// CompleteQualityAgentGoal marks a goal complete.
func (s *DataQualityService) CompleteQualityAgentGoal(ctx context.Context, req *connect.Request[v1pb.CompleteQualityAgentGoalRequest]) (*connect.Response[v1pb.QualityAgentGoal], error) {
	return s.transitionGoal(ctx, req.Msg.GetName(), req.Msg.GetExpectedRevision(),
		storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_COMPLETE)
}

// ClearQualityAgentGoal tombstones a goal; the journal stays in the audit trail.
func (s *DataQualityService) ClearQualityAgentGoal(ctx context.Context, req *connect.Request[v1pb.ClearQualityAgentGoalRequest]) (*connect.Response[emptypb.Empty], error) {
	_, resourceID, err := s.qualityAgentGoalResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	cleared, err := s.store.ClearQualityAgentGoal(ctx, resourceID, req.Msg.GetExpectedRevision(), time.Now())
	if err != nil {
		return nil, err
	}
	if !cleared {
		return nil, connect.NewError(connect.CodeAborted,
			errors.New("the goal changed since it was read; reload and try again"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// transitionGoal applies one human phase change with a revision compare-and-set.
func (s *DataQualityService) transitionGoal(ctx context.Context, name string, expectedRevision int32, phase storepb.QualityAgentGoalPhase) (*connect.Response[v1pb.QualityAgentGoal], error) {
	workspaceID, resourceID, err := s.qualityAgentGoalResourceID(ctx, name)
	if err != nil {
		return nil, err
	}
	message, applied, err := s.store.UpdateQualityAgentGoalState(ctx, resourceID, expectedRevision, phase, "", "", nil)
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, connect.NewError(connect.CodeAborted,
			errors.New("the goal changed since it was read; reload and try again"))
	}
	return connect.NewResponse(convertQualityAgentGoalToV1(workspaceID, message)), nil
}

// qualityAgentGoalResourceID resolves a goal name to its workspace and resource id.
func (s *DataQualityService) qualityAgentGoalResourceID(ctx context.Context, name string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualityAgentGoalPrefix)
	if err != nil {
		return "", "", connectErrorInvalid("invalid quality agent goal name")
	}
	if err := s.validateResourceWorkspace(ctx, tokens[0]); err != nil {
		return "", "", err
	}
	return tokens[0], tokens[1], nil
}

func (s *DataQualityService) fetchQualityAgentGoal(ctx context.Context, resourceID string) (*store.QualityAgentGoalMessage, error) {
	message, err := s.store.GetQualityAgentGoal(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if message == nil {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("quality agent goal %q not found", resourceID))
	}
	return message, nil
}

// convertQualityAgentGoalToV1 renders one goal.
func convertQualityAgentGoalToV1(workspaceID string, message *store.QualityAgentGoalMessage) *v1pb.QualityAgentGoal {
	if message == nil {
		return nil
	}
	return &v1pb.QualityAgentGoal{
		Name:          common.FormatWorkspace(workspaceID) + "/" + qualityAgentGoalPrefix + message.ResourceID,
		Kind:          v1pb.QualityAgentGoalKind(message.Kind),
		Trigger:       v1pb.QualityAgentGoalTrigger(message.Trigger),
		TriggerRef:    message.TriggerRef,
		Target:        &v1pb.QualityTarget{MetaGuid: message.TargetGUID, MetaType: v1pb.MetaType(message.TargetType)},
		Database:      message.DatabaseRef,
		Agent:         common.FormatUserUID(int(message.AgentPrincipalID)),
		Objective:     message.Objective,
		Phase:         v1pb.QualityAgentGoalPhase(message.Phase),
		BlockedCode:   message.BlockedCode,
		BlockedReason: message.BlockedReason,
		Revision:      message.Revision,
		RoundsStarted: message.RoundsStarted,
		MaxRounds:     message.MaxRounds,
		TokensUsed:    message.TokensUsed,
		NextWakeTime:  timestampOrNil(message.NextWakeAt),
		CreateTime:    timestamppb.New(message.CreatedAt),
		UpdateTime:    timestamppb.New(message.UpdatedAt),
	}
}
