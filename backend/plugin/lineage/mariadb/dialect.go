// Package mariadb provides direct lineage analysis for MariaDB queries.
//
// The traversal in analyzer_body_gen.go is generated from the MySQL analyzer in
// backend/plugin/lineage/mysql; this file holds what is MariaDB's own — the omni
// parser it is built on, the engine it registers and the two hooks the shared
// traversal varies by. Edit the traversal in the MySQL file and run
// `go generate ./backend/plugin/lineage/mysql`.
//
// Known gaps and their intended resolution are tracked in
// plan/mysql_lineage_optimization_plan.md; defects in omni itself are recorded
// in docs/omni_upstream_defects.md.
package mariadb

import (
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"

	nodes "github.com/bytebase/omni/mariadb/ast"
)

// Constants for special table/column markers.
const (
	resultTableName   = model.ResultTableName
	deletionFieldName = "__deletion__"
	wildcardColumn    = model.WildcardColumn
	fileSourceMarker  = "__file__" // Special marker for LOAD DATA source
)

func init() {
	// Only this dialect is claimed here; the other MySQL-family engines register
	// their own analyzers in their own packages.
	lineage.RegisterAnalyzeRelation(storepb.Engine_MARIADB, Analyze, splitStatements)
}

// valuesQueryPrimary returns the VALUES query primary of a select statement
// (`SELECT * FROM (VALUES ROW(1)) v`), or nil when the statement has none. This
// dialect's AST models the form as a field of SelectStmt.
func valuesQueryPrimary(stmt *nodes.SelectStmt) *nodes.ValuesStmt {
	return stmt.ValuesSource
}

// rowAliasNames returns the row alias an INSERT's VALUES row declares. MariaDB has
// no row aliases in INSERT ... VALUES ... ON DUPLICATE KEY UPDATE, and omni's
// MariaDB AST has no field for one either, so the form is absent in this dialect.
// See docs/omni_upstream_defects.md.
func rowAliasNames(*nodes.InsertStmt) (string, []string) {
	return "", nil
}
