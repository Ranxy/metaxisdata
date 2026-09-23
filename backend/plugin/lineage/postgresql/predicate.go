package postgresql

import (
	"strings"

	pgast "github.com/bytebase/omni/pg/ast"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"
)

// predicateInfluence is one column a WHERE, HAVING or ON predicate depends on,
// together with the clause that makes it an influence.
type predicateInfluence struct {
	// key is the real relation the column resolved to. It is zero when the
	// predicate is over a query-local relation, whose lineage is traced instead.
	key       predicateKey
	relation  *scope.TableRef
	column    string
	transform model.Transformation
}

// predicateKey identifies a predicate column, so a column used by several
// clauses collapses to the one edge the deduplication keeps.
type predicateKey struct {
	database string
	table    string
	column   string
}

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
					a.recordPredicate(sp, scope.ColumnRef{Schema: ref.Schema, Table: ref.Table, Column: column}, transform, false)
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
// as an influence on the statement's target rows. resolveAliases is set for a
// clause that may name a select-list alias, as HAVING does.
func (a *Analyzer) collectPredicates(expr pgast.Node, sp *scope.Scope, transform model.Transformation, resolveAliases bool) {
	if expr == nil {
		return
	}
	for _, ref := range a.extractColumnsFromNode(expr, sp) {
		a.recordPredicate(sp, ref, transform, resolveAliases)
	}
}

// recordPredicate resolves one predicate column. An unresolvable column is
// dropped, the same way an unresolvable source column is, so a predicate never
// invents a relation.
func (a *Analyzer) recordPredicate(sp *scope.Scope, ref scope.ColumnRef, transform model.Transformation, resolveAliases bool) {
	// A select-list alias is not a column of any relation. The clause influences
	// the rows the aggregate behind the alias produced, so the influence belongs
	// to that output column's own sources.
	if resolveAliases && ref.Table == "" && ref.Column != "" {
		for _, output := range sp.GetOutputColumns() {
			if output.Alias != ref.Column {
				continue
			}
			for _, source := range output.SourceColumns {
				a.recordPredicate(sp, source, transform, false)
			}
			return
		}
	}

	resolutions, err := sp.ResolveColumnRefs(ref)
	if err != nil {
		return
	}
	for _, res := range resolutions {
		influence := predicateInfluence{transform: transform}
		if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
			influence.relation = res.Relation
			influence.column = res.Ref.Column
		} else {
			influence.key = predicateKey{database: res.Ref.Schema, table: res.Ref.Table, column: res.Ref.Column}
		}
		a.predicates = append(a.predicates, influence)
	}
}

// emitPredicateInfluences adds one edge per predicate column to the rows the
// statement produces. The target column is empty: a predicate decides which rows
// are emitted, not the value of any one column.
func (a *Analyzer) emitPredicateInfluences(targetSchema, targetTable string) {
	if len(a.predicates) == 0 {
		return
	}
	isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetSchema, targetTable)
	for _, influence := range a.predicates {
		if influence.relation != nil {
			a.traceThroughTableLineageToTarget(influence.relation, influence.column, targetSchema, targetTable, "", []model.Transformation{influence.transform})
			continue
		}
		a.addRelation(NewLineageEdge(
			influence.key.database, influence.key.table, influence.key.column,
			targetSchema, targetTable, "",
			[]model.Transformation{influence.transform},
			isTemp,
		))
	}
	a.predicates = nil
}
