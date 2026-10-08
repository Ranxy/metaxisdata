// Package tidb provides direct lineage analysis for TiDB queries.
//
// The traversal in analyzer_body_gen.go is generated from the MySQL analyzer in
// backend/plugin/lineage/mysql; this file holds what is TiDB's own — the omni
// parser it is built on, the engine it registers and the two hooks the shared
// traversal varies by. Edit the traversal in the MySQL file and run
// `go generate ./backend/plugin/lineage/mysql`.
//
// Known gaps and their intended resolution are tracked in
// .agents/docs/lineage-semantics.md; defects in omni itself are recorded
// in .agents/docs/omni-upstream-defects.md.
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
	deletionFieldName = model.DeletionColumnName
	wildcardColumn    = model.WildcardColumn
	fileSourceMarker  = model.FileSourceName
)

// Registration binds TiDB to the analyzer this package provides. The process
// assembles the registered engines where it is built, so this package does not
// register itself into package state.
func Registration() lineage.EngineRegistration {
	return lineage.EngineRegistration{
		Engine:  storepb.Engine_TIDB,
		Analyze: Analyze,
		Split:   SplitStatements,
	}
}

// valuesQueryPrimary returns the VALUES query primary of a select statement. This
// dialect's AST has no field for the form — omni models a VALUES row set only as a
// standalone statement here — so a `VALUES` primary in a derived table or a set
// operation stays unrepresentable. See .agents/docs/omni-upstream-defects.md.
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
