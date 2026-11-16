package mcp

import (
	"context"
	"encoding/json"

	"github.com/Ranxy/metaxisdata/backend/store"
)

// The quality tool bodies live in component/tools/quality so the built-in agent
// runs the same implementation.

func (s *Server) runGetQualitySignal(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	payload, err := s.qualityTools.GetQualitySignal(ctx, raw)
	return payload, asToolError(err)
}

func (s *Server) runListQualityTargets(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	payload, err := s.qualityTools.ListQualityTargets(ctx, raw)
	return payload, asToolError(err)
}

func (s *Server) runGetQualityTarget(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	payload, err := s.qualityTools.GetQualityTarget(ctx, raw)
	return payload, asToolError(err)
}

func (s *Server) runListQualityIssues(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	payload, err := s.qualityTools.ListQualityIssues(ctx, raw)
	return payload, asToolError(err)
}

func (s *Server) runListQualityExecutions(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	payload, err := s.qualityTools.ListQualityExecutions(ctx, raw)
	return payload, asToolError(err)
}

func (s *Server) runGetQualityExecution(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	payload, err := s.qualityTools.GetQualityExecution(ctx, raw)
	return payload, asToolError(err)
}

func (s *Server) runGetQualityImpact(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	payload, err := s.qualityTools.GetQualityImpact(ctx, raw)
	return payload, asToolError(err)
}

func (s *Server) runGetQualityConsumptionCoverage(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	payload, err := s.qualityTools.GetQualityConsumptionCoverage(ctx, raw)
	return payload, asToolError(err)
}
