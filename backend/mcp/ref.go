package mcp

import (
	"errors"

	"github.com/Ranxy/metaxisdata/backend/component/tools/metadata"
)

// Name addressing lives in component/tools/metadata. The aliases keep this
// package's schema tests reading the same shapes; asToolError maps the shared
// failure onto the MCP envelope.
type (
	objectRef     = metadata.ObjectRef
	analysisScope = metadata.AnalysisScope
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
