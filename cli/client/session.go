package client

import (
	"context"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/cli/config"
)

// ErrSessionExpired means the stored CLI session cannot be rotated any more: the
// refresh token was consumed and not persisted by a concurrent process, is
// expired or revoked, or the connection was cut from the Connected apps page. It
// is a refusal, not a transport failure: the recovery is always `mxd auth login`.
var ErrSessionExpired = errors.New("the CLI session is no longer valid")

// A rotation that was refused may have lost a race with another process sharing
// the credentials file: that process consumed the token and is about to write its
// replacement. The file is re-read a few times before the refusal is believed.
const (
	sessionReloadAttempts = 5
	sessionReloadInterval = 100 * time.Millisecond
)

// authServiceProcedurePrefix is the service whose Unauthenticated answers are a
// form or session error rather than an expired access token. A refresh must never
// be attempted from inside one of those calls (Login's wrong password is the
// obvious case), and the refresh RPC itself must not recurse.
const authServiceProcedurePrefix = "/metaxisdata.v1.AuthService/"

// tokenSource holds the access token the transport attaches. The session
// refresher replaces it under the mutex when it rotates the credential, so every
// later request in the same process uses the new token.
type tokenSource struct {
	mu    sync.RWMutex
	token string
}

func (t *tokenSource) get() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.token
}

func (t *tokenSource) set(token string) {
	t.mu.Lock()
	t.token = token
	t.mu.Unlock()
}

// refreshEndpoint is the one RPC the session needs. Narrowing it here keeps the
// rotation testable without a server.
type refreshEndpoint interface {
	ExchangeRefreshToken(context.Context, *connect.Request[v1pb.ExchangeRefreshTokenRequest]) (*connect.Response[v1pb.ExchangeRefreshTokenResponse], error)
}

// sessionRefresher rotates the CLI session stored in the credentials file. It
// owns the only path that may change the stored pair, so a refresh cannot race
// itself within one process and cannot write a stale token over another process's
// rotation.
type sessionRefresher struct {
	mu sync.Mutex
	// endpoint is the anonymous AuthService client; it must not carry the bearer
	// token, because a refresh is exactly the call made when that token is dead.
	endpoint refreshEndpoint
	// tokens is the live access token the transport attaches.
	tokens *tokenSource
	// path is the credentials file the rotated pair is written back to.
	path        string
	credentials *config.Credentials
	// refreshToken is the token currently in use. Guarded by mu.
	refreshToken string
	// dead is set when the server refused the refresh credential, so later calls in
	// this process report the same refusal without another round trip.
	dead bool
	// now and sleep are injected so a test does not wait.
	now   func() time.Time
	sleep func(time.Duration)
}

// interceptor replays one request after a single renewal. The replay is safe —
// the server refused the request before doing any work — and it happens at most
// once, so a session that is genuinely gone cannot put the client in a refresh
// loop.
func (r *sessionRefresher) interceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
			// The token is captured before the call so a refresh that a concurrent
			// request already performed is adopted instead of being rotated again.
			usedToken := r.tokens.get()
			response, err := next(ctx, request)
			if err == nil || connect.CodeOf(err) != connect.CodeUnauthenticated || isAuthServiceProcedure(request.Spec().Procedure) {
				return response, err
			}
			refreshed, refreshErr := r.refreshIfStale(ctx, usedToken)
			if refreshErr != nil {
				if errors.Is(refreshErr, ErrSessionExpired) {
					// Report the transport's own refusal: it is what maps to exit 2
					// and the login hint. A file-write or network failure is reported
					// as itself, so a broken config directory is not mistaken for a
					// signed-out session.
					return nil, err
				}
				return nil, refreshErr
			}
			request.Header().Set("Authorization", "Bearer "+refreshed)
			return next(ctx, request)
		}
	}
}

// refreshIfStale renews the session unless another caller already did. A waiting
// caller therefore re-reads the rotated token instead of consuming one that is
// already gone — single-use rotation makes the in-process single flight a
// correctness requirement, not an optimization.
func (r *sessionRefresher) refreshIfStale(ctx context.Context, usedToken string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current := r.tokens.get(); current != "" && current != usedToken {
		return current, nil
	}
	return r.rotateLocked(ctx)
}

// refresh forces a rotation. Logout uses it to obtain a live token for a session
// whose access token may already have expired.
func (r *sessionRefresher) refresh(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rotateLocked(ctx)
}

// rotateLocked consumes the current refresh token and adopts the replacement.
//
// A refusal is only believed after the credentials file is re-read: another
// process sharing the file may have won the rotation and written its pair. When
// nothing replaced the token, the session is genuinely gone and the refusal is
// reported; the file is deliberately left alone, because clearing it here could
// destroy a concurrent process's freshly written session.
func (r *sessionRefresher) rotateLocked(ctx context.Context) (string, error) {
	presented := r.refreshToken
	if r.dead || presented == "" {
		return "", ErrSessionExpired
	}
	for attempt := 0; ; attempt++ {
		response, err := r.endpoint.ExchangeRefreshToken(ctx, connect.NewRequest(&v1pb.ExchangeRefreshTokenRequest{RefreshToken: presented}))
		if err == nil {
			token := response.Msg.GetAccessToken()
			if saveErr := r.adopt(token, response.Msg.GetRefreshToken(), response.Msg.GetExpiresIn(), response.Msg.GetRefreshExpiresIn()); saveErr != nil {
				return "", saveErr
			}
			return token, nil
		}
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			return "", err
		}
		if token, ok := r.adoptFromDisk(presented); ok {
			return token, nil
		}
		if attempt == sessionReloadAttempts-1 {
			r.dead = true
			return "", ErrSessionExpired
		}
		r.sleep(sessionReloadInterval)
	}
}

// adopt makes the rotated pair the live one and persists it. The in-memory pair
// is authoritative for this invocation even if the write fails, but the failure is
// reported: without the write, the next invocation would have to sign in again.
func (r *sessionRefresher) adopt(accessToken, refreshToken string, accessExpiresIn, refreshExpiresIn int64) error {
	now := r.now()
	r.tokens.set(accessToken)
	r.refreshToken = refreshToken
	r.credentials.Token = accessToken
	r.credentials.RefreshToken = refreshToken
	r.credentials.TokenExpiresAt = now.Add(time.Duration(accessExpiresIn) * time.Second)
	r.credentials.RefreshExpiresAt = now.Add(time.Duration(refreshExpiresIn) * time.Second)
	if err := config.Save(r.path, r.credentials); err != nil {
		return errors.Wrap(err, "failed to save the rotated session")
	}
	return nil
}

// adoptFromDisk takes over a session another process rotated. It reports false
// when the file still holds the token this invocation presented, which means the
// refusal did not come from a concurrent rotation.
func (r *sessionRefresher) adoptFromDisk(presented string) (string, bool) {
	loaded, err := config.Load(r.path)
	if err != nil || loaded.Token == "" || loaded.RefreshToken == "" || loaded.RefreshToken == presented {
		return "", false
	}
	r.tokens.set(loaded.Token)
	r.refreshToken = loaded.RefreshToken
	*r.credentials = *loaded
	return loaded.Token, true
}

// isAuthServiceProcedure reports whether the procedure belongs to the AuthService,
// whose Unauthenticated answers are not an expired session.
func isAuthServiceProcedure(procedure string) bool {
	return strings.HasPrefix(procedure, authServiceProcedurePrefix)
}
