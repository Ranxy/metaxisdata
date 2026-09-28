package auth

import "time"

// GenerateMCPAccessToken mints an access token for the MCP resource server.
//
// The audience is the MCP endpoint's canonical resource identifier (RFC 8707),
// which is the same string the protected-resource metadata advertises. Binding
// the token to that identifier means VerifyAccessToken keeps refusing it on the
// user API, and a token issued by another deployment never verifies here. scope
// is what the client was granted; the MCP endpoint requires the one scope its
// metadata advertises.
func GenerateMCPAccessToken(userName string, userID int, audience, scope, secret string, tokenDuration time.Duration) (string, error) {
	return generateToken(userName, userID, audience, scope, time.Now().Add(tokenDuration), []byte(secret), "")
}
