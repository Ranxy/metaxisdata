package starrocks

import (
	"strings"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/algorithm"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"

	nodes "github.com/bytebase/omni/starrocks/ast"
)

// collectJoinPredicates records the columns a join condition depends on. ON is
// recorded from its expression; USING names the shared column, which is a join
// key of every relation the two sides introduce. NATURAL JOIN is not recorded,
// because which columns it coalesces is a catalog question and omni does not
// answer it: the MySQL analyzer behaves the same way.
func (a *Analyzer) collectJoinPredicates(te nodes.Node) {
	join, ok := te.(*nodes.JoinClause)
	if !ok {
		return
	}
	a.collectJoinPredicates(join.Left)
	a.collectJoinPredicates(join.Right)

	sp := a.currentScope()
	if join.On != nil {
		a.collectPredicates(join.On, sp, model.NewJoinTransformation(a.exprTextOf(join.On)), false)
		return
	}
	if len(join.Using) > 0 {
		transform := model.NewJoinTransformation(usingClauseText(join))
		for _, side := range []nodes.Node{join.Left, join.Right} {
			for _, ref := range joinSideRefs(side) {
				for _, column := range join.Using {
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
func usingClauseText(join *nodes.JoinClause) string {
	return "USING(" + strings.Join(join.Using, ",") + ")"
}

// joinSideRefs returns the relation a join operand introduces, addressed the way
// a SQL reference addresses it: its alias when it has one, otherwise its name.
func joinSideRefs(te nodes.Node) []scope.ColumnRef {
	switch t := te.(type) {
	case *nodes.TableRef:
		if t.Subquery != nil {
			// A derived table's columns are traced, not addressed by name here.
			return nil
		}
		schema, table, ok := tableRefFromObjectName(t.Name)
		if !ok {
			return nil
		}
		if t.Alias != "" {
			table = t.Alias
		}
		return []scope.ColumnRef{{Schema: schema, Table: table}}
	case *nodes.JoinClause:
		return append(joinSideRefs(t.Left), joinSideRefs(t.Right)...)
	default:
		return nil
	}
}

// collectPredicates resolves every column a predicate depends on and records it
// as an influence on the rows the current scope produces. resolveAliases is set
// for a clause that may name a select-list alias, as HAVING does.
//
// A subquery operand is a leaf carrying only raw text here, so its own columns
// are reached through expressionSubquerySources; the same call analyzes the
// subquery, whose own predicates become influences of the rows it contributes.
func (a *Analyzer) collectPredicates(expr nodes.Node, sp *scope.Scope, transform model.Transformation, resolveAliases bool) {
	if expr == nil {
		return
	}
	refs := collectColumns(expr)
	refs = append(refs, a.expressionSubquerySources(expr, sp)...)
	for _, ref := range refs {
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
		NewEdge: scope.NewLineageEdge,
	})
}
