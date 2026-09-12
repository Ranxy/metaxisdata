package server

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/Ranxy/metaxisdata/backend/config"
)

// csrfProtectionMiddleware rejects state-changing requests that ride the web
// session cookie but come from another site. Bearer/API-token requests are
// exempt because a browser never attaches those automatically, so they are not
// forgeable.
//
// The check is a second line of defense: the cookie is SameSite=Lax unless the
// deployment advertises an https external URL, where cross-origin frontends
// require SameSite=None. Origins listed in the CORS allowlist are trusted.
func csrfProtectionMiddleware(profile *config.Profile) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			switch req.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				return next(c)
			}
			if req.Header.Get("Authorization") != "" {
				return next(c)
			}
			if !hasSessionCookie(req) {
				return next(c)
			}
			if isTrustedRequestOrigin(req, profile.CORSAllowOrigins) {
				return next(c)
			}
			return echo.NewHTTPError(http.StatusForbidden, "cross-site request rejected")
		}
	}
}

func hasSessionCookie(req *http.Request) bool {
	if _, err := req.Cookie(accessTokenCookieName); err == nil {
		return true
	}
	// The cookie may not be parsed yet, so fall back to the raw header.
	return strings.Contains(req.Header.Get("Cookie"), accessTokenCookieName+"=")
}

// accessTokenCookieName mirrors auth.AccessTokenCookieName. It is duplicated to
// keep this middleware free of a dependency cycle on the auth package.
const accessTokenCookieName = "access-token"

// isTrustedRequestOrigin decides whether a browser request originated from this
// site. Modern browsers send Sec-Fetch-Site; older ones send Origin (for
// cross-site POSTs) or Referer. A request without any of them is a non-browser
// client, which cannot be a CSRF vector.
func isTrustedRequestOrigin(req *http.Request, allowedOrigins []string) bool {
	if site := req.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none" || site == "same-site"
	}
	origin := req.Header.Get("Origin")
	if origin == "" {
		origin = refererOrigin(req.Header.Get("Referer"))
	}
	if origin == "" || origin == "null" {
		// "null" is the opaque origin of sandboxed iframes and data: URLs.
		return origin == ""
	}
	if _, err := url.Parse(origin); err != nil {
		return false
	}
	for _, allowed := range allowedOrigins {
		if strings.EqualFold(strings.TrimRight(allowed, "/"), strings.TrimRight(origin, "/")) {
			return true
		}
	}
	return sameHost(origin, req.Host)
}

func refererOrigin(referer string) string {
	if referer == "" {
		return ""
	}
	parsed, err := url.Parse(referer)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func sameHost(origin, host string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, host)
}
