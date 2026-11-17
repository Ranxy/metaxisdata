package v1

import (
	"context"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const qualityAgentReportPrefix = "qualityAgentReports/"

// ListQualityAgentReports lists one agent's reports, newest period first. The
// owner and administrators may read.
func (s *DataQualityService) ListQualityAgentReports(ctx context.Context, req *connect.Request[v1pb.ListQualityAgentReportsRequest]) (*connect.Response[v1pb.ListQualityAgentReportsResponse], error) {
	workspaceID, resourceID, err := s.qualityAgentResourceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	agent, _, err := s.qualityAgentConversationAccess(ctx, resourceID, false)
	if err != nil {
		return nil, err
	}
	reports, err := s.store.ListQualityAgentReports(ctx, agent.ID, int(req.Msg.GetPageSize()))
	if err != nil {
		return nil, err
	}
	response := &v1pb.ListQualityAgentReportsResponse{}
	for _, report := range reports {
		response.QualityAgentReports = append(response.QualityAgentReports,
			convertQualityAgentReportToV1(workspaceID, agent.ResourceID, report))
	}
	return connect.NewResponse(response), nil
}

// GetQualityAgentReport reads one report.
func (s *DataQualityService) GetQualityAgentReport(ctx context.Context, req *connect.Request[v1pb.GetQualityAgentReportRequest]) (*connect.Response[v1pb.QualityAgentReport], error) {
	workspaceID, resourceID, err := s.qualityAgentReportResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	report, err := s.store.GetQualityAgentReportByResourceID(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if report == nil {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("quality agent report %q not found", resourceID))
	}
	agent, err := s.store.GetQualityAgentByID(ctx, report.AgentID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("the report's agent no longer exists"))
	}
	if _, _, err := s.qualityAgentConversationAccess(ctx, agent.ResourceID, false); err != nil {
		return nil, err
	}
	return connect.NewResponse(convertQualityAgentReportToV1(workspaceID, agent.ResourceID, report)), nil
}

func (s *DataQualityService) qualityAgentReportResourceID(ctx context.Context, name string) (string, string, error) {
	workspace, resourceID, err := parseQualityAgentReportName(name)
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

func parseQualityAgentReportName(name string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualityAgentReportPrefix)
	if err != nil {
		return "", "", errors.Wrap(err, "invalid quality agent report name")
	}
	return tokens[0], tokens[1], nil
}

func convertQualityAgentReportToV1(workspaceID, agentResourceID string, report *store.QualityAgentReportMessage) *v1pb.QualityAgentReport {
	if report == nil {
		return nil
	}
	view := &v1pb.QualityAgentReport{
		Name:                 common.FormatWorkspace(workspaceID) + "/" + qualityAgentReportPrefix + report.ResourceID,
		Agent:                common.FormatWorkspace(workspaceID) + "/" + qualityAgentPrefix + agentResourceID,
		Trigger:              qualityAgentReportTriggerToV1(report.Trigger),
		PeriodKind:           qualityAgentReportPeriodToV1(report.PeriodKind),
		PeriodKey:            report.PeriodKey,
		PeriodStart:          timestamppb.New(report.PeriodStart),
		PeriodEnd:            timestamppb.New(report.PeriodEnd),
		State:                qualityAgentReportStateToV1(report.State),
		Databases:            append([]string(nil), report.Scope...),
		TargetCount:          report.TargetCount,
		CoveredTargetCount:   report.CoveredTargetCount,
		RuleCount:            report.RuleCount,
		ExecutionCount:       report.ExecutionCount,
		PassedCount:          report.PassedCount,
		WarnedCount:          report.WarnedCount,
		FailedCount:          report.FailedCount,
		ErroredCount:         report.ErroredCount,
		NewIssueCount:        report.NewIssueCount,
		OpenIssueCount:       report.OpenIssueCount,
		PendingProposalCount: report.PendingProposalCount,
		TokensUsed:           report.TokensUsed,
		BodyMarkdown:         report.BodyMarkdown,
		FailureReason:        report.FailureReason,
		CreateTime:           timestamppb.New(report.CreatedAt),
	}
	if report.GeneratedAt != nil {
		view.GenerateTime = timestamppb.New(*report.GeneratedAt)
	}
	return view
}

func qualityAgentReportTriggerToV1(trigger store.QualityAgentReportTrigger) v1pb.QualityAgentReportTrigger {
	switch trigger {
	case store.QualityAgentReportTriggerScheduled:
		return v1pb.QualityAgentReportTrigger_QUALITY_AGENT_REPORT_TRIGGER_SCHEDULED
	case store.QualityAgentReportTriggerOnDemand:
		return v1pb.QualityAgentReportTrigger_QUALITY_AGENT_REPORT_TRIGGER_ON_DEMAND
	default:
		return v1pb.QualityAgentReportTrigger_QUALITY_AGENT_REPORT_TRIGGER_UNSPECIFIED
	}
}

func qualityAgentReportPeriodToV1(period store.QualityAgentReportPeriod) v1pb.QualityAgentReportPeriod {
	switch period {
	case store.QualityAgentReportPeriodDaily:
		return v1pb.QualityAgentReportPeriod_QUALITY_AGENT_REPORT_PERIOD_DAILY
	case store.QualityAgentReportPeriodWeekly:
		return v1pb.QualityAgentReportPeriod_QUALITY_AGENT_REPORT_PERIOD_WEEKLY
	case store.QualityAgentReportPeriodOnDemand:
		return v1pb.QualityAgentReportPeriod_QUALITY_AGENT_REPORT_PERIOD_ON_DEMAND
	default:
		return v1pb.QualityAgentReportPeriod_QUALITY_AGENT_REPORT_PERIOD_UNSPECIFIED
	}
}

func qualityAgentReportStateToV1(state store.QualityAgentReportState) v1pb.QualityAgentReportState {
	switch state {
	case store.QualityAgentReportStatePending:
		return v1pb.QualityAgentReportState_QUALITY_AGENT_REPORT_STATE_PENDING
	case store.QualityAgentReportStateReady:
		return v1pb.QualityAgentReportState_QUALITY_AGENT_REPORT_STATE_READY
	case store.QualityAgentReportStateFailed:
		return v1pb.QualityAgentReportState_QUALITY_AGENT_REPORT_STATE_FAILED
	default:
		return v1pb.QualityAgentReportState_QUALITY_AGENT_REPORT_STATE_UNSPECIFIED
	}
}
