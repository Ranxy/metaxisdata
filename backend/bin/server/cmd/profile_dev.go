package cmd

import (
	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/config"
)

func activeProfile(dataDir string) *config.Profile {
	p := getBaseProfile(dataDir)
	p.Mode = common.ReleaseModeDev
	p.Secret = "00000000-0000-0000-0000-000000000000"
	return p
}
