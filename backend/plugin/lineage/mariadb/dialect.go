// Package mariadb provides direct lineage analysis for MariaDB queries.
//
// The traversal in analyzer_body_gen.go is generated from the MySQL analyzer in
// backend/plugin/lineage/mysql; this file holds what is MariaDB's own — the omni
// parser it is built on, the engine it registers and the two hooks the shared
// traversal varies by. Edit the traversal in the MySQL file and run
// `go generate ./backend/plugin/lineage/mysql`.
//
// Known gaps and their intended resolution are tracked in
// .agents/docs/lineage-semantics.md; defects in omni itself are recorded
// in .agents/docs/omni-upstream-defects.md.
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
	deletionFieldName = model.DeletionColumnName
	wildcardColumn    = model.WildcardColumn
	fileSourceMarker  = model.FileSourceName
)

// Registration binds MariaDB to the analyzer this package provides. The process
// assembles the registered engines where it is built, so this package does not
// register itself into package state.
func Registration() lineage.EngineRegistration {
	return lineage.EngineRegistration{
		Engine:  storepb.Engine_MARIADB,
		Analyze: Analyze,
		Split:   SplitStatements,
	}
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
// See .agents/docs/omni-upstream-defects.md.
func rowAliasNames(*nodes.InsertStmt) (string, []string) {
	return "", nil
}
