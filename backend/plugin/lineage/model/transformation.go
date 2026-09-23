package model

import "slices"

// OperationType represents the type of transformation operation.
type OperationType string

const (
	// OperationDelete represents a DELETE operation.
	OperationDelete OperationType = "DELETE"
	// OperationUnion represents a UNION operation.
	OperationUnion OperationType = "UNION"
	// OperationIntersect represents an INTERSECT operation.
	OperationIntersect OperationType = "INTERSECT"
	// OperationExcept represents an EXCEPT operation.
	OperationExcept OperationType = "EXCEPT"
	// OperationProject represents a projection (simple column reference or expression).
	OperationProject OperationType = "PROJECT"
	// OperationFunction represents a function call.
	OperationFunction OperationType = "FUNCTION"
	// OperationAggregate represents an aggregate function (COUNT, SUM, etc.).
	OperationAggregate OperationType = "AGGREGATE"
	// OperationWindow represents a window function.
	OperationWindow OperationType = "WINDOW"
	// OperationOperator represents an arithmetic or comparison operator.
	OperationOperator OperationType = "OPERATOR"
	// OperationCase represents a CASE expression.
	OperationCase OperationType = "CASE"
	// OperationFilter represents a predicate that decides which rows a statement
	// produces without its value flowing into any output column (WHERE, HAVING).
	OperationFilter OperationType = "FILTER"
	// OperationJoin represents a join condition.
	OperationJoin OperationType = "JOIN"
	// OperationGroupBy represents a grouping that decides which rows are
	// aggregated together. The SQL analyzers record it as AGGREGATE group keys;
	// this value carries OpenLineage's INDIRECT/GROUP_BY subtype.
	OperationGroupBy OperationType = "GROUP_BY"
	// OperationSort represents an ordering of the output rows. The SQL analyzers
	// do not produce it yet; it carries OpenLineage's INDIRECT/SORT subtype.
	OperationSort OperationType = "SORT"
)

// Transformation represents a data transformation operation in the lineage.
// It uses a tagged union pattern where Operation determines which fields are relevant.
type Transformation struct {
	// Operation is the type of transformation operation.
	Operation OperationType `json:"operation"`

	// Expression is the text representation of the expression (used by most operation types).
	Expression string `json:"expression,omitempty"`

	// FunctionName is the name of the function (for FUNCTION, AGGREGATE, WINDOW operations).
	FunctionName string `json:"function_name,omitempty"`

	// Arguments contains function arguments (for FUNCTION operation).
	Arguments []string `json:"arguments,omitempty"`

	// GroupKeys contains GROUP BY keys (for AGGREGATE operation).
	GroupKeys []string `json:"group_keys,omitempty"`

	// PartitionBy contains PARTITION BY columns (for WINDOW operation).
	PartitionBy []string `json:"partition_by,omitempty"`

	// OrderBy contains ORDER BY columns (for WINDOW operation).
	OrderBy []string `json:"order_by,omitempty"`

	// OpType is the operator kind for an OPERATOR transformation, e.g.
	// "ADDITION", "SUBTRACTION", "EQUALS".
	OpType string `json:"op_type,omitempty"`

	// Condition is the predicate text a row-set operation carries: the DELETE
	// condition, or the WHERE / HAVING / ON expression of a FILTER or JOIN
	// influence edge.
	Condition string `json:"condition,omitempty"`

	// All records that a set operation keeps duplicate rows instead of removing
	// them, which is the difference between UNION ALL and UNION (and between
	// INTERSECT ALL / EXCEPT ALL and their plain forms). It is meaningful for
	// the set-operation kinds only.
	All bool `json:"all,omitempty"`
}

// NewFilterTransformation creates a Transformation for a predicate that selects
// which rows a statement produces.
func NewFilterTransformation(condition string) Transformation {
	return Transformation{
		Operation: OperationFilter,
		Condition: condition,
	}
}

// NewJoinTransformation creates a Transformation for a join condition.
func NewJoinTransformation(condition string) Transformation {
	return Transformation{
		Operation: OperationJoin,
		Condition: condition,
	}
}

// NewDeleteTransformation creates a Transformation for DELETE operations.
func NewDeleteTransformation(condition string) Transformation {
	return Transformation{
		Operation: OperationDelete,
		Condition: condition,
	}
}

// NewUnionTransformation creates a Transformation for a UNION, or for a UNION ALL
// when all is set.
func NewUnionTransformation(all bool) Transformation {
	return Transformation{
		Operation: OperationUnion,
		All:       all,
	}
}

// NewIntersectTransformation creates a Transformation for an INTERSECT, or for an
// INTERSECT ALL when all is set.
func NewIntersectTransformation(all bool) Transformation {
	return Transformation{
		Operation: OperationIntersect,
		All:       all,
	}
}

// NewExceptTransformation creates a Transformation for an EXCEPT, or for an
// EXCEPT ALL when all is set.
func NewExceptTransformation(all bool) Transformation {
	return Transformation{
		Operation: OperationExcept,
		All:       all,
	}
}

// NewProjectTransformation creates a Transformation for simple projection.
func NewProjectTransformation(expression string) Transformation {
	return Transformation{
		Operation:  OperationProject,
		Expression: expression,
	}
}

// NewFunctionTransformation creates a Transformation for function calls.
func NewFunctionTransformation(functionName, expression string, arguments []string) Transformation {
	return Transformation{
		Operation:    OperationFunction,
		FunctionName: functionName,
		Expression:   expression,
		Arguments:    arguments,
	}
}

// NewAggregateTransformation creates a Transformation for aggregate functions.
func NewAggregateTransformation(functionName, expression string, groupKeys []string) Transformation {
	return Transformation{
		Operation:    OperationAggregate,
		FunctionName: functionName,
		Expression:   expression,
		GroupKeys:    groupKeys,
	}
}

// NewWindowTransformation creates a Transformation for window functions.
func NewWindowTransformation(functionName, expression string, partitionBy, orderBy []string) Transformation {
	return Transformation{
		Operation:    OperationWindow,
		FunctionName: functionName,
		Expression:   expression,
		PartitionBy:  partitionBy,
		OrderBy:      orderBy,
	}
}

// NewOperatorTransformation creates a Transformation for operator expressions.
func NewOperatorTransformation(opType, expression string) Transformation {
	return Transformation{
		Operation:  OperationOperator,
		OpType:     opType,
		Expression: expression,
	}
}

// NewCaseTransformation creates a Transformation for CASE expressions.
func NewCaseTransformation(expression string) Transformation {
	return Transformation{
		Operation:  OperationCase,
		Expression: expression,
	}
}

// CombineTransformations joins an edge's own transformations with the ones a
// consumer adds on top of them, returning a list the caller owns.
//
// Neither argument is written to and the result never shares an array with
// either. Appending to the first argument instead returns a list backed by the
// array the argument already had, and a later append into the combined list then
// overwrites the transformations of every other edge built from the same base.
func CombineTransformations(base, additional []Transformation) []Transformation {
	switch {
	case len(base) == 0:
		return append([]Transformation(nil), additional...)
	case len(additional) == 0:
		return append([]Transformation(nil), base...)
	}
	out := make([]Transformation, 0, len(base)+len(additional))
	out = append(out, base...)
	return append(out, additional...)
}

// Equal reports whether two transformations describe the same operation with the
// same fields, field by field.
func (t Transformation) Equal(other Transformation) bool {
	return t.Operation == other.Operation &&
		t.All == other.All &&
		t.Expression == other.Expression &&
		t.FunctionName == other.FunctionName &&
		t.OpType == other.OpType &&
		t.Condition == other.Condition &&
		slices.Equal(t.Arguments, other.Arguments) &&
		slices.Equal(t.GroupKeys, other.GroupKeys) &&
		slices.Equal(t.PartitionBy, other.PartitionBy) &&
		slices.Equal(t.OrderBy, other.OrderBy)
}

// SameTransformations reports whether two transformation lists are equal in
// order and content. It is how an edge's identity is decided: two edges that
// connect the same columns are the same edge only when the same operations
// produced both.
func SameTransformations(a, b []Transformation) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}
