package v1

import (
	"context"

	"connectrpc.com/connect"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// CSV export handlers. The export streams one CSV document built from the same
// filters as the matching list RPC; the implementation lives in this file.

// ExportQualityExecutions streams the executions the request's filters select as
// one CSV document.
func (s *DataQualityService) ExportQualityExecutions(_ context.Context, _ *connect.Request[v1pb.ExportQualityExecutionsRequest], _ *connect.ServerStream[v1pb.ExportQualityDataChunk]) error {
	return connect.NewError(connect.CodeUnimplemented, nil)
}

// ExportQualityIssues streams the issues the request's filters select as one CSV
// document.
func (s *DataQualityService) ExportQualityIssues(_ context.Context, _ *connect.Request[v1pb.ExportQualityIssuesRequest], _ *connect.ServerStream[v1pb.ExportQualityDataChunk]) error {
	return connect.NewError(connect.CodeUnimplemented, nil)
}

// ExportQualityTargets streams the targets the request's filters select as one
// CSV document.
func (s *DataQualityService) ExportQualityTargets(_ context.Context, _ *connect.Request[v1pb.ExportQualityTargetsRequest], _ *connect.ServerStream[v1pb.ExportQualityDataChunk]) error {
	return connect.NewError(connect.CodeUnimplemented, nil)
}
