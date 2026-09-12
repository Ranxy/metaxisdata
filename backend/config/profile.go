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
}
