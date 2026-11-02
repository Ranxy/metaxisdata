package client

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

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
	credentials := &config.Credentials{Server: "https://mx.example.com", Token: "old-token", RefreshToken: "old-refresh"}
	endpoint := &fakeRefreshEndpoint{handler: func(string) (*v1pb.ExchangeRefreshTokenResponse, error) {
		return nil, connect.NewError(connect.CodeUnauthenticated, context.Canceled)
	}}
	refresher := newTestSession(t, endpoint, path, credentials)

	// The other process already rotated and wrote its pair.
	rotated := &config.Credentials{
		Server:           "https://mx.example.com",
		Token:            "winner-token",
		RefreshToken:     "winner-refresh",
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
	require.Len(t, endpoint.requests, sessionReloadAttempts,
		"a refusal is retried while a concurrent rotation may still be writing")

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
