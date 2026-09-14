package server

import (
	// Drivers.
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/mssql"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/mysql"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/pg"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/starrocks"

	// Schema.
	_ "github.com/Ranxy/metaxisdata/backend/plugin/schema/mssql"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/schema/mysql"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/schema/pg"

	// Lineage.
	_ "github.com/Ranxy/metaxisdata/backend/plugin/lineage/mariadb"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/lineage/mysql"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/lineage/postgresql"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/lineage/tidb"
)
