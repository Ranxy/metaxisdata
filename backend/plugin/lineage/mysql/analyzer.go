// Package mysql provides direct lineage analysis for MySQL queries.
package mysql

import (
	"context"
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"

	"github.com/bytebase/parser/mysql"
)

func init() {
	lineage.RegisterAnalyzeRelation(storepb.Engine_MYSQL, Analyze)
	lineage.RegisterAnalyzeRelation(storepb.Engine_TIDB, Analyze)
}

// Constants for special table/column markers.
const (
	resultTableName   = "__result__"
	deletionFieldName = "__deletion__"
	wildcardColumn    = "*"
	fileSourceMarker  = "__file__" // Special marker for LOAD DATA source
)

// Constants for transformation operation types.
const (
	deleteOperation    = "DELETE"
	unionOperation     = "UNION"
	projectOperation   = "PROJECT"
	aggregateOperation = "AGGREGATE"
	functionOperation  = "FUNCTION"
	operatorOperation  = "OPERATOR"
	caseOperation      = "CASE"
	windowOperation    = "WINDOW"
)

// Analyzer performs direct lineage analysis on MySQL queries.
type Analyzer struct {
	ctx context.Context
	sql string
	// Current scope stack
	scopeStack []*scope.Scope
	// Collected column relations
	edges []model.ColumnRelation
	// Map for efficient edge deduplication (key: edge signature)
	edgeSet map[string]struct{}
	// Errors encountered during analysis
	errors []string
	// Optional catalog provider for wildcard expansion and metadata lookup
	catalog catalog.Provide
	// Flag to indicate if we're processing a SELECT within INSERT/REPLACE
	// When true, generateEdges should skip creating edges to __result__
	inInsertReplaceContext bool
	// Track temporary table names (CTEs, subqueries) to filter intermediate results
	tempTables map[string]struct{}
}

func Analyze(ctx context.Context, sql string) ([]model.ColumnRelation, error) {
	analyzer := NewAnalyzer(ctx, sql, lineage.CatelogProvide)
	return analyzer.AnalyzeRelations()
}

// NewAnalyzer creates a new MySQL lineage analyzer.
func NewAnalyzer(ctx context.Context, sql string, catalogProvide catalog.Provide) *Analyzer {
	a := &Analyzer{
		ctx:        ctx,
		sql:        sql,
		scopeStack: []*scope.Scope{scope.NewScope(nil)}, // Root scope
		edges:      make([]model.ColumnRelation, 0),
		edgeSet:    make(map[string]struct{}),
		errors:     make([]string, 0),
		catalog:    catalogProvide,
		tempTables: make(map[string]struct{}),
	}

	return a
}

// AnalyzeRelations parses the SQL and returns column relations.
func (a *Analyzer) AnalyzeRelations() ([]model.ColumnRelation, error) {
	// Parse SQL
	input := antlr.NewInputStream(a.sql)
	lexer := mysql.NewMySQLLexer(input)
	stream := antlr.NewCommonTokenStream(lexer, 0)
	parser := mysql.NewMySQLParser(stream)

	// Disable error output
	parser.RemoveErrorListeners()
	lexer.RemoveErrorListeners()

	// Parse the query
	tree := parser.Query()

	// Cast to concrete type
	if queryCtx, ok := tree.(*mysql.QueryContext); ok {
		a.processQuery(queryCtx)
	}

	if len(a.errors) > 0 {
		return nil, errors.Errorf("analysis errors: %s", strings.Join(a.errors, "; "))
	}

	// Return edges directly (no conversion needed)
	return a.edges, nil
}

// isTableTempInCurrentScope checks if a table is a CTE or subquery.
func (a *Analyzer) isTableTempInCurrentScope(tableName string) bool {
	for _, scope := range a.scopeStack {
		if _, ok := scope.FindCTE(tableName); ok {
			return true
		}
		if tableRef, ok := scope.FindTable(tableName); ok {
			return tableRef.IsSubquery || tableRef.IsCTE
		}
	}
	return false
}

// markTempTable records a temporary table name (CTE or subquery) for filtering intermediate edges.
func (a *Analyzer) markTempTable(name string) {
	if name == "" {
		return
	}
	a.tempTables[name] = struct{}{}
}

// isTempTable checks if a table name was marked as temporary.
func (a *Analyzer) isTempTable(name string) bool {
	_, ok := a.tempTables[name]
	return ok
}

// processQuery processes the query context.
func (a *Analyzer) processQuery(ctx *mysql.QueryContext) {
	if ctx.SimpleStatement() != nil {
		if simpleCtx, ok := ctx.SimpleStatement().(*mysql.SimpleStatementContext); ok {
			a.processSimpleStatement(simpleCtx)
		}
	}
}

// processSimpleStatement processes simple statements.
func (a *Analyzer) processSimpleStatement(ctx *mysql.SimpleStatementContext) {
	if ctx.SelectStatement() != nil {
		if selectCtx, ok := ctx.SelectStatement().(*mysql.SelectStatementContext); ok {
			a.processSelectStatement(selectCtx)
		}
	} else if ctx.InsertStatement() != nil {
		if insertCtx, ok := ctx.InsertStatement().(*mysql.InsertStatementContext); ok {
			a.processInsertStatement(insertCtx)
		}
	} else if ctx.ReplaceStatement() != nil {
		if replaceCtx, ok := ctx.ReplaceStatement().(*mysql.ReplaceStatementContext); ok {
			a.processReplaceStatement(replaceCtx)
		}
	} else if ctx.CreateStatement() != nil {
		if createCtx, ok := ctx.CreateStatement().(*mysql.CreateStatementContext); ok {
			a.processCreateStatement(createCtx)
		}
	} else if ctx.UpdateStatement() != nil {
		if updateCtx, ok := ctx.UpdateStatement().(*mysql.UpdateStatementContext); ok {
			a.processUpdateStatement(updateCtx)
		}
	} else if ctx.DeleteStatement() != nil {
		if deleteCtx, ok := ctx.DeleteStatement().(*mysql.DeleteStatementContext); ok {
			a.processDeleteStatement(deleteCtx)
		}
	} else if ctx.LoadStatement() != nil {
		if loadCtx, ok := ctx.LoadStatement().(*mysql.LoadStatementContext); ok {
			a.processLoadStatement(loadCtx)
		}
	}
}

// processSelectStatement processes SELECT statements.
func (a *Analyzer) processSelectStatement(ctx *mysql.SelectStatementContext) {
	if ctx.QueryExpression() != nil {
		if queryExprCtx, ok := ctx.QueryExpression().(*mysql.QueryExpressionContext); ok {
			a.processQueryExpression(queryExprCtx)
		}
	}
}

// processInsertStatement processes INSERT statements.
func (a *Analyzer) processInsertStatement(ctx *mysql.InsertStatementContext) {
	targetTable := ""
	targetSchema := ""
	if ctx.TableRef() != nil {
		targetTable = a.getTableName(ctx.TableRef())
		targetSchema = a.getSchemaName(ctx.TableRef())
	}

	var targetColumns []string
	if ctx.InsertQueryExpression() != nil {
		if insertQueryCtx, ok := ctx.InsertQueryExpression().(*mysql.InsertQueryExpressionContext); ok {
			if insertQueryCtx.Fields() != nil {
				targetColumns = a.extractFieldsFromInsert(insertQueryCtx.Fields())
			}
			if insertQueryCtx.QueryExpressionOrParens() != nil {
				a.inInsertReplaceContext = true
				a.processQueryExpressionOrParens(insertQueryCtx.QueryExpressionOrParens())
				a.inInsertReplaceContext = false
			}
		}
	}

	a.generateEdgesForDataModification(targetSchema, targetTable, targetColumns)

	// Process ON DUPLICATE KEY UPDATE clause if present
	if ctx.InsertUpdateList() != nil {
		if insertUpdateCtx, ok := ctx.InsertUpdateList().(*mysql.InsertUpdateListContext); ok {
			a.processInsertUpdateList(insertUpdateCtx, targetSchema, targetTable)
		}
	}
}

// processReplaceStatement processes REPLACE statements.
// REPLACE is similar to INSERT but with DELETE-then-INSERT semantics for duplicates.
// For lineage purposes, it behaves identically to INSERT.
func (a *Analyzer) processReplaceStatement(ctx *mysql.ReplaceStatementContext) {
	targetTable := ""
	targetSchema := ""
	if ctx.TableRef() != nil {
		targetTable = a.getTableName(ctx.TableRef())
		targetSchema = a.getSchemaName(ctx.TableRef())
	}

	var targetColumns []string
	if ctx.InsertQueryExpression() != nil {
		if insertQueryCtx, ok := ctx.InsertQueryExpression().(*mysql.InsertQueryExpressionContext); ok {
			if insertQueryCtx.Fields() != nil {
				targetColumns = a.extractFieldsFromInsert(insertQueryCtx.Fields())
			}
			if insertQueryCtx.QueryExpressionOrParens() != nil {
				a.inInsertReplaceContext = true
				a.processQueryExpressionOrParens(insertQueryCtx.QueryExpressionOrParens())
				a.inInsertReplaceContext = false
			}
		}
	}

	a.generateEdgesForDataModification(targetSchema, targetTable, targetColumns)
}

// generateEdgesForDataModification is a helper that generates lineage edges for INSERT/REPLACE statements.
// It handles mapping source columns from a SELECT query to target table columns.
func (a *Analyzer) generateEdgesForDataModification(targetSchema, targetTable string, targetColumns []string) {
	sp := a.currentScope()
	outputColumns := sp.GetOutputColumns()

	// Map output columns to target columns
	for i, outputCol := range outputColumns {
		targetColName := outputCol.Alias
		if i < len(targetColumns) {
			// Use explicit target column name if specified
			targetColName = targetColumns[i]
		}

		// Generate edges from source columns to target table
		for _, sourceCol := range outputCol.SourceColumns {
			resolved, err := sp.ResolveColumn(sourceCol)
			if err != nil {
				continue
			}

			// Check if source is from a CTE or subquery - trace through
			if tableRef, ok := sp.FindTable(resolved.Table); ok && (tableRef.IsCTE || tableRef.IsSubquery) {
				a.traceThroughTableLineageToTarget(tableRef, resolved.Column, targetSchema, targetTable, targetColName, outputCol.Transform)
				continue
			}

			// Regular table - add relation directly
			isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetTable)
			relation := scope.NewLineageEdge(
				resolved.Schema,
				resolved.Table,
				resolved.Column,
				targetSchema,
				targetTable,
				targetColName,
				outputCol.Transform,
				isTemp,
			)
			a.addRelation(relation)
		}
	}
}

// processCreateStatement processes CREATE statements.
func (a *Analyzer) processCreateStatement(ctx *mysql.CreateStatementContext) {
	// Handle CREATE TABLE with AS SELECT
	if ctx.CreateTable() != nil {
		if createTableCtx, ok := ctx.CreateTable().(*mysql.CreateTableContext); ok {
			a.processCreateTable(createTableCtx)
		}
	}
	// Handle CREATE VIEW
	if ctx.CreateView() != nil {
		if createViewCtx, ok := ctx.CreateView().(*mysql.CreateViewContext); ok {
			a.processCreateView(createViewCtx)
		}
	}
}

// processCreateTable processes CREATE TABLE statements.
func (a *Analyzer) processCreateTable(ctx *mysql.CreateTableContext) {
	// Get target table name
	targetTable := ""
	targetSchema := ""
	if ctx.TableName() != nil {
		if ctx.TableName().QualifiedIdentifier() != nil {
			fullText := ctx.TableName().QualifiedIdentifier().GetText()
			// Parse schema.table format
			if strings.Contains(fullText, ".") {
				parts := strings.Split(fullText, ".")
				if len(parts) >= 2 {
					targetSchema = strings.Join(parts[:len(parts)-1], ".")
					targetTable = parts[len(parts)-1]
				}
			} else {
				targetTable = fullText
			}
		}
	}

	// Check if this is CREATE TABLE AS SELECT
	if ctx.DuplicateAsQueryExpression() != nil {
		if dupAsCtx, ok := ctx.DuplicateAsQueryExpression().(*mysql.DuplicateAsQueryExpressionContext); ok {
			// Process the SELECT query
			if dupAsCtx.QueryExpressionOrParens() != nil {
				a.processQueryExpressionOrParens(dupAsCtx.QueryExpressionOrParens())
			}

			// Get the output columns from the SELECT query
			sp := a.currentScope()
			outputColumns := sp.GetOutputColumns()

			// Generate edges from source columns to target table
			for _, outputCol := range outputColumns {
				targetColName := outputCol.Alias

				// Generate edges from source columns to target table
				for _, sourceCol := range outputCol.SourceColumns {
					resolved, err := sp.ResolveColumn(sourceCol)
					if err != nil {
						continue
					}

					// Check if source is from a CTE or subquery - trace through
					if tableRef, ok := sp.FindTable(resolved.Table); ok && (tableRef.IsCTE || tableRef.IsSubquery) {
						// Trace through the lineage
						a.traceThroughTableLineageToTarget(tableRef, resolved.Column, targetSchema, targetTable, targetColName, outputCol.Transform)
						continue
					}

					// Regular table - add edge directly
					isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetTable)
					relation := scope.NewLineageEdge(
						resolved.Schema, resolved.Table, resolved.Column,
						targetSchema, targetTable, targetColName,
						outputCol.Transform,
						isTemp,
					)
					a.addRelation(relation)
				}
			}
		}
	}
}

// processCreateView processes CREATE VIEW statements.
func (a *Analyzer) processCreateView(ctx *mysql.CreateViewContext) {
	// Get target view name
	targetView := ""
	targetSchema := ""
	if ctx.ViewName() != nil {
		if ctx.ViewName().QualifiedIdentifier() != nil {
			fullText := ctx.ViewName().QualifiedIdentifier().GetText()
			// Parse schema.view format
			if strings.Contains(fullText, ".") {
				parts := strings.Split(fullText, ".")
				if len(parts) >= 2 {
					targetSchema = strings.Join(parts[:len(parts)-1], ".")
					targetView = parts[len(parts)-1]
				}
			} else {
				targetView = fullText
			}
		}
	}

	// Check if this is a view with AS SELECT
	if ctx.ViewTail() != nil {
		if viewTailCtx, ok := ctx.ViewTail().(*mysql.ViewTailContext); ok {
			// Extract explicit column names if present
			var explicitColumnNames []string
			if viewTailCtx.ColumnInternalRefList() != nil {
				if colListCtx, ok := viewTailCtx.ColumnInternalRefList().(*mysql.ColumnInternalRefListContext); ok {
					colRefs := colListCtx.AllColumnInternalRef()
					for _, colRef := range colRefs {
						if colRefCtx, ok := colRef.(*mysql.ColumnInternalRefContext); ok {
							if colRefCtx.Identifier() != nil {
								explicitColumnNames = append(explicitColumnNames, colRefCtx.Identifier().GetText())
							}
						}
					}
				}
			}

			if viewTailCtx.ViewSelect() != nil {
				if viewSelectCtx, ok := viewTailCtx.ViewSelect().(*mysql.ViewSelectContext); ok {
					// Process the SELECT query
					if viewSelectCtx.QueryExpressionOrParens() != nil {
						a.processQueryExpressionOrParens(viewSelectCtx.QueryExpressionOrParens())
					}

					// Get the output columns from the SELECT query
					sp := a.currentScope()
					outputColumns := sp.GetOutputColumns()

					// Generate edges from source columns to target view
					for i, outputCol := range outputColumns {
						// Use explicit column name if provided, otherwise use SELECT column alias
						targetColName := outputCol.Alias
						if i < len(explicitColumnNames) {
							targetColName = explicitColumnNames[i]
						}

						// Generate edges from source columns to target view
						for _, sourceCol := range outputCol.SourceColumns {
							resolved, err := sp.ResolveColumn(sourceCol)
							if err != nil {
								continue
							}

							// Check if source is from a CTE or subquery - trace through
							if tableRef, ok := sp.FindTable(resolved.Table); ok && (tableRef.IsCTE || tableRef.IsSubquery) {
								// Trace through the lineage
								a.traceThroughTableLineageToTarget(tableRef, resolved.Column, targetSchema, targetView, targetColName, outputCol.Transform)
								continue
							}

							// Regular table - add edge directly
							isTemp := targetView == resultTableName || a.isTableTempInCurrentScope(targetView)
							relation := scope.NewLineageEdge(
								resolved.Schema, resolved.Table, resolved.Column,
								targetSchema, targetView, targetColName,
								outputCol.Transform,
								isTemp,
							)
							a.addRelation(relation)
						}
					}
				}
			}
		}
	}
}

// processUpdateStatement processes UPDATE statements.
func (a *Analyzer) processUpdateStatement(ctx *mysql.UpdateStatementContext) {
	// Process WITH clause if present
	if ctx.WithClause() != nil {
		if withCtx, ok := ctx.WithClause().(*mysql.WithClauseContext); ok {
			a.processWithClause(withCtx)
		}
	}

	// Process table references (for JOINs in UPDATE)
	if ctx.TableReferenceList() != nil {
		if tableRefListCtx, ok := ctx.TableReferenceList().(*mysql.TableReferenceListContext); ok {
			a.processTableReferenceList(tableRefListCtx)
		}
	}

	// Process WHERE clause if present (to track columns used in filtering)
	if ctx.WhereClause() != nil {
		if whereCtx, ok := ctx.WhereClause().(*mysql.WhereClauseContext); ok {
			// Extract columns from WHERE conditions
			if whereCtx.Expr() != nil {
				a.extractColumnsFromExpression(whereCtx.Expr())
			}
		}
	}

	// Process UPDATE list (SET clause)
	if ctx.UpdateList() != nil {
		if updateListCtx, ok := ctx.UpdateList().(*mysql.UpdateListContext); ok {
			a.processUpdateList(updateListCtx)
		}
	}
}

// processUpdateList processes the SET clause in UPDATE statements.
func (a *Analyzer) processUpdateList(ctx *mysql.UpdateListContext) {
	// Get all update elements (column = expression pairs)
	allUpdateElements := ctx.AllUpdateElement()
	sp := a.currentScope()

	for _, updateElemCtx := range allUpdateElements {
		if updateElem, ok := updateElemCtx.(*mysql.UpdateElementContext); ok {
			// Get target column
			targetCol := scope.ColumnRef{}
			if updateElem.ColumnRef() != nil {
				if colRefCtx, ok := updateElem.ColumnRef().(*mysql.ColumnRefContext); ok {
					targetCol = a.extractColumnRef(colRefCtx)
				}
			}

			// Resolve target column to get actual table
			resolved, err := sp.ResolveColumn(targetCol)
			if err != nil {
				continue
			}

			// Get source columns from the expression
			var sourceColumns []scope.ColumnRef
			var isDerived bool
			var transformInfo []model.Transformation

			if updateElem.Expr() != nil {
				sourceColumns = a.extractColumnsFromExpression(updateElem.Expr())
				exprText := normalizeExpressionText(updateElem.Expr().GetText())

				// Check if this is a derived expression (not just a simple column reference)
				isDerived = len(sourceColumns) != 1 || exprText != targetCol.Column

				if isDerived && exprText != "" {
					// Use enhanced operator analysis for UPDATE expressions
					transformInfo = a.analyzeExpressionOperator(updateElem.Expr())
				}
			}

			// Special case: if expression doesn't reference columns (e.g., DEFAULT, literal)
			// we still want to show that the column is being updated
			if len(sourceColumns) == 0 {
				// Create a self-reference or mark as derived from literals
				sourceColumns = []scope.ColumnRef{{
					Schema: resolved.Schema,
					Table:  resolved.Table,
					Column: wildcardColumn, // Indicate this is a synthetic/literal update
				}}
				if updateElem.Expr() != nil {
					transformInfo = a.analyzeExpressionOperator(updateElem.Expr())
				}
			}

			// Generate edges from source columns to target column
			for _, sourceCol := range sourceColumns {
				resolvedSource, err := sp.ResolveColumn(sourceCol)
				if err != nil {
					// If we can't resolve, might be from outer scope or literal
					// Use the column reference as-is
					resolvedSource = &sourceCol
				}

				isTemp := resolved.Table == resultTableName || a.isTableTempInCurrentScope(resolved.Table)
				relation := scope.NewLineageEdge(
					resolvedSource.Schema, resolvedSource.Table, resolvedSource.Column,
					resolved.Schema, resolved.Table, resolved.Column,
					transformInfo,
					isTemp,
				)
				a.addRelation(relation)
			}
		}
	}
}

// processInsertUpdateList processes the ON DUPLICATE KEY UPDATE clause in INSERT statements.
// This handles UPDATE operations that occur when a duplicate key is detected during INSERT.
func (a *Analyzer) processInsertUpdateList(ctx *mysql.InsertUpdateListContext, targetSchema, targetTable string) {
	// Get the update list (similar to UPDATE statement)
	if ctx.UpdateList() == nil {
		return
	}

	updateListCtx, ok := ctx.UpdateList().(*mysql.UpdateListContext)
	if !ok {
		return
	}

	// Get all update elements (column = expression pairs)
	allUpdateElements := updateListCtx.AllUpdateElement()
	sp := a.currentScope()

	for _, updateElemCtx := range allUpdateElements {
		if updateElem, ok := updateElemCtx.(*mysql.UpdateElementContext); ok {
			// Get target column
			targetCol := scope.ColumnRef{}
			if updateElem.ColumnRef() != nil {
				if colRefCtx, ok := updateElem.ColumnRef().(*mysql.ColumnRefContext); ok {
					targetCol = a.extractColumnRef(colRefCtx)
				}
			}

			// For ON DUPLICATE KEY UPDATE, target is always the insert table
			// Resolve using table context if column is unqualified
			if targetCol.Table == "" {
				targetCol.Table = targetTable
			}

			// Get source columns from the expression
			var sourceColumns []scope.ColumnRef
			var transformInfo []model.Transformation

			if updateElem.Expr() != nil {
				sourceColumns = a.extractColumnsFromExpression(updateElem.Expr())
				exprText := normalizeExpressionText(updateElem.Expr().GetText())

				// Check if expression uses VALUES() function
				// VALUES(col_name) refers to the value that would have been inserted
				if strings.Contains(strings.ToUpper(exprText), "VALUES(") {
					// For VALUES() references, create lineage from INSERT columns
					// This is a special case where the source is the INSERT value, not existing row
					transformInfo = a.analyzeExpressionOperator(updateElem.Expr())
				} else if len(sourceColumns) > 0 {
					// Regular expression referencing other columns
					transformInfo = a.analyzeExpressionOperator(updateElem.Expr())
				}
			}

			// Special case: if expression doesn't reference columns (e.g., literal, NOW())
			// we still want to show that the column is being updated
			if len(sourceColumns) == 0 {
				// For literals/functions without column references
				sourceColumns = []scope.ColumnRef{{
					Schema: targetSchema,
					Table:  targetTable,
					Column: wildcardColumn, // Indicate this is a synthetic/literal update
				}}
				if updateElem.Expr() != nil {
					transformInfo = a.analyzeExpressionOperator(updateElem.Expr())
				}
			}

			// Generate edges from source columns to target column
			for _, sourceCol := range sourceColumns {
				resolvedSource := &sourceCol

				// Try to resolve the source column through scope
				if sourceCol.Table == "" || sourceCol.Column != wildcardColumn {
					if resolved, err := sp.ResolveColumn(sourceCol); err == nil {
						resolvedSource = resolved
					}
				}

				isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetTable)
				relation := scope.NewLineageEdge(
					resolvedSource.Schema, resolvedSource.Table, resolvedSource.Column,
					targetSchema, targetTable, targetCol.Column,
					transformInfo,
					isTemp,
				)
				a.addRelation(relation)
			}
		}
	}
}

// processDeleteStatement processes DELETE statements.
func (a *Analyzer) processDeleteStatement(ctx *mysql.DeleteStatementContext) {
	// Process WITH clause if present
	if ctx.WithClause() != nil {
		if withCtx, ok := ctx.WithClause().(*mysql.WithClauseContext); ok {
			a.processWithClause(withCtx)
		}
	}

	// Get target table(s) from DELETE statement
	var targetTables []scope.TableRef

	// Single table delete: DELETE FROM table WHERE ...
	if ctx.TableRef() != nil {
		tableName := a.getTableName(ctx.TableRef())
		schemaName := a.getSchemaName(ctx.TableRef())
		alias := ""
		if ctx.TableAlias() != nil {
			// Extract just the identifier, not the AS keyword
			if ctx.TableAlias().Identifier() != nil {
				alias = ctx.TableAlias().Identifier().GetText()
			}
		}
		targetTables = append(targetTables, scope.TableRef{
			Schema: schemaName,
			Table:  tableName,
			Alias:  alias,
		})
	}

	// Multi-table delete: DELETE t1, t2 FROM t1 JOIN t2 WHERE ...
	if ctx.TableAliasRefList() != nil {
		if aliasRefListCtx, ok := ctx.TableAliasRefList().(*mysql.TableAliasRefListContext); ok {
			// Extract table references that are being deleted
			for _, refCtx := range aliasRefListCtx.AllTableRefWithWildcard() {
				if tableRefCtx, ok := refCtx.(*mysql.TableRefWithWildcardContext); ok {
					if tableRefCtx.Identifier() != nil {
						// This is the alias or table name being deleted
						tableName := tableRefCtx.Identifier().GetText()
						targetTables = append(targetTables, scope.TableRef{
							Table: tableName,
							Alias: tableName, // For multi-table, this is typically an alias
						})
					}
				}
			}
		}
	}

	// Process table references (for JOINs in DELETE)
	if ctx.TableReferenceList() != nil {
		if tableRefListCtx, ok := ctx.TableReferenceList().(*mysql.TableReferenceListContext); ok {
			a.processTableReferenceList(tableRefListCtx)
		}
	}

	// If we still don't have target tables but have table references in scope,
	// use the first table as a fallback for single-table deletes without explicit target
	if len(targetTables) == 0 {
		scope := a.currentScope()
		tables := scope.GetTables()
		if len(tables) > 0 {
			for _, tableRef := range tables {
				targetTables = append(targetTables, *tableRef)
				break // Just use the first one
			}
		}
	}

	// Process WHERE clause - this is the key for DELETE lineage
	// Shows which columns influence which rows get deleted
	if ctx.WhereClause() != nil {
		if whereCtx, ok := ctx.WhereClause().(*mysql.WhereClauseContext); ok {
			if whereCtx.Expr() != nil {
				// Extract columns from WHERE conditions
				conditionColumns := a.extractColumnsFromExpression(whereCtx.Expr())
				sp := a.currentScope()

				// Generate edges showing these columns influence the deletion
				for _, targetTable := range targetTables {
					// For multi-table DELETE, resolve the target table alias to actual table
					actualTargetTable := targetTable
					if targetTable.Alias != "" {
						// Look up the alias in scope to get the actual table name
						if foundTable, ok := sp.FindTable(targetTable.Alias); ok {
							actualTargetTable = *foundTable
						}
					}

					for _, condCol := range conditionColumns {
						resolved, err := sp.ResolveColumn(condCol)
						if err != nil {
							// Use unresolved column
							resolved = &condCol
						}

						transform := []model.Transformation{
							model.NewDeleteTransformation(normalizeExpressionText(whereCtx.Expr().GetText())),
						}

						// If condition column comes from temp table, trace through its lineage
						if tableRef, ok := sp.FindTable(resolved.Table); ok && (tableRef.IsCTE || tableRef.IsSubquery) {
							a.traceThroughTableLineageToTarget(tableRef, resolved.Column, actualTargetTable.Schema, actualTargetTable.Table, deletionFieldName, transform)
							continue
						}

						// If it's a CTE reference, build a temporary TableRef from the CTE definition
						if cte, ok := sp.FindCTE(resolved.Table); ok {
							tempRef := &scope.TableRef{
								Schema:     "",
								Table:      cte.Name,
								Alias:      cte.Name,
								IsSubquery: false,
								IsCTE:      true,
								Columns:    cte.Columns,
								Lineage:    cte.Lineage,
							}
							a.traceThroughTableLineageToTarget(tempRef, resolved.Column, actualTargetTable.Schema, actualTargetTable.Table, deletionFieldName, transform)
							continue
						}

						// Create edge showing this column influences deletion of target table
						isTemp := actualTargetTable.Table == resultTableName || a.isTableTempInCurrentScope(actualTargetTable.Table)
						relation := scope.NewLineageEdge(
							resolved.Schema, resolved.Table, resolved.Column,
							actualTargetTable.Schema, actualTargetTable.Table, deletionFieldName,
							transform,
							isTemp,
						)
						a.addRelation(relation)
					}
				}
			}
		}
	}
}

// processLoadStatement processes LOAD DATA INFILE statements.
func (a *Analyzer) processLoadStatement(ctx *mysql.LoadStatementContext) {
	// Get target table
	targetTable := ""
	targetSchema := ""
	if ctx.TableRef() != nil {
		targetTable = a.getTableName(ctx.TableRef())
		targetSchema = a.getSchemaName(ctx.TableRef())
	}

	// Get source file path (optional, for documentation)
	sourceFile := fileSourceMarker
	if ctx.TextLiteral() != nil {
		// Could extract filename, but we use generic marker for now
		sourceFile = fileSourceMarker
	}

	// Get column list from loadDataFileTargetList (columns to load into)
	var targetColumns []string
	if ctx.LoadDataFileTail() != nil {
		if tailCtx, ok := ctx.LoadDataFileTail().(*mysql.LoadDataFileTailContext); ok {
			if tailCtx.LoadDataFileTargetList() != nil {
				if targetListCtx, ok := tailCtx.LoadDataFileTargetList().(*mysql.LoadDataFileTargetListContext); ok {
					targetColumns = a.extractLoadDataTargetColumns(targetListCtx)
				}
			}

			// Process SET clause if present (computed columns)
			if tailCtx.UpdateList() != nil {
				if updateListCtx, ok := tailCtx.UpdateList().(*mysql.UpdateListContext); ok {
					a.processLoadDataSetClause(updateListCtx, targetSchema, targetTable, targetColumns, sourceFile)
				}
			}
		}
	}

	// Generate lineage edges from file to target columns
	// If no explicit column list, all table columns are assumed to be loaded
	if len(targetColumns) == 0 {
		// No explicit columns - would need catalog to determine all columns
		// For now, we create a generic edge
		isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetTable)
		relation := scope.NewLineageEdge(
			"", sourceFile, wildcardColumn,
			targetSchema, targetTable, wildcardColumn,
			nil,
			isTemp,
		)
		a.addRelation(relation)
	} else {
		// Create edge for each explicitly listed column
		for i, colName := range targetColumns {
			isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetTable)
			relation := scope.NewLineageEdge(
				"", sourceFile, fmt.Sprintf("col%d", i+1),
				targetSchema, targetTable, colName,
				nil,
				isTemp,
			)
			a.addRelation(relation)
		}
	}
}

// extractLoadDataTargetColumns extracts column names from LOAD DATA target list.
func (a *Analyzer) extractLoadDataTargetColumns(ctx *mysql.LoadDataFileTargetListContext) []string {
	var columns []string

	if ctx.FieldOrVariableList() == nil {
		return columns
	}

	fieldOrVarListCtx, ok := ctx.FieldOrVariableList().(*mysql.FieldOrVariableListContext)
	if !ok {
		return columns
	}

	// Extract column references (skip user variables)
	for _, colRefCtx := range fieldOrVarListCtx.AllColumnRef() {
		if colRef, ok := colRefCtx.(*mysql.ColumnRefContext); ok {
			col := a.extractColumnRef(colRef)
			columns = append(columns, col.Column)
		}
	}

	return columns
}

// processLoadDataSetClause processes the SET clause in LOAD DATA statements.
// This handles computed columns like: SET col3 = col1 + col2.
func (a *Analyzer) processLoadDataSetClause(ctx *mysql.UpdateListContext, targetSchema, targetTable string, loadedColumns []string, sourceFile string) {
	allUpdateElements := ctx.AllUpdateElement()
	sp := a.currentScope()

	for _, updateElemCtx := range allUpdateElements {
		if updateElem, ok := updateElemCtx.(*mysql.UpdateElementContext); ok {
			// Get target column
			targetCol := scope.ColumnRef{}
			if updateElem.ColumnRef() != nil {
				if colRefCtx, ok := updateElem.ColumnRef().(*mysql.ColumnRefContext); ok {
					targetCol = a.extractColumnRef(colRefCtx)
				}
			}

			// Get source columns from the expression
			var sourceColumns []scope.ColumnRef
			var transformInfo []model.Transformation

			if updateElem.Expr() != nil {
				sourceColumns = a.extractColumnsFromExpression(updateElem.Expr())
				transformInfo = a.analyzeExpressionOperator(updateElem.Expr())
			}

			// Special case: if expression doesn't reference columns (e.g., literal, NOW())
			if len(sourceColumns) == 0 {
				sourceColumns = []scope.ColumnRef{{
					Schema: "",
					Table:  sourceFile,
					Column: wildcardColumn,
				}}
			}

			// Generate edges from source columns to target column
			for _, sourceCol := range sourceColumns {
				// Check if this source column is in the loaded columns list
				// If so, it comes from the file, not an existing table column
				isFromFile := false
				for _, loadedCol := range loadedColumns {
					if sourceCol.Column == loadedCol {
						isFromFile = true
						break
					}
				}

				var fromSchema, fromTable, fromField string
				if isFromFile {
					// Column comes from the file
					fromSchema = ""
					fromTable = sourceFile
					fromField = sourceCol.Column
				} else {
					// Column might be from the table itself or another source
					// Try to resolve it
					resolved, err := sp.ResolveColumn(sourceCol)
					if err != nil {
						// If we can't resolve, assume it's from the file
						fromSchema = ""
						fromTable = sourceFile
						fromField = sourceCol.Column
					} else {
						fromSchema = resolved.Schema
						fromTable = resolved.Table
						fromField = resolved.Column
					}
				}

				isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetTable)
				relation := scope.NewLineageEdge(
					fromSchema, fromTable, fromField,
					targetSchema, targetTable, targetCol.Column,
					transformInfo,
					isTemp,
				)
				a.addRelation(relation)
			}
		}
	}
}

// processQueryExpressionOrParens processes a query expression or parenthesized query.
func (a *Analyzer) processQueryExpressionOrParens(ctx mysql.IQueryExpressionOrParensContext) {
	if ctx.QueryExpression() != nil {
		if queryExprCtx, ok := ctx.QueryExpression().(*mysql.QueryExpressionContext); ok {
			a.processQueryExpression(queryExprCtx)
		}
	} else if ctx.QueryExpressionParens() != nil {
		a.processQueryExpressionParens(ctx.QueryExpressionParens())
	}
}

// traceThroughTableLineageToTarget traces lineage through a table (CTE or subquery) to a specified target.
func (a *Analyzer) traceThroughTableLineageToTarget(tableRef *scope.TableRef, columnName string, targetSchema string, targetTable string, targetColumn string, transform []model.Transformation) {
	// Find edges in table lineage that produce the requested column
	for _, edge := range tableRef.Lineage {
		if columnName != wildcardColumn && edge.Target.Name != columnName {
			continue
		}

		actualTargetColumn := targetColumn
		if columnName == wildcardColumn && targetColumn == wildcardColumn {
			actualTargetColumn = edge.Target.Name
		}

		// Combine transformations if both exist
		combinedTransform := combineTransformations(edge.Transformation, transform)

		// Determine if target is temporary
		isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetTable)

		// Create relation from original source to target table
		resultRelation := scope.NewLineageEdge(
			edge.Source.Table.Schema,
			edge.Source.Table.Name,
			edge.Source.Name,
			targetSchema,
			targetTable,
			actualTargetColumn,
			combinedTransform,
			isTemp,
		)

		a.addRelation(resultRelation)
	}
}

// appendFlattenedLineage traces through nested temporary tables to build lineage edges that originate from real tables.
func (a *Analyzer) appendFlattenedLineage(lineage *[]model.ColumnRelation, sp *scope.Scope, tableRef *scope.TableRef, columnName string, targetTable string, targetColumn string, transform []model.Transformation) {
	for _, edge := range tableRef.Lineage {
		if columnName != wildcardColumn && edge.Target.Name != columnName {
			continue
		}

		actualTarget := targetColumn
		if columnName == wildcardColumn && targetColumn == wildcardColumn {
			actualTarget = edge.Target.Name
		}

		combinedTransform := combineTransformations(edge.Transformation, transform)
		sourceTableName := edge.Source.Table.Name

		// If the source is another temp table, recurse to flatten to real tables
		if nestedRef, ok := sp.FindTable(sourceTableName); ok && (nestedRef.IsCTE || nestedRef.IsSubquery) {
			a.appendFlattenedLineage(lineage, sp, nestedRef, edge.Source.Name, targetTable, actualTarget, combinedTransform)
			continue
		}

		*lineage = append(*lineage, scope.NewLineageEdge(
			edge.Source.Table.Schema,
			sourceTableName,
			edge.Source.Name,
			"",
			targetTable,
			actualTarget,
			combinedTransform,
			true,
		))
	}
}

// flattenTempSourceLineage resolves a column that originates from a temporary table (CTE or subquery)
// into base table lineage and appends it to the provided lineage slice. Returns true if handled.
func (a *Analyzer) flattenTempSourceLineage(sp *scope.Scope, resolved *scope.ColumnRef, targetTable string, targetColumn string, transform []model.Transformation, lineage *[]model.ColumnRelation) bool {
	if resolved == nil {
		return false
	}

	if tableRef, ok := sp.FindTable(resolved.Table); ok && (tableRef.IsSubquery || tableRef.IsCTE) {
		a.appendFlattenedLineage(lineage, sp, tableRef, resolved.Column, targetTable, targetColumn, transform)
		return true
	}

	if cte, ok := sp.FindCTE(resolved.Table); ok {
		tempRef := &scope.TableRef{
			Schema:     "",
			Table:      cte.Name,
			Alias:      cte.Name,
			IsSubquery: false,
			IsCTE:      true,
			Columns:    cte.Columns,
			Lineage:    cte.Lineage,
		}
		a.appendFlattenedLineage(lineage, sp, tempRef, resolved.Column, targetTable, targetColumn, transform)
		return true
	}

	return false
}

// extractFieldsFromInsert extracts field names from INSERT column list.
func (a *Analyzer) extractFieldsFromInsert(ctx mysql.IFieldsContext) []string {
	fields := make([]string, 0)
	if ctx.AllInsertIdentifier() != nil {
		for _, field := range ctx.AllInsertIdentifier() {
			// InsertIdentifier contains either ColumnRef or TableWild
			if field.ColumnRef() != nil {
				if colRefCtx, ok := field.ColumnRef().(*mysql.ColumnRefContext); ok {
					colRef := a.extractColumnRef(colRefCtx)
					if colRef.Column != "" {
						fields = append(fields, colRef.Column)
					}
				}
			}
		}
	}
	return fields
}

// processQueryExpression processes query expressions.
func (a *Analyzer) processQueryExpression(ctx *mysql.QueryExpressionContext) {
	// Handle WITH clause (CTEs) if present
	if ctx.WithClause() != nil {
		if withCtx, ok := ctx.WithClause().(*mysql.WithClauseContext); ok {
			a.processWithClause(withCtx)
		}
	}

	// Handle query body
	if ctx.QueryExpressionBody() != nil {
		if bodyCtx, ok := ctx.QueryExpressionBody().(*mysql.QueryExpressionBodyContext); ok {
			a.processQueryExpressionBody(bodyCtx)
		}
	}
}

// processWithClause processes WITH (CTE) clauses.
func (a *Analyzer) processWithClause(ctx *mysql.WithClauseContext) {
	for _, cteCtx := range ctx.AllCommonTableExpression() {
		a.processCTE(cteCtx)
	}
}

// processCTE processes a single CTE.
func (a *Analyzer) processCTE(ctx mysql.ICommonTableExpressionContext) {
	// Get CTE name
	cteName := ""
	if ctx.Identifier() != nil {
		cteName = ctx.Identifier().GetText()
	}

	// Record CTE name as temporary to filter intermediate edges
	a.markTempTable(cteName)

	// Get column list if specified
	var columns []string
	if ctx.ColumnInternalRefList() != nil {
		columns = a.extractColumnNames(ctx.ColumnInternalRefList())
	}

	// Process the CTE's subquery in a new scope
	var lineage []model.ColumnRelation
	if ctx.Subquery() != nil {
		a.pushScope()
		a.processSubquery(ctx.Subquery())
		cteScope := a.popScope()

		// Generate lineage from the CTE's scope
		for _, outputCol := range cteScope.GetOutputColumns() {
			for _, sourceCol := range outputCol.SourceColumns {
				// Resolve the source column
				resolved, err := cteScope.ResolveColumn(sourceCol)
				if err != nil {
					continue
				}

				// If the source is a temp table (CTE/subquery), flatten through its lineage
				if a.flattenTempSourceLineage(cteScope, resolved, cteName, outputCol.Alias, outputCol.Transform, &lineage) {
					continue
				}

				// Create lineage edge using NewLineageEdge
				edge := scope.NewLineageEdge(
					resolved.Schema,
					resolved.Table,
					resolved.Column,
					"",
					cteName,
					outputCol.Alias,
					outputCol.Transform,
					true, // CTE is temporary
				)

				lineage = append(lineage, edge)
			}
		}
	}

	// Create CTE definition with lineage
	cte := &scope.CTEDefinition{
		Name:          cteName,
		Columns:       columns,
		DefiningScope: a.currentScope(),
		Lineage:       lineage,
	}

	// Add CTE to current scope
	a.currentScope().AddCTE(cte)
}

// processQueryExpressionBody processes the query body.
func (a *Analyzer) processQueryExpressionBody(ctx *mysql.QueryExpressionBodyContext) {
	allPrimary := ctx.AllQueryPrimary()

	if len(allPrimary) == 1 {
		// Simple case: single query without UNION
		if primaryCtx, ok := allPrimary[0].(*mysql.QueryPrimaryContext); ok {
			a.processQueryPrimary(primaryCtx)
		}
	} else if len(allPrimary) > 1 {
		// UNION/INTERSECT/EXCEPT: Process all queries and merge their output columns
		a.processUnionQueries(allPrimary)
	}
}

// processUnionQueries handles UNION/INTERSECT/EXCEPT operations by merging outputs from multiple queries.
func (a *Analyzer) processUnionQueries(allPrimary []mysql.IQueryPrimaryContext) {
	// Each query in a UNION must have the same number of columns with compatible types
	// The column names come from the first query

	baseScope := a.currentScope()
	var allOutputColumns [][]scope.OutputColumn

	for i, primary := range allPrimary {
		if primaryCtx, ok := primary.(*mysql.QueryPrimaryContext); ok {
			if i == 0 {
				// First query - process normally to get column names
				a.processQueryPrimary(primaryCtx)
				allOutputColumns = append(allOutputColumns, baseScope.GetOutputColumns())
			} else {
				// Subsequent queries - process in a temporary scope to capture their lineage
				tempScope := scope.NewScope(baseScope.Parent())
				// Copy CTEs from base scope (CTEs should be available)
				for _, cte := range baseScope.GetCTEs() {
					tempScope.AddCTE(cte)
				}

				// Temporarily replace current scope
				originalScope := a.currentScope()
				a.scopeStack[len(a.scopeStack)-1] = tempScope

				// Process this query
				a.processQueryPrimary(primaryCtx)

				// Restore original scope
				a.scopeStack[len(a.scopeStack)-1] = originalScope

				// Collect output columns from temp scope
				allOutputColumns = append(allOutputColumns, tempScope.GetOutputColumns())
			}
		}
	}

	// Merge outputs: For each position in the result, collect sources from all queries
	a.mergeUnionOutputColumns(baseScope, allOutputColumns)
}

// mergeUnionOutputColumns merges output columns from multiple queries in a UNION.
func (*Analyzer) mergeUnionOutputColumns(baseScope *scope.Scope, allOutputColumns [][]scope.OutputColumn) {
	if len(allOutputColumns) == 0 || len(allOutputColumns[0]) == 0 {
		return
	}

	firstQueryOutputs := allOutputColumns[0]

	// For UNION, we need to merge columns by position
	for colIdx := 0; colIdx < len(firstQueryOutputs); colIdx++ {
		firstCol := firstQueryOutputs[colIdx]

		// Collect source columns from this position across all queries
		var mergedSources []scope.ColumnRef
		var hasDerivedTransform bool

		for queryIdx := 0; queryIdx < len(allOutputColumns); queryIdx++ {
			if colIdx < len(allOutputColumns[queryIdx]) {
				queryCol := allOutputColumns[queryIdx][colIdx]
				mergedSources = append(mergedSources, queryCol.SourceColumns...)
				if queryCol.IsDerived {
					hasDerivedTransform = true
				}
			}
		}

		// Update the first query's output column with merged sources
		firstCol.SourceColumns = mergedSources
		if hasDerivedTransform && firstCol.Transform == nil {
			firstCol.Transform = []model.Transformation{model.NewUnionTransformation()}
		}
		baseScope.SetOutputColumn(colIdx, firstCol)
	}
}

// processQueryPrimary processes a query primary.
func (a *Analyzer) processQueryPrimary(ctx *mysql.QueryPrimaryContext) {
	if ctx.QuerySpecification() != nil {
		if querySpecCtx, ok := ctx.QuerySpecification().(*mysql.QuerySpecificationContext); ok {
			a.processQuerySpecification(querySpecCtx)
		}
	}
}

// processQuerySpecification processes the main SELECT query.
func (a *Analyzer) processQuerySpecification(ctx *mysql.QuerySpecificationContext) {
	scope := a.currentScope()

	// First, process FROM clause to populate available tables
	if ctx.FromClause() != nil {
		if fromCtx, ok := ctx.FromClause().(*mysql.FromClauseContext); ok {
			a.processFromClause(fromCtx)
		}
	}

	// Then process SELECT list
	if ctx.SelectItemList() != nil {
		a.processSelectItemList(ctx.SelectItemList(), scope)
	}

	// Generate edges from sources to outputs
	a.generateEdges(scope)
}

// processFromClause processes the FROM clause.
func (a *Analyzer) processFromClause(ctx *mysql.FromClauseContext) {
	if ctx.TableReferenceList() != nil {
		if tableRefListCtx, ok := ctx.TableReferenceList().(*mysql.TableReferenceListContext); ok {
			a.processTableReferenceList(tableRefListCtx)
		}
	}
}

// processTableReferenceList processes the list of table references.
func (a *Analyzer) processTableReferenceList(ctx *mysql.TableReferenceListContext) {
	for _, tableRef := range ctx.AllTableReference() {
		if tableRefCtx, ok := tableRef.(*mysql.TableReferenceContext); ok {
			a.processTableReference(tableRefCtx)
		}
	}
}

// processTableReference processes a single table reference.
func (a *Analyzer) processTableReference(ctx *mysql.TableReferenceContext) {
	if ctx.TableFactor() != nil {
		if tableFactorCtx, ok := ctx.TableFactor().(*mysql.TableFactorContext); ok {
			a.processTableFactor(tableFactorCtx)
		}
	}
	// Handle JOINs
	for _, joinCtx := range ctx.AllJoinedTable() {
		if joinedTableCtx, ok := joinCtx.(*mysql.JoinedTableContext); ok {
			a.processJoinedTable(joinedTableCtx)
		}
	}
}

// processTableFactor processes table factors.
func (a *Analyzer) processTableFactor(ctx *mysql.TableFactorContext) {
	if ctx.SingleTable() != nil {
		a.processSingleTable(ctx.SingleTable())
	} else if ctx.SingleTableParens() != nil {
		if ctx.SingleTableParens().SingleTable() != nil {
			a.processSingleTable(ctx.SingleTableParens().SingleTable())
		}
	} else if ctx.DerivedTable() != nil {
		a.processDerivedTable(ctx.DerivedTable())
	}
}

// processSingleTable processes a single table reference.
func (a *Analyzer) processSingleTable(ctx mysql.ISingleTableContext) {
	if ctx.TableRef() == nil {
		return
	}

	tableRef := ctx.TableRef()
	tableName := a.getTableName(tableRef)
	schema := a.getSchemaName(tableRef)
	alias := a.getTableAlias(ctx)

	// Check if this is a CTE reference
	if cte, ok := a.currentScope().FindCTE(tableName); ok {
		// This is a CTE - add it as a table reference with CTE flag
		ref := &scope.TableRef{
			Schema:     "",
			Table:      tableName,
			Alias:      alias,
			IsSubquery: false,
			IsCTE:      true,
			Columns:    cte.Columns,
			Lineage:    cte.Lineage,
		}
		a.currentScope().AddTable(ref)
		return
	}

	// Regular table reference
	ref := &scope.TableRef{
		Schema:     schema,
		Table:      tableName,
		Alias:      alias,
		IsSubquery: false,
		IsCTE:      false,
		Columns:    []string{},
	}

	a.currentScope().AddTable(ref)
}

// processDerivedTable processes a derived table (subquery).
func (a *Analyzer) processDerivedTable(ctx mysql.IDerivedTableContext) {
	alias := ""
	if ctx.TableAlias() != nil {
		// Extract just the identifier, not the AS keyword
		if ctx.TableAlias().Identifier() != nil {
			alias = ctx.TableAlias().Identifier().GetText()
		}
	}

	// Record derived table alias as temporary to filter intermediate edges
	a.markTempTable(alias)

	// Create a new scope for the subquery
	a.pushScope()

	// Process the subquery
	if ctx.Subquery() != nil {
		a.processSubquery(ctx.Subquery())
	}

	// Get output columns from subquery scope
	subqueryScope := a.popScope()
	columns := make([]string, 0)
	lineage := make([]model.ColumnRelation, 0)

	for _, col := range subqueryScope.GetOutputColumns() {
		colName := col.Alias
		if colName == "" {
			colName = "column"
		}
		columns = append(columns, colName)

		// Generate lineage for this subquery
		for _, sourceCol := range col.SourceColumns {
			resolved, err := subqueryScope.ResolveColumn(sourceCol)
			if err != nil {
				continue
			}

			// If the source is a temp table (subquery/CTE), flatten through its lineage
			if a.flattenTempSourceLineage(subqueryScope, resolved, alias, colName, col.Transform, &lineage) {
				continue
			}

			// Create lineage edge using NewLineageEdge
			edge := scope.NewLineageEdge(
				resolved.Schema,
				resolved.Table,
				resolved.Column,
				"",
				alias,
				colName,
				col.Transform,
				true, // Subquery is temporary
			)

			lineage = append(lineage, edge)
		}
	}

	// Add the subquery as a table in the parent scope
	ref := &scope.TableRef{
		Schema:     "",
		Table:      alias,
		Alias:      alias,
		IsSubquery: true,
		IsCTE:      false,
		Columns:    columns,
		Lineage:    lineage,
	}

	a.currentScope().AddTable(ref)
}

// processSubquery processes a subquery.
func (a *Analyzer) processSubquery(ctx mysql.ISubqueryContext) {
	if ctx.QueryExpressionParens() != nil {
		a.processQueryExpressionParens(ctx.QueryExpressionParens())
	}
}

// processQueryExpressionParens processes parenthesized query expressions.
func (a *Analyzer) processQueryExpressionParens(ctx mysql.IQueryExpressionParensContext) {
	if ctx.QueryExpression() != nil {
		if queryExprCtx, ok := ctx.QueryExpression().(*mysql.QueryExpressionContext); ok {
			a.processQueryExpression(queryExprCtx)
		}
	} else if ctx.QueryExpressionParens() != nil {
		a.processQueryExpressionParens(ctx.QueryExpressionParens())
	}
}

// processJoinedTable processes JOIN clauses.
func (a *Analyzer) processJoinedTable(ctx *mysql.JoinedTableContext) {
	// JOINs can have either a TableFactor or a TableReference
	if ctx.TableFactor() != nil {
		if tableFactorCtx, ok := ctx.TableFactor().(*mysql.TableFactorContext); ok {
			a.processTableFactor(tableFactorCtx)
		}
	} else if ctx.TableReference() != nil {
		if tableRefCtx, ok := ctx.TableReference().(*mysql.TableReferenceContext); ok {
			a.processTableReference(tableRefCtx)
		}
	}
}

// processSelectItemList processes the SELECT item list.
func (a *Analyzer) processSelectItemList(ctx mysql.ISelectItemListContext, sp *scope.Scope) {
	if ctx == nil {
		return
	}

	selectListCtx, ok := ctx.(*mysql.SelectItemListContext)
	if !ok {
		return
	}

	// Handle SELECT *
	if selectListCtx.MULT_OPERATOR() != nil {
		tables := sp.GetTables()
		for _, tableRef := range tables {
			// Try to expand wildcard using catalog if available
			if a.catalog != nil && !tableRef.IsSubquery && !tableRef.IsCTE {
				expanded := a.expandWildcardWithCatalog(tableRef, sp)
				if expanded {
					continue // Successfully expanded
				}
			}

			// Fallback: add wildcard as-is
			outputCol := scope.OutputColumn{
				Alias:         wildcardColumn,
				Expression:    wildcardColumn,
				SourceColumns: []scope.ColumnRef{{Schema: tableRef.Schema, Table: tableRef.Table, Column: wildcardColumn}},
				IsDerived:     false,
			}
			sp.AddOutputColumn(outputCol)
		}
		return
	}

	for _, item := range selectListCtx.AllSelectItem() {
		a.processSelectItem(item, sp)
	}
}

// processSelectItem processes a single SELECT item.
func (a *Analyzer) processSelectItem(ctx mysql.ISelectItemContext, sp *scope.Scope) {
	selectItemCtx, ok := ctx.(*mysql.SelectItemContext)
	if !ok {
		return
	}

	// Handle table.* notation
	if selectItemCtx.TableWild() != nil {
		tableName := selectItemCtx.TableWild().GetText()
		tableName = strings.TrimSuffix(tableName, ".*")

		if tableRef, ok := sp.FindTable(tableName); ok {
			// Try to expand wildcard using catalog if available
			if a.catalog != nil && !tableRef.IsSubquery && !tableRef.IsCTE {
				expanded := a.expandWildcardWithCatalog(tableRef, sp)
				if expanded {
					return // Successfully expanded
				}
			}

			// Fallback: add wildcard as-is
			outputCol := scope.OutputColumn{
				Alias:         wildcardColumn,
				Expression:    tableName + "." + wildcardColumn,
				SourceColumns: []scope.ColumnRef{{Schema: tableRef.Schema, Table: tableRef.Table, Column: wildcardColumn}},
				IsDerived:     false,
			}
			sp.AddOutputColumn(outputCol)
		}
		return
	}

	// Handle regular expressions
	if selectItemCtx.Expr() != nil {
		expr := selectItemCtx.Expr()
		exprText := expr.GetText()
		alias := ""

		// Check for AS alias
		if selectItemCtx.SelectAlias() != nil {
			alias = a.getSelectAlias(selectItemCtx.SelectAlias())
		} else {
			// If no alias, try to infer from the expression
			// For simple column references like "users.id", use "id"
			// For complex expressions, use the full text
			alias = a.inferColumnAlias(exprText)
		}

		// Extract source columns from expression
		sourceColumns := a.extractColumnsFromExpression(expr)

		// Determine if this is derived
		isDerived := a.isExpressionDerived(expr)

		// Special case: If expression is derived but has no column references (e.g., COUNT(*))
		// create a synthetic reference to all tables in scope
		if isDerived && len(sourceColumns) == 0 {
			// Add references to all tables in the current scope
			tables := sp.GetTables()
			for _, tableRef := range tables {
				sourceColumns = append(sourceColumns, scope.ColumnRef{
					Schema: tableRef.Schema,
					Table:  tableRef.Table,
					Column: wildcardColumn, // Use * to indicate aggregate from entire table
				})
			}
		}

		outputCol := scope.OutputColumn{
			Alias:         alias,
			Expression:    exprText,
			SourceColumns: sourceColumns,
			IsDerived:     isDerived,
		}

		if isDerived {
			// Use enhanced operator analysis instead of simple expression text
			outputCol.Transform = a.analyzeExpressionOperator(expr)
		}

		sp.AddOutputColumn(outputCol)
	}
}

// generateEdges creates ColumnRelation objects from the scope's output columns.
func (a *Analyzer) generateEdges(sp *scope.Scope) {
	// Skip generating edges to __result__ if we're in INSERT/REPLACE context
	// The INSERT/REPLACE handler will create edges directly to the target table
	if a.inInsertReplaceContext {
		return
	}
	// Only the root query should emit edges to the final result; subqueries/CTEs
	// should rely on their lineage being traced when referenced by the parent.
	if sp == nil || sp.Parent() != nil {
		return
	}

	for _, outputCol := range sp.GetOutputColumns() {
		for _, sourceCol := range outputCol.SourceColumns {
			// Resolve the source column
			resolved, err := sp.ResolveColumn(sourceCol)
			if err != nil {
				continue
			}

			// Check if this is from a table marked as CTE
			if tableRef, ok := sp.FindTable(resolved.Table); ok && tableRef.IsCTE {
				// Trace through CTE lineage
				a.traceThroughTableLineage(tableRef, resolved.Column, outputCol.Alias, outputCol.Transform)
				continue
			}

			// Check if this is from a subquery - if so, trace through its lineage
			if tableRef, ok := sp.FindTable(resolved.Table); ok && tableRef.IsSubquery {
				// Trace through subquery lineage
				a.traceThroughTableLineage(tableRef, resolved.Column, outputCol.Alias, outputCol.Transform)
				continue
			}

			// Regular table - add edge directly
			// Result table is always temporary
			isTemp := true
			relation := scope.NewLineageEdge(
				resolved.Schema, resolved.Table, resolved.Column,
				"", resultTableName, outputCol.Alias,
				outputCol.Transform,
				isTemp,
			)
			a.addRelation(relation)
		}
	}
}

// traceThroughTableLineage traces lineage through a table (CTE or subquery) to the original source tables.
func (a *Analyzer) traceThroughTableLineage(tableRef *scope.TableRef, columnName string, outputAlias string, transform []model.Transformation) {
	// Skip generating edges to __result__ if we're in INSERT/REPLACE context
	// The INSERT/REPLACE handler will create edges directly to the target table
	if a.inInsertReplaceContext {
		return
	}

	// Find edges in table lineage that produce the requested column
	for _, edge := range tableRef.Lineage {
		if columnName != wildcardColumn && edge.Target.Name != columnName {
			continue
		}

		actualOutput := outputAlias
		if columnName == wildcardColumn && outputAlias == wildcardColumn {
			actualOutput = edge.Target.Name
		}

		// Combine transformations if both exist
		combinedTransform := combineTransformations(edge.Transformation, transform)

		// Create relation from original source to final output
		resultRelation := scope.NewLineageEdge(
			edge.Source.Table.Schema,
			edge.Source.Table.Name,
			edge.Source.Name,
			"",
			resultTableName,
			actualOutput,
			combinedTransform,
			true, // __result__ is always temporary
		)

		a.addRelation(resultRelation)
	}
}

// Helper methods

// combineTransformations merges two transformation chains.
// The base transform is applied first, followed by the additional transform.
// This function creates a new slice to avoid modifying the original slices.
func combineTransformations(base, additional []model.Transformation) []model.Transformation {
	if len(base) == 0 {
		return additional
	}
	if len(additional) == 0 {
		return base
	}
	// Create a new slice with exact capacity to avoid modifying base
	combined := make([]model.Transformation, len(base)+len(additional))
	copy(combined, base)
	copy(combined[len(base):], additional)
	return combined
}

// Helper functions to create operator-specific transformation info

// createFunctionOperatorInfo creates transformation info for function calls
func createFunctionOperatorInfo(functionName string, exprText string, args []string) model.Transformation {
	return model.NewFunctionTransformation(functionName, exprText, args)
}

// createAggregateOperatorInfo creates transformation info for aggregate operations
func createAggregateOperatorInfo(functionName string, exprText string, groupKeys []string) model.Transformation {
	return model.NewAggregateTransformation(functionName, exprText, groupKeys)
}

// createOperatorExprInfo creates transformation info for operator expressions (arithmetic, comparison, etc.)
func createOperatorExprInfo(opType string, exprText string) model.Transformation {
	return model.NewOperatorTransformation(opType, exprText)
}

// createCaseOperatorInfo creates transformation info for CASE expressions
func createCaseOperatorInfo(exprText string) model.Transformation {
	return model.NewCaseTransformation(exprText)
}

// createWindowOperatorInfo creates transformation info for window functions
func createWindowOperatorInfo(functionName string, exprText string, partitionBy []string, orderBy []string) model.Transformation {
	return model.NewWindowTransformation(functionName, exprText, partitionBy, orderBy)
}

// normalizeExpressionText removes whitespace from expression text for consistency.
func normalizeExpressionText(text string) string {
	return strings.ReplaceAll(text, " ", "")
}

// parseQualifiedIdentifier splits a qualified identifier into parts (e.g., "schema.table.column").
// ignore schema because mysql does not have it.
func parseQualifiedIdentifier(fullText string) (table, name string) {
	if !strings.Contains(fullText, ".") {
		return "", fullText
	}

	parts := strings.Split(fullText, ".")
	name = parts[len(parts)-1]

	if len(parts) == 2 {
		table = parts[0]
	} else if len(parts) >= 3 {
		table = parts[len(parts)-2]
	}

	return table, name
}

// extractColumnsFromExpression extracts column references from an expression.
func (a *Analyzer) extractColumnsFromExpression(expr mysql.IExprContext) []scope.ColumnRef {
	columns := make([]scope.ColumnRef, 0)
	a.visitExprForColumns(expr, &columns)
	return columns
}

// visitExprForColumns recursively visits expression nodes to find column references.
func (a *Analyzer) visitExprForColumns(node antlr.ParseTree, columns *[]scope.ColumnRef) {
	if node == nil {
		return
	}

	// Check if this is a ColumnRef node
	if colRefCtx, ok := node.(*mysql.ColumnRefContext); ok {
		colRef := a.extractColumnRef(colRefCtx)
		if colRef.Column != "" {
			*columns = append(*columns, colRef)
		}
	}

	// Visit children
	if ruleNode, ok := node.(antlr.RuleNode); ok {
		for i := 0; i < ruleNode.GetChildCount(); i++ {
			a.visitExprForColumns(ruleNode.GetChild(i).(antlr.ParseTree), columns)
		}
	}
}

// extractColumnRef extracts a column reference from a ColumnRef context.
func (*Analyzer) extractColumnRef(ctx *mysql.ColumnRefContext) scope.ColumnRef {
	colRef := scope.ColumnRef{}

	if ctx.FieldIdentifier() != nil {
		fullText := ctx.FieldIdentifier().GetText()
		table, column := parseQualifiedIdentifier(fullText)
		colRef.Table = table
		colRef.Column = column
	}

	return colRef
}

// isExpressionDerived checks if an expression involves transformations.
func (*Analyzer) isExpressionDerived(expr mysql.IExprContext) bool {
	text := expr.GetText()
	upperText := strings.ToUpper(text)
	return strings.Contains(text, "(") ||
		strings.Contains(text, "+") ||
		strings.Contains(text, "-") ||
		strings.Contains(text, "*") ||
		strings.Contains(text, "/") ||
		strings.Contains(upperText, "CASE") ||
		strings.Contains(upperText, "WHEN")
}

// analyzeExpressionOperator analyzes an expression and returns detailed operator information.
// This function identifies the type of operation (function, aggregate, operator, case, window)
// and extracts relevant metadata for operator-level lineage tracking.
func (a *Analyzer) analyzeExpressionOperator(expr mysql.IExprContext) []model.Transformation {
	if expr == nil {
		return nil
	}

	exprText := expr.GetText()

	// Try to detect aggregate functions FIRST (before general functions)
	// because aggregates are a special type of function
	if aggInfo, ok := a.detectAggregateFunction(expr); ok {
		return []model.Transformation{aggInfo}
	}

	// Try to detect window functions
	if windowInfo, ok := a.detectWindowFunction(expr); ok {
		return []model.Transformation{windowInfo}
	}

	// Try to detect regular function calls
	if funcInfo, ok := a.detectFunctionCall(expr); ok {
		return []model.Transformation{funcInfo}
	}

	// Try to detect CASE expressions
	if caseInfo, ok := a.detectCaseExpression(expr); ok {
		return []model.Transformation{caseInfo}
	}

	// Try to detect arithmetic/comparison operators
	if opInfo, ok := a.detectOperatorExpression(expr); ok {
		return []model.Transformation{opInfo}
	}

	// Fallback to generic expression
	return []model.Transformation{model.NewProjectTransformation(exprText)}
}

// detectFunctionCall checks if expression is a function call and extracts function info
func (a *Analyzer) detectFunctionCall(expr mysql.IExprContext) (model.Transformation, bool) {
	// Walk the expression tree to find function calls
	var funcName string
	var args []string

	// Check if it's a SimpleExprFunction context (regular functions)
	if a.containsFunctionContext(expr, &funcName, &args) {
		if funcName != "" {
			t := createFunctionOperatorInfo(funcName, expr.GetText(), args)
			return t, true
		}
	}

	return model.Transformation{}, false
}

// Common aggregate functions
var aggregateFunctions = map[string]bool{
	"COUNT": true, "SUM": true, "AVG": true, "MAX": true, "MIN": true,
	"GROUP_CONCAT": true, "STD": true, "STDDEV": true, "STDDEV_POP": true,
	"STDDEV_SAMP": true, "VAR_POP": true, "VAR_SAMP": true, "VARIANCE": true,
}

// detectAggregateFunction checks if expression is an aggregate function
func (a *Analyzer) detectAggregateFunction(expr mysql.IExprContext) (model.Transformation, bool) {
	var funcName string
	var args []string

	if a.containsFunctionContext(expr, &funcName, &args) {
		upperFuncName := strings.ToUpper(funcName)
		if aggregateFunctions[upperFuncName] {
			return createAggregateOperatorInfo(upperFuncName, expr.GetText(), nil), true
		}
	}

	return model.Transformation{}, false
}

// Common window functions
var windowFunctions = map[string]bool{
	"ROW_NUMBER": true, "RANK": true, "DENSE_RANK": true, "NTILE": true,
	"LEAD": true, "LAG": true, "FIRST_VALUE": true, "LAST_VALUE": true,
	"NTH_VALUE": true, "CUME_DIST": true, "PERCENT_RANK": true,
}

// detectWindowFunction checks if expression contains a window function with OVER clause
func (a *Analyzer) detectWindowFunction(expr mysql.IExprContext) (model.Transformation, bool) {
	// Check for window function pattern: function(...) OVER (...)
	exprText := expr.GetText()
	if !strings.Contains(strings.ToUpper(exprText), "OVER") {
		return model.Transformation{}, false
	}

	var funcName string
	var args []string

	if a.containsFunctionContext(expr, &funcName, &args) {
		upperFuncName := strings.ToUpper(funcName)
		// Check if it's a window function OR an aggregate used as window function
		if windowFunctions[upperFuncName] || aggregateFunctions[upperFuncName] {
			// Try to extract PARTITION BY and ORDER BY info
			partitionBy, orderBy := a.extractWindowClauses(expr)
			return createWindowOperatorInfo(upperFuncName, exprText, partitionBy, orderBy), true
		}
	}

	return model.Transformation{}, false
}

// detectCaseExpression checks if expression is a CASE expression
func (*Analyzer) detectCaseExpression(expr mysql.IExprContext) (model.Transformation, bool) {
	exprText := strings.ToUpper(expr.GetText())
	if strings.Contains(exprText, "CASE") && strings.Contains(exprText, "WHEN") {
		return createCaseOperatorInfo(expr.GetText()), true
	}
	return model.Transformation{}, false
}

// detectOperatorExpression checks if expression uses arithmetic or comparison operators
func (*Analyzer) detectOperatorExpression(expr mysql.IExprContext) (model.Transformation, bool) {
	exprText := expr.GetText()

	// Check for arithmetic operators
	if strings.Contains(exprText, "+") {
		return createOperatorExprInfo("ADDITION", exprText), true
	}
	if strings.Contains(exprText, "-") && !strings.HasPrefix(exprText, "-") {
		return createOperatorExprInfo("SUBTRACTION", exprText), true
	}
	if strings.Contains(exprText, "*") && !strings.Contains(exprText, "COUNT(*)") {
		return createOperatorExprInfo("MULTIPLICATION", exprText), true
	}
	if strings.Contains(exprText, "/") {
		return createOperatorExprInfo("DIVISION", exprText), true
	}

	// Check for comparison operators
	if strings.Contains(exprText, "=") && !strings.Contains(exprText, "!=") && !strings.Contains(exprText, ">=") && !strings.Contains(exprText, "<=") {
		return createOperatorExprInfo("EQUALS", exprText), true
	}
	if strings.Contains(exprText, ">") && !strings.Contains(exprText, ">=") {
		return createOperatorExprInfo("GREATER_THAN", exprText), true
	}
	if strings.Contains(exprText, "<") && !strings.Contains(exprText, "<=") && !strings.Contains(exprText, "<>") {
		return createOperatorExprInfo("LESS_THAN", exprText), true
	}

	return model.Transformation{}, false
}

// containsFunctionContext walks the expression tree to find function call contexts
func (a *Analyzer) containsFunctionContext(node antlr.ParseTree, funcName *string, args *[]string) bool {
	if node == nil {
		return false
	}

	// Check for different function context types in MySQL grammar
	switch ctx := node.(type) {
	case *mysql.SimpleExprFunctionContext:
		// Regular functions like CONCAT, UPPER, etc.
		if ctx.FunctionCall() != nil {
			if funcCall := ctx.FunctionCall(); funcCall != nil {
				if funcCall.PureIdentifier() != nil {
					*funcName = funcCall.PureIdentifier().GetText()
					// Try to extract arguments
					if funcCall.ExprList() != nil {
						for _, arg := range funcCall.ExprList().AllExpr() {
							*args = append(*args, arg.GetText())
						}
					} else if funcCall.UdfExprList() != nil {
						for _, arg := range funcCall.UdfExprList().AllUdfExpr() {
							*args = append(*args, arg.GetText())
						}
					}
					return true
				}
			}
		}
	case *mysql.SimpleExprRuntimeFunctionContext:
		// Runtime functions like IF, COALESCE
		if ctx.RuntimeFunctionCall() != nil {
			funcText := ctx.GetText()
			if strings.HasPrefix(strings.ToUpper(funcText), "IF(") {
				*funcName = "IF"
				return true
			} else if strings.HasPrefix(strings.ToUpper(funcText), "COALESCE(") {
				*funcName = "COALESCE"
				return true
			}
		}
	case *mysql.SimpleExprSumContext:
		// This wraps SumExprContext - extract the actual SumExpr
		if ctx.SumExpr() != nil {
			return a.containsFunctionContext(ctx.SumExpr(), funcName, args)
		}
	case *mysql.SumExprContext:
		// Aggregate functions like COUNT, SUM
		if ctx.AVG_SYMBOL() != nil {
			*funcName = "AVG"
			return true
		} else if ctx.COUNT_SYMBOL() != nil {
			*funcName = "COUNT"
			if strings.Contains(ctx.GetText(), "*") {
				*args = []string{"*"}
			}
			return true
		} else if ctx.MAX_SYMBOL() != nil {
			*funcName = "MAX"
			return true
		} else if ctx.MIN_SYMBOL() != nil {
			*funcName = "MIN"
			return true
		} else if ctx.SUM_SYMBOL() != nil {
			*funcName = "SUM"
			return true
		} else if ctx.GROUP_CONCAT_SYMBOL() != nil {
			*funcName = "GROUP_CONCAT"
			return true
		}
	default:
	}

	// Recursively check children
	if ruleNode, ok := node.(antlr.RuleNode); ok {
		for i := 0; i < ruleNode.GetChildCount(); i++ {
			if a.containsFunctionContext(ruleNode.GetChild(i).(antlr.ParseTree), funcName, args) {
				return true
			}
		}
	}

	return false
}

// extractWindowClauses extracts PARTITION BY and ORDER BY clauses from window expression
func (*Analyzer) extractWindowClauses(expr mysql.IExprContext) (partitionBy []string, orderBy []string) {
	// This is a simplified extraction - a full implementation would walk the AST
	// For now, we'll do text-based extraction
	exprText := expr.GetText()

	// Extract PARTITION BY columns (simplified)
	if idx := strings.Index(strings.ToUpper(exprText), "PARTITIONBY"); idx >= 0 {
		// This is a simplified approach - in production, you'd parse the AST properly
		// For now, we'll just note that partition exists
		partitionBy = []string{"<partition_columns>"}
	}

	// Extract ORDER BY columns (simplified)
	if idx := strings.Index(strings.ToUpper(exprText), "ORDERBY"); idx >= 0 {
		orderBy = []string{"<order_columns>"}
	}

	return partitionBy, orderBy
}

// getTableName extracts the table name from a TableRef context.
func (*Analyzer) getTableName(ctx mysql.ITableRefContext) string {
	if ctx.QualifiedIdentifier() != nil {
		_, name := parseQualifiedIdentifier(ctx.QualifiedIdentifier().GetText())
		return name
	}
	return ""
}

// getSchemaName extracts the schema name from a TableRef context.
func (*Analyzer) getSchemaName(ctx mysql.ITableRefContext) string {
	if ctx.QualifiedIdentifier() != nil {
		fullText := ctx.QualifiedIdentifier().GetText()
		if strings.Contains(fullText, ".") {
			parts := strings.Split(fullText, ".")
			if len(parts) >= 2 {
				// Return all parts except the last one (schema)
				return strings.Join(parts[:len(parts)-1], ".")
			}
		}
	}
	return ""
}

// getTableAlias extracts the table alias from a SingleTable context.
func (*Analyzer) getTableAlias(ctx mysql.ISingleTableContext) string {
	if ctx.TableAlias() != nil {
		alias := ctx.TableAlias()
		if alias.Identifier() != nil {
			return alias.Identifier().GetText()
		}
	}
	return ""
}

// getSelectAlias extracts the alias from a SelectAlias context.
func (*Analyzer) getSelectAlias(ctx mysql.ISelectAliasContext) string {
	if ctx.Identifier() != nil {
		return ctx.Identifier().GetText()
	}
	if ctx.TextStringLiteral() != nil {
		text := ctx.TextStringLiteral().GetText()
		return strings.Trim(text, "'\"")
	}
	return ""
}

// extractColumnNames extracts column names from a column list.
func (*Analyzer) extractColumnNames(ctx mysql.IColumnInternalRefListContext) []string {
	columns := make([]string, 0)
	if ctx.AllColumnInternalRef() != nil {
		for _, colRef := range ctx.AllColumnInternalRef() {
			if colRef.Identifier() != nil {
				columns = append(columns, colRef.Identifier().GetText())
			}
		}
	}
	return columns
}

// inferColumnAlias infers an alias from an expression text.
// For simple column references like "table.column", returns "column".
// For complex expressions, returns the full text.
func (*Analyzer) inferColumnAlias(exprText string) string {
	// Check if this looks like a qualified column reference
	if strings.Contains(exprText, ".") && !strings.Contains(exprText, "(") {
		parts := strings.Split(exprText, ".")
		return parts[len(parts)-1]
	}
	return exprText
}

// pushScope creates and pushes a new scope onto the stack.
func (a *Analyzer) pushScope() {
	parent := a.currentScope()
	newScope := scope.NewScope(parent)
	a.scopeStack = append(a.scopeStack, newScope)
}

// popScope removes and returns the top scope from the stack.
func (a *Analyzer) popScope() *scope.Scope {
	if len(a.scopeStack) == 0 {
		return nil
	}
	scope := a.scopeStack[len(a.scopeStack)-1]
	a.scopeStack = a.scopeStack[:len(a.scopeStack)-1]
	return scope
}

// currentScope returns the current (top) scope.
func (a *Analyzer) currentScope() *scope.Scope {
	if len(a.scopeStack) == 0 {
		return nil
	}
	return a.scopeStack[len(a.scopeStack)-1]
}

// addRelation adds a column relation, avoiding duplicates using a map for O(1) lookup.
func (a *Analyzer) addRelation(relation model.ColumnRelation) {
	// Skip intermediate edges that originate from temporary tables
	if a.isTempTable(relation.Source.Table.Name) {
		return
	}
	// Skip edges targeting temporary tables (except the final result)
	if a.isTempTable(relation.Target.Table.Name) && relation.Target.Table.Name != resultTableName {
		return
	}

	// Create a unique signature for the relation (excluding Transformation which is metadata)
	signature := fmt.Sprintf("%s.%s.%s->%s.%s.%s",
		relation.Source.Table.Schema, relation.Source.Table.Name, relation.Source.Name,
		relation.Target.Table.Schema, relation.Target.Table.Name, relation.Target.Name)

	// Check if this relation already exists
	if _, exists := a.edgeSet[signature]; exists {
		return // Relation already exists, don't add duplicate
	}

	// Add to both the set and the list
	a.edgeSet[signature] = struct{}{}
	a.edges = append(a.edges, relation)
}

// expandWildcardWithCatalog expands SELECT * using catalog metadata.
// Returns true if expansion was successful, false to fallback.
func (a *Analyzer) expandWildcardWithCatalog(tableRef *scope.TableRef, sp *scope.Scope) bool {
	// Build table identifier for catalog lookup
	tableID := model.ObjectIdentifier{
		Schema: tableRef.Schema,
		Name:   tableRef.Table,
	}

	// Query catalog
	tableMeta, err := a.catalog.GetTable(a.ctx, tableID)
	if err != nil || tableMeta == nil {
		return false // Catalog lookup failed, use fallback
	}

	// Expand columns from catalog metadata
	for _, colMeta := range tableMeta.Columns {
		outputCol := scope.OutputColumn{
			Alias:      colMeta.Name,
			Expression: tableRef.Table + "." + colMeta.Name,
			SourceColumns: []scope.ColumnRef{{
				Schema: tableRef.Schema,
				Table:  tableRef.Table,
				Column: colMeta.Name,
			}},
			IsDerived: false,
		}
		sp.AddOutputColumn(outputCol)
	}

	return true
}

// // expandTableWildcardWithCatalog expands table.* using catalog metadata.
// // Returns true if expansion was successful, false to fallback.
// func (a *Analyzer) expandTableWildcardWithCatalog(tableRef *scope.TableRef, sp *scope.Scope) bool {
// 	// Build table identifier for catalog lookup
// 	tableID := model.ObjectIdentifier{
// 		Schema: tableRef.Schema,
// 		Name:   tableRef.Table,
// 	}

// 	// Query catalog
// 	tableMeta, err := a.catalog.GetTable(tableID)
// 	if err != nil || tableMeta == nil {
// 		return false // Catalog lookup failed, use fallback
// 	}

// 	// Expand columns from catalog metadata
// 	for _, colMeta := range tableMeta.Columns {
// 		outputCol := scope.OutputColumn{
// 			Alias:      colMeta.Name,
// 			Expression: tableRef.Table + "." + colMeta.Name,
// 			SourceColumns: []scope.ColumnRef{{
// 				Schema: tableRef.Schema,
// 				Table:  tableRef.Table,
// 				Column: colMeta.Name,
// 			}},
// 			IsDerived: false,
// 		}
// 		sp.AddOutputColumn(outputCol)
// 	}

// 	return true
// }
