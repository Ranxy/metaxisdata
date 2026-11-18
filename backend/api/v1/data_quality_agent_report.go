package v1

import (
	"context"

	"connectrpc.com/connect"
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
	agent, err := s.qualityAgentConversationAccess(ctx, resourceID, false)
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

func convertQualityAgentReportToV1(workspaceID, agentResourceID string, report *store.QualityAgentReportMessage) *v1pb.QualityAgentReport {
	if report == nil {
		return nil
	}
	view := &v1pb.QualityAgentReport{
		Name:                 common.FormatWorkspace(workspaceID) + "/" + qualityAgentReportPrefix + report.ResourceID,
		Agent:                common.FormatWorkspace(workspaceID) + "/" + qualityAgentPrefix + agentResourceID,
		PeriodKind:           qualityAgentReportPeriodToV1(report.PeriodKind),
		PeriodKey:            report.PeriodKey,
		PeriodStart:          timestamppb.New(report.PeriodStart),
		PeriodEnd:            timestamppb.New(report.PeriodEnd),
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
		BodyMarkdown:         report.BodyMarkdown,
		CreateTime:           timestamppb.New(report.CreatedAt),
	}
	if report.GeneratedAt != nil {
		view.GenerateTime = timestamppb.New(*report.GeneratedAt)
	}
	return view
}

func qualityAgentReportPeriodToV1(period store.QualityAgentReportPeriod) v1pb.QualityAgentReportPeriod {
	switch period {
	case store.QualityAgentReportPeriodDaily:
		return v1pb.QualityAgentReportPeriod_QUALITY_AGENT_REPORT_PERIOD_DAILY
	case store.QualityAgentReportPeriodWeekly:
		return v1pb.QualityAgentReportPeriod_QUALITY_AGENT_REPORT_PERIOD_WEEKLY
	default:
		return v1pb.QualityAgentReportPeriod_QUALITY_AGENT_REPORT_PERIOD_UNSPECIFIED
	}
}
