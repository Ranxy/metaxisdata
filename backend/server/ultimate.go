//go:build !minidemo

package server

import (
	// Drivers.
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/mysql"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/pg"

	// Schema.
	_ "github.com/Ranxy/metaxisdata/backend/plugin/schema/mysql"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/schema/pg"

	// Lineage.
	_ "github.com/Ranxy/metaxisdata/backend/plugin/lineage/mysql"
)
