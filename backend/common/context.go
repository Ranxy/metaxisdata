//nolint:revive
package common

import "context"

// ContextKey is the key type of context value.
type ContextKey int

const (
	// UserContextKey is the key name used to store user message in the context.
	UserContextKey ContextKey = iota
	AuthContextKey
	// TokenRestrictionContextKey is the key name used to store the restriction
	// carried by the access token that authenticated the request. It is absent
	// for a full-access token.
	TokenRestrictionContextKey
)

type AuthContext struct {
	Audit                  bool
	AllowWithoutCredential bool
	Permission             string
}

func GetAuthContextFromContext(ctx context.Context) (*AuthContext, bool) {
	authCtx, ok := ctx.Value(AuthContextKey).(*AuthContext)
	return authCtx, ok
}
