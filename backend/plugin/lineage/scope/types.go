package scope

import "github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"

// ColumnRef represents a reference to a column in the query.
type ColumnRef struct {
	Schema string
	Table  string
	Column string
}

// TableRef represents a table or table-like source (subquery, CTE).
type TableRef struct {
	Schema string
	Table  string
	Alias  string
	// For subqueries and CTEs
	IsSubquery bool
	IsCTE      bool
	// Columns available from this table source
	Columns []string
	// Lineage edges for subqueries (how output columns relate to source tables)
	Lineage []model.ColumnRelation
}

// CTEDefinition represents a Common Table Expression.
type CTEDefinition struct {
	Name    string
	Columns []string
	// The scope in which this CTE was defined
	DefiningScope *Scope
	// Lineage edges within the CTE
	Lineage []model.ColumnRelation
}

// OutputColumn represents a column in the SELECT output.
type OutputColumn struct {
	Alias      string // The alias given to this column (AS clause)
	Expression string // The expression text
	// Source columns that this output depends on
	SourceColumns []ColumnRef
	// Whether this is a derived/transformed column
	IsDerived bool
	Transform []model.Transformation
}

// NewLineageEdge creates a new LineageEdge (ColumnRelation) from field-edge parameters.
// This helper is used during the migration from FieldEdge to ColumnRelation.
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

	// Check transformation content to determine type
	for _, t := range transform {
		switch t.Operation {
		case model.OperationDelete:
			return model.RelationTypeIndirect
		case model.OperationUnion:
			return model.RelationTypeUnion
		case model.OperationAggregate:
			return model.RelationTypeGroup
		default:
			// For other operations, it's indirect
			return model.RelationTypeIndirect
		}
	}

	// Default to indirect if there's any transformation
	return model.RelationTypeIndirect
}
