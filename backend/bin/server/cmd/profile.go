package cmd

import (
	"os"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/config"
)

func getBaseProfile(_ string) *config.Profile {
	config := &config.Profile{
		Mode:  common.ReleaseMode("dev"),
		Port:  flags.port,
		PgURL: os.Getenv("PG_URL"),
		// The JWT signing key is injected through the environment. When it is
		// absent, the server falls back to the per-deployment AUTH_SECRET setting
		// stored in the database.
		Secret: os.Getenv("JWT_SECRET"),
	}

	config.RuntimeDebug.Store(flags.debug)
	return config
}
