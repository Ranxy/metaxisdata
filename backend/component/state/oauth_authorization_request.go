package state

import (
	"sync"
	"time"

	"github.com/pkg/errors"
)

// OAuth authorization bounds. A request authorizes exactly one token, so both
// the consent interaction and the code it yields are deliberately short-lived.
const (
	// OAuthAuthorizationRequestTTL is how long a pending request stays
	// answerable.
	OAuthAuthorizationRequestTTL = 10 * time.Minute
	// OAuthAuthorizationCodeTTL is how long an issued authorization code can be
	// exchanged for a token.
	OAuthAuthorizationCodeTTL = 60 * time.Second
	// oauthAuthorizationRequestCapacity bounds the in-flight requests held in
	// memory. It is the ceiling on what an unauthenticated caller can make the
	// server hold.
	oauthAuthorizationRequestCapacity = 10000
	// oauthAuthorizationCodeLength is the length of the opaque request id and of
	// the authorization code. The device-login alphabet carries 62 symbols, so
	// 43 characters hold 256 bits, which is what keeps both unguessable.
	oauthAuthorizationCodeLength = 43
)

// OAuthAuthorizationState is where a pending request stands.
type OAuthAuthorizationState string

const (
	// OAuthAuthorizationPending waits for the signed-in user to approve or deny
	// it.
	OAuthAuthorizationPending OAuthAuthorizationState = "PENDING"
	// OAuthAuthorizationApproved can be completed into an authorization code.
	OAuthAuthorizationApproved OAuthAuthorizationState = "APPROVED"
	// OAuthAuthorizationDenied was refused by the user.
	OAuthAuthorizationDenied OAuthAuthorizationState = "DENIED"
)

// OAuth authorization lifecycle errors.
var (
	// ErrOAuthRequestNotFound means the request id is unknown, already consumed,
	// or evicted.
	ErrOAuthRequestNotFound = errors.New("oauth authorization request not found")
	// ErrOAuthRequestExpired means the request is past its pending lifetime.
	ErrOAuthRequestExpired = errors.New("oauth authorization request expired")
	// ErrOAuthRequestNotPending means the request was already approved or denied.
	ErrOAuthRequestNotPending = errors.New("oauth authorization request is not pending")
	// ErrOAuthRequestNotApproved means the request is not, or is no longer, in a
	// state that can be completed into a code.
	ErrOAuthRequestNotApproved = errors.New("oauth authorization request is not approved")
	// ErrOAuthRequestWrongUser means the request belongs to another user.
	ErrOAuthRequestWrongUser = errors.New("oauth authorization request belongs to another user")
	// ErrOAuthCodeNotFound means the authorization code is unknown, already
	// exchanged, or evicted.
	ErrOAuthCodeNotFound = errors.New("oauth authorization code not found")
	// ErrOAuthCodeExpired means the authorization code is past its lifetime.
	ErrOAuthCodeExpired = errors.New("oauth authorization code expired")
)

// OAuthAuthorizationRequest is one authorization request and, once Complete has
// run, the single-use code minted from it.
type OAuthAuthorizationRequest struct {
	// RequestID is opaque and assigned by the store; it is the only handle the
	// consent page and the audit log ever see.
	RequestID string
	ClientID  string
	// ClientName is display only and not verified.
	ClientName          string
	RedirectURI         string
	Resource            string
	Scopes              []string
	CodeChallenge       string
	CodeChallengeMethod string
	// ClientState is the client's opaque `state`, echoed back when the flow
	// completes.
	ClientState string
	// UserID is the signed-in user the request belongs to; no method will
	// resolve the request for anybody else.
	UserID           int
	ApprovedByUserID int
	State            OAuthAuthorizationState
	// Code is minted by Complete and consumed by Exchange.
	Code           string
	RequestIP      string
	CreateTime     time.Time
	ExpireTime     time.Time
	CodeExpireTime time.Time
}

// deadline is when the request stops being usable, with the error that reports
// it: the pending lifetime until a code is minted, and the code lifetime after
// that. The code therefore outlives the consent interaction it came from.
func (r *OAuthAuthorizationRequest) deadline() (time.Time, error) {
	if r.Code == "" {
		return r.ExpireTime, ErrOAuthRequestExpired
	}
	return r.CodeExpireTime, ErrOAuthCodeExpired
}

// OAuthAuthorizationRequestStore holds in-flight authorization requests and the
// authorization codes minted from them. Like every other cache in this package
// it is process-local, so a deployment with several replicas must pin a
// browser's authorization flow to a single replica (or front them with sticky
// routing).
type OAuthAuthorizationRequestStore struct {
	mu sync.Mutex
	// byRequestID owns the entries; byCode points at the same record once
	// Complete has minted a code, so Exchange can resolve it.
	byRequestID map[string]*OAuthAuthorizationRequest
	byCode      map[string]*OAuthAuthorizationRequest
}

func NewOAuthAuthorizationRequestStore() *OAuthAuthorizationRequestStore {
	return &OAuthAuthorizationRequestStore{
		byRequestID: map[string]*OAuthAuthorizationRequest{},
		byCode:      map[string]*OAuthAuthorizationRequest{},
	}
}

// Create stores a new pending request. The id, the state and the code are the
// store's to assign, so a caller can neither seed an approved request nor name a
// code. A generated id that collides with a live request is redrawn, so the
// returned id is always fresh.
func (s *OAuthAuthorizationRequestStore) Create(request OAuthAuthorizationRequest, now time.Time) (OAuthAuthorizationRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for range codeAttempts {
		requestID, err := randomCode(deviceLoginAlphabet, oauthAuthorizationCodeLength)
		if err != nil {
			return OAuthAuthorizationRequest{}, err
		}
		if _, ok := s.byRequestID[requestID]; ok {
			continue
		}

		s.pruneLocked(now)

		request.RequestID = requestID
		request.ApprovedByUserID = 0
		request.State = OAuthAuthorizationPending
		request.Code = ""
		request.CodeExpireTime = time.Time{}
		request.CreateTime = now
		request.ExpireTime = now.Add(OAuthAuthorizationRequestTTL)

		stored := &request
		s.byRequestID[requestID] = stored
		return *stored, nil
	}
	return OAuthAuthorizationRequest{}, errors.New("failed to allocate a unique oauth authorization request id")
}

// Get returns a pending request so the consent page can show what is about to be
// approved. A request is visible only to the user it was created for.
func (s *OAuthAuthorizationRequestStore) Get(requestID string, userID int, now time.Time) (OAuthAuthorizationRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	request, err := s.resolveLocked(requestID, userID, now)
	if err != nil {
		return OAuthAuthorizationRequest{}, err
	}
	return *request, nil
}

// Approve records the user's decision. Only a pending request can be decided,
// and a denial leaves ApprovedByUserID unset because nobody approved it.
func (s *OAuthAuthorizationRequestStore) Approve(requestID string, userID int, approve bool, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	request, err := s.resolveLocked(requestID, userID, now)
	if err != nil {
		return err
	}
	if request.State != OAuthAuthorizationPending {
		return ErrOAuthRequestNotPending
	}

	if approve {
		request.State = OAuthAuthorizationApproved
		request.ApprovedByUserID = userID
	} else {
		request.State = OAuthAuthorizationDenied
	}
	return nil
}

// Complete mints the authorization code for an approved request. The code is
// minted once: a retry is refused rather than handed a second code.
func (s *OAuthAuthorizationRequestStore) Complete(requestID string, userID int, now time.Time) (OAuthAuthorizationRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	request, err := s.resolveLocked(requestID, userID, now)
	if err != nil {
		return OAuthAuthorizationRequest{}, err
	}

	switch request.State {
	case OAuthAuthorizationApproved:
		// Only an approved request gets a code; fall out of the switch to mint it.
	case OAuthAuthorizationPending:
		return OAuthAuthorizationRequest{}, ErrOAuthRequestNotPending
	case OAuthAuthorizationDenied:
		return OAuthAuthorizationRequest{}, ErrOAuthRequestNotApproved
	default:
		return OAuthAuthorizationRequest{}, ErrOAuthRequestNotApproved
	}
	if request.Code != "" {
		return OAuthAuthorizationRequest{}, ErrOAuthRequestNotPending
	}

	for range codeAttempts {
		code, err := randomCode(deviceLoginAlphabet, oauthAuthorizationCodeLength)
		if err != nil {
			return OAuthAuthorizationRequest{}, err
		}
		if _, ok := s.byCode[code]; ok {
			continue
		}

		request.Code = code
		request.CodeExpireTime = now.Add(OAuthAuthorizationCodeTTL)
		s.byCode[code] = request
		return *request, nil
	}
	return OAuthAuthorizationRequest{}, errors.New("failed to allocate a unique oauth authorization code")
}

// Exchange consumes an authorization code. The record is deleted here, so a code
// resolves exactly once and a replay reports it as unknown.
func (s *OAuthAuthorizationRequestStore) Exchange(code string, now time.Time) (OAuthAuthorizationRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	request, ok := s.byCode[code]
	if !ok {
		return OAuthAuthorizationRequest{}, ErrOAuthCodeNotFound
	}
	if deadline, _ := request.deadline(); !now.Before(deadline) {
		s.removeLocked(request)
		return OAuthAuthorizationRequest{}, ErrOAuthCodeExpired
	}

	answer := *request
	s.removeLocked(request)
	return answer, nil
}

// resolveLocked finds a request the given user owns and that is still inside its
// deadline. Ownership is checked before expiry, and a request past its deadline
// is dropped rather than returned stale.
func (s *OAuthAuthorizationRequestStore) resolveLocked(requestID string, userID int, now time.Time) (*OAuthAuthorizationRequest, error) {
	request, ok := s.byRequestID[requestID]
	if !ok {
		return nil, ErrOAuthRequestNotFound
	}
	if request.UserID != userID {
		return nil, ErrOAuthRequestWrongUser
	}
	if deadline, expired := request.deadline(); !now.Before(deadline) {
		s.removeLocked(request)
		return nil, expired
	}
	return request, nil
}

func (s *OAuthAuthorizationRequestStore) removeLocked(request *OAuthAuthorizationRequest) {
	delete(s.byRequestID, request.RequestID)
	if request.Code != "" {
		delete(s.byCode, request.Code)
	}
}

// pruneLocked drops requests past their deadline and, when the store is still at
// its capacity, the oldest one. Only Create grows the store, so dropping a
// single entry per call holds the ceiling.
func (s *OAuthAuthorizationRequestStore) pruneLocked(now time.Time) {
	for _, request := range s.byRequestID {
		if deadline, _ := request.deadline(); !now.Before(deadline) {
			s.removeLocked(request)
		}
	}
	if len(s.byRequestID) < oauthAuthorizationRequestCapacity {
		return
	}

	var oldest *OAuthAuthorizationRequest
	for _, request := range s.byRequestID {
		if oldest == nil || request.CreateTime.Before(oldest.CreateTime) {
			oldest = request
		}
	}
	if oldest != nil {
		s.removeLocked(oldest)
	}
}
