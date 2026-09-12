package v1

import (
	"context"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// SettingService implements the setting service.
type SettingService struct {
	v1connect.UnimplementedSettingServiceHandler
	store *store.Store
}

// NewSettingService creates a new SettingService.
func NewSettingService(store *store.Store) *SettingService {
	return &SettingService{store: store}
}

// GetWorkspaceProfileSetting gets the workspace profile setting.
func (s *SettingService) GetWorkspaceProfileSetting(ctx context.Context, _ *connect.Request[v1pb.GetWorkspaceProfileSettingRequest]) (*connect.Response[v1pb.WorkspaceProfileSetting], error) {
	setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get workspace profile setting"))
	}
	return connect.NewResponse(convertToWorkspaceProfileSetting(setting)), nil
}

// UpdateWorkspaceProfileSetting updates the workspace profile setting.
func (s *SettingService) UpdateWorkspaceProfileSetting(ctx context.Context, request *connect.Request[v1pb.UpdateWorkspaceProfileSettingRequest]) (*connect.Response[v1pb.WorkspaceProfileSetting], error) {
	if request.Msg.Setting == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("setting must be set"))
	}
	if request.Msg.UpdateMask == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("update_mask must be set"))
	}

	setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get workspace profile setting"))
	}
	for _, path := range request.Msg.UpdateMask.Paths {
		switch path {
		case "external_url":
			setting.ExternalUrl = request.Msg.Setting.ExternalUrl
		case "disallow_signup":
			setting.DisallowSignup = request.Msg.Setting.DisallowSignup
		case "disallow_password_signin":
			setting.DisallowPasswordSignin = request.Msg.Setting.DisallowPasswordSignin
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported update_mask %q", path))
		}
	}

	payload, err := protojson.Marshal(setting)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to marshal workspace profile setting"))
	}
	if _, err := s.store.UpsertSetting(ctx, &store.SetSettingMessage{
		Name:  storepb.SettingName_WORKSPACE_PROFILE,
		Value: string(payload),
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update workspace profile setting"))
	}
	return connect.NewResponse(convertToWorkspaceProfileSetting(setting)), nil
}

func convertToWorkspaceProfileSetting(setting *storepb.WorkspaceProfileSetting) *v1pb.WorkspaceProfileSetting {
	return &v1pb.WorkspaceProfileSetting{
		ExternalUrl:            setting.GetExternalUrl(),
		DisallowSignup:         setting.GetDisallowSignup(),
		DisallowPasswordSignin: setting.GetDisallowPasswordSignin(),
	}
}
