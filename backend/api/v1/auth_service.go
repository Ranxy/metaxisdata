package v1

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/plugin/idp/oauth2"

	"github.com/pkg/errors"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	"github.com/Ranxy/metaxisdata/backend/component/audit"

	"github.com/Ranxy/metaxisdata/backend/config"
	"github.com/Ranxy/metaxisdata/backend/store"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

var (
	invalidUserOrPasswordError = connect.NewError(connect.CodeUnauthenticated, errors.Errorf("the email or password is not valid"))

	// dummyPasswordHash is compared when the email is unknown so the login path
	// costs the same whether or not the account exists. It hashes a fixed,
	// unguessable string at the default bcrypt cost.
	dummyPasswordHash = sync.OnceValues(func() ([]byte, error) {
		return bcrypt.GenerateFromPassword([]byte("metaxisdata-timing-equalizer"), bcrypt.DefaultCost)
	})
)

// AuthService implements the auth service.
type AuthService struct {
	v1connect.UnimplementedAuthServiceHandler
	store    *store.Store
	secret   string
	profile  *config.Profile
	stateCfg *state.State
}

// NewAuthService creates a new AuthService.
func NewAuthService(store *store.Store, secret string, profile *config.Profile, stateCfg *state.State) *AuthService {
	return &AuthService{
		store:    store,
		secret:   secret,
		profile:  profile,
		stateCfg: stateCfg,
	}
}

// Login is the auth login method including SSO.
func (s *AuthService) Login(ctx context.Context, req *connect.Request[v1pb.LoginRequest]) (*connect.Response[v1pb.LoginResponse], error) {
	request := req.Msg
	var loginUser *store.UserMessage
	loginViaIDP := request.GetIdpName() != ""

	response := &v1pb.LoginResponse{}
	resp := connect.NewResponse(response)
	var err error
	if loginViaIDP {
		var accountAdopted bool
		loginUser, accountAdopted, err = s.getOrCreateUserWithIDP(ctx, request)
		if err != nil {
			return nil, err
		}
		// The client has to tell the user their password no longer works; the
		// login that just adopted the account is the only moment we can reach
		// them.
		response.AccountAdopted = accountAdopted
	} else {
		// The source is the resolved client address, not the TCP peer: behind a
		// proxy the peer is the proxy for every caller, which would let one client
		// lock every account and would collapse all sources onto one budget. The
		// same resolver the audit ledger uses keeps the two from disagreeing.
		source := audit.BuildRequestMetadata(req.Header(), req.Peer().Addr, s.profile.TrustedProxies).GetIp()
		if s.stateCfg.LoginLimiter.Blocked(request.Email, source, time.Now()) {
			return nil, connect.NewError(connect.CodeResourceExhausted, errors.Errorf("too many failed login attempts, try again later"))
		}
		loginUser, err = s.getAndVerifyUser(ctx, request)
		if err != nil {
			s.stateCfg.LoginLimiter.RecordFailure(request.Email, source, time.Now())
			return nil, err
		}
		s.stateCfg.LoginLimiter.ResetAccount(request.Email)
		// Reset password restriction only works for end user with email & password login.
		response.RequireResetPassword = s.needResetPassword(ctx, loginUser)
	}

	if loginUser.MemberDeleted {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("user has been deactivated by administrators"))
	}

	setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to find workspace setting, error"))
	}
	isWorkspaceAdmin, err := isUserWorkspaceAdmin(ctx, s.store, loginUser)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to check user roles, error"))
	}
	// disallow_password_signin covers every principal that authenticates with a
	// password-style credential. A service account signs in with its access
	// key, so exempting it was a policy bypass.
	if !isWorkspaceAdmin && setting.DisallowPasswordSignin && !loginViaIDP {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("password signin is disallowed"))
	}
	if !isWorkspaceAdmin && loginUser.Type == storepb.PrincipalType_END_USER {
		// Check domain restriction for end users.
		if err := validateEmailWithDomains(ctx, s.store, loginUser.Email, false); err != nil {
			return nil, err
		}
	}

	tokenDuration := auth.GetTokenDuration(ctx, s.store)

	var loginToken string
	switch loginUser.Type {
	case storepb.PrincipalType_END_USER:
		var token string
		var err error
		if response.RequireResetPassword {
			// The password policy requires a rotation, so the token only
			// authorizes the rotation itself instead of the whole API.
			token, err = auth.GenerateRestrictedAccessToken(loginUser.Name, loginUser.ID, s.profile.Mode, s.secret, tokenDuration, auth.TokenRestrictionResetPassword)
		} else {
			token, err = auth.GenerateAccessToken(loginUser.Name, loginUser.ID, s.profile.Mode, s.secret, tokenDuration)
		}
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate API access token"))
		}
		loginToken = token
	case storepb.PrincipalType_SERVICE_ACCOUNT:
		token, err := auth.GenerateAPIToken(loginUser.Name, loginUser.ID, s.profile.Mode, s.secret)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate API access token"))
		}
		loginToken = token
	default:
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("user type %s cannot login", loginUser.Type))
	}

	// A web login carries the token in an HttpOnly cookie only. Echoing it in
	// the response body as well would let any script that reads the response
	// recover it, which defeats the point of HttpOnly.
	if !request.Web {
		response.Token = loginToken
	}

	if request.Web {
		// Only allow end users to use web login, not service accounts.
		if loginUser.Type != storepb.PrincipalType_END_USER {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("only users can use web login"))
		}

		cookie := auth.GetTokenCookie(ctx, s.store, loginToken)
		resp.Header().Add("Set-Cookie", cookie.String())
	}

	// Stamp the login time with a targeted JSONB write instead of rewriting the
	// whole profile column: the row this request holds may already be stale, and
	// a rewrite would undo the password change time an adopted account carries,
	// which is what retires the previous holder's sessions.
	if err := s.store.RecordLastLogin(ctx, loginUser, time.Now()); err != nil {
		slog.Error("failed to update user profile", log.WithError(err), slog.Int("user_id", loginUser.ID))
	}

	// The response reports the login this request just recorded. The profile is
	// cloned so the entry the store caches is never mutated in place.
	responseUser := *loginUser
	responseUser.Profile = profileWithLastLogin(loginUser.Profile)
	response.User = convertToUser(&responseUser)

	return resp, nil
}

// randomPasswordHash hashes a password nobody ever learns. An account that signs
// in through an identity provider still needs a hash in the column, and adopting
// an account replaces the password its previous holder knew with exactly this, so
// that password stops working along with the sessions it authorized.
func randomPasswordHash() (string, error) {
	password, err := common.RandomString(20)
	if err != nil {
		return "", errors.Errorf("failed to generate a random password")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", errors.Errorf("failed to generate password hash")
	}
	return string(hash), nil
}

// profileWithLastLogin returns a copy of the current profile with
// LastLoginTime set to now. Copying keeps the caller from mutating the profile
// the store caches, and patching the whole profile as a clone preserves every
// other field instead of replacing the column with just the fields named here.
func profileWithLastLogin(current *storepb.UserProfile) *storepb.UserProfile {
	profile := proto.CloneOf(current)
	if profile == nil {
		profile = &storepb.UserProfile{}
	}
	profile.LastLoginTime = timestamppb.Now()
	return profile
}

func (s *AuthService) needResetPassword(ctx context.Context, user *store.UserMessage) bool {
	// Reset password restriction only works for end user with email & password login.
	if user.Type != storepb.PrincipalType_END_USER {
		return false
	}

	passwordRestriction, err := s.store.GetPasswordRestrictionSetting(ctx)
	if err != nil {
		slog.Error("failed to get password restriction", log.WithError(err))
		return false
	}

	if user.Profile.LastLoginTime == nil {
		if !passwordRestriction.RequireResetPasswordForFirstLogin {
			return false
		}
		count, err := s.store.CountUsers(ctx, storepb.PrincipalType_END_USER)
		if err != nil {
			slog.Error("failed to count end users", log.WithError(err))
			return false
		}
		// The 1st workspace admin login don't need to reset the password
		return count > 1
	}

	if passwordRestriction.PasswordRotation != nil {
		lastChangePasswordTime := user.CreatedAt
		if user.Profile.LastChangePasswordTime != nil {
			lastChangePasswordTime = user.Profile.LastChangePasswordTime.AsTime()
		}
		if lastChangePasswordTime.Add(passwordRestriction.PasswordRotation.AsDuration()).Before(time.Now()) {
			return true
		}
	}

	return false
}

// CreateSSOState issues a one-time OAuth2 state nonce. The client passes it to
// the identity provider and then back with the login request.
func (s *AuthService) CreateSSOState(_ context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[v1pb.CreateSSOStateResponse], error) {
	state, err := common.RandomString(32)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate the SSO state"))
	}
	s.stateCfg.SSOStateCache.Add(state, time.Now())
	return connect.NewResponse(&v1pb.CreateSSOStateResponse{State: state}), nil
}

// consumeSSOState validates a state nonce and removes it: a state can be used
// exactly once and only within state.SSOStateTTL.
func (s *AuthService) consumeSSOState(nonce string) bool {
	if nonce == "" {
		return false
	}
	issuedAt, ok := s.stateCfg.SSOStateCache.Get(nonce)
	if !ok {
		return false
	}
	s.stateCfg.SSOStateCache.Remove(nonce)
	return time.Since(issuedAt) <= state.SSOStateTTL
}

// Logout is the auth logout method.
func (s *AuthService) Logout(ctx context.Context, req *connect.Request[v1pb.LogoutRequest]) (*connect.Response[emptypb.Empty], error) {
	accessTokenStr, err := auth.GetTokenFromHeaders(req.Header())
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	// Only a token this server actually issued may be revoked. Without this check
	// an unauthenticated caller could flood the revocation records with forged
	// strings. The audience is deliberately not checked: an MCP token carries the
	// MCP endpoint's resource identifier as its audience, and revoking one has to
	// work, or a leaked MCP token would have no self-service remedy at all.
	identity, err := auth.VerifyAccessTokenProvenance(accessTokenStr, s.secret)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("invalid access token"))
	}
	// Every token this server signs carries a jti, and it is what the revocation
	// record is keyed by. Refusing here keeps a logout from reporting success
	// without a record that Resolve can act on.
	if identity.TokenID == "" {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("access token carries no revocable token id"))
	}
	// The record is persistent, keyed by the token's jti and kept only until the
	// token's own expiry, so a caller cannot evict it by logging in and out and
	// another replica refuses the token too. The cache makes this process refuse
	// it immediately instead of on the next table read.
	if err := s.store.RevokeToken(ctx, identity.TokenID, identity.ExpiresAt); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to revoke the access token"))
	}
	s.stateCfg.TokenRevocationCache.Revoke(identity.TokenID, time.Now())

	resp := connect.NewResponse(&emptypb.Empty{})

	cookie := auth.GetTokenCookie(ctx, s.store, "")
	resp.Header().Add("Set-Cookie", cookie.String())
	return resp, nil
}

func (s *AuthService) getAndVerifyUser(ctx context.Context, request *v1pb.LoginRequest) (*store.UserMessage, error) {
	user, err := s.store.GetUserByEmail(ctx, request.Email)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get user by email %q", request.Email))
	}
	if user == nil {
		// Still spend the bcrypt time of a real comparison: otherwise the
		// response time tells an attacker which emails exist.
		if hash, err := dummyPasswordHash(); err == nil {
			_ = bcrypt.CompareHashAndPassword(hash, []byte(request.Password))
		}
		return nil, invalidUserOrPasswordError
	}
	// Compare the stored hashed password, with the hashed version of the password that was received.
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(request.Password)); err != nil {
		// If the two passwords don't match, return a 401 status.
		return nil, invalidUserOrPasswordError
	}
	return user, nil
}

func (s *AuthService) getOrCreateUserWithIDP(ctx context.Context, request *v1pb.LoginRequest) (*store.UserMessage, bool, error) {
	idpID, err := common.GetIdentityProviderID(request.IdpName)
	if err != nil {
		return nil, false, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "failed to get identity provider ID"))
	}
	idp, err := s.store.GetIdentityProvider(ctx, &store.FindIdentityProviderMessage{
		ResourceID: &idpID,
	})
	if err != nil {
		return nil, false, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get identity provider"))
	}
	if idp == nil {
		return nil, false, connect.NewError(connect.CodeNotFound, errors.Errorf("identity provider not found"))
	}

	setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return nil, false, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get workspace setting"))
	}
	// For a provider an administrator has listed as trusting its own address
	// claim, the address may stand in for the subject and a login may make an
	// existing account with that address reachable through it.
	allowEmailIdentity := slices.Contains(setting.GetSsoEmailIdentityIdps(), idpID)

	var userInfo *storepb.IdentityProviderUserInfo
	switch idp.Type {
	case storepb.IdentityProviderType_OAUTH2:
		oauth2Context := request.IdpContext.GetOauth2Context()
		if oauth2Context == nil {
			return nil, false, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("missing OAuth2 context"))
		}
		oauth2Config := idp.Config.GetOauth2Config()
		if err := validateIDPSubjectMapping(oauth2Config.GetFieldMapping(), allowEmailIdentity); err != nil {
			return nil, false, connect.NewError(connect.CodeFailedPrecondition, errors.Wrapf(err, "identity provider %q cannot resolve logins", idp.Title))
		}
		// The state binds the callback to the client that started the flow,
		// which is what stops an attacker from completing a code exchange in
		// someone else's browser (login CSRF / authorization code injection).
		if !s.consumeSSOState(oauth2Context.State) {
			return nil, false, connect.NewError(connect.CodePermissionDenied, errors.Errorf("invalid or expired OAuth2 state, request a new one first"))
		}
		oauth2IdentityProvider, err := oauth2.NewIdentityProvider(oauth2Config)
		if err != nil {
			return nil, false, connect.NewError(connect.CodeFailedPrecondition, errors.Wrapf(err, "failed to create new OAuth2 identity provider"))
		}
		if setting.ExternalUrl == "" {
			return nil, false, connect.NewError(connect.CodeFailedPrecondition, errors.New("external URL is not configured: set it in the workspace general settings"))
		}
		redirectURL := fmt.Sprintf("%s/oauth/callback", setting.ExternalUrl)
		token, err := oauth2IdentityProvider.ExchangeToken(ctx, redirectURL, oauth2Context.Code, oauth2Context.CodeVerifier)
		if err != nil {
			return nil, false, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to exchange token"))
		}
		userInfo, _, err = oauth2IdentityProvider.UserInfo(token)
		if err != nil {
			return nil, false, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get user info"))
		}
	default:
		return nil, false, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("identity provider type %s not supported", idp.Type.String()))
	}
	if userInfo == nil {
		return nil, false, connect.NewError(connect.CodeNotFound, errors.Errorf("failed to get user info from identity provider %q", idp.Title))
	}
	if userInfo.Identifier == "" {
		return nil, false, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("missing identifier in user info from identity provider %q", idp.Title))
	}
	// The subject is the identity the account is bound to. The identifier — the
	// email claim — is mutable, so by default a login must never resolve against
	// it: that is how a member who registered (or moved to) someone else's
	// address took over the account the real owner signed in with. The workspace
	// can accept the address as an identity instead, which is what
	// allowEmailIdentity carries; the switch is administrator-set and the
	// identity provider it applies to is administrator-configured.
	subject, err := idpLoginSubject(idp.Config.GetOauth2Config().GetFieldMapping(), userInfo, allowEmailIdentity)
	if err != nil {
		return nil, false, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "identity provider %q", idp.Title))
	}

	boundUser, err := s.store.GetUserByIDPBinding(ctx, &store.IDPBinding{ResourceID: idpID, Subject: subject})
	if err != nil {
		return nil, false, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to find the user bound to identity provider %q", idp.Title))
	}
	if boundUser != nil {
		// A deactivated account stays deactivated: restoring it is an explicit
		// administrative decision, not a side effect of signing in. Its groups
		// are left alone too — the login above refuses it before any of this
		// matters.
		if !boundUser.MemberDeleted && userInfo.HasGroups {
			// Sync user groups with the identity provider.
			// The userInfo.Groups is the groups that the user belongs to in the identity provider.
			if err := s.syncUserGroups(ctx, boundUser, userInfo.Groups); err != nil {
				return nil, false, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to sync user groups"))
			}
		}
		return boundUser, false, nil
	}

	// The userinfo's email comes from identity provider, it has to be converted to lower-case.
	email := strings.ToLower(userInfo.Identifier)
	if err := validateEmail(email); err != nil {
		// If the email is invalid, we will try to use the domain and identifier to construct the email.
		domain := extractDomain(idp.Domain)
		if domain != "" {
			email = strings.ToLower(fmt.Sprintf("%s@%s", email, domain))
		} else {
			return nil, false, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid email %q", userInfo.Identifier))
		}
	}
	// If the email is still invalid, we will return an error.
	if err := validateEmailWithDomains(ctx, s.store, email, false); err != nil {
		return nil, false, err
	}

	// A deleted row is ignored: it cannot sign in, its address is free again
	// (which is what the partial unique email index assumes), and refusing it
	// would make the remedy — an administrator deletes the account that squats
	// the address — impossible to carry out.
	existedUser, err := s.store.GetActiveUserByEmail(ctx, email)
	if err != nil {
		return nil, false, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to find user by email %s", email))
	}
	if existedUser != nil {
		if !allowEmailIdentity {
			return nil, false, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf("email %s already belongs to an active account that is not linked to identity provider %q; an administrator has to move that account's address, or delete it, before this login can create the account", email, idp.Title))
		}
		// The workspace lists this provider as one whose address claim is its
		// identity, and its configuration is administrator-written, so the login
		// makes the account reachable through it instead of refusing. An account
		// that had no provider binding is adopted: its password is voided, which
		// also retires whatever sessions the previous holder had.
		if existedUser.Type != storepb.PrincipalType_END_USER {
			return nil, false, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf("email %s belongs to a %s, which cannot sign in through an identity provider", email, existedUser.Type.String()))
		}
		passwordHash, err := randomPasswordHash()
		if err != nil {
			return nil, false, connect.NewError(connect.CodeInternal, err)
		}
		adopted, err := s.store.BindAccountForSSO(ctx, existedUser, &store.IDPBinding{ResourceID: idpID, Subject: subject}, passwordHash)
		if err != nil {
			if common.ErrorCode(err) == common.Conflict {
				return nil, false, connect.NewError(connect.CodeFailedPrecondition, errors.Wrapf(err, "account %s; sign in again", email))
			}
			return nil, false, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to bind user %d to identity provider %q", existedUser.ID, idp.Title))
		}
		if adopted {
			slog.Warn("an SSO login adopted an existing account and invalidated its password",
				slog.String("idp", idp.Title),
				slog.Int("user_id", existedUser.ID),
				slog.String("email", email))
		}
		// Re-read: the caller keeps the returned user, and the adoption just
		// rewrote its profile, which is where the password change time lives.
		if user, err := s.store.GetUserByID(ctx, existedUser.ID); err != nil {
			return nil, false, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to reload user %d after binding it", existedUser.ID))
		} else if user != nil {
			existedUser = user
		}
		if userInfo.HasGroups {
			if err := s.syncUserGroups(ctx, existedUser, userInfo.Groups); err != nil {
				return nil, false, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to sync user groups"))
			}
		}
		return existedUser, adopted, nil
	}

	// Create new user from identity provider. The password exists so the column
	// stays a valid hash and is never disclosed: the account signs in through the
	// provider. It is generated here rather than up front so that a login that is
	// refused costs no bcrypt work.
	passwordHash, err := randomPasswordHash()
	if err != nil {
		return nil, false, connect.NewError(connect.CodeInternal, err)
	}
	newUser, err := s.store.CreateUser(ctx, &store.UserMessage{
		Name:         clampUserTitle(userInfo.DisplayName),
		Email:        email,
		Phone:        userInfo.Phone,
		Type:         storepb.PrincipalType_END_USER,
		PasswordHash: passwordHash,
	}, &store.IDPBinding{ResourceID: idpID, Subject: subject})
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to create user")
	}
	if userInfo.HasGroups {
		// Sync user groups with the identity provider.
		// The userInfo.Groups is the groups that the user belongs to in the identity provider.
		if err := s.syncUserGroups(ctx, newUser, userInfo.Groups); err != nil {
			return nil, false, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to sync user groups"))
		}
	}
	return newUser, false, nil
}

// syncUserGroups syncs the user groups with the given groups.
// The given groups are the groups that the user belongs to in the identity provider.
// Supported groups format: ["group1", "group2", ...], ["dev.example.com", ...]
func (s *AuthService) syncUserGroups(ctx context.Context, user *store.UserMessage, groups []string) error {
	groupMessageList, err := s.store.ListGroups(ctx, &store.FindGroupMessage{})
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list groups"))
	}

	for _, groupMessage := range groupMessageList {
		var isMember bool
		for _, group := range groups {
			// Only the group's resource identifier is matched. The title is a
			// mutable label an admin can set to anything, so it must not decide
			// who inherits the role the group carries.
			if groupMessage.Email == group {
				isMember = true
				break
			}
		}
		var isGroupMember bool
		for _, member := range groupMessage.Payload.Members {
			if member.Member == common.FormatUserUID(user.ID) {
				isGroupMember = true
				break
			}
		}
		if isMember != isGroupMember {
			if isMember {
				// Add the user to the group.
				groupMessage.Payload.Members = append(groupMessage.Payload.Members, &storepb.GroupMember{
					Role:   storepb.GroupMember_MEMBER,
					Member: common.FormatUserUID(user.ID),
				})
			} else {
				// Remove the user from the group.
				groupMessage.Payload.Members = slices.DeleteFunc(groupMessage.Payload.Members, func(member *storepb.GroupMember) bool {
					return member.Member == common.FormatUserUID(user.ID)
				})
			}
			if _, err := s.store.UpdateGroup(ctx, groupMessage.Email, &store.UpdateGroupMessage{
				Payload: groupMessage.Payload,
			}); err != nil {
				return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to update group %q", groupMessage.Email))
			}
		}
	}

	return nil
}
