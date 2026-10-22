package v1

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// qualityLLMUsageResponse assembles the read-only quota view from the resolved
// settings and the process-wide counters. It is pure so the mapping is testable
// without a store or a server.
func qualityLLMUsageResponse(settings dataquality.Settings, analyses, columnAnalyses, inFlight int, resetsAt time.Time) *v1pb.QualityLLMUsage {
	limits := dataquality.LimitsFromSettings(settings)
	return &v1pb.QualityLLMUsage{
		DailyAnalysisUsed:        int32(analyses),
		DailyAnalysisLimit:       int32(limits.DailyAnalyses),
		DailyColumnAnalysisUsed:  int32(columnAnalyses),
		DailyColumnAnalysisLimit: int32(limits.DailyColumnAnalyses),
		InFlight:                 int32(inFlight),
		ConcurrencyLimit:         int32(limits.Concurrency),
		ResetsAt:                 timestamppb.New(resetsAt),
	}
}

// GetQualityLLMUsage reports today's LLM analysis usage and the effective limits,
// so the settings page can show them without a metrics scrape. The counters are
// process state: a restart resets them and they roll over at 00:00 UTC.
func (s *DataQualityService) GetQualityLLMUsage(ctx context.Context, req *connect.Request[v1pb.GetQualityLLMUsageRequest]) (*connect.Response[v1pb.QualityLLMUsage], error) {
	if req == nil {
		return nil, connectErrorInvalid("a request is required")
	}
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	settings, err := dataquality.LoadSettings(ctx, s.store)
	if err != nil {
		return nil, errLoadQualitySettings(err)
	}
	analyses, columnAnalyses, inFlight := s.quota.Usage()
	now := time.Now().UTC()
	return connect.NewResponse(qualityLLMUsageResponse(settings, analyses, columnAnalyses, inFlight,
		s.quota.NextReset(now))), nil
}
