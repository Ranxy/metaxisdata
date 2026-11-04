package auth

import (
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// GenerateGrantAccessToken mints an access token bound to a connected app.
//
// The audience decides which surface accepts it: the MCP endpoint passes its
// resource identifier, the CLI passes the user API audience. Both kinds carry the
// client and grant ids, so TokenAuthenticator.Resolve refuses them once the
// connection is revoked — that is what makes revoking a connected app take effect
// on tokens minted before the revocation, for an MCP client and a CLI session
// alike.
func GenerateGrantAccessToken(userName string, userID int, clientID string, grantID int64, audience, scope, secret string, tokenDuration time.Duration) (string, error) {
	return generateToken(userName, userID, audience, scope, time.Now().Add(tokenDuration), []byte(secret), "", clientID, grantID)
}

// GenerateCLIAccessToken mints a CLI session's access token: a user API token that
// names the connected app the session belongs to. The user API audience keeps it
// out of /mcp, where only the MCP resource audience is accepted.
func GenerateCLIAccessToken(userName string, userID int, clientID string, grantID int64, mode common.ReleaseMode, secret string, tokenDuration time.Duration) (string, error) {
	return GenerateGrantAccessToken(userName, userID, clientID, grantID, AccessTokenAudience(mode), "", secret, tokenDuration)
}

// GenerateAgentAccessToken mints an agent principal's bearer token: an ordinary
// user-API access token for a SERVICE_ACCOUNT principal with no connected app, so
// disabling the principal (or rotating the token) ends it. Agent tokens outlive a
// human session; the caller chooses the duration.
func GenerateAgentAccessToken(principalName string, principalID int, mode common.ReleaseMode, secret string, tokenDuration time.Duration) (string, error) {
	return generateToken(principalName, principalID, AccessTokenAudience(mode), "",
		time.Now().Add(tokenDuration), []byte(secret), "", "", 0)
}
