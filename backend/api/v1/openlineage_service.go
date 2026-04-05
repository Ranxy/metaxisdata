package v1

import (
	"context"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// OpenLineageService implements the OpenLineageServiceHandler for managing namespace mappings and API keys.
type OpenLineageService struct {
	v1connect.UnimplementedOpenLineageServiceHandler
	store *store.Store
}

// NewOpenLineageService creates a new OpenLineageService.
func NewOpenLineageService(s *store.Store) *OpenLineageService {
	return &OpenLineageService{store: s}
}

func (s *OpenLineageService) CreateNamespaceMapping(ctx context.Context, req *connect.Request[v1pb.CreateNamespaceMappingRequest]) (*connect.Response[v1pb.NamespaceMappingResource], error) {
	mapping := req.Msg.GetMapping()
	if mapping.GetNamespace() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("namespace is required"))
	}
	if mapping.GetInstanceResourceId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("instance_resource_id is required"))
	}

	result, err := s.store.CreateNamespaceMapping(ctx, &store.NamespaceMappingMessage{
		Namespace:          mapping.GetNamespace(),
		InstanceResourceID: mapping.GetInstanceResourceId(),
		DatabaseName:       mapping.GetDatabaseName(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create namespace mapping"))
	}

	return connect.NewResponse(convertNamespaceMapping(result)), nil
}

func (s *OpenLineageService) ListNamespaceMapping(ctx context.Context, _ *connect.Request[v1pb.ListNamespaceMappingRequest]) (*connect.Response[v1pb.ListNamespaceMappingResponse], error) {
	list, err := s.store.ListNamespaceMapping(ctx, &store.FindNamespaceMappingMessage{})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list namespace mappings"))
	}

	resp := &v1pb.ListNamespaceMappingResponse{}
	for _, m := range list {
		resp.Mappings = append(resp.Mappings, convertNamespaceMapping(m))
	}
	return connect.NewResponse(resp), nil
}

func (s *OpenLineageService) UpdateNamespaceMapping(ctx context.Context, req *connect.Request[v1pb.UpdateNamespaceMappingRequest]) (*connect.Response[v1pb.NamespaceMappingResource], error) {
	mapping := req.Msg.GetMapping()
	result, err := s.store.UpdateNamespaceMapping(ctx, req.Msg.GetId(), &store.NamespaceMappingMessage{
		Namespace:          mapping.GetNamespace(),
		InstanceResourceID: mapping.GetInstanceResourceId(),
		DatabaseName:       mapping.GetDatabaseName(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update namespace mapping"))
	}
	return connect.NewResponse(convertNamespaceMapping(result)), nil
}

func (s *OpenLineageService) DeleteNamespaceMapping(ctx context.Context, req *connect.Request[v1pb.DeleteNamespaceMappingRequest]) (*connect.Response[emptypb.Empty], error) {
	if err := s.store.DeleteNamespaceMapping(ctx, req.Msg.GetId()); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to delete namespace mapping"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (s *OpenLineageService) CreateAPIKey(ctx context.Context, req *connect.Request[v1pb.CreateAPIKeyRequest]) (*connect.Response[v1pb.CreateAPIKeyResponse], error) {
	if req.Msg.GetDescription() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("description is required"))
	}

	user, ok := GetUserFromContext(ctx)
	createdBy := ""
	if ok && user != nil {
		createdBy = user.Email
	}

	plainKey, keyMsg, err := s.store.CreateOpenLineageAPIKey(ctx, req.Msg.GetDescription(), createdBy)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create API key"))
	}

	return connect.NewResponse(&v1pb.CreateAPIKeyResponse{
		Key:    plainKey,
		ApiKey: convertAPIKey(keyMsg),
	}), nil
}

func (s *OpenLineageService) ListAPIKey(ctx context.Context, _ *connect.Request[v1pb.ListAPIKeyRequest]) (*connect.Response[v1pb.ListAPIKeyResponse], error) {
	list, err := s.store.ListOpenLineageAPIKey(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list API keys"))
	}

	resp := &v1pb.ListAPIKeyResponse{}
	for _, k := range list {
		resp.ApiKeys = append(resp.ApiKeys, convertAPIKey(k))
	}
	return connect.NewResponse(resp), nil
}

func (s *OpenLineageService) RevokeAPIKey(ctx context.Context, req *connect.Request[v1pb.RevokeAPIKeyRequest]) (*connect.Response[emptypb.Empty], error) {
	if err := s.store.RevokeOpenLineageAPIKey(ctx, req.Msg.GetId()); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to revoke API key"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func convertNamespaceMapping(m *store.NamespaceMappingMessage) *v1pb.NamespaceMappingResource {
	return &v1pb.NamespaceMappingResource{
		Id:                 m.ID,
		Namespace:          m.Namespace,
		InstanceResourceId: m.InstanceResourceID,
		DatabaseName:       m.DatabaseName,
		CreatedAt:          timestamppb.New(m.CreatedAt),
		UpdatedAt:          timestamppb.New(m.UpdatedAt),
	}
}

func convertAPIKey(k *store.OpenLineageAPIKeyMessage) *v1pb.APIKeyResource {
	res := &v1pb.APIKeyResource{
		Id:          k.ID,
		Description: k.Description,
		CreatedBy:   k.CreatedBy,
		CreatedAt:   timestamppb.New(k.CreatedAt),
	}
	if k.RevokedAt != nil {
		res.RevokedAt = timestamppb.New(*k.RevokedAt)
	}
	return res
}
