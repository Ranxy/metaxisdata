// Package algorithm holds the parts of lineage analysis that do not depend on
// the SQL dialect: how a lineage edge is identified for deduplication, how a
// row-set predicate is attributed to the scope whose rows it decides, and how
// the arms of a set operation merge. Every dialect analyzer keeps its own syntax
// tree walk and AST adaptation on top of it.
//
// The split exists because the same mechanism written once per dialect did drift
// apart: the MySQL family and StarRocks were still dropping a real relation's
// lineage when a CTE shared its name, losing one of two transformations that
// reached the same target column, and attributing an unreferenced CTE's WHERE to
// the statement's result — all three of which PostgreSQL had already fixed in
// its own copy of the same code.
package algorithm

import (
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// EdgeSet accumulates column lineage edges, dropping duplicates.
//
// An edge's identity is its endpoints together with the transformations that
// produced it. The endpoints alone are not enough: one column can reach the same
// target column through two different expressions (`SELECT x + 1 AS a, x + 2 AS
// a FROM t`), and a column can both be projected into a target and decide which
// rows reach it.
//
// Endpoints are compared as identifier values and transformations are compared
// field by field, never through a rendering of either. Joining the fields into a
// string to get one comparable key cannot tell an identifier that contains the
// separator from two identifiers around it.
type EdgeSet struct {
	edges []model.ColumnRelation
	seen  map[endpoints][]model.ColumnRelation
}

// NewEdgeSet creates an empty edge set.
func NewEdgeSet() *EdgeSet {
	return &EdgeSet{seen: make(map[endpoints][]model.ColumnRelation)}
}

// Add records one edge unless an edge with the same identity is already there.
func (s *EdgeSet) Add(relation model.ColumnRelation) {
	key := endpointsOf(relation)
	for _, existing := range s.seen[key] {
		if model.SameTransformations(existing.Transformation, relation.Transformation) {
			return
		}
	}
	s.seen[key] = append(s.seen[key], relation)
	s.edges = append(s.edges, relation)
}

// Edges returns the distinct edges in the order they were added.
func (s *EdgeSet) Edges() []model.ColumnRelation {
	return s.edges
}

// Len returns the number of distinct edges recorded.
func (s *EdgeSet) Len() int {
	return len(s.edges)
}

// NewEdgeFunc builds an edge from its endpoints. The dialects differ in where
// they record a reference's qualifier — the MySQL family and StarRocks name a
// database, PostgreSQL a schema — so each one passes the constructor it records
// with.
type NewEdgeFunc func(fromQualifier, fromTable, fromField, toQualifier, toTable, toField string, transform []model.Transformation, isTemp bool) model.ColumnRelation

// endpoints is the part of an edge that a comparison can index by: the two
// columns it connects.
type endpoints struct {
	source      model.ObjectIdentifier
	sourceField string
	target      model.ObjectIdentifier
	targetField string
}

func endpointsOf(relation model.ColumnRelation) endpoints {
	return endpoints{
		source:      relation.Source.Table,
		sourceField: relation.Source.Name,
		target:      relation.Target.Table,
		targetField: relation.Target.Name,
	}
}
