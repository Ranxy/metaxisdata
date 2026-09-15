package authflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// fakeAuth answers CreateDeviceLogin once and then walks a scripted list of
// exchange states, so the polling loop can be tested without a server or a wait.
type fakeAuth struct {
	createResponse *v1pb.CreateDeviceLoginResponse
	states         []v1pb.DeviceLoginState
	tokens         map[v1pb.DeviceLoginState]string
	exchangeErr    error
	created        int
	exchanged      int
}

func (f *fakeAuth) CreateDeviceLogin(context.Context, *connect.Request[v1pb.CreateDeviceLoginRequest]) (*connect.Response[v1pb.CreateDeviceLoginResponse], error) {
	f.created++
	return connect.NewResponse(f.createResponse), nil
}

func (f *fakeAuth) ExchangeDeviceLogin(context.Context, *connect.Request[v1pb.ExchangeDeviceLoginRequest]) (*connect.Response[v1pb.ExchangeDeviceLoginResponse], error) {
	f.exchanged++
	if f.exchangeErr != nil {
		return nil, f.exchangeErr
	}
	state := f.states[min(f.exchanged-1, len(f.states)-1)]
	return connect.NewResponse(&v1pb.ExchangeDeviceLoginResponse{
		State: state,
		Token: f.tokens[state],
		User:  &v1pb.User{Email: "dev@example.com"},
	}), nil
}

func newFake(states ...v1pb.DeviceLoginState) *fakeAuth {
	return &fakeAuth{
		createResponse: &v1pb.CreateDeviceLoginResponse{
			DeviceCode:              "device-secret",
			UserCode:                "7Q2X-9M4K",
			VerificationUri:         "https://mx.example.com/device",
			VerificationUriComplete: "https://mx.example.com/device?user_code=7Q2X-9M4K",
			ExpiresIn:               600,
			Interval:                3,
		},
		states: states,
		tokens: map[v1pb.DeviceLoginState]string{
			v1pb.DeviceLoginState_APPROVED: "issued-token",
		},
	}
}

func noWait() Options {
	return Options{
		Sleep:       func(time.Duration) {},
		Now:         func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
		OpenBrowser: func(string) error { return nil },
	}
}

// advancingClock moves forward on every read, which is what a real clock does
// and what makes the request TTL the loop's actual bound.
func advancingClock(step time.Duration) func() time.Time {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return func() time.Time {
		now = now.Add(step)
		return now
	}
}

func TestRunPollsUntilApproved(t *testing.T) {
	t.Parallel()

	api := newFake(v1pb.DeviceLoginState_PENDING, v1pb.DeviceLoginState_PENDING, v1pb.DeviceLoginState_APPROVED)
	var lines []string

	result, err := Run(context.Background(), api, func(format string, _ ...any) {
		lines = append(lines, format)
	}, noWait())

	require.NoError(t, err)
	require.Equal(t, "issued-token", result.Token)
	require.Equal(t, "dev@example.com", result.User.GetEmail())
	require.Equal(t, 3, api.exchanged)
	require.NotEmpty(t, lines, "the code and the URL are shown to the person")
}

// The code is not put in the URL unless the caller asked for it: a prefilled
// link is what turns a device login into a phishing tool.
func TestRunPrintsTheBareURLByDefault(t *testing.T) {
	t.Parallel()

	api := newFake(v1pb.DeviceLoginState_APPROVED)
	var opened string
	options := noWait()
	options.OpenBrowser = func(url string) error {
		opened = url
		return nil
	}

	_, err := Run(context.Background(), api, func(string, ...any) {}, options)
	require.NoError(t, err)
	require.Equal(t, "https://mx.example.com/device", opened)

	options.PrefillURL = true
	api = newFake(v1pb.DeviceLoginState_APPROVED)
	_, err = Run(context.Background(), api, func(string, ...any) {}, options)
	require.NoError(t, err)
	require.Equal(t, "https://mx.example.com/device?user_code=7Q2X-9M4K", opened)
}

func TestRunReportsDeniedAndExpired(t *testing.T) {
	t.Parallel()

	denied := newFake(v1pb.DeviceLoginState_DENIED)
	_, err := Run(context.Background(), denied, func(string, ...any) {}, noWait())
	require.ErrorIs(t, err, ErrDenied)

	expired := newFake(v1pb.DeviceLoginState_PENDING, v1pb.DeviceLoginState_EXPIRED)
	_, err = Run(context.Background(), expired, func(string, ...any) {}, noWait())
	require.ErrorIs(t, err, ErrExpired)
}

// The TTL is the server's deadline, and the loop has to respect it even if the
// server never reports the expiry itself.
func TestRunGivesUpAtTheDeadline(t *testing.T) {
	t.Parallel()

	api := newFake(v1pb.DeviceLoginState_PENDING, v1pb.DeviceLoginState_PENDING)
	options := noWait()
	options.Now = advancingClock(5 * time.Minute)

	_, err := Run(context.Background(), api, func(string, ...any) {}, options)
	require.ErrorIs(t, err, ErrExpired)
}

// A slow_down is not fatal: the loop backs off and keeps waiting.
func TestRunBacksOffOnSlowDown(t *testing.T) {
	t.Parallel()

	api := newFake(v1pb.DeviceLoginState_APPROVED)
	api.exchangeErr = connect.NewError(connect.CodeResourceExhausted, errors.New("slow down"))
	options := noWait()
	options.Now = advancingClock(time.Minute)

	_, err := Run(context.Background(), api, func(string, ...any) {}, options)
	require.ErrorIs(t, err, ErrExpired, "it keeps waiting rather than failing outright")
	require.Greater(t, api.exchanged, 1)
}

// A workspace without an external URL returns no address at all. Saying "ask an
// administrator" and stopping would leave the person with no way to approve the
// request, so the client substitutes the address it is already talking to.
func TestRunFallsBackToTheServerAddress(t *testing.T) {
	t.Parallel()

	api := newFake(v1pb.DeviceLoginState_APPROVED)
	api.createResponse.VerificationUri = ""
	api.createResponse.VerificationUriComplete = ""

	var opened string
	options := noWait()
	options.ServerURL = "http://localhost:8080/"
	options.OpenBrowser = func(url string) error {
		opened = url
		return nil
	}
	var lines []string
	progress := func(format string, _ ...any) { lines = append(lines, format) }

	_, err := Run(context.Background(), api, progress, options)
	require.NoError(t, err)
	require.Equal(t, "http://localhost:8080/device", opened, "the trailing slash is not doubled")

	// With --prefill-url the code is carried by the address we built ourselves.
	api = newFake(v1pb.DeviceLoginState_APPROVED)
	api.createResponse.VerificationUri = ""
	api.createResponse.VerificationUriComplete = ""
	options.PrefillURL = true
	options.OpenBrowser = func(url string) error {
		opened = url
		return nil
	}
	_, err = Run(context.Background(), api, progress, options)
	require.NoError(t, err)
	require.Equal(t, "http://localhost:8080/device?user_code=7Q2X-9M4K", opened)
}

// The server's address wins whenever the workspace has one, even with the
// fallback available.
func TestRunPrefersTheServerSuppliedAddress(t *testing.T) {
	t.Parallel()

	api := newFake(v1pb.DeviceLoginState_APPROVED)
	var opened string
	options := noWait()
	options.ServerURL = "http://localhost:8080"
	options.OpenBrowser = func(url string) error {
		opened = url
		return nil
	}

	_, err := Run(context.Background(), api, func(string, ...any) {}, options)
	require.NoError(t, err)
	require.Equal(t, "https://mx.example.com/device", opened)
}

// Without an address and without a server there is nothing to print, and the
// message has to say what is missing rather than fail silently.
func TestRunWithoutAnyAddressSaysWhatIsMissing(t *testing.T) {
	t.Parallel()

	api := newFake(v1pb.DeviceLoginState_APPROVED)
	api.createResponse.VerificationUri = ""
	api.createResponse.VerificationUriComplete = ""
	options := noWait()
	options.OpenBrowser = func(string) error {
		t.Fatal("nothing should be opened")
		return nil
	}

	var lines []string
	_, err := Run(context.Background(), api, func(format string, _ ...any) {
		lines = append(lines, format)
	}, options)
	require.NoError(t, err)
	require.Contains(t, strings.Join(lines, "\n"), "No confirmation address is available")
}
