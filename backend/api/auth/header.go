package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/store"
)

// GatewayResponseModifier is the response modifier for grpc gateway.
type GatewayResponseModifier struct{}

// Modify is the mux option for modifying response header.
func (*GatewayResponseModifier) Modify(ctx context.Context, response http.ResponseWriter, _ proto.Message) error {
	md, ok := runtime.ServerMetadataFromContext(ctx)
	if !ok {
		return errors.Errorf("failed to get ServerMetadata from context in the gateway response modifier")
	}

	if vs := md.HeaderMD.Get("Set-Cookie"); len(vs) > 0 {
		for _, v := range vs {
			response.Header().Add("Set-Cookie", v)
		}
	}
	return nil
}

// GetTokenCookie builds the web session cookie. Passing token="" clears it.
//
// Secure and SameSite are derived from the server-side external URL, never from
// the client-controlled Origin header: a client could otherwise claim https and
// force SameSite=None, which is exactly the cross-site cookie behavior CSRF
// protection wants to avoid. Deployments that serve the SPA from another origin
// must configure an https external URL, which selects SameSite=None; Secure.
func GetTokenCookie(ctx context.Context, stores *store.Store, token string) *http.Cookie {
	if token == "" {
		return &http.Cookie{
			Name:    AccessTokenCookieName,
			Value:   "",
			Expires: time.Unix(0, 0),
			Path:    "/",
		}
	}
	secure, sameSite := cookieSecurity(ctx, stores)
	tokenDuration := GetTokenDuration(ctx, stores)
	return &http.Cookie{
		Name:  AccessTokenCookieName,
		Value: token,
		// CookieExpDuration expires slightly earlier than the jwt expiration. Client would be logged out if the user
		// cookie expires, thus the client would always logout first before attempting to make a request with the expired jwt.
		// Suppose we have a valid refresh token, we will refresh the token in 2 cases:
		// 1. The access token is about to expire in <<refreshThresholdDuration>>
		// 2. The access token has already expired, we refresh the token so that the ongoing request can pass through.
		Expires: time.Now().Add(tokenDuration - 1*time.Second),
		Path:    "/",
		// Http-only helps mitigate the risk of client side script accessing the protected cookie.
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
	}
}

// cookieSecurity reports whether the session cookie must be Secure and which
// SameSite policy applies. It reads the admin-configured external URL from the
// database; an unconfigured or plain-http deployment gets SameSite=Lax, which
// blocks cross-site form posts and XHR.
func cookieSecurity(ctx context.Context, stores *store.Store) (bool, http.SameSite) {
	if stores == nil {
		return cookieSecurityForExternalURL("")
	}
	setting, err := stores.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return cookieSecurityForExternalURL("")
	}
	return cookieSecurityForExternalURL(setting.GetExternalUrl())
}

func cookieSecurityForExternalURL(externalURL string) (bool, http.SameSite) {
	if strings.HasPrefix(externalURL, "https://") {
		return true, http.SameSiteNoneMode
	}
	return false, http.SameSiteLaxMode
}

func GetTokenDuration(_ context.Context, _ *store.Store) time.Duration {
	tokenDuration := DefaultTokenDuration
	// maybe we can add a setting for token duration in the future

	return tokenDuration
}
