// Package engines assembles the SQL lineage analyzers this build supports.
//
// The set is a compile-time list rather than package state an init function fills
// in: every dialect exports a Registration binding it to the engine it serves, and
// this package names the dialects that are part of the build. The process builds
// one lineage analyzer from it at startup, so what is registered cannot change
// under a running server, and a test can build an analyzer over exactly the
// engines it needs.
//
// OceanBase and Doris are deliberately absent: the runner records a per-object
// skip for them rather than analyzing their SQL with another engine's grammar.
package engines

import (
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/mariadb"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/mysql"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/postgresql"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/starrocks"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/tidb"
)

// Registrations returns one binding per supported engine. TiDB and MariaDB are
// separate entries even though their traversal is generated from MySQL: each has
// its own parser, and a statement one accepts the others may not.
func Registrations() []lineage.EngineRegistration {
	return []lineage.EngineRegistration{
		mysql.Registration(),
		tidb.Registration(),
		mariadb.Registration(),
		postgresql.Registration(),
		starrocks.Registration(),
	}
}
