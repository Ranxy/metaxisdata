package mcp

import (
	"context"
	"net/http"
	"strconv"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	"github.com/Ranxy/metaxisdata/backend/api/oauth"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// userExtraKey is where the verifier parks the resolved principal inside
// TokenInfo.Extra. TokenInfo has no field for a store record, and the tool layer
// needs it for two things: the handlers read the user from the context, and the
// IAM check runs against the same record.
const userExtraKey = "metaxisdata.user"

// EndpointsFunc is the OAuth package's resolver type, aliased here because the
// verifier is wired from the same place the protocol endpoints are.
type EndpointsFunc = oauth.EndpointsFunc

// NewTokenVerifier returns the bearer-token verifier for the MCP endpoint.
//
// The token is resolved by the same authenticator the ConnectRPC API uses, but
// against the MCP resource audience: a web or CLI token is refused here, and so
// is a token minted for another deployment. Because that shared chain also
// checks revocation, deactivation and the password-change cutoff, a user who has
// been deactivated stops being accepted here at the same moment as everywhere
// else — which is exactly what a second, ad-hoc verification path would have
// missed.
func NewTokenVerifier(endpoints EndpointsFunc, tokens *auth.TokenAuthenticator) sdkauth.TokenVerifier {
	return func(ctx context.Context, token string, _ *http.Request) (*sdkauth.TokenInfo, error) {
		resolved, enabled, err := endpoints(ctx)
		if err != nil || !enabled {
			return nil, sdkauth.ErrInvalidToken
		}
		user, identity, err := tokens.Resolve(ctx, token, resolved.Resource)
		if err != nil {
			return nil, sdkauth.ErrInvalidToken
		}
		// The scopes come from the token, never from a constant: the transport
		// middleware is what turns a missing scope into 403 insufficient_scope,
		// and it can only do that if it is told the truth.
		return &sdkauth.TokenInfo{
			UserID:     strconv.Itoa(user.ID),
			Scopes:     identity.Scopes,
			Expiration: identity.ExpiresAt,
			Extra:      map[string]any{userExtraKey: user},
		}, nil
	}
}

// UserFromTokenInfo returns the principal the verifier resolved, if any.
func UserFromTokenInfo(info *sdkauth.TokenInfo) (*store.UserMessage, bool) {
	if info == nil {
		return nil, false
	}
	user, ok := info.Extra[userExtraKey].(*store.UserMessage)
	if !ok || user == nil {
		return nil, false
	}
	return user, true
}

// UserFromRequest returns the principal the verifier resolved for a tool call.
// A tool that finds nothing here was reached without the bearer middleware, and
// must refuse rather than run without an authorization check.
func UserFromRequest(req *mcpsdk.CallToolRequest) (*store.UserMessage, bool) {
	if req == nil {
		return nil, false
	}
	extra := req.GetExtra()
	if extra == nil {
		return nil, false
	}
	return UserFromTokenInfo(extra.TokenInfo)
}
