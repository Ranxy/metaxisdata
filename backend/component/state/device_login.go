package state

import (
	"crypto/rand"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
)

// Device login codes. The generator lives here because this store owns the
// uniqueness constraint the codes have to satisfy.
const (
	// deviceCodeLength is the length of the polling secret. The alphabet below
	// carries 62 symbols, so 43 characters hold 256 bits.
	deviceCodeLength = 43
	// userCodeLength is the number of Crockford base32 characters in the human
	// code. Five bits each gives 40 bits of entropy, far more than an online
	// guesser can walk through within the TTL.
	userCodeLength = 8
	// userCodeDashAt is where the human code is split for readability.
	userCodeDashAt = 4
	// codeAttempts bounds the retries when a generated code collides with an
	// in-flight request.
	codeAttempts = 5
	// deviceLoginAlphabet is the alphabet of the polling secret.
	deviceLoginAlphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	// crockfordAlphabet is Crockford base32: the digits and the letters minus I,
	// L, O and U (of which the first three decode to a digit), so a character
	// cannot be misread as a different one.
	crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
)

// Device login bounds. A request authorizes exactly one token, so the window is
// deliberately short and its interval is enforced.
const (
	// DeviceLoginTTL is how long a device login stays answerable.
	DeviceLoginTTL = 10 * time.Minute
	// DeviceLoginMinPollInterval is the shortest interval between two polls of
	// the same request. RFC 8628 answers a faster poll with "slow_down".
	DeviceLoginMinPollInterval = 1 * time.Second
	// DeviceLoginRecommendedPollInterval is the interval the server asks
	// clients to poll at. It is longer than the enforced minimum on purpose.
	DeviceLoginRecommendedPollInterval = 3 * time.Second
	// deviceLoginCapacity bounds the in-flight requests held in memory. It is
	// the ceiling on what an unauthenticated caller can make the server hold.
	deviceLoginCapacity = 10000
)

// DeviceLoginState is the lifecycle of a device authorization request.
type DeviceLoginState int

const (
	// DeviceLoginPending waits for a signed-in user to approve or deny it.
	DeviceLoginPending DeviceLoginState = iota
	// DeviceLoginApproved is approved and has not been exchanged yet.
	DeviceLoginApproved
	// DeviceLoginDenied was refused by the user.
	DeviceLoginDenied
	// DeviceLoginExpired was never approved, or was approved but not exchanged
	// within the TTL.
	DeviceLoginExpired
)

// Device login lifecycle errors.
var (
	// ErrDeviceLoginNotFound means the code is unknown, already consumed, or
	// evicted.
	ErrDeviceLoginNotFound = errors.New("device login not found")
	// ErrDeviceLoginCodeTaken means one of the generated codes is already in
	// use; the caller must generate another one.
	ErrDeviceLoginCodeTaken = errors.New("device login code already in use")
	// ErrDeviceLoginExpired means the request is past its expiry.
	ErrDeviceLoginExpired = errors.New("device login expired")
	// ErrDeviceLoginNotPending means the request was already approved or denied.
	ErrDeviceLoginNotPending = errors.New("device login is not pending")
	// ErrDeviceLoginSlowDown means the request was polled again too soon.
	ErrDeviceLoginSlowDown = errors.New("device login polled too soon")
)

// DeviceLogin is one in-flight device authorization request. It is reachable by
// both its secret device code and its human-readable user code.
type DeviceLogin struct {
	DeviceCode string
	UserCode   string
	State      DeviceLoginState

	ClientName       string
	ClientVersion    string
	RequestIP        string
	RequestUserAgent string

	CreateTime time.Time
	ExpireTime time.Time

	// ApprovedByUserID is the approver, set once the request is approved.
	ApprovedByUserID int

	// lastPollAt backs the minimum polling interval.
	lastPollAt time.Time
}

// DeviceLoginStore holds in-flight device authorization requests. Like every
// other cache in this package it is process-local, so a deployment with several
// replicas must pin a client's requests to a single replica (or front them with
// sticky routing).
type DeviceLoginStore struct {
	mu sync.Mutex
	// The two maps point at the same request, so either code resolves it.
	byUserCode   map[string]*DeviceLogin
	byDeviceCode map[string]*DeviceLogin
}

func newDeviceLoginStore() *DeviceLoginStore {
	return &DeviceLoginStore{
		byUserCode:   map[string]*DeviceLogin{},
		byDeviceCode: map[string]*DeviceLogin{},
	}
}

// Create stores a new pending request. The caller generates both codes; a
// collision is reported so it can generate another pair. The returned value is
// the stored request, with its state, times and poll clock normalised.
func (s *DeviceLoginStore) Create(login DeviceLogin, now time.Time) (DeviceLogin, error) {
	if login.DeviceCode == "" || login.UserCode == "" {
		return DeviceLogin{}, errors.New("a device login needs both a device code and a user code")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.byUserCode[login.UserCode]; ok {
		return DeviceLogin{}, ErrDeviceLoginCodeTaken
	}
	if _, ok := s.byDeviceCode[login.DeviceCode]; ok {
		return DeviceLogin{}, ErrDeviceLoginCodeTaken
	}

	s.pruneLocked(now)

	login.State = DeviceLoginPending
	login.CreateTime = now
	login.ExpireTime = now.Add(DeviceLoginTTL)
	login.ApprovedByUserID = 0
	login.lastPollAt = time.Time{}

	stored := &login
	s.byUserCode[stored.UserCode] = stored
	s.byDeviceCode[stored.DeviceCode] = stored
	return *stored, nil
}

// NewDeviceLogin allocates fresh codes for a request and stores it. Codes are
// generated here, next to the uniqueness check they have to satisfy, and a
// collision simply draws another pair.
func (s *DeviceLoginStore) NewDeviceLogin(login DeviceLogin, now time.Time) (DeviceLogin, error) {
	for range codeAttempts {
		deviceCode, err := randomCode(deviceLoginAlphabet, deviceCodeLength)
		if err != nil {
			return DeviceLogin{}, err
		}
		userCode, err := newUserCode()
		if err != nil {
			return DeviceLogin{}, err
		}
		login.DeviceCode = deviceCode
		login.UserCode = userCode

		created, err := s.Create(login, now)
		if err == nil {
			return created, nil
		}
		if !errors.Is(err, ErrDeviceLoginCodeTaken) {
			return DeviceLogin{}, err
		}
	}
	return DeviceLogin{}, ErrDeviceLoginCodeTaken
}

// GetByUserCode returns the request a user code refers to, so the confirmation
// page can show what is about to be approved. An expired request is reported as
// expired rather than missing.
func (s *DeviceLoginStore) GetByUserCode(userCode string, now time.Time) (DeviceLogin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	login, ok := s.byUserCode[userCode]
	if !ok {
		return DeviceLogin{}, ErrDeviceLoginNotFound
	}
	s.expireLocked(login, now)
	return *login, nil
}

// Approve records the user's decision. Only a pending request can be decided,
// and the approver is bound to it so the token can be issued for them.
func (s *DeviceLoginStore) Approve(userCode string, approverUserID int, approve bool, now time.Time) (DeviceLogin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	login, ok := s.byUserCode[userCode]
	if !ok {
		return DeviceLogin{}, ErrDeviceLoginNotFound
	}
	if s.expireLocked(login, now) {
		return *login, ErrDeviceLoginExpired
	}
	if login.State != DeviceLoginPending {
		return *login, ErrDeviceLoginNotPending
	}

	if approve {
		login.State = DeviceLoginApproved
		login.ApprovedByUserID = approverUserID
	} else {
		login.State = DeviceLoginDenied
	}
	return *login, nil
}

// Exchange polls a request by its device code. An approved request is consumed
// here: the caller holds the only copy from that moment on, so the token can be
// issued exactly once. A denied request is consumed the same way after its
// state has been reported once.
func (s *DeviceLoginStore) Exchange(deviceCode string, now time.Time) (DeviceLogin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	login, ok := s.byDeviceCode[deviceCode]
	if !ok {
		return DeviceLogin{}, ErrDeviceLoginNotFound
	}
	if s.expireLocked(login, now) {
		return *login, nil
	}
	// Throttle only the pending phase: once there is an answer, holding it back
	// would be pointless. The clock is not advanced on a refused poll, so the
	// client has to wait out the interval.
	if login.State == DeviceLoginPending && !login.lastPollAt.IsZero() &&
		now.Sub(login.lastPollAt) < DeviceLoginMinPollInterval {
		return *login, ErrDeviceLoginSlowDown
	}
	login.lastPollAt = now

	switch login.State {
	case DeviceLoginApproved, DeviceLoginDenied:
		answer := *login
		s.removeLocked(login)
		return answer, nil
	case DeviceLoginPending, DeviceLoginExpired:
		return *login, nil
	default:
		return *login, nil
	}
}

// expireLocked marks an expired request and drops it. It reports whether the
// request was expired by this call.
func (s *DeviceLoginStore) expireLocked(login *DeviceLogin, now time.Time) bool {
	if now.Before(login.ExpireTime) {
		return false
	}
	login.State = DeviceLoginExpired
	s.removeLocked(login)
	return true
}

func (s *DeviceLoginStore) removeLocked(login *DeviceLogin) {
	delete(s.byUserCode, login.UserCode)
	delete(s.byDeviceCode, login.DeviceCode)
}

// pruneLocked drops expired requests and, when the store is still at its
// capacity, the oldest one. Only Create grows the store, so dropping a single
// entry per call is enough to hold the ceiling.
func (s *DeviceLoginStore) pruneLocked(now time.Time) {
	for _, login := range s.byDeviceCode {
		if !now.Before(login.ExpireTime) {
			s.removeLocked(login)
		}
	}
	if len(s.byDeviceCode) < deviceLoginCapacity {
		return
	}

	var oldest *DeviceLogin
	for _, login := range s.byDeviceCode {
		if oldest == nil || login.CreateTime.Before(oldest.CreateTime) {
			oldest = login
		}
	}
	if oldest != nil {
		s.removeLocked(oldest)
	}
}

// randomCode draws n characters uniformly from alphabet.
func randomCode(alphabet string, n int) (string, error) {
	code := make([]byte, n)
	limit := big.NewInt(int64(len(alphabet)))
	for i := range code {
		draw, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", errors.Wrap(err, "failed to draw a random character")
		}
		code[i] = alphabet[draw.Int64()]
	}
	return string(code), nil
}

// newUserCode draws userCodeLength Crockford base32 characters and lays them out
// as XXXX-XXXX.
func newUserCode() (string, error) {
	code := make([]byte, 0, userCodeLength+1)
	for i := range userCodeLength {
		if i == userCodeDashAt {
			code = append(code, '-')
		}
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(crockfordAlphabet))))
		if err != nil {
			return "", errors.Wrap(err, "failed to draw a user code character")
		}
		code = append(code, crockfordAlphabet[n.Int64()])
	}
	return string(code), nil
}

// NormalizeUserCode accepts a code as a human would type it: any case, with or
// without the dash, and the characters Crockford decodes to a digit ("O" to
// "0", "I" and "L" to "1"). It returns the canonical XXXX-XXXX form, or "" when
// the input cannot be a code.
func NormalizeUserCode(raw string) string {
	canonical := make([]byte, 0, userCodeLength)
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		switch r {
		case '-', ' ', '_':
			continue
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		default:
		}
		if !strings.ContainsRune(crockfordAlphabet, r) {
			return ""
		}
		canonical = append(canonical, byte(r))
	}
	if len(canonical) != userCodeLength {
		return ""
	}
	return string(canonical[:userCodeDashAt]) + "-" + string(canonical[userCodeDashAt:])
}
