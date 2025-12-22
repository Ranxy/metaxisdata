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
	}

	config.RuntimeDebug.Store(flags.debug)
	return config
}
