package algorithm

import (
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"
)

// Influence is one column a WHERE, HAVING or ON predicate depends on, together
// with the clause that makes it an influence.
type Influence struct {
	// Relation is the query-local relation the column resolved to, whose lineage
	// is traced instead. It is nil when the column resolved to a stored relation,
	// which Key then names.
	Relation *scope.TableRef
	Column   string
	// Key names the stored relation the column resolved to.
	Key       PredicateKey
	Transform model.Transformation
}

// PredicateKey identifies a stored relation's column, so a column used by
// several clauses collapses to the one edge the deduplication keeps.
type PredicateKey struct {
	// Qualifier is the database (MySQL family, StarRocks) or schema (PostgreSQL)
	// the relation was named with, and is empty when the query does not name one.
	Qualifier string
	Table     string
	Column    string
}

// Influences holds the row-set influences collected while a scope was current.
// They belong to the rows that scope produces and are inherited by whatever
// consumes them, so a scope whose rows reach no output cannot influence the
// statement's result.
//
// The influences used to sit in one statement-wide list that the last emitter
// drained. That attributed an unreferenced CTE's WHERE to a result it never
// reached — `WITH unused AS (SELECT id FROM t WHERE x = 1) SELECT 1` recorded
// `t.x -> __result__` — and silently dropped an UPDATE's WHERE, which nothing
// consumed.
type Influences struct {
	byScope map[*scope.Scope][]Influence
	byCTE   map[*scope.CTEDefinition][]Influence
	// notes, when set, receives every predicate column that resolved to nothing.
	// A predicate that cannot be attributed is dropped, and without a note the
	// missing influence edge would read as one the SQL never had.
	notes *Diagnostics
}

// NewInfluences creates an empty influence store. A nil notes store drops
// unresolvable predicate columns silently, which is what a caller that only wants
// the edges asks for.
func NewInfluences(notes *Diagnostics) *Influences {
	return &Influences{
		byScope: make(map[*scope.Scope][]Influence),
		byCTE:   make(map[*scope.CTEDefinition][]Influence),
		notes:   notes,
	}
}

// Reset forgets every influence. Influences belong to the statement that
// collected them, never to the next one.
func (s *Influences) Reset() {
	s.byScope = make(map[*scope.Scope][]Influence)
	s.byCTE = make(map[*scope.CTEDefinition][]Influence)
}

// Add records one influence against the scope whose rows it decides.
func (s *Influences) Add(sp *scope.Scope, influence Influence) {
	if sp == nil {
		return
	}
	s.byScope[sp] = append(s.byScope[sp], influence)
}

// Inherit moves the influences collected in from into to. A construct that
// consumes from's rows carries the predicates that shaped them, so a derived
// table or an expression subquery passes its WHERE to the query that reads it. A
// scope whose rows are never consumed keeps its influences instead: they are
// dropped with the statement rather than attributed to a result they do not
// reach, which is what an unreferenced CTE is.
func (s *Influences) Inherit(from, to *scope.Scope) {
	if from == nil || to == nil || from == to {
		return
	}
	if influences := s.byScope[from]; len(influences) > 0 {
		s.byScope[to] = append(s.byScope[to], influences...)
	}
	delete(s.byScope, from)
}

// BindCTE holds the influences a CTE body produced against the definition they
// belong to. They reach the query only if it references the CTE.
func (s *Influences) BindCTE(cte *scope.CTEDefinition, from *scope.Scope) {
	if cte == nil || from == nil {
		return
	}
	if influences := s.byScope[from]; len(influences) > 0 {
		s.byCTE[cte] = append(s.byCTE[cte], influences...)
	}
	delete(s.byScope, from)
}

// InheritCTE hands a referenced CTE's influences to the scope that reads it. The
// entry is kept rather than moved because one CTE can be referenced from several
// places, and the emitted edges are deduplicated anyway.
func (s *Influences) InheritCTE(sp *scope.Scope, cte *scope.CTEDefinition) {
	if sp == nil || cte == nil {
		return
	}
	if influences := s.byCTE[cte]; len(influences) > 0 {
		s.byScope[sp] = append(s.byScope[sp], influences...)
	}
}

// Emitter receives the edges a predicate influence produces. Tracing a
// query-local relation has to go through the dialect, because only it knows the
// lineage recorded for that relation.
type Emitter struct {
	// Trace expands a reference that resolved to a query-local relation, so the
	// edge names the stored relation behind it rather than the CTE or alias.
	Trace func(relation *scope.TableRef, column, targetQualifier, targetTable, targetColumn string, transform []model.Transformation)
	// AddEdge records one edge.
	AddEdge func(model.ColumnRelation)
	// NewEdge builds an edge whose source is a stored relation.
	NewEdge NewEdgeFunc
}

// Emit adds one edge per predicate column of sp to the rows the statement
// produces, and forgets them. The target column is empty: a predicate decides
// which rows are emitted, not the value of any one column.
func (s *Influences) Emit(sp *scope.Scope, targetQualifier, targetTable string, isTemp bool, e Emitter) {
	influences := s.byScope[sp]
	if len(influences) == 0 {
		return
	}
	delete(s.byScope, sp)
	for _, influence := range influences {
		transform := []model.Transformation{influence.Transform}
		if influence.Relation != nil {
			e.Trace(influence.Relation, influence.Column, targetQualifier, targetTable, "", transform)
			continue
		}
		e.AddEdge(e.NewEdge(
			influence.Key.Qualifier, influence.Key.Table, influence.Key.Column,
			targetQualifier, targetTable, "",
			transform,
			isTemp,
		))
	}
}

// Resolve records one predicate column. An unresolvable column is dropped, the
// same way an unresolvable source column is, so a predicate never invents a
// relation.
//
// A select-list alias is not a column of any relation. The clause influences the
// rows the aggregate behind the alias produced, so the influence belongs to that
// output column's own sources; resolveAliases is set for a clause that may name
// one, as HAVING does.
func (s *Influences) Resolve(sp *scope.Scope, ref scope.ColumnRef, transform model.Transformation, resolveAliases bool) {
	if sp == nil {
		return
	}
	if resolveAliases && ref.Table == "" && ref.Column != "" {
		for _, output := range sp.GetOutputColumns() {
			if output.Alias != ref.Column {
				continue
			}
			for _, source := range output.Sources {
				s.Resolve(sp, source.Ref, transform, false)
			}
			return
		}
	}

	resolutions, err := sp.ResolveColumnRefs(ref)
	if err != nil {
		s.notes.Unresolved("a predicate", ref)
		return
	}
	for _, res := range resolutions {
		influence := Influence{Transform: transform}
		if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
			influence.Relation = res.Relation
			influence.Column = res.Ref.Column
		} else {
			influence.Key = PredicateKey{
				Qualifier: res.Ref.Schema,
				Table:     res.Ref.Table,
				Column:    res.Ref.Column,
			}
		}
		s.Add(sp, influence)
	}
}
