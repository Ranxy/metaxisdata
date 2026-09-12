//go:build release

package cmd

import (
	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/config"
)

func activeProfile() *config.Profile {
	p := getBaseProfile()
	p.Mode = common.ReleaseModeProd
	return p
}
