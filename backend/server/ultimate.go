//go:build !minidemo

package server

import (
	// Drivers.
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/mysql"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/pg"
)
