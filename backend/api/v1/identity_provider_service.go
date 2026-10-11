package v1

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/plugin/idp/oauth2"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// identityProviderIDMatcher is the shape of the id that becomes the last segment
// of `idps/{idp}`. It is the same shape a role id uses, and it is enforced here
// rather than derived silently: a caller that sends the wrong id should be told,
// not given a different provider than it asked for.
var identityProviderIDMatcher = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)

// IdentityProviderService implements the identity provider management API. It is
// the write path the `idp` table never had: until it existed a provider could
// only be configured by hand-written SQL.
type IdentityProviderService struct {
	v1connect.UnimplementedIdentityProviderServiceHandler
	store *store.Store
}

// NewIdentityProviderService creates a new IdentityProviderService.
func NewIdentityProviderService(stores *store.Store) *IdentityProviderService {
	return &IdentityProviderService{store: stores}
}

var _ v1connect.IdentityProviderServiceHandler = (*IdentityProviderService)(nil)

// GetIdentityProvider gets an identity provider by name.
func (s *IdentityProviderService) GetIdentityProvider(ctx context.Context, req *connect.Request[v1pb.GetIdentityProviderRequest]) (*connect.Response[v1pb.IdentityProvider], error) {
	message, err := s.getIdentityProviderMessage(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertToIdentityProvider(message)), nil
}

// ListIdentityProviders lists every configured identity provider. It is public so
// the login page can render the SSO targets before a session exists; the client
// secret is never part of the answer.
func (s *IdentityProviderService) ListIdentityProviders(ctx context.Context, _ *connect.Request[v1pb.ListIdentityProvidersRequest]) (*connect.Response[v1pb.ListIdentityProvidersResponse], error) {
	messages, err := s.store.ListIdentityProviders(ctx, &store.FindIdentityProviderMessage{})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list identity providers"))
	}
	response := &v1pb.ListIdentityProvidersResponse{}
	for _, message := range messages {
		response.IdentityProviders = append(response.IdentityProviders, convertToIdentityProvider(message))
	}
	return connect.NewResponse(response), nil
}

// CreateIdentityProvider creates an identity provider.
func (s *IdentityProviderService) CreateIdentityProvider(ctx context.Context, req *connect.Request[v1pb.CreateIdentityProviderRequest]) (*connect.Response[v1pb.IdentityProvider], error) {
	identityProvider := req.Msg.GetIdentityProvider()
	if identityProvider == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("identity_provider must be set"))
	}
	idpID := req.Msg.GetIdentityProviderId()
	if !identityProviderIDMatcher.MatchString(idpID) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid identity provider ID %q: it must start with a lower-case letter and contain only lower-case letters, digits and hyphens", idpID))
	}
	if domain := identityProvider.GetDomain(); domain != "" && strings.ToLower(domain) != domain {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("domain must be lower-case"))
	}
	if err := validateIdentityProviderConfig(identityProvider.GetType(), identityProvider.GetConfig(), true); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	created, err := s.store.CreateIdentityProvider(ctx, &store.IdentityProviderMessage{
		ResourceID: idpID,
		Title:      identityProvider.GetTitle(),
		Domain:     identityProvider.GetDomain(),
		Type:       toStoreIdentityProviderType(identityProvider.GetType()),
		Config:     convertIdentityProviderConfigToStore(identityProvider.GetConfig()),
	})
	if err != nil {
		return nil, connectErrorFromStore(err, "failed to create identity provider")
	}
	return connect.NewResponse(convertToIdentityProvider(created)), nil
}

// UpdateIdentityProvider updates an identity provider.
func (s *IdentityProviderService) UpdateIdentityProvider(ctx context.Context, req *connect.Request[v1pb.UpdateIdentityProviderRequest]) (*connect.Response[v1pb.IdentityProvider], error) {
	identityProvider := req.Msg.GetIdentityProvider()
	if identityProvider == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("identity_provider must be set"))
	}
	if req.Msg.GetUpdateMask() == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("update_mask must be set"))
	}
	current, err := s.getIdentityProviderMessage(ctx, identityProvider.GetName())
	if err != nil {
		return nil, err
	}

	patch := &store.UpdateIdentityProviderMessage{ResourceID: current.ResourceID}
	for _, path := range req.Msg.GetUpdateMask().GetPaths() {
		switch path {
		case "title":
			title := identityProvider.GetTitle()
			patch.Title = &title
		case "domain":
			domain := identityProvider.GetDomain()
			if domain != "" && strings.ToLower(domain) != domain {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("domain must be lower-case"))
			}
			patch.Domain = &domain
		case "config":
			config := identityProvider.GetConfig()
			// The read path never hands the stored secret back, so an update that
			// does not replace it sends an empty one. An empty secret therefore
			// means "keep the stored secret", which is the only value a client
			// that loaded the provider first could have sent.
			if config != nil && config.GetOauth2Config() != nil && config.GetOauth2Config().GetClientSecret() == "" {
				config.GetOauth2Config().ClientSecret = current.Config.GetOauth2Config().GetClientSecret()
			}
			if err := validateIdentityProviderConfig(toV1IdentityProviderType(current.Type), config, true); err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, err)
			}
			patch.Config = convertIdentityProviderConfigToStore(config)
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("the field %q is not mutable", path))
		}
	}

	updated, err := s.store.UpdateIdentityProvider(ctx, patch)
	if err != nil {
		return nil, connectErrorFromStore(err, "failed to update identity provider")
	}
	return connect.NewResponse(convertToIdentityProvider(updated)), nil
}

// DeleteIdentityProvider deletes an identity provider.
func (s *IdentityProviderService) DeleteIdentityProvider(ctx context.Context, req *connect.Request[v1pb.DeleteIdentityProviderRequest]) (*connect.Response[emptypb.Empty], error) {
	idpID, err := common.GetIdentityProviderID(req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "failed to get identity provider ID"))
	}
	if err := s.store.DeleteIdentityProvider(ctx, idpID); err != nil {
		return nil, connectErrorFromStore(err, "failed to delete identity provider")
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// TestIdentityProvider completes a real authorization-code exchange against a
// stored provider and reports the identity it resolved.
//
// It runs the same preconditions a login runs, including the subject-mapping
// check, so a provider that would refuse every login is reported here rather than
// after an administrator tells somebody to try again. The credentials themselves
// have to be sound for the exchange to get this far.
func (s *IdentityProviderService) TestIdentityProvider(ctx context.Context, req *connect.Request[v1pb.TestIdentityProviderRequest]) (*connect.Response[v1pb.TestIdentityProviderResponse], error) {
	message, err := s.getIdentityProviderMessage(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	if message.Type != storepb.IdentityProviderType_OAUTH2 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf("identity provider type %s is not supported", message.Type.String()))
	}
	oauth2Context := req.Msg.GetOauth2Context()
	if oauth2Context == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("oauth2_context must be set"))
	}

	setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get workspace setting"))
	}
	// The mapping check mirrors the login path: report the same refusal here, with
	// the same remedies, instead of letting the exchange succeed and the login
	// fail afterwards.
	allowEmailIdentity := false
	for _, listed := range setting.GetSsoEmailIdentityIdps() {
		if listed == message.ResourceID {
			allowEmailIdentity = true
			break
		}
	}
	if err := validateIDPSubjectMapping(message.Config.GetOauth2Config().GetFieldMapping(), allowEmailIdentity); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Wrapf(err, "identity provider %q cannot resolve logins", message.Title))
	}

	if setting.GetExternalUrl() == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("external URL is not configured: the callback URL cannot be built without it"))
	}
	identityProvider, err := oauth2.NewIdentityProvider(message.Config.GetOauth2Config())
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Wrap(err, "failed to build the OAuth2 identity provider"))
	}

	redirectURL := fmt.Sprintf("%s/oauth/callback", setting.GetExternalUrl())
	token, err := identityProvider.ExchangeToken(ctx, redirectURL, oauth2Context.GetCode(), oauth2Context.GetCodeVerifier())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to exchange the authorization code"))
	}
	userInfo, _, err := identityProvider.UserInfo(ctx, token)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the user info"))
	}
	if userInfo == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("identity provider %q returned no user info", message.Title))
	}

	return connect.NewResponse(&v1pb.TestIdentityProviderResponse{
		UserInfo: convertIdentityProviderUserInfo(userInfo),
	}), nil
}

// ListIdentityProviderBindings lists which account is bound to which provider
// subject.
func (s *IdentityProviderService) ListIdentityProviderBindings(ctx context.Context, req *connect.Request[v1pb.ListIdentityProviderBindingsRequest]) (*connect.Response[v1pb.ListIdentityProviderBindingsResponse], error) {
	find := &store.FindIdentityProviderBindingMessage{}
	if req.Msg.GetIdp() != "" {
		idpID, err := common.GetIdentityProviderID(req.Msg.GetIdp())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "failed to get identity provider ID"))
		}
		find.IdpResourceID = &idpID
	}
	if req.Msg.GetUser() != "" {
		userID, err := common.GetUserID(req.Msg.GetUser())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "failed to get user ID"))
		}
		find.PrincipalID = &userID
	}

	bindings, err := s.store.ListIdentityProviderBindings(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list identity provider bindings"))
	}
	response := &v1pb.ListIdentityProviderBindingsResponse{}
	for _, binding := range bindings {
		response.IdentityProviderBindings = append(response.IdentityProviderBindings, &v1pb.IdentityProviderBinding{
			User:      common.FormatUserUID(binding.PrincipalID),
			UserEmail: binding.UserEmail,
			UserTitle: binding.UserTitle,
			Idp:       common.FormatIdentityProviderUID(binding.IdpResourceID),
			Subject:   binding.Subject,
			CreatedAt: timestamppb.New(binding.CreatedAt),
		})
	}
	return connect.NewResponse(response), nil
}

// DeleteIdentityProviderBinding unbinds one account from one provider subject.
func (s *IdentityProviderService) DeleteIdentityProviderBinding(ctx context.Context, req *connect.Request[v1pb.DeleteIdentityProviderBindingRequest]) (*connect.Response[emptypb.Empty], error) {
	userID, err := common.GetUserID(req.Msg.GetUser())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "failed to get user ID"))
	}
	idpID, err := common.GetIdentityProviderID(req.Msg.GetIdp())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "failed to get identity provider ID"))
	}
	if req.Msg.GetSubject() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("subject must be set"))
	}
	if err := s.store.DeleteIdentityProviderBinding(ctx, userID, idpID, req.Msg.GetSubject()); err != nil {
		return nil, connectErrorFromStore(err, "failed to delete identity provider binding")
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (s *IdentityProviderService) getIdentityProviderMessage(ctx context.Context, name string) (*store.IdentityProviderMessage, error) {
	idpID, err := common.GetIdentityProviderID(name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "failed to get identity provider ID"))
	}
	message, err := s.store.GetIdentityProvider(ctx, &store.FindIdentityProviderMessage{ResourceID: &idpID})
	if err != nil {
		return nil, connectErrorFromStore(err, "failed to get identity provider")
	}
	if message == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("identity provider %q not found", name))
	}
	return message, nil
}

// connectErrorFromStore maps the store's common error codes onto Connect codes,
// so a duplicate provider is AlreadyExists and a missing one is NotFound instead
// of both being Internal.
func connectErrorFromStore(err error, message string) error {
	code := connect.CodeInternal
	var commonErr *common.Error
	if errors.As(err, &commonErr) {
		switch commonErr.Code {
		case common.NotFound:
			code = connect.CodeNotFound
		case common.Conflict:
			code = connect.CodeAlreadyExists
		case common.Invalid:
			code = connect.CodeInvalidArgument
		default:
			code = connect.CodeInternal
		}
	}
	return connect.NewError(code, errors.Wrap(err, message))
}

// convertToIdentityProvider converts the stored message into the API shape. The
// client secret is deliberately dropped: the read path never hands it back, which
// is what makes "an empty secret on update keeps the stored one" meaningful.
func convertToIdentityProvider(message *store.IdentityProviderMessage) *v1pb.IdentityProvider {
	if message == nil {
		return nil
	}
	return &v1pb.IdentityProvider{
		Name:   common.FormatIdentityProviderUID(message.ResourceID),
		Title:  message.Title,
		Domain: message.Domain,
		Type:   toV1IdentityProviderType(message.Type),
		Config: convertIdentityProviderConfigFromStore(message.Config),
	}
}

func convertIdentityProviderConfigFromStore(config *storepb.IdentityProviderConfig) *v1pb.IdentityProviderConfig {
	if oauth2Config := config.GetOauth2Config(); oauth2Config != nil {
		return &v1pb.IdentityProviderConfig{
			Config: &v1pb.IdentityProviderConfig_Oauth2Config{
				Oauth2Config: &v1pb.OAuth2IdentityProviderConfig{
					AuthUrl:       oauth2Config.GetAuthUrl(),
					TokenUrl:      oauth2Config.GetTokenUrl(),
					UserInfoUrl:   oauth2Config.GetUserInfoUrl(),
					ClientId:      oauth2Config.GetClientId(),
					ClientSecret:  "",
					Scopes:        oauth2Config.GetScopes(),
					FieldMapping:  convertFieldMappingFromStore(oauth2Config.GetFieldMapping()),
					SkipTlsVerify: oauth2Config.GetSkipTlsVerify(),
					AuthStyle:     v1pb.OAuth2AuthStyle(oauth2Config.GetAuthStyle()),
				},
			},
		}
	}
	return nil
}

func convertIdentityProviderConfigToStore(config *v1pb.IdentityProviderConfig) *storepb.IdentityProviderConfig {
	if oauth2Config := config.GetOauth2Config(); oauth2Config != nil {
		return &storepb.IdentityProviderConfig{
			Config: &storepb.IdentityProviderConfig_Oauth2Config{
				Oauth2Config: &storepb.OAuth2IdentityProviderConfig{
					AuthUrl:       oauth2Config.GetAuthUrl(),
					TokenUrl:      oauth2Config.GetTokenUrl(),
					UserInfoUrl:   oauth2Config.GetUserInfoUrl(),
					ClientId:      oauth2Config.GetClientId(),
					ClientSecret:  oauth2Config.GetClientSecret(),
					Scopes:        oauth2Config.GetScopes(),
					FieldMapping:  convertFieldMappingToStore(oauth2Config.GetFieldMapping()),
					SkipTlsVerify: oauth2Config.GetSkipTlsVerify(),
					AuthStyle:     storepb.OAuth2AuthStyle(oauth2Config.GetAuthStyle()),
				},
			},
		}
	}
	return nil
}

func convertFieldMappingFromStore(mapping *storepb.FieldMapping) *v1pb.FieldMapping {
	if mapping == nil {
		return nil
	}
	return &v1pb.FieldMapping{
		Identifier:  mapping.GetIdentifier(),
		DisplayName: mapping.GetDisplayName(),
		Phone:       mapping.GetPhone(),
		Subject:     mapping.GetSubject(),
		Groups:      mapping.GetGroups(),
	}
}

func convertFieldMappingToStore(mapping *v1pb.FieldMapping) *storepb.FieldMapping {
	if mapping == nil {
		return nil
	}
	return &storepb.FieldMapping{
		Identifier:  mapping.GetIdentifier(),
		DisplayName: mapping.GetDisplayName(),
		Phone:       mapping.GetPhone(),
		Subject:     mapping.GetSubject(),
		Groups:      mapping.GetGroups(),
	}
}

func convertIdentityProviderUserInfo(info *storepb.IdentityProviderUserInfo) *v1pb.IdentityProviderUserInfo {
	if info == nil {
		return nil
	}
	return &v1pb.IdentityProviderUserInfo{
		Identifier:  info.GetIdentifier(),
		Subject:     info.GetSubject(),
		DisplayName: info.GetDisplayName(),
		Phone:       info.GetPhone(),
		Groups:      info.GetGroups(),
		HasGroups:   info.GetHasGroups(),
	}
}

// toV1IdentityProviderType and toStoreIdentityProviderType bridge the API enum
// and the stored enum. They are declared separately on purpose: proto/store is
// the row shape and must not drift with the API, so the mapping is explicit
// rather than a cast.
func toV1IdentityProviderType(identityProviderType storepb.IdentityProviderType) v1pb.IdentityProviderType {
	switch identityProviderType {
	case storepb.IdentityProviderType_OAUTH2:
		return v1pb.IdentityProviderType_OAUTH2
	default:
		return v1pb.IdentityProviderType_IDENTITY_PROVIDER_TYPE_UNSPECIFIED
	}
}

func toStoreIdentityProviderType(identityProviderType v1pb.IdentityProviderType) storepb.IdentityProviderType {
	switch identityProviderType {
	case v1pb.IdentityProviderType_OAUTH2:
		return storepb.IdentityProviderType_OAUTH2
	default:
		return storepb.IdentityProviderType_IDENTITY_PROVIDER_TYPE_UNSPECIFIED
	}
}

func validateIdentityProviderConfig(identityProviderType v1pb.IdentityProviderType, config *v1pb.IdentityProviderConfig, requireSecret bool) error {
	switch identityProviderType {
	case v1pb.IdentityProviderType_OAUTH2:
		oauth2Config := config.GetOauth2Config()
		if oauth2Config == nil {
			return errors.New("the oauth2_config must be set for an OAUTH2 identity provider")
		}
		for field, value := range map[string]string{
			"config.oauth2Config.authUrl":     oauth2Config.GetAuthUrl(),
			"config.oauth2Config.tokenUrl":    oauth2Config.GetTokenUrl(),
			"config.oauth2Config.userInfoUrl": oauth2Config.GetUserInfoUrl(),
			"config.oauth2Config.clientId":    oauth2Config.GetClientId(),
		} {
			if strings.TrimSpace(value) == "" {
				return errors.Errorf("the field %q is empty but required", field)
			}
		}
		if requireSecret && oauth2Config.GetClientSecret() == "" {
			return errors.New(`the field "config.oauth2Config.clientSecret" is empty but required`)
		}
		if oauth2Config.GetFieldMapping().GetIdentifier() == "" {
			return errors.New(`the field "config.oauth2Config.fieldMapping.identifier" is empty but required: it names the claim the account's address comes from`)
		}
		return nil
	default:
		return errors.Errorf("identity provider type %s is not supported: only OAUTH2 is implemented", identityProviderType.String())
	}
}
