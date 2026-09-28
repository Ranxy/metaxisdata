package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestOAuthAuthorizationStore() (*OAuthAuthorizationRequestStore, time.Time) {
	return NewOAuthAuthorizationRequestStore(), time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
}

func oauthAuthorizationRequestFor(userID int) OAuthAuthorizationRequest {
	return OAuthAuthorizationRequest{
		ClientID:            "client-1",
		ClientName:          "Claude Desktop",
		RedirectURI:         "http://127.0.0.1:7777/callback",
		Resource:            "https://metaxis.example/mcp",
		Scopes:              []string{"metaxisdata.mcp.read"},
		CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		CodeChallengeMethod: "S256",
		ClientState:         "client-state",
		UserID:              userID,
		RequestIP:           "203.0.113.7",
	}
}

func mustCreateOAuthRequest(t *testing.T, s *OAuthAuthorizationRequestStore, now time.Time, userID int) OAuthAuthorizationRequest {
	t.Helper()
	request, err := s.Create(oauthAuthorizationRequestFor(userID), now)
	require.NoError(t, err)
	return request
}

func mustApproveOAuthRequest(t *testing.T, s *OAuthAuthorizationRequestStore, requestID string, userID int, now time.Time) {
	t.Helper()
	require.NoError(t, s.Approve(requestID, userID, true, now))
}

func mustCompleteOAuthRequest(t *testing.T, s *OAuthAuthorizationRequestStore, requestID string, userID int, now time.Time) OAuthAuthorizationRequest {
	t.Helper()
	request, err := s.Complete(requestID, userID, now)
	require.NoError(t, err)
	return request
}

// mustCompleteApprovedOAuthRequest walks one request through approval and
// completion, which is the state every Exchange test starts from.
func mustCompleteApprovedOAuthRequest(t *testing.T, s *OAuthAuthorizationRequestStore, now time.Time, userID int) OAuthAuthorizationRequest {
	t.Helper()
	request := mustCreateOAuthRequest(t, s, now, userID)
	mustApproveOAuthRequest(t, s, request.RequestID, userID, now)
	return mustCompleteOAuthRequest(t, s, request.RequestID, userID, now)
}

func TestOAuthAuthorizationCreateMintsTheRequestIdentity(t *testing.T) {
	t.Parallel()

	s, now := newTestOAuthAuthorizationStore()
	supplied := oauthAuthorizationRequestFor(7)
	supplied.RequestID = "caller-supplied"
	supplied.State = OAuthAuthorizationApproved
	supplied.Code = "caller-supplied"
	supplied.ApprovedByUserID = 99
	supplied.CodeExpireTime = now.Add(time.Hour)

	created, err := s.Create(supplied, now)
	require.NoError(t, err)

	require.NotEmpty(t, created.RequestID)
	require.NotEqual(t, "caller-supplied", created.RequestID, "the request id is the store's to assign")
	require.Equal(t, OAuthAuthorizationPending, created.State, "a caller cannot seed an approved request")
	require.Empty(t, created.Code, "no code exists before Complete")
	require.Zero(t, created.ApprovedByUserID)
	require.True(t, created.CodeExpireTime.IsZero())
	require.Equal(t, now, created.CreateTime)
	require.Equal(t, now.Add(OAuthAuthorizationRequestTTL), created.ExpireTime)

	// Everything the browser flow displays is carried through unchanged.
	require.Equal(t, "client-1", created.ClientID)
	require.Equal(t, "Claude Desktop", created.ClientName)
	require.Equal(t, "http://127.0.0.1:7777/callback", created.RedirectURI)
	require.Equal(t, "https://metaxis.example/mcp", created.Resource)
	require.Equal(t, []string{"metaxisdata.mcp.read"}, created.Scopes)
	require.Equal(t, "S256", created.CodeChallengeMethod)
	require.Equal(t, "client-state", created.ClientState)
	require.Equal(t, 7, created.UserID)
	require.Equal(t, "203.0.113.7", created.RequestIP)

	stored, err := s.Get(created.RequestID, 7, now)
	require.NoError(t, err)
	require.Equal(t, created, stored)
}

func TestOAuthAuthorizationRefusesAnotherUsersRequest(t *testing.T) {
	t.Parallel()

	s, now := newTestOAuthAuthorizationStore()
	pending := mustCreateOAuthRequest(t, s, now, 7)
	approved := mustCreateOAuthRequest(t, s, now, 7)
	mustApproveOAuthRequest(t, s, approved.RequestID, 7, now)

	_, err := s.Get(pending.RequestID, 8, now)
	require.ErrorIs(t, err, ErrOAuthRequestWrongUser)
	require.ErrorIs(t, s.Approve(pending.RequestID, 8, true, now), ErrOAuthRequestWrongUser)
	_, err = s.Complete(pending.RequestID, 8, now)
	require.ErrorIs(t, err, ErrOAuthRequestWrongUser)

	// An approved request is just as protected, and a refusal changes nothing.
	require.ErrorIs(t, s.Approve(approved.RequestID, 8, false, now), ErrOAuthRequestWrongUser)
	_, err = s.Complete(approved.RequestID, 8, now)
	require.ErrorIs(t, err, ErrOAuthRequestWrongUser)

	stored, err := s.Get(pending.RequestID, 7, now)
	require.NoError(t, err)
	require.Equal(t, OAuthAuthorizationPending, stored.State)

	stored, err = s.Get(approved.RequestID, 7, now)
	require.NoError(t, err)
	require.Equal(t, OAuthAuthorizationApproved, stored.State)
	require.Empty(t, stored.Code, "a foreign Complete never mints a code")
}

func TestOAuthAuthorizationIsScopedToItsOwner(t *testing.T) {
	t.Parallel()

	s, now := newTestOAuthAuthorizationStore()
	completed := mustCompleteApprovedOAuthRequest(t, s, now, 8)
	require.Equal(t, 8, completed.UserID)

	// The request is invisible to everybody else, but the code itself carries
	// its owner through an exchange.
	_, err := s.Get(completed.RequestID, 7, now)
	require.ErrorIs(t, err, ErrOAuthRequestWrongUser)
	answer, err := s.Exchange(completed.Code, now)
	require.NoError(t, err)
	require.Equal(t, 8, answer.UserID)
}

func TestOAuthAuthorizationApproveRecordsTheDecisionOnce(t *testing.T) {
	t.Parallel()

	s, now := newTestOAuthAuthorizationStore()
	request := mustCreateOAuthRequest(t, s, now, 7)

	require.NoError(t, s.Approve(request.RequestID, 7, true, now))
	decided, err := s.Get(request.RequestID, 7, now)
	require.NoError(t, err)
	require.Equal(t, OAuthAuthorizationApproved, decided.State)
	require.Equal(t, 7, decided.ApprovedByUserID)

	// A second decision is refused, and the first one stands.
	require.ErrorIs(t, s.Approve(request.RequestID, 7, false, now), ErrOAuthRequestNotPending)
	again, err := s.Get(request.RequestID, 7, now)
	require.NoError(t, err)
	require.Equal(t, OAuthAuthorizationApproved, again.State)
	require.Equal(t, 7, again.ApprovedByUserID)

	denied, err := s.Create(oauthAuthorizationRequestFor(7), now)
	require.NoError(t, err)
	require.NoError(t, s.Approve(denied.RequestID, 7, false, now))
	stored, err := s.Get(denied.RequestID, 7, now)
	require.NoError(t, err)
	require.Equal(t, OAuthAuthorizationDenied, stored.State)
	require.Zero(t, stored.ApprovedByUserID, "a denial approves nobody")

	// A denial is final too.
	require.ErrorIs(t, s.Approve(denied.RequestID, 7, true, now), ErrOAuthRequestNotPending)
}

func TestOAuthAuthorizationCompleteMintsOneCode(t *testing.T) {
	t.Parallel()

	s, now := newTestOAuthAuthorizationStore()

	pending := mustCreateOAuthRequest(t, s, now, 7)
	_, err := s.Complete(pending.RequestID, 7, now)
	require.ErrorIs(t, err, ErrOAuthRequestNotPending)

	denied, err := s.Create(oauthAuthorizationRequestFor(7), now)
	require.NoError(t, err)
	require.NoError(t, s.Approve(denied.RequestID, 7, false, now))
	_, err = s.Complete(denied.RequestID, 7, now)
	require.ErrorIs(t, err, ErrOAuthRequestNotApproved)

	approved := mustCreateOAuthRequest(t, s, now, 7)
	mustApproveOAuthRequest(t, s, approved.RequestID, 7, now)
	completed := mustCompleteOAuthRequest(t, s, approved.RequestID, 7, now)
	require.NotEmpty(t, completed.Code)
	require.Equal(t, now.Add(OAuthAuthorizationCodeTTL), completed.CodeExpireTime)
	require.Equal(t, OAuthAuthorizationApproved, completed.State)
	require.Equal(t, 7, completed.ApprovedByUserID)

	// Calling Complete again never hands out a second code.
	second, err := s.Complete(approved.RequestID, 7, now)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrOAuthRequestNotPending)
	require.Empty(t, second.Code)
	stored, err := s.Get(approved.RequestID, 7, now)
	require.NoError(t, err)
	require.Equal(t, completed.Code, stored.Code, "the already minted code is unchanged")
}

func TestOAuthAuthorizationCodeIsSingleUse(t *testing.T) {
	t.Parallel()

	s, now := newTestOAuthAuthorizationStore()
	completed := mustCompleteApprovedOAuthRequest(t, s, now, 7)

	first, err := s.Exchange(completed.Code, now)
	require.NoError(t, err)
	require.Equal(t, completed.Code, first.Code)
	require.Equal(t, 7, first.UserID)
	require.Equal(t, completed.RequestID, first.RequestID)

	_, err = s.Exchange(completed.Code, now)
	require.ErrorIs(t, err, ErrOAuthCodeNotFound, "a code is exchanged exactly once")

	// The consumed record is gone from both lookups.
	_, err = s.Get(completed.RequestID, 7, now)
	require.ErrorIs(t, err, ErrOAuthRequestNotFound)
	require.NotContains(t, s.byCode, completed.Code)
}

func TestOAuthAuthorizationRequestExpiresLazily(t *testing.T) {
	t.Parallel()

	s, now := newTestOAuthAuthorizationStore()
	request := mustCreateOAuthRequest(t, s, now, 7)

	// The deadline is inclusive: at the TTL the error replaces the stale record.
	_, err := s.Get(request.RequestID, 7, now.Add(OAuthAuthorizationRequestTTL))
	require.ErrorIs(t, err, ErrOAuthRequestExpired)
	_, err = s.Get(request.RequestID, 7, now.Add(OAuthAuthorizationRequestTTL))
	require.ErrorIs(t, err, ErrOAuthRequestNotFound, "the expired record is dropped")

	toApprove, err := s.Create(oauthAuthorizationRequestFor(7), now)
	require.NoError(t, err)
	require.ErrorIs(t, s.Approve(toApprove.RequestID, 7, true, now.Add(OAuthAuthorizationRequestTTL)), ErrOAuthRequestExpired)

	toComplete, err := s.Create(oauthAuthorizationRequestFor(7), now)
	require.NoError(t, err)
	_, err = s.Complete(toComplete.RequestID, 7, now.Add(OAuthAuthorizationRequestTTL))
	require.ErrorIs(t, err, ErrOAuthRequestExpired)

	// An approval that is never completed dies with the pending TTL too.
	uncompleted, err := s.Create(oauthAuthorizationRequestFor(7), now)
	require.NoError(t, err)
	mustApproveOAuthRequest(t, s, uncompleted.RequestID, 7, now)
	_, err = s.Complete(uncompleted.RequestID, 7, now.Add(OAuthAuthorizationRequestTTL))
	require.ErrorIs(t, err, ErrOAuthRequestExpired)
}

func TestOAuthAuthorizationCodeExpiresLazily(t *testing.T) {
	t.Parallel()

	// A code outlives the consent window it came from: only its own TTL bounds
	// it, so an exchange late in the pending TTL still works.
	s, now := newTestOAuthAuthorizationStore()
	request := mustCreateOAuthRequest(t, s, now, 7)
	late := now.Add(OAuthAuthorizationRequestTTL - time.Second)
	mustApproveOAuthRequest(t, s, request.RequestID, 7, late)
	completed := mustCompleteOAuthRequest(t, s, request.RequestID, 7, late)

	exchanged, err := s.Exchange(completed.Code, now.Add(OAuthAuthorizationRequestTTL+time.Second))
	require.NoError(t, err)
	require.Equal(t, completed.Code, exchanged.Code)

	// Past the code TTL the code is reported expired and dropped.
	s2, now2 := newTestOAuthAuthorizationStore()
	completed2 := mustCompleteApprovedOAuthRequest(t, s2, now2, 7)
	_, err = s2.Exchange(completed2.Code, now2.Add(OAuthAuthorizationCodeTTL))
	require.ErrorIs(t, err, ErrOAuthCodeExpired)
	_, err = s2.Exchange(completed2.Code, now2.Add(OAuthAuthorizationCodeTTL))
	require.ErrorIs(t, err, ErrOAuthCodeNotFound, "the expired code is dropped")

	// A read of a completed request is bounded by the code TTL as well.
	s3, now3 := newTestOAuthAuthorizationStore()
	completed3 := mustCompleteApprovedOAuthRequest(t, s3, now3, 7)
	_, err = s3.Get(completed3.RequestID, 7, now3.Add(OAuthAuthorizationCodeTTL))
	require.ErrorIs(t, err, ErrOAuthCodeExpired)

	// One nanosecond earlier both the read and the exchange still work.
	s4, now4 := newTestOAuthAuthorizationStore()
	completed4 := mustCompleteApprovedOAuthRequest(t, s4, now4, 7)
	alive := now4.Add(OAuthAuthorizationCodeTTL - time.Nanosecond)
	_, err = s4.Get(completed4.RequestID, 7, alive)
	require.NoError(t, err)
	_, err = s4.Exchange(completed4.Code, alive)
	require.NoError(t, err)
}

func TestOAuthAuthorizationUnknownLookups(t *testing.T) {
	t.Parallel()

	s, now := newTestOAuthAuthorizationStore()

	_, err := s.Get("nope", 7, now)
	require.ErrorIs(t, err, ErrOAuthRequestNotFound)
	require.ErrorIs(t, s.Approve("nope", 7, true, now), ErrOAuthRequestNotFound)
	_, err = s.Complete("nope", 7, now)
	require.ErrorIs(t, err, ErrOAuthRequestNotFound)
	_, err = s.Exchange("nope", now)
	require.ErrorIs(t, err, ErrOAuthCodeNotFound)
}

func TestOAuthAuthorizationPruneDropsExpiredRequests(t *testing.T) {
	t.Parallel()

	s, now := newTestOAuthAuthorizationStore()
	first := mustCreateOAuthRequest(t, s, now, 7)
	second := mustCreateOAuthRequest(t, s, now.Add(time.Minute), 7)

	// A create prunes the entry that has passed its TTL, code or not.
	mustCreateOAuthRequest(t, s, now.Add(OAuthAuthorizationRequestTTL), 7)
	require.Len(t, s.byRequestID, 2)
	require.NotContains(t, s.byRequestID, first.RequestID)
	require.Contains(t, s.byRequestID, second.RequestID)
}

func TestOAuthAuthorizationPruneReapsCodesWithTheirRequest(t *testing.T) {
	t.Parallel()

	s, now := newTestOAuthAuthorizationStore()
	completed := mustCompleteApprovedOAuthRequest(t, s, now, 7)
	require.Contains(t, s.byCode, completed.Code)

	// A create past the code TTL reaps the whole record, code included.
	mustCreateOAuthRequest(t, s, now.Add(OAuthAuthorizationCodeTTL), 7)
	require.NotContains(t, s.byCode, completed.Code)
	require.NotContains(t, s.byRequestID, completed.RequestID)
}

func TestOAuthAuthorizationEvictsTheOldestAtCapacity(t *testing.T) {
	t.Parallel()

	s, now := newTestOAuthAuthorizationStore()
	ids := make([]string, 0, oauthAuthorizationRequestCapacity)
	// Every request stays inside its TTL, so the ceiling — not expiry — is what
	// the next create has to relieve.
	for i := range oauthAuthorizationRequestCapacity {
		request, err := s.Create(oauthAuthorizationRequestFor(7), now.Add(time.Duration(i)*time.Microsecond))
		require.NoError(t, err)
		require.Equal(t, OAuthAuthorizationPending, request.State)
		ids = append(ids, request.RequestID)
	}
	require.Len(t, s.byRequestID, oauthAuthorizationRequestCapacity)

	latest := mustCreateOAuthRequest(t, s, now.Add(time.Second), 7)

	require.Len(t, s.byRequestID, oauthAuthorizationRequestCapacity)
	_, err := s.Get(ids[0], 7, now.Add(time.Second))
	require.ErrorIs(t, err, ErrOAuthRequestNotFound, "the oldest request was evicted")
	_, err = s.Get(ids[1], 7, now.Add(time.Second))
	require.NoError(t, err, "the next-oldest request survives")
	_, err = s.Get(latest.RequestID, 7, now.Add(time.Second))
	require.NoError(t, err, "the newest request survives")
}

func TestOAuthAuthorizationCreateNeverRepeatsARequestID(t *testing.T) {
	t.Parallel()

	s, now := newTestOAuthAuthorizationStore()
	seen := make(map[string]struct{}, 1000)
	for range 1000 {
		request := mustCreateOAuthRequest(t, s, now, 7)
		_, duplicate := seen[request.RequestID]
		require.False(t, duplicate, "request ids are never reused")
		seen[request.RequestID] = struct{}{}
	}
	require.Len(t, seen, 1000)
}
