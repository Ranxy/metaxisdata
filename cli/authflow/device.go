// Package authflow drives the device login: it asks the server for a request,
// tells the person where to approve it, and polls until the server has an
// answer.
package authflow

import (
	"context"
	neturl "net/url"
	"strings"
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
	// BareURL prints the page address without the code, so it has to be typed
	// there. The default carries the code: the terminal that starts the request
	// is the one that prints the link, so retyping the code it just showed only
	// adds a way to mistype it. The page still shows the code next to the client
	// name, version, source address and time, and still asks for an explicit
	// decision, which is what lets a person notice a request they did not start.
	BareURL bool
	// NoBrowser skips the attempt to open a browser.
	NoBrowser bool
	// ServerURL is the address the client is talking to. It is the fallback
	// confirmation address when the workspace has no external URL configured,
	// which is the normal state of a fresh or local deployment: the server
	// cannot name a page it does not know about, but the client at least knows
	// where it is sending requests.
	ServerURL string
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

// devicePagePath is the confirmation page inside the web application.
const devicePagePath = "/device"

// confirmationURL picks the address to send the person to, and reports whether
// it had to fall back.
//
// The server names the page only when the workspace has an external URL; a
// fresh or locally run deployment has none, and answering "ask an
// administrator" would leave the person with no way forward. It is empty rather
// than wrong on purpose, so the client substitutes the address it is already
// talking to.
//
// The code is carried in the address unless the caller asked for the bare page:
// the terminal that prints this link is the one that started the request, so
// retyping the code it just showed only adds a way to mistype it.
func confirmationURL(login *v1pb.CreateDeviceLoginResponse, opts Options) (url string, fallback bool) {
	page := login.GetVerificationUri()
	if page == "" {
		base := strings.TrimSuffix(opts.ServerURL, "/")
		if base == "" {
			return "", false
		}
		page, fallback = base+devicePagePath, true
	}
	if opts.BareURL {
		return page, fallback
	}
	if complete := login.GetVerificationUriComplete(); complete != "" {
		return complete, fallback
	}
	return page + "?user_code=" + neturl.QueryEscape(login.GetUserCode()), fallback
}

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

	verificationURL, fallback := confirmationURL(login, opts)
	if fallback {
		progress("The workspace has no external URL configured; using the server address.")
	}
	if verificationURL == "" {
		progress("No confirmation address is available: ask an administrator to configure the workspace external URL.")
	} else {
		progress("Approve this request in a browser:\n  %s\n  code: %s", verificationURL, login.GetUserCode())
		if fallback {
			// The server address is the best guess the client can make. It is
			// right whenever the server also serves the web application, and
			// wrong when the SPA runs elsewhere (the usual local setup), so say
			// what to change instead of letting the person stare at a 404.
			progress("If that page is not served there, set the workspace external URL so the server can name it.")
		}
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
