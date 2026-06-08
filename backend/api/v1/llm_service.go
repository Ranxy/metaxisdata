package v1

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// LLMService implements the LLM service.
type LLMService struct {
	v1connect.UnimplementedLLMServiceHandler
	store    *store.Store
	registry *llm.Registry
}

// NewLLMService creates a new LLMService.
func NewLLMService(st *store.Store, registry *llm.Registry) *LLMService {
	return &LLMService{store: st, registry: registry}
}

// ---- builtin catalog ----

func builtinDefinitions() []*v1pb.LlmProviderDefinition {
	return []*v1pb.LlmProviderDefinition{
		{Id: "openai", Label: "OpenAI", Description: "OpenAI GPT models.", DefaultBaseUrl: "https://api.openai.com"},
		{Id: "deepseek", Label: "DeepSeek", Description: "DeepSeek AI models.", DefaultBaseUrl: "https://api.deepseek.com"},
		{Id: "openrouter", Label: "OpenRouter", Description: "OpenRouter aggregates hundreds of models from multiple providers via a single API.", DefaultBaseUrl: "https://openrouter.ai/api"},
		{Id: "custom", Label: "Custom", Description: "User-defined OpenAI-compatible API endpoint.", DefaultBaseUrl: ""},
	}
}

func getDefaultBaseURL(providerID string) string {
	for _, d := range builtinDefinitions() {
		if d.Id == providerID {
			return d.DefaultBaseUrl
		}
	}
	return ""
}

// ---- RPC handlers ----

func (s *LLMService) ListLLMProviderProfiles(ctx context.Context, req *connect.Request[v1pb.ListLLMProviderProfilesRequest]) (*connect.Response[v1pb.ListLLMProviderProfilesResponse], error) {
	limit := int(req.Msg.PageSize)
	if limit <= 0 {
		limit = 50
	}
	offset := 0

	profiles, err := s.store.ListLLMProfiles(ctx, &store.FindLLMProfileMessage{
		Limit:  &limit,
		Offset: &offset,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list LLM profiles"))
	}

	pbProfiles := make([]*v1pb.LlmProviderProfile, 0, len(profiles))
	for _, p := range profiles {
		pbProfiles = append(pbProfiles, convertProfileToV1(p))
	}

	return connect.NewResponse(&v1pb.ListLLMProviderProfilesResponse{
		Profiles:    pbProfiles,
		Definitions: builtinDefinitions(),
	}), nil
}

func (s *LLMService) CreateLLMProviderProfile(ctx context.Context, req *connect.Request[v1pb.CreateLLMProviderProfileRequest]) (*connect.Response[v1pb.LlmProviderProfile], error) {
	pbProfile := req.Msg.Profile
	if pbProfile == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("profile is required"))
	}
	if pbProfile.Type == v1pb.LLMProviderType_LLM_PROVIDER_TYPE_UNSPECIFIED {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("provider type is required"))
	}

	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to generate resource ID"))
	}

	baseURL := pbProfile.BaseUrl
	if baseURL == "" {
		baseURL = getDefaultBaseURL(providerTypeToString(pbProfile.Type))
	}

	title := pbProfile.Title
	if title == "" {
		title = autoGenerateTitle(pbProfile)
	}

	meta := &storepb.LlmProviderProfile{
		Title:           title,
		Type:            convertV1LLMProviderType(pbProfile.Type),
		BaseUrl:         baseURL,
		ApiKeyEncrypted: pbProfile.ApiKey,
		Models:          convertV1ModelsToStore(pbProfile.Models),
	}

	msg, err := s.store.CreateLLMProfile(ctx, resourceID, meta)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create LLM profile"))
	}

	return connect.NewResponse(convertProfileToV1(msg)), nil
}

func (s *LLMService) UpdateLLMProviderProfile(ctx context.Context, req *connect.Request[v1pb.UpdateLLMProviderProfileRequest]) (*connect.Response[v1pb.LlmProviderProfile], error) {
	pbProfile := req.Msg.Profile
	if pbProfile == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("profile is required"))
	}

	resourceID := extractLLMProfileResourceID(pbProfile.Name)
	if resourceID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid profile name"))
	}

	update := &store.UpdateLLMProfileMessage{ResourceID: resourceID}

	if len(req.Msg.UpdateMask.GetPaths()) == 0 {
		title := pbProfile.Title
		update.Title = &title
		baseURL := pbProfile.BaseUrl
		update.BaseURL = &baseURL
		update.Models = convertV1ModelsToStore(pbProfile.Models)
		if pbProfile.ApiKey != "" {
			apiKey := pbProfile.ApiKey
			update.APIKey = &apiKey
		}
	} else {
		for _, path := range req.Msg.UpdateMask.GetPaths() {
			switch path {
			case "title":
				title := pbProfile.Title
				update.Title = &title
			case "base_url":
				baseURL := pbProfile.BaseUrl
				update.BaseURL = &baseURL
			case "models":
				update.Models = convertV1ModelsToStore(pbProfile.Models)
			case "api_key":
				if pbProfile.ApiKey != "" {
					apiKey := pbProfile.ApiKey
					update.APIKey = &apiKey
				}
			default:
			}
		}
	}

	msg, err := s.store.UpdateLLMProfile(ctx, update)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update LLM profile"))
	}

	return connect.NewResponse(convertProfileToV1(msg)), nil
}

func (s *LLMService) DeleteLLMProviderProfile(ctx context.Context, req *connect.Request[v1pb.DeleteLLMProviderProfileRequest]) (*connect.Response[emptypb.Empty], error) {
	resourceID := extractLLMProfileResourceID(req.Msg.Name)
	if resourceID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid profile name"))
	}

	if err := s.store.DeleteLLMProfile(ctx, resourceID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to delete LLM profile"))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (s *LLMService) FetchLLMModels(ctx context.Context, req *connect.Request[v1pb.FetchLLMModelsRequest]) (*connect.Response[v1pb.FetchLLMModelsResponse], error) {
	var baseURL, apiKey string

	if req.Msg.Name != "" {
		resourceID := extractLLMProfileResourceID(req.Msg.Name)
		if resourceID == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid profile name"))
		}
		prof, err := s.store.GetLLMProfile(ctx, &store.FindLLMProfileMessage{ResourceID: &resourceID})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get LLM profile"))
		}
		if prof == nil {
			return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("LLM profile %q not found", req.Msg.Name))
		}
		baseURL = prof.Metadata.BaseUrl
		apiKey = req.Msg.ApiKey
		if apiKey == "" {
			apiKey = prof.Metadata.ApiKeyEncrypted
		}
	} else if req.Msg.ProviderType != v1pb.LLMProviderType_LLM_PROVIDER_TYPE_UNSPECIFIED {
		baseURL = getDefaultBaseURL(providerTypeToString(req.Msg.ProviderType))
		if baseURL == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cannot resolve base URL for the given provider type"))
		}
		apiKey = req.Msg.ApiKey
	} else {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("either name or provider_type is required"))
	}

	if baseURL == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("base_url is required"))
	}

	modelIDs, err := llm.FetchModels(ctx, baseURL, apiKey)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to fetch models"))
	}

	return connect.NewResponse(&v1pb.FetchLLMModelsResponse{
		ModelIds: modelIDs,
	}), nil
}

// ---- conversion helpers ----

func convertProfileToV1(msg *store.LLMProfileMessage) *v1pb.LlmProviderProfile {
	if msg == nil {
		return nil
	}
	meta := msg.Metadata
	return &v1pb.LlmProviderProfile{
		Name:         meta.Name,
		Title:        meta.Title,
		Type:         convertLLMProviderType(meta.Type),
		BaseUrl:      meta.BaseUrl,
		Models:       convertStoreModelsToV1(meta.Models),
		CreateTime:   meta.CreateTime,
		UpdateTime:   meta.UpdateTime,
		MaskedApiKey: maskAPIKey(meta.ApiKeyEncrypted),
	}
}

func maskAPIKey(key string) string {
	if key == "" {
		return ""
	}
	runes := []rune(key)
	n := len(runes)
	if n <= 8 {
		return strings.Repeat("*", n)
	}
	return string(runes[:3]) + strings.Repeat("*", n-6) + string(runes[n-3:])
}

func convertV1ModelsToStore(pbModels []*v1pb.LlmProviderModel) []*storepb.LlmProviderModel {
	models := make([]*storepb.LlmProviderModel, 0, len(pbModels))
	for _, m := range pbModels {
		models = append(models, &storepb.LlmProviderModel{
			Name:    m.Name,
			Enabled: m.Enabled,
		})
	}
	return models
}

func convertStoreModelsToV1(models []*storepb.LlmProviderModel) []*v1pb.LlmProviderModel {
	v1Models := make([]*v1pb.LlmProviderModel, 0, len(models))
	for _, m := range models {
		v1Models = append(v1Models, &v1pb.LlmProviderModel{
			Name:    m.Name,
			Enabled: m.Enabled,
		})
	}
	return v1Models
}

func convertLLMProviderType(t storepb.LLMProviderType) v1pb.LLMProviderType {
	switch t {
	case storepb.LLMProviderType_LLM_PROVIDER_TYPE_OPENAI:
		return v1pb.LLMProviderType_LLM_PROVIDER_TYPE_OPENAI
	case storepb.LLMProviderType_LLM_PROVIDER_TYPE_DEEPSEEK:
		return v1pb.LLMProviderType_LLM_PROVIDER_TYPE_DEEPSEEK
	case storepb.LLMProviderType_LLM_PROVIDER_TYPE_OPENROUTER:
		return v1pb.LLMProviderType_LLM_PROVIDER_TYPE_OPENROUTER
	case storepb.LLMProviderType_LLM_PROVIDER_TYPE_CUSTOM:
		return v1pb.LLMProviderType_LLM_PROVIDER_TYPE_CUSTOM
	default:
		return v1pb.LLMProviderType_LLM_PROVIDER_TYPE_UNSPECIFIED
	}
}

func convertV1LLMProviderType(t v1pb.LLMProviderType) storepb.LLMProviderType {
	switch t {
	case v1pb.LLMProviderType_LLM_PROVIDER_TYPE_OPENAI:
		return storepb.LLMProviderType_LLM_PROVIDER_TYPE_OPENAI
	case v1pb.LLMProviderType_LLM_PROVIDER_TYPE_DEEPSEEK:
		return storepb.LLMProviderType_LLM_PROVIDER_TYPE_DEEPSEEK
	case v1pb.LLMProviderType_LLM_PROVIDER_TYPE_OPENROUTER:
		return storepb.LLMProviderType_LLM_PROVIDER_TYPE_OPENROUTER
	case v1pb.LLMProviderType_LLM_PROVIDER_TYPE_CUSTOM:
		return storepb.LLMProviderType_LLM_PROVIDER_TYPE_CUSTOM
	default:
		return storepb.LLMProviderType_LLM_PROVIDER_TYPE_UNSPECIFIED
	}
}

func providerTypeToString(t v1pb.LLMProviderType) string {
	switch t {
	case v1pb.LLMProviderType_LLM_PROVIDER_TYPE_OPENAI:
		return "openai"
	case v1pb.LLMProviderType_LLM_PROVIDER_TYPE_DEEPSEEK:
		return "deepseek"
	case v1pb.LLMProviderType_LLM_PROVIDER_TYPE_OPENROUTER:
		return "openrouter"
	case v1pb.LLMProviderType_LLM_PROVIDER_TYPE_CUSTOM:
		return "custom"
	default:
		return ""
	}
}

func autoGenerateTitle(pb *v1pb.LlmProviderProfile) string {
	label := providerTypeToString(pb.Type)
	modelNames := make([]string, 0, len(pb.Models))
	for _, m := range pb.Models {
		if m.Enabled {
			modelNames = append(modelNames, m.Name)
		}
	}
	if len(modelNames) > 0 {
		return fmt.Sprintf("%s — %s", label, stringsJoin(modelNames, ", "))
	}
	return label
}

func stringsJoin(parts []string, sep string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}

func extractLLMProfileResourceID(name string) string {
	const prefix = "llm-provider-profiles/"
	if len(name) > len(prefix) && name[:len(prefix)] == prefix {
		return name[len(prefix):]
	}
	return ""
}
