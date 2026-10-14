package mssql

import (
	nodes "github.com/bytebase/omni/mssql/ast"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/algorithm"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"
)

// collectJoinPredicates records the columns a join condition depends on.
//
// `USING` and `NATURAL` are not handled because omni's T-SQL grammar answers
// neither: `JOIN t USING (a)` fails to parse, and `NATURAL JOIN` is read as an alias
// named NATURAL (see relationAlias and .agents/docs/omni-upstream-defects.md).
func (a *Analyzer) collectJoinPredicates(te nodes.Node) {
	join, ok := te.(*nodes.JoinClause)
	if !ok {
		return
	}
	a.collectJoinPredicates(join.Left)
	a.collectJoinPredicates(join.Right)

	// CROSS APPLY and OUTER APPLY are joins with no condition; only an ON
	// predicate decides rows.
	if join.Condition == nil {
		return
	}
	a.collectPredicates(join.Condition, a.currentScope(), model.NewJoinTransformation(a.exprTextOf(join.Condition)), false)
}

// collectPredicates resolves every column a predicate depends on and records it as
// an influence on the rows the current scope produces. resolveAliases is set for a
// clause that may name a select-list alias, as HAVING does.
func (a *Analyzer) collectPredicates(expr nodes.Node, sp *scope.Scope, transform model.Transformation, resolveAliases bool) {
	if expr == nil {
		return
	}
	for _, ref := range a.extractColumnsFromNode(expr, sp) {
		a.influences.Resolve(sp, ref, transform, resolveAliases)
	}
}

// emitPredicateInfluences adds one edge per predicate column of sp to the rows the
// statement produces. The target column is empty: a predicate decides which rows
// are emitted, not the value of any one column.
func (a *Analyzer) emitPredicateInfluences(sp *scope.Scope, targetSchema, targetTable string) {
	a.influences.Emit(sp, targetSchema, targetTable, targetTable == resultTableName, algorithm.Emitter{
		Trace:   a.traceThroughTableLineageToTarget,
		AddEdge: a.addRelation,
		NewEdge: scope.NewSchemaLineageEdge,
	})
}
