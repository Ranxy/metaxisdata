package algorithm

import (
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"
)

// AddEdgeFunc records one edge in the graph being built.
type AddEdgeFunc func(model.ColumnRelation)

// TraceThroughTableLineage records a query-local relation's own lineage against
// the statement's result rows, combining each edge's transformations with the
// ones the reference adds.
func TraceThroughTableLineage(newEdge NewEdgeFunc, add AddEdgeFunc, tableRef *scope.TableRef, columnName, outputAlias string, transform []model.Transformation) {
	for _, edge := range model.AnsweringLineage(tableRef.Lineage, columnName) {
		actualOutput := outputAlias
		// For wildcard expansion, use the actual column name from the edge.
		if columnName == model.WildcardColumn && outputAlias == model.WildcardColumn {
			actualOutput = edge.Target.Name
		}

		add(newEdge(
			scope.RelationKeyOf(edge.Source.Table).Qualifier, edge.Source.Table.Name, edge.Source.Name,
			"", model.ResultTableName, actualOutput,
			model.CombineTransformations(edge.Transformation, transform),
			true,
		))
	}
}

// TraceThroughTableLineageToTarget records a query-local relation's own lineage
// against a specific real target table, so an edge never names a CTE or a
// derived-table alias as if it were a stored object.
func TraceThroughTableLineageToTarget(newEdge NewEdgeFunc, add AddEdgeFunc, tableRef *scope.TableRef, columnName, targetQualifier, targetTable, targetColumn string, transform []model.Transformation) {
	for _, edge := range model.AnsweringLineage(tableRef.Lineage, columnName) {
		actualTargetColumn := targetColumn
		if columnName == model.WildcardColumn && targetColumn == model.WildcardColumn {
			actualTargetColumn = edge.Target.Name
		}

		add(newEdge(
			scope.RelationKeyOf(edge.Source.Table).Qualifier, edge.Source.Table.Name, edge.Source.Name,
			targetQualifier, targetTable, actualTargetColumn,
			model.CombineTransformations(edge.Transformation, transform),
			targetTable == model.ResultTableName,
		))
	}
}

// FlattenTempSourceLineage appends a query-local relation's lineage, resolved
// down to stored relations, to a temporary relation's own lineage. It reports
// whether the source was a query-local relation and was therefore handled.
func FlattenTempSourceLineage(newEdge NewEdgeFunc, sp *scope.Scope, relation *scope.TableRef, columnName, targetTable, targetColumn string, transform []model.Transformation, lineage *[]model.ColumnRelation) bool {
	if relation == nil || (!relation.IsSubquery && !relation.IsCTE) {
		return false
	}
	appendFlattenedLineage(newEdge, lineage, sp, relation, columnName, targetTable, targetColumn, transform)
	return true
}

func appendFlattenedLineage(newEdge NewEdgeFunc, lineage *[]model.ColumnRelation, sp *scope.Scope, tableRef *scope.TableRef, columnName, targetTable, targetColumn string, transform []model.Transformation) {
	for _, edge := range model.AnsweringLineage(tableRef.Lineage, columnName) {
		actualTarget := targetColumn
		if columnName == model.WildcardColumn && targetColumn == model.WildcardColumn {
			actualTarget = edge.Target.Name
		}

		combinedTransform := model.CombineTransformations(edge.Transformation, transform)

		// A temporary relation can be built on another one, so the search
		// continues until an edge names a stored relation.
		if nestedRef, ok := sp.FindRelation(scope.RelationKeyOf(edge.Source.Table)); ok && (nestedRef.IsCTE || nestedRef.IsSubquery) {
			appendFlattenedLineage(newEdge, lineage, sp, nestedRef, edge.Source.Name, targetTable, actualTarget, combinedTransform)
			continue
		}

		*lineage = append(*lineage, newEdge(
			scope.RelationKeyOf(edge.Source.Table).Qualifier, edge.Source.Table.Name, edge.Source.Name,
			"", targetTable, actualTarget,
			combinedTransform,
			true,
		))
	}
}

// FlattenTempSources replaces a source that resolved to a query-local relation
// with the stored relations that relation's own lineage came from, combining the
// transformations along the way. A query-local relation's name is an alias that
// exists only in the scope the query wrote it in, so a reference carried into an
// enclosing scope must not keep it: resolving the alias again there can bind to
// an unrelated real relation that happens to share the name.
func FlattenTempSources(sp *scope.Scope, ref scope.ColumnRef, relation *scope.TableRef, transform []model.Transformation) []scope.ColumnSource {
	if relation == nil || (!relation.IsCTE && !relation.IsSubquery) {
		return []scope.ColumnSource{{Ref: ref, Transform: transform}}
	}

	var out []scope.ColumnSource
	for _, edge := range model.AnsweringLineage(relation.Lineage, ref.Column) {
		combinedTransform := model.CombineTransformations(edge.Transformation, transform)
		nested, ok := sp.FindRelation(scope.RelationKeyOf(edge.Source.Table))
		if ok && (nested.IsCTE || nested.IsSubquery) {
			nestedRef := scope.ColumnRef{Schema: edge.Source.Table.Schema, Table: edge.Source.Table.Name, Column: edge.Source.Name}
			out = append(out, FlattenTempSources(sp, nestedRef, nested, combinedTransform)...)
			continue
		}
		out = append(out, scope.ColumnSource{
			Ref: scope.ColumnRef{
				Schema:   edge.Source.Table.Schema,
				Table:    edge.Source.Table.Name,
				Column:   edge.Source.Name,
				Resolved: true,
			},
			Transform: combinedTransform,
		})
	}
	return out
}
