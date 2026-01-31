package model

// OperationType represents the type of transformation operation.
type OperationType string

const (
	// OperationDelete represents a DELETE operation.
	OperationDelete OperationType = "DELETE"
	// OperationUnion represents a UNION operation.
	OperationUnion OperationType = "UNION"
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

	// OpType is the operator type like "+", "-", "=" (for OPERATOR operation).
	OpType string `json:"op_type,omitempty"`

	// Condition is the WHERE condition expression (for DELETE operation).
	Condition string `json:"condition,omitempty"`
}

// NewDeleteTransformation creates a Transformation for DELETE operations.
func NewDeleteTransformation(condition string) Transformation {
	return Transformation{
		Operation: OperationDelete,
		Condition: condition,
	}
}

// NewUnionTransformation creates a Transformation for UNION operations.
func NewUnionTransformation() Transformation {
	return Transformation{
		Operation: OperationUnion,
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
