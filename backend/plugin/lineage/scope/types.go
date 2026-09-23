package scope

import "github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"

// ColumnRef represents a reference to a column in the query.
type ColumnRef struct {
	Schema string
	Table  string
	Column string
	// Resolved marks a reference that was already resolved against the scope it
	// originated in. The StarRocks analyzer sets it when it merges
	// set-operation arms or flattens an expression subquery, where the resolved
	// table lives in a sibling scope that a later lookup cannot see. Every
	// other analyzer leaves it false and is unaffected.
	Resolved bool
}

// TableRef represents a table or table-like source (subquery, CTE).
type TableRef struct {
	Schema string
	Table  string
	Alias  string
	// For subqueries and CTEs
	IsSubquery bool
	IsCTE      bool
	// Lineage edges for subqueries (how output columns relate to source tables)
	Lineage []model.ColumnRelation
}

// CTEDefinition represents a Common Table Expression.
type CTEDefinition struct {
	Name    string
	Columns []string
	// Lineage edges within the CTE
	Lineage []model.ColumnRelation
}

// OutputColumn represents a column in the SELECT output.
type OutputColumn struct {
	Alias string // The alias given to this column (AS clause)
	// Source columns that this output depends on
	SourceColumns []ColumnRef
	// Whether this is a derived/transformed column
	IsDerived bool
	Transform []model.Transformation
}

// NewLineageEdge creates a ColumnRelation from field-edge parameters.
func NewLineageEdge(fromSchema, fromTable, fromField, toSchema, toTable, toField string, transform []model.Transformation, isTemp bool) model.ColumnRelation {
	// Determine relation type based on transformation
	relType := determineRelationType(transform)

	// For MySQL, the schema qualifier in SQL is actually the database name,
	// so map it to ObjectIdentifier.Database instead of Schema.
	return model.ColumnRelation{
		Source: model.Column{
			Table: model.ObjectIdentifier{
				Database: fromSchema,
				Name:     fromTable,
			},
			Name: fromField,
		},
		Target: model.Column{
			Table: model.ObjectIdentifier{
				Database: toSchema,
				Name:     toTable,
			},
			Name: toField,
		},
		Transformation: transform,
		RelationType:   relType,
		IsTemp:         isTemp,
	}
}

// determineRelationType infers the relation type from transformation info.
func determineRelationType(transform []model.Transformation) model.RelationType {
	if len(transform) == 0 {
		return model.RelationTypeDirect
	}
	// The first transformation is the outermost operation, so it decides the
	// relation type.
	switch transform[0].Operation {
	case model.OperationDelete:
		return model.RelationTypeIndirect
	case model.OperationUnion:
		return model.RelationTypeUnion
	case model.OperationIntersect:
		return model.RelationTypeIntersect
	case model.OperationExcept:
		return model.RelationTypeExcept
	case model.OperationAggregate:
		return model.RelationTypeGroup
	default:
		return model.RelationTypeIndirect
	}
}
