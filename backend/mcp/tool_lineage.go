package mcp

import (
	"context"
	"encoding/json"

	"github.com/Ranxy/metaxisdata/backend/store"
)

// The lineage tool bodies live in component/tools/lineage so the built-in agent
// runs the same implementation.

func (s *Server) runAnalyzeSQL(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	payload, err := s.lineageTools.AnalyzeSQL(ctx, raw)
	return payload, asToolError(err)
}

func (s *Server) runGetLineageGraph(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	payload, err := s.lineageTools.GetLineageGraph(ctx, raw)
	return payload, asToolError(err)
}
