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

	ExternalURL string

	// EncryptionKey is the key material used to encrypt stored credentials. It
	// comes from METADATA_SECRET_KEY; without it the store falls back to the
	// AUTH_SECRET setting in the database.
	EncryptionKey string

	// can be set in runtime
	RuntimeDebug atomic.Bool

	Secret string
}
