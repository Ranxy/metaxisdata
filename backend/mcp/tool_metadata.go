package mcp

import (
	"context"
	"encoding/json"

	"github.com/Ranxy/metaxisdata/backend/store"
)

// The metadata tools' bodies live in component/tools/metadata so the built-in
// agent runs the same implementation. These wrappers keep the transport's
// signature and map the shared error envelope onto the MCP one.

func (s *Server) runListInstances(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	payload, err := s.catalog.ListInstances(ctx, raw)
	return payload, asToolError(err)
}

func (s *Server) runListDatabases(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	payload, err := s.catalog.ListDatabases(ctx, raw)
	return payload, asToolError(err)
}

func (s *Server) runSearchMetadata(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	payload, err := s.catalog.SearchMetadata(ctx, raw)
	return payload, asToolError(err)
}

func (s *Server) runListMetadata(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	payload, err := s.catalog.ListMetadata(ctx, raw)
	return payload, asToolError(err)
}

func (s *Server) runGetMetadata(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	payload, err := s.catalog.GetMetadata(ctx, raw)
	return payload, asToolError(err)
}

func (s *Server) runGetDDL(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	payload, err := s.catalog.GetDDL(ctx, raw)
	return payload, asToolError(err)
}

func (s *Server) runWhoami(ctx context.Context, _ *store.UserMessage, _ json.RawMessage) (any, error) {
	payload, err := s.catalog.Whoami(ctx)
	return payload, asToolError(err)
}
