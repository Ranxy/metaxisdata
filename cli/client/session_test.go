package client

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/cli/config"
)

// fakeRefreshEndpoint answers ExchangeRefreshToken for a test and records what it
// was asked to consume.
type fakeRefreshEndpoint struct {
	requests []string
	handler  func(refreshToken string) (*v1pb.ExchangeRefreshTokenResponse, error)
}

func (f *fakeRefreshEndpoint) ExchangeRefreshToken(_ context.Context, req *connect.Request[v1pb.ExchangeRefreshTokenRequest]) (*connect.Response[v1pb.ExchangeRefreshTokenResponse], error) {
	f.requests = append(f.requests, req.Msg.GetRefreshToken())
	if f.handler == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, context.Canceled)
	}
	response, err := f.handler(req.Msg.GetRefreshToken())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

// newTestSession builds a refresher over a temporary credentials file.
func newTestSession(t *testing.T, endpoint refreshEndpoint, path string, credentials *config.Credentials) *sessionRefresher {
	t.Helper()
	require.NoError(t, config.Save(path, credentials))
	return &sessionRefresher{
		endpoint:     endpoint,
		tokens:       &tokenSource{token: credentials.Token},
		path:         path,
		credentials:  credentials,
		refreshToken: credentials.RefreshToken,
		now:          func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) },
		sleep:        func(time.Duration) {},
	}
}

// A rotation adopts the replacement pair, writes it back, and makes it the live
// token for every later request in the process.
func TestSessionRotationPersistsAndAdoptsThePair(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	credentials := &config.Credentials{Server: "https://mx.example.com", Token: "old-token", RefreshToken: "old-refresh"}
	endpoint := &fakeRefreshEndpoint{handler: func(refreshToken string) (*v1pb.ExchangeRefreshTokenResponse, error) {
		require.Equal(t, "old-refresh", refreshToken)
		return &v1pb.ExchangeRefreshTokenResponse{
			AccessToken:      "new-token",
			RefreshToken:     "new-refresh",
			ExpiresIn:        3600,
			RefreshExpiresIn: 7200,
		}, nil
	}}
	refresher := newTestSession(t, endpoint, path, credentials)

	token, err := refresher.refreshIfStale(context.Background(), "old-token")
	require.NoError(t, err)
	require.Equal(t, "new-token", token)
	require.Equal(t, "new-token", refresher.tokens.get())

	stored, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, "new-token", stored.Token)
	require.Equal(t, "new-refresh", stored.RefreshToken)
	require.True(t, stored.RefreshExpiresAt.Equal(refresher.now().Add(2*time.Hour)))

	// A caller that was refused with the old token re-reads the rotated one instead
	// of consuming the replacement: rotation is single-use.
	replayed, err := refresher.refreshIfStale(context.Background(), "old-token")
	require.NoError(t, err)
	require.Equal(t, "new-token", replayed)
	require.Len(t, endpoint.requests, 1)
}

// Two processes share one file. The loser's rotation is refused, but the winner's
// rotated pair is on disk, so the loser adopts it rather than signing the user out.
func TestSessionAdoptsAConcurrentRotationFromDisk(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	credentials := &config.Credentials{
		Server:       "https://mx.example.com",
		Token:        "old-token",
		RefreshToken: "old-refresh",
		User:         &config.User{Email: "dev@example.com"},
	}
	endpoint := &fakeRefreshEndpoint{handler: func(string) (*v1pb.ExchangeRefreshTokenResponse, error) {
		return nil, connect.NewError(connect.CodeUnauthenticated, context.Canceled)
	}}
	refresher := newTestSession(t, endpoint, path, credentials)

	// The other process already rotated and wrote its pair. The identity is
	// preserved, which is what distinguishes a rotation from a different login.
	rotated := &config.Credentials{
		Server:           "https://mx.example.com",
		Token:            "winner-token",
		RefreshToken:     "winner-refresh",
		User:             &config.User{Email: "dev@example.com"},
		RefreshExpiresAt: refresher.now().Add(time.Hour),
	}
	require.NoError(t, config.Save(path, rotated))

	token, err := refresher.refreshIfStale(context.Background(), "old-token")
	require.NoError(t, err)
	require.Equal(t, "winner-token", token)
	require.Equal(t, "winner-token", refresher.tokens.get())
	require.Equal(t, "winner-refresh", credentials.RefreshToken)
}

// A refusal that nothing replaced is real: the session is gone, and the file is
// left alone so a concurrent process's rotation cannot be clobbered.
func TestSessionRefusalIsNotWrittenBack(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	credentials := &config.Credentials{Server: "https://mx.example.com", Token: "old-token", RefreshToken: "old-refresh"}
	endpoint := &fakeRefreshEndpoint{handler: func(string) (*v1pb.ExchangeRefreshTokenResponse, error) {
		return nil, connect.NewError(connect.CodeUnauthenticated, context.Canceled)
	}}
	refresher := newTestSession(t, endpoint, path, credentials)

	_, err := refresher.refreshIfStale(context.Background(), "old-token")
	require.ErrorIs(t, err, ErrSessionExpired)
	require.Len(t, endpoint.requests, 1,
		"a deterministic refusal is not re-presented; only the file re-read is retried")

	stored, loadErr := config.Load(path)
	require.NoError(t, loadErr)
	require.Equal(t, "old-refresh", stored.RefreshToken)
}

// A transport failure is not a signed-out session: it is reported as itself, and
// the stored pair is not touched.
func TestSessionTransportFailureIsReportedAsItself(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	credentials := &config.Credentials{Server: "https://mx.example.com", Token: "old-token", RefreshToken: "old-refresh"}
	endpoint := &fakeRefreshEndpoint{handler: func(string) (*v1pb.ExchangeRefreshTokenResponse, error) {
		return nil, connect.NewError(connect.CodeUnavailable, context.Canceled)
	}}
	refresher := newTestSession(t, endpoint, path, credentials)

	_, err := refresher.refreshIfStale(context.Background(), "old-token")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrSessionExpired)
	require.Len(t, endpoint.requests, 1, "a transport failure is not retried as a lost race")
}

// RefreshSession on a client whose token came from a flag or the environment has
// no session to rotate and must not fail the caller.
func TestRefreshSessionIsANoOpWithoutAStoredSession(t *testing.T) {
	t.Parallel()

	connection := &Client{}
	require.NoError(t, connection.RefreshSession(context.Background()))
}

// The re-read loop exists for a writer that lands while this process waits, not
// only for one that already finished before the refusal.
func TestSessionAdoptsAWriterThatLandsDuringTheWait(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	credentials := &config.Credentials{
		Server:       "https://mx.example.com",
		Token:        "old-token",
		RefreshToken: "old-refresh",
		User:         &config.User{Email: "dev@example.com"},
	}
	endpoint := &fakeRefreshEndpoint{handler: func(string) (*v1pb.ExchangeRefreshTokenResponse, error) {
		return nil, connect.NewError(connect.CodeUnauthenticated, context.Canceled)
	}}
	refresher := newTestSession(t, endpoint, path, credentials)

	rotated := &config.Credentials{
		Server:       "https://mx.example.com",
		Token:        "winner-token",
		RefreshToken: "winner-refresh",
		User:         &config.User{Email: "dev@example.com"},
	}
	writes := 0
	refresher.sleep = func(time.Duration) {
		if writes == 0 {
			writes++
			require.NoError(t, config.Save(path, rotated))
		}
	}

	token, err := refresher.refreshIfStale(context.Background(), "old-token")
	require.NoError(t, err)
	require.Equal(t, "winner-token", token)
	require.Equal(t, 1, writes)
	require.Len(t, endpoint.requests, 1, "the refusal is not re-presented while waiting")
}

// A different login (another account or another server) written to the same file
// must not be adopted: replaying the caller's request under it would be a
// confused deputy.
func TestSessionDoesNotAdoptADifferentLogin(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		rotated  *config.Credentials
		wantFail bool
	}{
		{
			name: "another user's login",
			rotated: &config.Credentials{
				Server:       "https://mx.example.com",
				Token:        "other-token",
				RefreshToken: "other-refresh",
				User:         &config.User{Email: "someone@example.com"},
			},
			wantFail: true,
		},
		{
			name: "a login against another server",
			rotated: &config.Credentials{
				Server:       "https://other.example.com",
				Token:        "other-token",
				RefreshToken: "other-refresh",
				User:         &config.User{Email: "dev@example.com"},
			},
			wantFail: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.json")
			credentials := &config.Credentials{
				Server:       "https://mx.example.com",
				Token:        "old-token",
				RefreshToken: "old-refresh",
				User:         &config.User{Email: "dev@example.com"},
			}
			endpoint := &fakeRefreshEndpoint{handler: func(string) (*v1pb.ExchangeRefreshTokenResponse, error) {
				return nil, connect.NewError(connect.CodeUnauthenticated, context.Canceled)
			}}
			refresher := newTestSession(t, endpoint, path, credentials)
			refresher.now = func() time.Time { return now }

			require.NoError(t, config.Save(path, tc.rotated))

			_, err := refresher.refreshIfStale(context.Background(), "old-token")
			require.ErrorIs(t, err, ErrSessionExpired)
			require.Equal(t, "old-token", refresher.tokens.get(),
				"a different login must never become the live credential of this process")
			require.Equal(t, "dev@example.com", credentials.User.Email)
		})
	}
}

// The interceptor replays a refused request once after renewing the session, and
// the replay carries the rotated token.
func TestInterceptorRefreshesAndReplaysOnce(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	credentials := &config.Credentials{
		Server:       "https://mx.example.com",
		Token:        "old-token",
		RefreshToken: "old-refresh",
		User:         &config.User{Email: "dev@example.com"},
	}
	endpoint := &fakeRefreshEndpoint{handler: func(string) (*v1pb.ExchangeRefreshTokenResponse, error) {
		return &v1pb.ExchangeRefreshTokenResponse{
			AccessToken:      "new-token",
			RefreshToken:     "new-refresh",
			ExpiresIn:        3600,
			RefreshExpiresIn: 7200,
		}, nil
	}}
	refresher := newTestSession(t, endpoint, path, credentials)

	var calls int
	var authorization []string
	next := func(_ context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
		calls++
		authorization = append(authorization, request.Header().Get("Authorization"))
		if calls == 1 {
			return nil, connect.NewError(connect.CodeUnauthenticated, context.Canceled)
		}
		return connect.NewResponse(&emptypb.Empty{}), nil
	}

	response, err := refresher.interceptor()(next)(context.Background(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Equal(t, 2, calls, "one refresh, one replay")
	require.Len(t, endpoint.requests, 1)
	require.Equal(t, "Bearer new-token", authorization[1])
}

// A refused renewal returns the transport's own 401 (exit code 2), and the
// interceptor does not retry it a second time.
func TestInterceptorReturnsTheOriginalRefusal(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	credentials := &config.Credentials{
		Server:       "https://mx.example.com",
		Token:        "old-token",
		RefreshToken: "old-refresh",
		User:         &config.User{Email: "dev@example.com"},
	}
	endpoint := &fakeRefreshEndpoint{handler: func(string) (*v1pb.ExchangeRefreshTokenResponse, error) {
		return nil, connect.NewError(connect.CodeUnauthenticated, context.Canceled)
	}}
	refresher := newTestSession(t, endpoint, path, credentials)

	refusal := connect.NewError(connect.CodeUnauthenticated, context.Canceled)
	var calls int
	next := func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		calls++
		return nil, refusal
	}
	_, err := refresher.interceptor()(next)(context.Background(), connect.NewRequest(&emptypb.Empty{}))
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	require.Same(t, refusal, err, "the caller sees the server's refusal, not a synthetic one")
	require.Equal(t, 1, calls, "a refused renewal is not retried")
}

// A renewal that fails for a transport or disk reason is reported as itself, so a
// broken config directory is not mistaken for a signed-out session.
func TestInterceptorSurfacesARenewalFailure(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	credentials := &config.Credentials{
		Server:       "https://mx.example.com",
		Token:        "old-token",
		RefreshToken: "old-refresh",
		User:         &config.User{Email: "dev@example.com"},
	}
	endpoint := &fakeRefreshEndpoint{handler: func(string) (*v1pb.ExchangeRefreshTokenResponse, error) {
		return nil, connect.NewError(connect.CodeUnavailable, context.Canceled)
	}}
	refresher := newTestSession(t, endpoint, path, credentials)

	next := func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, connect.NewError(connect.CodeUnauthenticated, context.Canceled)
	}
	_, err := refresher.interceptor()(next)(context.Background(), connect.NewRequest(&emptypb.Empty{}))
	require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err))
}

// The interceptor's early return uses this classifier. A behavior-level test
// cannot stub connect.AnyRequest (it has unexported methods), so this pins the
// classifier itself; the other interceptor tests pin the refresh/replay behavior.
func TestAuthServiceProcedureClassification(t *testing.T) {
	t.Parallel()

	require.True(t, isAuthServiceProcedure("/metaxisdata.v1.AuthService/Login"))
	require.True(t, isAuthServiceProcedure("/metaxisdata.v1.AuthService/ExchangeRefreshToken"))
	require.False(t, isAuthServiceProcedure("/metaxisdata.v1.UserService/GetCurrentUser"))
}
