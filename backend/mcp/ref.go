package mcp

import (
	"context"
	"errors"

	"github.com/Ranxy/metaxisdata/backend/component/tools/metadata"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// Name addressing is shared with the built-in agent through
// component/tools/metadata. The types are aliases so this package's tools and
// tests keep reading the same shapes, and the wrappers keep the error envelope
// mapping in one place.
type (
	objectRef      = metadata.ObjectRef
	resolvedObject = metadata.Resolved
	analysisScope  = metadata.AnalysisScope
)

// asToolError maps a shared resolver failure onto the MCP error envelope. A
// failure that is already an MCP tool error (or a plain one) is preserved.
// It returns the error interface so a nil failure stays nil instead of becoming a
// typed nil in a caller's return value.
func asToolError(err error) error {
	if err == nil {
		return nil
	}
	var failure *toolError
	if errors.As(err, &failure) {
		return failure
	}
	var metaFailure *metadata.Error
	if errors.As(err, &metaFailure) {
		return &toolError{
			Code:    metaFailure.Code,
			Message: metaFailure.Message,
			Hint:    metaFailure.Hint,
			Details: metaFailure.Details,
		}
	}
	return internalFailure(err)
}

// resolveObject turns an object reference into the GUID the read RPCs take.
func (s *Server) resolveObject(ctx context.Context, ref objectRef, metaType v1pb.MetaType) (resolvedObject, error) {
	resolved, err := s.resolver.ResolveObject(ctx, ref, metaType)
	if err != nil {
		return resolvedObject{}, asToolError(err)
	}
	return resolved, nil
}

// resolveScope turns one scope into the GUID the SQL analyzer resolves unqualified
// names against.
func (s *Server) resolveScope(ctx context.Context, scope analysisScope) (string, error) {
	guid, err := s.resolver.ResolveScope(ctx, scope)
	if err != nil {
		return "", asToolError(err)
	}
	return guid, nil
}
