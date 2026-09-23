package starrocks

import (
	"strings"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"

	nodes "github.com/bytebase/omni/starrocks/ast"
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
// as an influence on the statement's target rows. resolveAliases is set for a
// clause that may name a select-list alias, as HAVING does.
//
// A subquery operand is a leaf carrying only raw text here, so its own columns
// are reached through expressionSubquerySources; the same call analyzes the
// subquery, which records the predicates of its own WHERE as influences too.
func (a *Analyzer) collectPredicates(expr nodes.Node, sp *scope.Scope, transform model.Transformation, resolveAliases bool) {
	if expr == nil {
		return
	}
	refs := collectColumns(expr)
	refs = append(refs, a.expressionSubquerySources(expr)...)
	for _, ref := range refs {
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
		a.addRelation(scope.NewLineageEdge(
			influence.key.database, influence.key.table, influence.key.column,
			targetSchema, targetTable, "",
			[]model.Transformation{influence.transform},
			isTemp,
		))
	}
	a.predicates = nil
}
