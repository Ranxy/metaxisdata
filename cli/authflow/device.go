// Package authflow drives the device login: it asks the server for a request,
// tells the person where to approve it, and polls until the server has an
// answer.
package authflow

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// DeviceLoginClient is the part of the auth client the flow needs. Narrowing it
// here is what lets the flow be tested against a fake.
type DeviceLoginClient interface {
	CreateDeviceLogin(context.Context, *connect.Request[v1pb.CreateDeviceLoginRequest]) (*connect.Response[v1pb.CreateDeviceLoginResponse], error)
	ExchangeDeviceLogin(context.Context, *connect.Request[v1pb.ExchangeDeviceLoginRequest]) (*connect.Response[v1pb.ExchangeDeviceLoginResponse], error)
}

// Options are the flow's knobs.
type Options struct {
	// ClientName and ClientVersion are shown on the confirmation page. They are
	// display only and the server never trusts them.
	ClientName    string
	ClientVersion string
	// Timeout bounds the whole flow, waiting for the person included.
	Timeout time.Duration
	// PrefillURL uses the address that already carries the code. The default is
	// the bare address, because a code the user has to read and type is what
	// makes the comparison on the page meaningful.
	PrefillURL bool
	// NoBrowser skips the attempt to open a browser.
	NoBrowser bool
	// OpenBrowser opens a URL. It is injected so tests do not launch anything.
	OpenBrowser func(url string) error
	// Sleep waits between polls. It is injected so tests do not wait.
	Sleep func(time.Duration)
	// Now reports the current time.
	Now func() time.Time
}

// Result is a completed login.
type Result struct {
	Token     string
	ExpiresIn int64
	User      *v1pb.User
}

// Progress receives the lines meant for a person. They never go to stdout.
type Progress func(format string, args ...any)

// ErrDenied is a request the user refused.
var ErrDenied = errors.New("the device login was denied")

// ErrExpired is a request that ran out of time before it was approved.
var ErrExpired = errors.New("the device login expired")

// Run performs the whole flow and returns once the server has answered.
func Run(ctx context.Context, api DeviceLoginClient, progress Progress, opts Options) (*Result, error) {
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	sleep := opts.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	created, err := api.CreateDeviceLogin(ctx, connect.NewRequest(&v1pb.CreateDeviceLoginRequest{
		ClientName:    opts.ClientName,
		ClientVersion: opts.ClientVersion,
	}))
	if err != nil {
		return nil, err
	}
	login := created.Msg

	verificationURL := login.GetVerificationUri()
	if opts.PrefillURL && login.GetVerificationUriComplete() != "" {
		verificationURL = login.GetVerificationUriComplete()
	}
	switch {
	case verificationURL == "":
		progress("Ask an administrator to configure the workspace external URL, then approve this request from the device page.")
	case opts.PrefillURL:
		progress("Approve this request in a browser:\n  %s\n  code: %s", verificationURL, login.GetUserCode())
	default:
		progress("Approve this request in a browser:\n  %s\n  code: %s", verificationURL, login.GetUserCode())
	}
	progress("The code is valid for %d seconds.", login.GetExpiresIn())

	if !opts.NoBrowser && verificationURL != "" && opts.OpenBrowser != nil {
		if err := opts.OpenBrowser(verificationURL); err != nil {
			// Not being able to open a browser is normal on a server, and the
			// URL has already been printed.
			progress("Could not open a browser automatically: %v", err)
		}
	}

	interval := time.Duration(login.GetInterval()) * time.Second
	if interval <= 0 {
		interval = 3 * time.Second
	}
	deadline := now().Add(time.Duration(login.GetExpiresIn()) * time.Second)

	for {
		if err := ctx.Err(); err != nil {
			return nil, errors.Wrap(err, "gave up waiting for the approval")
		}
		if now().After(deadline) {
			return nil, ErrExpired
		}
		sleep(interval)

		exchanged, err := api.ExchangeDeviceLogin(ctx, connect.NewRequest(&v1pb.ExchangeDeviceLoginRequest{
			DeviceCode: login.GetDeviceCode(),
		}))
		if err != nil {
			if connect.CodeOf(err) == connect.CodeResourceExhausted {
				// The server asked us to slow down; honour it and keep waiting.
				interval += time.Second
				continue
			}
			return nil, err
		}

		switch exchanged.Msg.GetState() {
		case v1pb.DeviceLoginState_APPROVED:
			return &Result{
				Token:     exchanged.Msg.GetToken(),
				ExpiresIn: exchanged.Msg.GetExpiresIn(),
				User:      exchanged.Msg.GetUser(),
			}, nil
		case v1pb.DeviceLoginState_DENIED:
			return nil, ErrDenied
		case v1pb.DeviceLoginState_EXPIRED:
			return nil, ErrExpired
		case v1pb.DeviceLoginState_PENDING, v1pb.DeviceLoginState_DEVICE_LOGIN_STATE_UNSPECIFIED:
			continue
		default:
			continue
		}
	}
}
