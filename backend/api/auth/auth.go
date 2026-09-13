// Package auth handles the auth of gRPC server.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/golang-jwt/jwt/v5"
	errs "github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/config"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	issuer = "metaxisdata"
	// Signing key section. For now, this is only used for signing, not for verifying since we only
	// have 1 version. But it will be used to maintain backward compatibility if we change the signing mechanism.
	keyID = "v1"
	// AccessTokenAudienceFmt is the format of the acccess token audience.
	AccessTokenAudienceFmt = "mt.user.access.%s"
	apiTokenDuration       = 1 * time.Hour
	// DefaultTokenDuration is the default token expiration duration.
	DefaultTokenDuration = 7 * 24 * time.Hour

	// AccessTokenCookieName is the cookie name of access token.
	AccessTokenCookieName = "access-token"
)

// APIAuthInterceptor is the auth interceptor for gRPC server.
type APIAuthInterceptor struct {
	store    *store.Store
	secret   string
	stateCfg *state.State
	profile  *config.Profile
}

// New returns a new API auth interceptor.
func New(
	store *store.Store,
	secret string,
	stateCfg *state.State,
	profile *config.Profile,
) *APIAuthInterceptor {
	return &APIAuthInterceptor{
		store:    store,
		secret:   secret,
		stateCfg: stateCfg,
		profile:  profile,
	}
}

// WrapUnary implements the ConnectRPC interceptor interface for unary RPCs.
func (in *APIAuthInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		authContext, err := getAuthContext(req.Spec().Procedure)
		if err != nil {
			return nil, err
		}
		ctx = context.WithValue(ctx, common.AuthContextKey, authContext)

		// A malformed Authorization header must not break a method that allows
		// anonymous access: the header is simply ignored there.
		accessTokenStr, tokenErr := GetTokenFromHeaders(req.Header())
		if tokenErr != nil {
			if IsAuthenticationAllowed(req.Spec().Procedure, authContext) {
				return next(ctx, req)
			}
			return nil, connect.NewError(connect.CodeUnauthenticated, tokenErr)
		}

		user, identity, err := in.getUserConnect(ctx, accessTokenStr, req.Spec().Procedure)
		if err != nil {
			if IsAuthenticationAllowed(req.Spec().Procedure, authContext) {
				return next(ctx, req)
			}
			return nil, err
		}

		ctx = context.WithValue(ctx, common.UserContextKey, user)
		if identity.Restriction != "" {
			ctx = context.WithValue(ctx, common.TokenRestrictionContextKey, identity.Restriction)
		}
		return next(ctx, req)
	}
}

// WrapStreamingClient implements the ConnectRPC interceptor interface for streaming clients.
func (*APIAuthInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		return next(ctx, spec)
	}
}

// WrapStreamingHandler implements the ConnectRPC interceptor interface for streaming handlers.
func (in *APIAuthInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		authContext, err := getAuthContext(conn.Spec().Procedure)
		if err != nil {
			return err
		}
		ctx = context.WithValue(ctx, common.AuthContextKey, authContext)

		accessTokenStr, tokenErr := GetTokenFromHeaders(conn.RequestHeader())
		if tokenErr != nil {
			if IsAuthenticationAllowed(conn.Spec().Procedure, authContext) {
				return next(ctx, conn)
			}
			return connect.NewError(connect.CodeUnauthenticated, tokenErr)
		}

		user, identity, err := in.getUserConnect(ctx, accessTokenStr, conn.Spec().Procedure)
		if err != nil {
			if IsAuthenticationAllowed(conn.Spec().Procedure, authContext) {
				return next(ctx, conn)
			}
			return err
		}

		ctx = context.WithValue(ctx, common.UserContextKey, user)
		if identity.Restriction != "" {
			ctx = context.WithValue(ctx, common.TokenRestrictionContextKey, identity.Restriction)
		}

		return next(ctx, conn)
	}
}

// TokenRestriction narrows what an access token may be used for. A restricted
// token is a fully valid, signed token; the auth interceptor refuses it on
// every RPC outside restrictedTokenAllowedProcedures.
type TokenRestriction string

const (
	// TokenRestrictionResetPassword is issued when the workspace password policy
	// requires the user to rotate their password. It only allows the password
	// change itself and logging out.
	TokenRestrictionResetPassword TokenRestriction = "reset_password"
)

// restrictedTokenAllowedProcedures is the allowlist of RPCs a token carrying a
// restriction may call. A restriction absent from this map may call nothing.
var restrictedTokenAllowedProcedures = map[TokenRestriction]map[string]struct{}{
	TokenRestrictionResetPassword: {
		"/metaxisdata.v1.UserService/UpdateUser": {},
		"/metaxisdata.v1.AuthService/Logout":     {},
	},
}

// allows reports whether a token carrying the restriction may call procedure. An
// empty restriction means a full-access token.
func (r TokenRestriction) allows(procedure string) bool {
	if r == "" {
		return true
	}
	_, ok := restrictedTokenAllowedProcedures[r][procedure]
	return ok
}

// AccessTokenIdentity is the verified identity carried by an access token.
type AccessTokenIdentity struct {
	UserID   int
	IssuedAt time.Time
	// Restriction is empty for a full-access token.
	Restriction TokenRestriction
}

// VerifyAccessToken validates an access token's signature, algorithm, issuer,
// audience and expiry. It performs no database lookup, so callers that need the
// principal record must still load it.
func VerifyAccessToken(accessTokenStr, secret string, mode common.ReleaseMode) (*AccessTokenIdentity, error) {
	claims := &claimsMessage{}
	if _, err := jwt.ParseWithClaims(accessTokenStr, claims, func(t *jwt.Token) (any, error) {
		if kid, ok := t.Header["kid"].(string); ok {
			if kid == keyID {
				return []byte(secret), nil
			}
		}
		return nil, errs.Errorf("unexpected access token kid=%v", t.Header["kid"])
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
	); err != nil {
		return nil, err
	}
	if !audienceContains(claims.Audience, fmt.Sprintf(AccessTokenAudienceFmt, mode)) {
		return nil, errs.Errorf(
			"invalid access token, audience mismatch, got %q, expected %q. you may send request to the wrong environment",
			claims.Audience,
			fmt.Sprintf(AccessTokenAudienceFmt, mode),
		)
	}
	principalID, err := strconv.Atoi(claims.Subject)
	if err != nil {
		return nil, errs.Wrapf(err, "malformed ID %s in the access token", claims.Subject)
	}
	identity := &AccessTokenIdentity{UserID: principalID, Restriction: TokenRestriction(claims.Restriction)}
	if claims.IssuedAtNanos != 0 {
		identity.IssuedAt = time.Unix(0, claims.IssuedAtNanos)
	} else if claims.IssuedAt != nil {
		identity.IssuedAt = claims.IssuedAt.Time
	}
	return identity, nil
}

// authenticateConnect is a ConnectRPC-specific version that returns ConnectRPC errors.
func (in *APIAuthInterceptor) authenticateConnect(ctx context.Context, accessTokenStr string) (*store.UserMessage, *AccessTokenIdentity, error) {
	if accessTokenStr == "" {
		return nil, nil, connect.NewError(connect.CodeUnauthenticated, errs.New("access token not found"))
	}
	if _, ok := in.stateCfg.TokenExpireCache.Get(accessTokenStr); ok {
		return nil, nil, connect.NewError(connect.CodeUnauthenticated, errs.New("access token expired"))
	}
	identity, err := VerifyAccessToken(accessTokenStr, in.secret, in.profile.Mode)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, nil, connect.NewError(connect.CodeUnauthenticated, errs.New("access token expired"))
		}
		return nil, nil, connect.NewError(connect.CodeUnauthenticated, errs.New("failed to parse claim"))
	}

	user, err := in.store.GetUserByID(ctx, identity.UserID)
	if err != nil {
		return nil, nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("failed to find user ID %d in the access token", identity.UserID))
	}
	if user == nil {
		return nil, nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("user ID %d not exists in the access token", identity.UserID))
	}
	if user.MemberDeleted {
		return nil, nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("user ID %d has been deactivated by administrators", user.ID))
	}
	// A token minted before the last password change must not survive it. The
	// comparison uses persisted state, so it holds across replicas. Both
	// timestamps come from this process's clock and the iat claim carries
	// sub-second precision, so the ordering is exact.
	if lastChange := user.Profile.GetLastChangePasswordTime(); lastChange != nil {
		if tokenPredatesPasswordChange(identity.IssuedAt, lastChange.AsTime()) {
			return nil, nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("access token of user ID %d was issued before the last password change", user.ID))
		}
	}

	return user, identity, nil
}

// tokenPredatesPasswordChange reports whether a token issued at issuedAt must be
// rejected because the password changed at changedAt afterwards. A zero time is
// treated as "not applicable" so tokens without an iat claim and users who never
// changed their password are unaffected.
func tokenPredatesPasswordChange(issuedAt, changedAt time.Time) bool {
	if issuedAt.IsZero() || changedAt.IsZero() {
		return false
	}
	return issuedAt.Before(changedAt)
}

// getUserConnect is a ConnectRPC-specific version that returns ConnectRPC errors.
// It also refuses a restricted token outside the RPCs that restriction allows.
func (in *APIAuthInterceptor) getUserConnect(ctx context.Context, accessTokenStr, procedure string) (*store.UserMessage, *AccessTokenIdentity, error) {
	user, identity, err := in.authenticateConnect(ctx, accessTokenStr)
	if err != nil {
		return nil, nil, err
	}
	if !identity.Restriction.allows(procedure) {
		return nil, nil, connect.NewError(connect.CodePermissionDenied, errs.Errorf("access token restricted to %q cannot call %s", identity.Restriction, procedure))
	}

	return user, identity, nil
}

// GetTokenFromHeaders extracts the access token from HTTP headers for ConnectRPC.
func GetTokenFromHeaders(headers http.Header) (string, error) {
	// Check Authorization header first
	authHeader := headers.Get("Authorization")
	if authHeader != "" {
		authHeaderParts := strings.Fields(authHeader)
		if len(authHeaderParts) != 2 || strings.ToLower(authHeaderParts[0]) != "bearer" {
			return "", errs.Errorf("authorization header format must be Bearer {token}")
		}
		return authHeaderParts[1], nil
	}

	// Check HTTP cookies
	var accessToken string
	cookieHeaders := headers.Values("Cookie")
	for _, cookieHeader := range cookieHeaders {
		header := http.Header{}
		header.Add("Cookie", cookieHeader)
		request := http.Request{Header: header}
		if cookie, _ := request.Cookie(AccessTokenCookieName); cookie != nil {
			accessToken = cookie.Value
			break
		}
	}
	return accessToken, nil
}

func audienceContains(audience jwt.ClaimStrings, token string) bool {
	for _, v := range audience {
		if v == token {
			return true
		}
	}
	return false
}

type claimsMessage struct {
	Name string `json:"name"`
	jwt.RegisteredClaims
	// IssuedAtNanos mirrors iat with the precision jwt's NumericDate drops
	// (TimePrecision defaults to one second), so a password change can be
	// ordered against the token without a whole-second blind spot.
	IssuedAtNanos int64 `json:"iat_ns"`
	// Restriction is empty for a full-access token.
	Restriction string `json:"rst,omitempty"`
}

// GenerateRestrictedAccessToken generates an access token the auth interceptor
// accepts only for the RPCs allowed by restriction.
func GenerateRestrictedAccessToken(userName string, userID int, mode common.ReleaseMode, secret string, tokenDuration time.Duration, restriction TokenRestriction) (string, error) {
	expirationTime := time.Now().Add(tokenDuration)
	return generateToken(userName, userID, fmt.Sprintf(AccessTokenAudienceFmt, mode), expirationTime, []byte(secret), restriction)
}

// GenerateAPIToken generates an API token.
func GenerateAPIToken(userName string, userID int, mode common.ReleaseMode, secret string) (string, error) {
	expirationTime := time.Now().Add(apiTokenDuration)
	return generateToken(userName, userID, fmt.Sprintf(AccessTokenAudienceFmt, mode), expirationTime, []byte(secret), "")
}

// GenerateAccessToken generates an access token for web.
func GenerateAccessToken(userName string, userID int, mode common.ReleaseMode, secret string, tokenDuration time.Duration) (string, error) {
	expirationTime := time.Now().Add(tokenDuration)
	return generateToken(userName, userID, fmt.Sprintf(AccessTokenAudienceFmt, mode), expirationTime, []byte(secret), "")
}

// Pay attention to this function. It holds the main JWT token generation logic.
func generateToken(userName string, userID int, aud string, expirationTime time.Time, secret []byte, restriction TokenRestriction) (string, error) {
	// The iat claim only has second granularity, so two logins in the same
	// second would otherwise produce byte-identical tokens. Logout revokes a
	// token by its string, so identical tokens would let one session's logout
	// kill the other session.
	tokenID, err := common.RandomString(16)
	if err != nil {
		return "", errs.Wrap(err, "failed to generate a token id")
	}
	now := time.Now()
	// Create the JWT claims, which includes the username and expiry time.
	claims := &claimsMessage{
		Name: userName,
		RegisteredClaims: jwt.RegisteredClaims{
			Audience: jwt.ClaimStrings{aud},
			// In JWT, the expiry time is expressed as unix milliseconds.
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    issuer,
			Subject:   strconv.Itoa(userID),
			ID:        tokenID,
		},
		IssuedAtNanos: now.UnixNano(),
		Restriction:   string(restriction),
	}

	// Declare the token with the HS256 algorithm used for signing, and the claims.
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = keyID

	// Create the JWT string.
	tokenString, err := token.SignedString(secret)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

func getAuthContext(fullMethod string) (*common.AuthContext, error) {
	methodTokens := strings.Split(fullMethod, "/")
	if len(methodTokens) != 3 {
		return nil, errs.Errorf("invalid full method name %q", fullMethod)
	}
	rd, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(methodTokens[1]))
	if err != nil {
		return nil, errs.Wrapf(err, "invalid registry service descriptor, full method name %q", fullMethod)
	}
	sd, ok := rd.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, errs.Errorf("invalid service descriptor, full method name %q", fullMethod)
	}
	methodDesc := sd.Methods().ByName(protoreflect.Name(methodTokens[2]))
	if methodDesc == nil {
		return nil, errs.Errorf("method %q not found in service %q", methodTokens[2], methodTokens[1])
	}
	md, ok := methodDesc.Options().(*descriptorpb.MethodOptions)
	if !ok {
		return nil, errs.Errorf("invalid method options, full method name %q", fullMethod)
	}
	allowWithoutCredentialAny := proto.GetExtension(md, v1pb.E_AllowWithoutCredential)
	allowWithoutCredential, ok := allowWithoutCredentialAny.(bool)
	if !ok {
		return nil, errs.Errorf("invalid allow without credential extension, full method name %q", fullMethod)
	}
	permissionAny := proto.GetExtension(md, v1pb.E_Permission)
	permission, ok := permissionAny.(string)
	if !ok {
		return nil, errs.Errorf("invalid permission extension, full method name %q", fullMethod)
	}
	auditAny := proto.GetExtension(md, v1pb.E_Audit)
	audit, ok := auditAny.(bool)
	if !ok {
		return nil, errs.Errorf("invalid audit extension, full method name %q", fullMethod)
	}

	return &common.AuthContext{
		AllowWithoutCredential: allowWithoutCredential,
		Permission:             permission,
		Audit:                  audit,
	}, nil
}
