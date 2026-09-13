package v1

import (
	"context"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// EnvironmentService implements the environment service.
type EnvironmentService struct {
	v1connect.UnimplementedEnvironmentServiceHandler
	store *store.Store
}

// NewEnvironmentService creates a new EnvironmentService.
func NewEnvironmentService(store *store.Store) *EnvironmentService {
	return &EnvironmentService{store: store}
}

// ListEnvironments lists the workspace environments.
func (s *EnvironmentService) ListEnvironments(ctx context.Context, req *connect.Request[v1pb.ListEnvironmentsRequest]) (*connect.Response[v1pb.ListEnvironmentsResponse], error) {
	size := int(req.Msg.GetPageSize())
	if size <= 0 {
		size = 50
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.GetPageToken(),
		limit:   size,
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}

	environments, err := s.store.ListEnvironments(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list environments"))
	}
	counts, err := s.store.CountInstancesByEnvironments(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to count instances by environment"))
	}
	// The list is materialized from the setting blob, so page it in memory.
	environments, nextPageToken, err := paginateInMemory(environments, offset)
	if err != nil {
		return nil, err
	}

	pbEnvironments := make([]*v1pb.Environment, 0, len(environments))
	for _, environment := range environments {
		pbEnvironment := convertToEnvironment(environment)
		pbEnvironment.InstanceCount = int32(counts[environment.GetId()])
		pbEnvironments = append(pbEnvironments, pbEnvironment)
	}
	return connect.NewResponse(&v1pb.ListEnvironmentsResponse{
		Environments:  pbEnvironments,
		NextPageToken: nextPageToken,
	}), nil
}

// CreateEnvironment creates an environment.
func (s *EnvironmentService) CreateEnvironment(ctx context.Context, req *connect.Request[v1pb.CreateEnvironmentRequest]) (*connect.Response[v1pb.Environment], error) {
	if req.Msg.Environment == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("environment must be set"))
	}
	// The name and the color are the server's to derive; whatever the client
	// sent for them is ignored.
	created, err := s.store.CreateEnvironment(ctx, &store.CreateEnvironmentMessage{
		Title: req.Msg.Environment.GetTitle(),
		Color: req.Msg.Environment.GetColor(),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertToEnvironment(created)), nil
}

// UpdateEnvironment updates an environment.
func (s *EnvironmentService) UpdateEnvironment(ctx context.Context, req *connect.Request[v1pb.UpdateEnvironmentRequest]) (*connect.Response[v1pb.Environment], error) {
	if req.Msg.Environment == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("environment must be set"))
	}
	if req.Msg.UpdateMask == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("update_mask must be set"))
	}
	id, err := common.GetEnvironmentID(req.Msg.Environment.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	patch := &store.EnvironmentPatch{}
	for _, path := range req.Msg.UpdateMask.GetPaths() {
		switch path {
		case "title":
			title := req.Msg.Environment.GetTitle()
			patch.Title = &title
		case "color":
			color := req.Msg.Environment.GetColor()
			patch.Color = &color
		case "tags":
			patch.Tags = req.Msg.Environment.GetTags()
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf(`unsupported update_mask "%s"`, path))
		}
	}

	updated, err := s.store.UpdateEnvironment(ctx, id, patch)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertToEnvironment(updated)), nil
}

// DeleteEnvironment deletes an environment.
func (s *EnvironmentService) DeleteEnvironment(ctx context.Context, req *connect.Request[v1pb.DeleteEnvironmentRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := common.GetEnvironmentID(req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	// Instances store the raw id, so deleting an environment that is still
	// assigned would leave those rows pointing at a value that no longer
	// resolves. Refuse instead of creating dangling references.
	count, err := s.store.CountInstancesByEnvironment(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to count instances for environment"))
	}
	if count > 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf("environment %q is used by %d instance(s)", id, count))
	}
	if err := s.store.DeleteEnvironment(ctx, id); err != nil {
		return nil, err
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func convertToEnvironment(environment *storepb.EnvironmentSetting_Environment) *v1pb.Environment {
	return &v1pb.Environment{
		Name:  common.FormatEnvironment(environment.GetId()),
		Title: environment.GetTitle(),
		Color: environment.GetColor(),
		Tags:  environment.GetTags(),
	}
}
