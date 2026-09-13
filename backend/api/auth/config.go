package auth

import (
	"github.com/Ranxy/metaxisdata/backend/common"
)

// IsAuthenticationAllowed returns whether the method is exempted from authentication.
//
// gRPC reflection is intentionally absent: its handlers are registered without
// the authentication interceptor and are therefore anonymous regardless of this
// function, and blanket-exempting the /grpc.reflection prefix here would be
// unreachable code.
func IsAuthenticationAllowed(_ string, authContext *common.AuthContext) bool {
	return authContext.AllowWithoutCredential
}
