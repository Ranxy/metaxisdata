package config

import (
	"sync/atomic"

	"github.com/Ranxy/metaxisdata/backend/common"
)

type Profile struct {
	// Mode can be "prod" or "dev"
	Mode common.ReleaseMode
	// Port is the binding port for the server.
	Port int
	// PgURL is the PostgreSQL instance connection url
	PgURL string

	// can be set in runtime
	RuntimeDebug atomic.Bool

	// Secret is the resolved per-deployment JWT signing key. It is populated
	// from the AUTH_SECRET setting in the database at startup, never from a
	// flag or environment variable.
	Secret string

	// CORSAllowOrigins is the exact list of browser origins allowed to call the
	// server with credentials. An empty list installs no CORS middleware at
	// all, so only the browser same-origin policy applies. It replaces the
	// former "any origin in dev" behavior: a wide-open credentialed CORS is a
	// CSRF vector and must be opted into explicitly.
	CORSAllowOrigins []string

	// TrustedProxies are the peer IPs or CIDRs whose X-Forwarded-For may be
	// believed when recording an audit source address. Empty means the
	// connection address is used, so a client cannot forge its audit IP.
	TrustedProxies []string
}
