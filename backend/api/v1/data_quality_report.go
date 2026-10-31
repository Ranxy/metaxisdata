package v1

import (
	"context"

	"connectrpc.com/connect"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// ExportQualityReport renders one target's quality report as a self-contained
// HTML document. The implementation lives in this file.
func (*DataQualityService) ExportQualityReport(_ context.Context, _ *connect.Request[v1pb.ExportQualityReportRequest]) (*connect.Response[v1pb.ExportQualityReportResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}
