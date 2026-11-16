package mcp

import (
	"context"
	"errors"
	"strings"

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
func asToolError(err error) *toolError {
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

// resolveInstance accepts a resource name, a bare id, or a display title.
func (s *Server) resolveInstance(ctx context.Context, value string) (string, error) {
	name, err := s.resolver.ResolveInstance(ctx, value)
	if err != nil {
		return "", asToolError(err)
	}
	return name, nil
}

// resolveDatabase accepts a GUID, a resource name, or a bare database name.
func (s *Server) resolveDatabase(ctx context.Context, instanceName, value string) (string, error) {
	guid, err := s.resolver.ResolveDatabase(ctx, instanceName, value)
	if err != nil {
		return "", asToolError(err)
	}
	return guid, nil
}

// resolveSchema finds a schema by name under a database.
func (s *Server) resolveSchema(ctx context.Context, databaseGUID, value string) (string, error) {
	guid, err := s.resolver.ResolveSchema(ctx, databaseGUID, value)
	if err != nil {
		return "", asToolError(err)
	}
	return guid, nil
}

// scopeRequired answers a statement that arrived without a scope.
func (s *Server) scopeRequired(ctx context.Context) *toolError {
	failure := s.resolver.ScopeRequired(ctx)
	if failure == nil {
		return nil
	}
	return &toolError{
		Code:    failure.Code,
		Message: failure.Message,
		Hint:    failure.Hint,
		Details: failure.Details,
	}
}

// lastPathSegment reads the trailing name out of a resource name such as
// "instances/1/databases/shop".
func lastPathSegment(name string) string {
	if index := strings.LastIndex(name, "/"); index >= 0 {
		return name[index+1:]
	}
	return name
}

// storedMetadataName reads an object's name out of the stored payload.
func storedMetadataName(stored *v1pb.StoredMetadata) string {
	return metadata.StoredMetadataName(stored)
}
