package postgresql

import (
	"strings"

	pgast "github.com/bytebase/omni/pg/ast"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/algorithm"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"
)

// collectJoinPredicates records the columns a join condition depends on. ON is
// recorded from its expression; USING names the shared column, which is a join
// key of every relation the two sides introduce. NATURAL JOIN is not recorded,
// because which columns it coalesces is a catalog question and omni does not
// answer it: the MySQL analyzer behaves the same way.
func (a *Analyzer) collectJoinPredicates(node pgast.Node) {
	join, ok := node.(*pgast.JoinExpr)
	if !ok {
		return
	}
	a.collectJoinPredicates(join.Larg)
	a.collectJoinPredicates(join.Rarg)

	sp := a.currentScope()
	if join.Quals != nil {
		a.collectPredicates(join.Quals, sp, model.NewJoinTransformation(a.exprTextOf(join.Quals)), false)
		return
	}
	if join.UsingClause != nil {
		transform := model.NewJoinTransformation(usingClauseText(join))
		for _, side := range []pgast.Node{join.Larg, join.Rarg} {
			for _, ref := range joinSideRefs(side) {
				for _, column := range stringList(join.UsingClause) {
					a.influences.Resolve(sp, scope.ColumnRef{Schema: ref.Schema, Table: ref.Table, Column: column}, transform, false)
				}
			}
		}
	}
}

// usingClauseText renders the USING clause a join was written with. Its column
// list comes from the parsed clause rather than from a source slice, because a
// nested join tree is a single node whose right operand's location ends inside
// its own parentheses.
func usingClauseText(join *pgast.JoinExpr) string {
	return "USING (" + strings.Join(stringList(join.UsingClause), ", ") + ")"
}

// joinSideRefs returns the relation a join operand introduces, addressed the way
// a SQL reference addresses it: its alias when it has one, otherwise its name.
func joinSideRefs(node pgast.Node) []scope.ColumnRef {
	switch t := node.(type) {
	case *pgast.RangeVar:
		name := t.Relname
		if t.Alias != nil && t.Alias.Aliasname != "" {
			name = t.Alias.Aliasname
		}
		return []scope.ColumnRef{{Schema: t.Schemaname, Table: name}}
	case *pgast.JoinExpr:
		return append(joinSideRefs(t.Larg), joinSideRefs(t.Rarg)...)
	default:
		// A derived table's columns are traced, not addressed by name here.
		return nil
	}
}

// collectPredicates resolves every column a predicate depends on and records it
// as an influence on the rows the current scope produces. resolveAliases is set
// for a clause that may name a select-list alias, as HAVING does.
func (a *Analyzer) collectPredicates(expr pgast.Node, sp *scope.Scope, transform model.Transformation, resolveAliases bool) {
	if expr == nil {
		return
	}
	for _, ref := range a.extractColumnsFromNode(expr, sp) {
		a.influences.Resolve(sp, ref, transform, resolveAliases)
	}
}

// emitPredicateInfluences adds one edge per predicate column of sp to the rows
// the statement produces. The target column is empty: a predicate decides which
// rows are emitted, not the value of any one column.
func (a *Analyzer) emitPredicateInfluences(sp *scope.Scope, targetSchema, targetTable string) {
	a.influences.Emit(sp, targetSchema, targetTable, targetTable == resultTableName, algorithm.Emitter{
		Trace:   a.traceThroughTableLineageToTarget,
		AddEdge: a.addRelation,
		NewEdge: scope.NewSchemaLineageEdge,
	})
}
