package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const tokenTestSecret = "0123456789abcdef0123456789abcdef"

// testPKCEChallenge is the S256 challenge of a verifier, as a client computes it.
func testPKCEChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// fakeTokenStore is the token endpoint's store seam: every method the rotation
// path calls is recorded, so a test can assert what the handler did (and did
// not) do without PostgreSQL.
type fakeTokenStore struct {
	grant *store.OAuthGrant
	user  *store.UserMessage

	createID  int64
	createErr error

	rotateID  int64
	rotateOK  bool
	rotateErr error

	getErr    error
	revokeErr error

	getCalls    int
	createCalls int
	rotateCalls int

	lastGetClientID  string
	lastGetTokenHash string
	lastCreated      *store.OAuthGrant
	lastRotateOld    string
	lastRotateNew    string
	lastRotateExpiry time.Time
	lastRotateNow    time.Time

	revokedIDs []int64
	revokedBys []*int
	touched    []string
}

func (f *fakeTokenStore) GetOAuthGrant(_ context.Context, clientID, tokenHash string) (*store.OAuthGrant, error) {
	f.getCalls++
	f.lastGetClientID = clientID
	f.lastGetTokenHash = tokenHash
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.grant, nil
}

func (f *fakeTokenStore) CreateOAuthGrant(_ context.Context, create *store.OAuthGrant) (int64, error) {
	f.createCalls++
	f.lastCreated = create
	return f.createID, f.createErr
}

func (f *fakeTokenStore) RotateOAuthGrant(_ context.Context, _, oldHash, newHash string, expiresAt, now time.Time) (int64, bool, error) {
	f.rotateCalls++
	f.lastRotateOld = oldHash
	f.lastRotateNew = newHash
	f.lastRotateExpiry = expiresAt
	f.lastRotateNow = now
	return f.rotateID, f.rotateOK, f.rotateErr
}

func (f *fakeTokenStore) RevokeOAuthGrant(_ context.Context, id int64, _ time.Time, revokedBy *int) (bool, error) {
	f.revokedIDs = append(f.revokedIDs, id)
	f.revokedBys = append(f.revokedBys, revokedBy)
	return true, f.revokeErr
}

func (f *fakeTokenStore) GetUserByID(_ context.Context, id int) (*store.UserMessage, error) {
	if f.user == nil || f.user.ID != id {
		return nil, nil
	}
	return f.user, nil
}

func (f *fakeTokenStore) TouchOAuthClient(_ context.Context, clientID string) error {
	f.touched = append(f.touched, clientID)
	return nil
}

// postToken drives the token handler with one form body.
func postToken(t *testing.T, server *Server, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	server.TokenHandler().ServeHTTP(recorder, request)
	return recorder
}

// refreshForm is the refresh_token grant request one client sends.
func refreshForm(scope string) url.Values {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {"refresh-1"},
		"client_id":     {"client-1"},
		"resource":      {testResource},
	}
	if scope != "" {
		form.Set("scope", scope)
	}
	return form
}

// newTokenTestServer builds a server whose clock is pinned and whose store is the
// fake, so the rotation path runs hermetically.
func newTokenTestServer(t *testing.T, fake *fakeTokenStore, now time.Time) (*Server, *state.State) {
	t.Helper()
	stateCfg, err := state.New()
	require.NoError(t, err)
	server := NewServer(ServerConfig{
		TokenStores: fake,
		State:       stateCfg,
		Secret:      tokenTestSecret,
		Endpoints:   testEndpoints,
	})
	server.now = func() time.Time { return now }
	return server, stateCfg
}

// activeGrant is a usable grant for user 7 and the test resource.
func activeGrant(now time.Time) *store.OAuthGrant {
	return &store.OAuthGrant{
		ID:        42,
		TokenHash: auth.HashToken("refresh-1"),
		ClientID:  "client-1",
		UserID:    7,
		Resource:  testResource,
		Scope:     MCPReadScope,
		ExpiresAt: now.Add(time.Hour),
		CreatedAt: now.Add(-time.Hour),
	}
}

func TestRefreshGrantRotatesTheRowInPlace(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fake := &fakeTokenStore{
		grant:    activeGrant(now),
		user:     &store.UserMessage{ID: 7, Name: "user@example.com", Email: "user@example.com"},
		rotateID: 42,
		rotateOK: true,
	}
	server, _ := newTokenTestServer(t, fake, now)

	recorder := postToken(t, server, refreshForm(MCPReadScope))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var response tokenResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.NotEmpty(t, response.AccessToken)
	require.NotEmpty(t, response.RefreshToken)
	require.NotEqual(t, "refresh-1", response.RefreshToken)
	require.Equal(t, MCPReadScope, response.Scope)

	// The old digest was claimed, the new one was stored, and the expiry slid
	// from the pinned clock rather than from the wall clock.
	require.Equal(t, auth.HashToken("refresh-1"), fake.lastRotateOld)
	require.Equal(t, auth.HashToken(response.RefreshToken), fake.lastRotateNew)
	require.WithinDuration(t, now.Add(auth.DefaultRefreshTokenDuration), fake.lastRotateExpiry, time.Second)
	require.Equal(t, now, fake.lastRotateNow)
	require.Equal(t, []string{"client-1"}, fake.touched)

	// The id survives the rotation, so the new access token still names the same
	// connection: revocation does not have to chase a new row.
	identity, err := auth.VerifyAccessTokenFor(response.AccessToken, tokenTestSecret, testResource)
	require.NoError(t, err)
	require.Equal(t, int64(42), identity.GrantID)
	require.Equal(t, "client-1", identity.ClientID)
}

func TestRefreshGrantRefusesBeforeBurningTheCredential(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	revokedAt := now.Add(-time.Minute)
	user := &store.UserMessage{ID: 7, Name: "user@example.com"}

	tests := []struct {
		name       string
		mutate     func(*store.OAuthGrant)
		wantStatus int
		wantCode   string
		rotate     bool
	}{
		{
			name:       "expired grants are refused and the row is kept for the management page",
			mutate:     func(grant *store.OAuthGrant) { grant.ExpiresAt = now.Add(-time.Second) },
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_grant",
		},
		{
			name:       "revoked grants are terminal",
			mutate:     func(grant *store.OAuthGrant) { grant.RevokedAt = &revokedAt },
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_grant",
		},
		{
			name:       "a moved external URL requires re-authorization",
			mutate:     func(grant *store.OAuthGrant) { grant.Resource = "https://old.example.com/mcp" },
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_grant",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			grant := activeGrant(now)
			tc.mutate(grant)
			fake := &fakeTokenStore{grant: grant, user: user, rotateID: 42, rotateOK: true}
			server, _ := newTokenTestServer(t, fake, now)

			recorder := postToken(t, server, refreshForm(MCPReadScope))
			require.Equal(t, tc.wantStatus, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), tc.wantCode)
			require.Zero(t, fake.rotateCalls, "a refusal the client can correct must not burn the credential")
			require.Empty(t, fake.revokedIDs, "an expired or revoked row is not revoked again")
		})
	}
}

func TestRefreshGrantRefusesAScopeDrift(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fake := &fakeTokenStore{
		grant:    activeGrant(now),
		user:     &store.UserMessage{ID: 7, Name: "user@example.com"},
		rotateID: 42,
		rotateOK: true,
	}
	server, _ := newTokenTestServer(t, fake, now)

	recorder := postToken(t, server, refreshForm("some.other.scope"))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "invalid_scope")
	require.Zero(t, fake.rotateCalls, "a scope correction must not cost the client its grant")
}

func TestRefreshGrantLosesTheRotationRace(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fake := &fakeTokenStore{
		grant:    activeGrant(now),
		user:     &store.UserMessage{ID: 7, Name: "user@example.com"},
		rotateOK: false,
	}
	server, _ := newTokenTestServer(t, fake, now)

	recorder := postToken(t, server, refreshForm(MCPReadScope))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "invalid_grant")
	require.Equal(t, 1, fake.rotateCalls)
	require.Empty(t, fake.revokedIDs, "a lost race is not a revocation")
}

func TestRefreshGrantEndsAnUnusableGrantAfterRotating(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	changedAt := now.Add(-30 * time.Minute)
	tests := []struct {
		name string
		user *store.UserMessage
	}{
		{
			name: "the principal no longer exists",
			user: nil,
		},
		{
			name: "the principal was deactivated",
			user: &store.UserMessage{ID: 7, Name: "user@example.com", MemberDeleted: true},
		},
		{
			name: "the grant predates the last password change",
			user: &store.UserMessage{
				ID:      7,
				Name:    "user@example.com",
				Profile: &storepb.UserProfile{LastChangePasswordTime: timestamppb.New(changedAt)},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeTokenStore{
				grant:    activeGrant(now),
				user:     tc.user,
				rotateID: 42,
				rotateOK: true,
			}
			server, stateCfg := newTokenTestServer(t, fake, now)

			recorder := postToken(t, server, refreshForm(MCPReadScope))
			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), "invalid_grant")

			// The rotation already replaced the digest, so the row must be ended
			// explicitly: a system revocation with no revoker.
			require.Equal(t, []int64{42}, fake.revokedIDs)
			require.Equal(t, []*int{nil}, fake.revokedBys)
			// And this replica refuses its tokens immediately, not after the cache
			// window.
			revoked, fresh := stateCfg.GrantRevocationCache.Lookup("42", now)
			require.True(t, fresh)
			require.True(t, revoked)
		})
	}
}

func TestAuthorizationCodeExchangeCreatesTheGrantBeforeTheToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fake := &fakeTokenStore{
		user:     &store.UserMessage{ID: 7, Name: "user@example.com", Email: "user@example.com"},
		createID: 99,
	}
	server, stateCfg := newTokenTestServer(t, fake, now)

	verifier := strings.Repeat("integration-verifier-", 3)
	pending, err := stateCfg.OAuthAuthorizationRequestStore.Create(state.OAuthAuthorizationRequest{
		ClientID:            "client-1",
		ClientName:          "Agent",
		RedirectURI:         "https://app.example.com/callback",
		Resource:            testResource,
		Scopes:              []string{MCPReadScope},
		CodeChallenge:       testPKCEChallenge(verifier),
		CodeChallengeMethod: "S256",
		UserID:              7,
	}, now)
	require.NoError(t, err)
	require.NoError(t, stateCfg.OAuthAuthorizationRequestStore.Approve(pending.RequestID, 7, true, now))
	completed, err := stateCfg.OAuthAuthorizationRequestStore.Complete(pending.RequestID, 7, now)
	require.NoError(t, err)

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {completed.Code},
		"client_id":     {"client-1"},
		"redirect_uri":  {"https://app.example.com/callback"},
		"code_verifier": {verifier},
		"resource":      {testResource},
	}
	recorder := postToken(t, server, form)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var response tokenResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, 1, fake.createCalls, "the grant row is created once")
	require.Equal(t, auth.HashToken(response.RefreshToken), fake.lastCreated.TokenHash)
	require.Equal(t, now, fake.lastCreated.CreatedAt, "created_at is the authorization time carried across rotations")
	require.Equal(t, MCPReadScope, fake.lastCreated.Scope)

	// The access token names the row the store just created, which is what makes
	// a later revocation reach tokens minted before it.
	identity, err := auth.VerifyAccessTokenFor(response.AccessToken, tokenTestSecret, testResource)
	require.NoError(t, err)
	require.Equal(t, int64(99), identity.GrantID)
}

func TestRefreshGrantPropagatesStoreFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fake := &fakeTokenStore{
		grant:    activeGrant(now),
		user:     &store.UserMessage{ID: 7, Name: "user@example.com"},
		getErr:   errors.New("store unavailable"),
		rotateOK: true,
	}
	server, _ := newTokenTestServer(t, fake, now)

	recorder := postToken(t, server, refreshForm(MCPReadScope))
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Zero(t, fake.rotateCalls, "a lookup failure must not rotate")
}
