// Package tidb provides direct lineage analysis for TiDB queries.
//
// The traversal in analyzer_body_gen.go is generated from the MySQL analyzer in
// backend/plugin/lineage/mysql; this file holds what is TiDB's own — the omni
// parser it is built on, the engine it registers and the two hooks the shared
// traversal varies by. Edit the traversal in the MySQL file and run
// `go generate ./backend/plugin/lineage/mysql`.
//
// Known gaps and their intended resolution are tracked in
// plan/mysql_lineage_optimization_plan.md; defects in omni itself are recorded
// in docs/omni_upstream_defects.md.
package tidb

import (
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"

	nodes "github.com/bytebase/omni/tidb/ast"
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
	lineage.RegisterAnalyzeRelation(storepb.Engine_TIDB, Analyze, splitStatements)
}

// valuesQueryPrimary returns the VALUES query primary of a select statement. This
// dialect's AST has no field for the form — omni models a VALUES row set only as a
// standalone statement here — so a `VALUES` primary in a derived table or a set
// operation stays unrepresentable. See docs/omni_upstream_defects.md.
func valuesQueryPrimary(*nodes.SelectStmt) *nodes.ValuesStmt {
	return nil
}

// rowAliasNames returns the row alias an INSERT's VALUES row declares together
// with its optional column list (`INSERT ... VALUES (...) AS new(a) ...`), which
// an upsert assignment may then use to name the proposed row. Both are empty when
// the statement declares none.
func rowAliasNames(stmt *nodes.InsertStmt) (string, []string) {
	return stmt.RowAlias, stmt.ColAliases
}
