package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestDeviceLogin(t *testing.T) (*DeviceLoginStore, time.Time) {
	t.Helper()
	return newDeviceLoginStore(), time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
}

func mustCreate(t *testing.T, s *DeviceLoginStore, now time.Time, userCode, deviceCode string) DeviceLogin {
	t.Helper()
	login, err := s.Create(DeviceLogin{UserCode: userCode, DeviceCode: deviceCode}, now)
	require.NoError(t, err)
	return login
}

func TestDeviceLoginCreateNormalisesTheRequest(t *testing.T) {
	t.Parallel()

	s, now := newTestDeviceLogin(t)
	login := mustCreate(t, s, now, "7Q2X-9M4K", "device-secret")

	require.Equal(t, DeviceLoginPending, login.State)
	require.Equal(t, now, login.CreateTime)
	require.Equal(t, now.Add(DeviceLoginTTL), login.ExpireTime)
	require.Zero(t, login.ApprovedByUserID)
}

func TestDeviceLoginCreateRejectsEmptyAndDuplicateCodes(t *testing.T) {
	t.Parallel()

	s, now := newTestDeviceLogin(t)

	_, err := s.Create(DeviceLogin{UserCode: "AAAA-AAAA"}, now)
	require.ErrorContains(t, err, "device code")

	_, err = s.Create(DeviceLogin{DeviceCode: "secret"}, now)
	require.ErrorContains(t, err, "user code")

	mustCreate(t, s, now, "AAAA-AAAA", "secret-1")

	_, err = s.Create(DeviceLogin{UserCode: "AAAA-AAAA", DeviceCode: "secret-2"}, now)
	require.ErrorIs(t, err, ErrDeviceLoginCodeTaken, "a user code is never reused")

	_, err = s.Create(DeviceLogin{UserCode: "BBBB-BBBB", DeviceCode: "secret-1"}, now)
	require.ErrorIs(t, err, ErrDeviceLoginCodeTaken, "a device code is never reused")
}

func TestDeviceLoginApprovalBindsTheApproverAndIsSingleUse(t *testing.T) {
	t.Parallel()

	s, now := newTestDeviceLogin(t)
	mustCreate(t, s, now, "7Q2X-9M4K", "device-secret")

	approved, err := s.Approve("7Q2X-9M4K", 42, true, now)
	require.NoError(t, err)
	require.Equal(t, DeviceLoginApproved, approved.State)
	require.Equal(t, 42, approved.ApprovedByUserID)

	// A second decision is refused, and the original approval stands.
	_, err = s.Approve("7Q2X-9M4K", 7, false, now)
	require.ErrorIs(t, err, ErrDeviceLoginNotPending)

	// The first exchange answers with the token holder and consumes the entry.
	answer, err := s.Exchange("device-secret", now.Add(DeviceLoginMinPollInterval))
	require.NoError(t, err)
	require.Equal(t, DeviceLoginApproved, answer.State)
	require.Equal(t, 42, answer.ApprovedByUserID)

	_, err = s.Exchange("device-secret", now.Add(DeviceLoginMinPollInterval))
	require.ErrorIs(t, err, ErrDeviceLoginNotFound, "an approved request is consumed exactly once")

	_, err = s.GetByUserCode("7Q2X-9M4K", now)
	require.ErrorIs(t, err, ErrDeviceLoginNotFound)
}

func TestDeviceLoginDenyIsReportedOnceThenConsumed(t *testing.T) {
	t.Parallel()

	s, now := newTestDeviceLogin(t)
	mustCreate(t, s, now, "7Q2X-9M4K", "device-secret")

	denied, err := s.Approve("7Q2X-9M4K", 42, false, now)
	require.NoError(t, err)
	require.Equal(t, DeviceLoginDenied, denied.State)

	answer, err := s.Exchange("device-secret", now.Add(DeviceLoginMinPollInterval))
	require.NoError(t, err)
	require.Equal(t, DeviceLoginDenied, answer.State)

	_, err = s.Exchange("device-secret", now.Add(DeviceLoginMinPollInterval))
	require.ErrorIs(t, err, ErrDeviceLoginNotFound)
}

func TestDeviceLoginPendingPollsAreThrottled(t *testing.T) {
	t.Parallel()

	s, now := newTestDeviceLogin(t)
	mustCreate(t, s, now, "7Q2X-9M4K", "device-secret")

	// The first poll is free: there is no clock yet.
	first, err := s.Exchange("device-secret", now)
	require.NoError(t, err)
	require.Equal(t, DeviceLoginPending, first.State)

	_, err = s.Exchange("device-secret", now.Add(DeviceLoginMinPollInterval-time.Millisecond))
	require.ErrorIs(t, err, ErrDeviceLoginSlowDown)

	second, err := s.Exchange("device-secret", now.Add(DeviceLoginMinPollInterval))
	require.NoError(t, err)
	require.Equal(t, DeviceLoginPending, second.State)

	// Once approved there is an answer, so the throttle no longer applies.
	_, err = s.Approve("7Q2X-9M4K", 42, true, now)
	require.NoError(t, err)
	answer, err := s.Exchange("device-secret", now.Add(DeviceLoginMinPollInterval+time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, DeviceLoginApproved, answer.State)
}

func TestDeviceLoginExpiresAndIsReportedAsExpired(t *testing.T) {
	t.Parallel()

	s, now := newTestDeviceLogin(t)
	mustCreate(t, s, now, "7Q2X-9M4K", "device-secret")

	// Polling after the TTL reports the expiry instead of a missing entry.
	expired, err := s.Exchange("device-secret", now.Add(DeviceLoginTTL))
	require.NoError(t, err)
	require.Equal(t, DeviceLoginExpired, expired.State)

	_, err = s.Exchange("device-secret", now.Add(DeviceLoginTTL))
	require.ErrorIs(t, err, ErrDeviceLoginNotFound, "the expired entry is dropped")

	// The confirmation page sees the same state while the entry is still there.
	s2, now2 := newTestDeviceLogin(t)
	mustCreate(t, s2, now2, "7Q2X-9M4K", "device-secret")
	page, err := s2.GetByUserCode("7Q2X-9M4K", now2.Add(2*DeviceLoginTTL))
	require.NoError(t, err)
	require.Equal(t, DeviceLoginExpired, page.State)

	// An approval that arrives after the TTL is refused.
	s3, now3 := newTestDeviceLogin(t)
	mustCreate(t, s3, now3, "7Q2X-9M4K", "device-secret")
	_, err = s3.Approve("7Q2X-9M4K", 42, true, now3.Add(DeviceLoginTTL))
	require.ErrorIs(t, err, ErrDeviceLoginExpired)
}

func TestDeviceLoginApprovedButUnexchangedExpires(t *testing.T) {
	t.Parallel()

	s, now := newTestDeviceLogin(t)
	mustCreate(t, s, now, "7Q2X-9M4K", "device-secret")
	_, err := s.Approve("7Q2X-9M4K", 42, true, now)
	require.NoError(t, err)

	answer, err := s.Exchange("device-secret", now.Add(DeviceLoginTTL))
	require.NoError(t, err)
	require.Equal(t, DeviceLoginExpired, answer.State, "the issuance window closed")
}

func TestDeviceLoginUnknownCodes(t *testing.T) {
	t.Parallel()

	s, now := newTestDeviceLogin(t)

	_, err := s.GetByUserCode("NOPE-NOPE", now)
	require.ErrorIs(t, err, ErrDeviceLoginNotFound)
	_, err = s.Approve("NOPE-NOPE", 1, true, now)
	require.ErrorIs(t, err, ErrDeviceLoginNotFound)
	_, err = s.Exchange("nope", now)
	require.ErrorIs(t, err, ErrDeviceLoginNotFound)
}

func TestDeviceLoginPruneDropsExpiredEntries(t *testing.T) {
	t.Parallel()

	s, now := newTestDeviceLogin(t)
	mustCreate(t, s, now, "AAAA-AAAA", "secret-a")
	mustCreate(t, s, now.Add(time.Minute), "BBBB-BBBB", "secret-b")

	// A new create prunes the entry that has passed its TTL.
	mustCreate(t, s, now.Add(DeviceLoginTTL), "CCCC-CCCC", "secret-c")
	require.Len(t, s.byDeviceCode, 2)
	require.NotContains(t, s.byUserCode, "AAAA-AAAA")
	require.Contains(t, s.byUserCode, "BBBB-BBBB")

	// The pruned entry is simply gone.
	_, err := s.GetByUserCode("AAAA-AAAA", now.Add(DeviceLoginTTL))
	require.ErrorIs(t, err, ErrDeviceLoginNotFound)
}

func TestDeviceLoginPruneEvictsTheOldestAtCapacity(t *testing.T) {
	t.Parallel()

	s, now := newTestDeviceLogin(t)
	// Every request stays inside its TTL, so the ceiling — not expiry — is what
	// the next create has to relieve.
	for i := range deviceLoginCapacity {
		login, err := s.Create(DeviceLogin{
			UserCode:   codeForIndex("U", i),
			DeviceCode: codeForIndex("D", i),
		}, now.Add(time.Duration(i)*time.Microsecond))
		require.NoError(t, err)
		require.Equal(t, DeviceLoginPending, login.State)
	}
	require.Len(t, s.byDeviceCode, deviceLoginCapacity)

	// One more request evicts the oldest rather than growing past the ceiling.
	mustCreate(t, s, now.Add(time.Second), "ZZZZ-ZZZZ", "secret-z")

	require.Len(t, s.byDeviceCode, deviceLoginCapacity)
	_, err := s.GetByUserCode(codeForIndex("U", 0), now.Add(time.Second))
	require.ErrorIs(t, err, ErrDeviceLoginNotFound, "the oldest entry was evicted")
	_, err = s.GetByUserCode(codeForIndex("U", 1), now.Add(time.Second))
	require.NoError(t, err, "the next-oldest entry survives")
	_, err = s.GetByUserCode("ZZZZ-ZZZZ", now.Add(time.Second))
	require.NoError(t, err, "the newest entry survives")
}

// codeForIndex builds a distinct fixed-width code so a test can address the
// entry it created at a given position.
func codeForIndex(prefix string, i int) string {
	const digits = "0123456789"
	code := []byte("XXXX-XXXX")
	for pos := range code {
		if code[pos] == '-' {
			continue
		}
		code[pos] = digits[0]
	}
	for pos := len(code) - 1; pos >= 0 && i > 0; pos-- {
		if code[pos] == '-' {
			continue
		}
		code[pos] = digits[i%10]
		i /= 10
	}
	return prefix + string(code)
}
