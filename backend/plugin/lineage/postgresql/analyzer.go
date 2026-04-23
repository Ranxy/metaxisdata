// Package postgresql provides direct lineage analysis for PostgreSQL queries.
//
// This package analyzes PostgreSQL SQL queries by directly traversing the ANTLR AST
// to extract field-to-field lineage relationships.
package postgresql

import (
	"context"
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	pg "github.com/bytebase/parser/postgresql"
	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"
)

func init() {
	lineage.RegisterAnalyzeRelation(storepb.Engine_POSTGRES, Analyze)
}

// Constants for special table/column markers
const (
	resultTableName   = "__result__"
	deletionFieldName = "__deletion__"
	wildcardColumn    = "*"
)

// minQuotedIdentifierLength is the minimum length for a quoted identifier (e.g., "x").
const minQuotedIdentifierLength = 2

// PostgreSQL aggregate functions
var aggregateFunctions = []string{"COUNT", "SUM", "AVG", "MAX", "MIN", "ARRAY_AGG", "STRING_AGG"}

// PostgreSQL window functions
var windowFunctions = []string{"ROW_NUMBER", "RANK", "DENSE_RANK", "LEAD", "LAG", "FIRST_VALUE", "LAST_VALUE"}

// Arithmetic operators
var arithmeticOperators = []string{"+", "-", "*", "/"}

// Analyzer performs direct lineage analysis on PostgreSQL queries.
type Analyzer struct {
	ctx    context.Context
	sql    string
	tokens *antlr.CommonTokenStream
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
	// Flag to indicate if we're processing a SELECT within INSERT
	inInsertContext bool
	// Track temporary table names (CTEs, subqueries) to filter intermediate results
	tempTables map[string]struct{}
}

func Analyze(ctx context.Context, sql string) ([]model.ColumnRelation, error) {
	analyzer := NewAnalyzer(ctx, sql, lineage.CatelogProvide)
	return analyzer.AnalyzeRelations()
}

// NewAnalyzer creates a new PostgreSQL lineage analyzer.
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
	input := antlr.NewInputStream(a.sql)
	lexer := pg.NewPostgreSQLLexer(input)
	stream := antlr.NewCommonTokenStream(lexer, 0)
	a.tokens = stream
	parser := pg.NewPostgreSQLParser(stream)

	parser.RemoveErrorListeners()
	lexer.RemoveErrorListeners()

	tree := parser.Root()

	if rootCtx, ok := tree.(*pg.RootContext); ok {
		a.processRoot(rootCtx)
	}

	if len(a.errors) > 0 {
		return nil, errors.Errorf("analysis errors: %s", strings.Join(a.errors, "; "))
	}

	return a.edges, nil
}

// processRoot processes the root context of the parse tree.
func (a *Analyzer) processRoot(ctx *pg.RootContext) {
	if ctx.Stmtblock() == nil {
		return
	}

	stmtBlockCtx, ok := ctx.Stmtblock().(*pg.StmtblockContext)
	if !ok {
		return
	}

	a.processStmtBlock(stmtBlockCtx)
}

// processStmtBlock processes a statement block containing multiple statements.
func (a *Analyzer) processStmtBlock(ctx *pg.StmtblockContext) {
	if ctx.Stmtmulti() == nil {
		return
	}

	stmtMultiCtx, ok := ctx.Stmtmulti().(*pg.StmtmultiContext)
	if !ok {
		return
	}

	for _, stmtCtx := range stmtMultiCtx.AllStmt() {
		stmt, ok := stmtCtx.(*pg.StmtContext)
		if !ok {
			continue
		}
		a.processStmt(stmt)
	}
}

// processStmt processes different types of SQL statements.
func (a *Analyzer) processStmt(ctx *pg.StmtContext) {
	if ctx.Selectstmt() != nil {
		a.processSelectStmt(ctx.Selectstmt())
		return
	}

	if ctx.Insertstmt() != nil {
		insertCtx, ok := ctx.Insertstmt().(*pg.InsertstmtContext)
		if ok {
			a.processInsertStmt(insertCtx)
		}
		return
	}

	if ctx.Updatestmt() != nil {
		updateCtx, ok := ctx.Updatestmt().(*pg.UpdatestmtContext)
		if ok {
			a.processUpdateStmt(updateCtx)
		}
		return
	}

	if ctx.Deletestmt() != nil {
		deleteCtx, ok := ctx.Deletestmt().(*pg.DeletestmtContext)
		if ok {
			a.processDeleteStmt(deleteCtx)
		}
		return
	}

	if ctx.Viewstmt() != nil {
		viewCtx, ok := ctx.Viewstmt().(*pg.ViewstmtContext)
		if ok {
			a.processViewStmt(viewCtx)
		}
		return
	}

	if ctx.Createasstmt() != nil {
		createAsCtx, ok := ctx.Createasstmt().(*pg.CreateasstmtContext)
		if ok {
			a.processCreateAsStmt(createAsCtx)
		}
	}
}

// processSelectStmt processes a SELECT statement context.
func (a *Analyzer) processSelectStmt(ctx pg.ISelectstmtContext) {
	stmt, ok := ctx.(*pg.SelectstmtContext)
	if !ok {
		return
	}

	if stmt.Select_no_parens() != nil {
		a.processSelectNoParens(stmt.Select_no_parens())
	} else if stmt.Select_with_parens() != nil {
		a.processSelectWithParens(stmt.Select_with_parens())
	}
}

// processSelectNoParens processes a SELECT statement without parentheses.
func (a *Analyzer) processSelectNoParens(ctx pg.ISelect_no_parensContext) {
	selectNoParens, ok := ctx.(*pg.Select_no_parensContext)
	if !ok {
		return
	}

	// Process WITH clause if present
	if selectNoParens.With_clause() != nil {
		a.processWithClause(selectNoParens.With_clause())
	}

	// Process select clause
	if selectNoParens.Select_clause() != nil {
		a.processSelectClause(selectNoParens.Select_clause())
	}
}

// processSelectWithParens processes a SELECT statement with parentheses.
func (a *Analyzer) processSelectWithParens(ctx pg.ISelect_with_parensContext) {
	selectWithParens, ok := ctx.(*pg.Select_with_parensContext)
	if !ok {
		return
	}

	if selectWithParens.Select_no_parens() != nil {
		a.processSelectNoParens(selectWithParens.Select_no_parens())
	} else if selectWithParens.Select_with_parens() != nil {
		a.processSelectWithParens(selectWithParens.Select_with_parens())
	}
}

// processWithClause processes a WITH (CTE) clause.
func (a *Analyzer) processWithClause(ctx pg.IWith_clauseContext) {
	withClause, ok := ctx.(*pg.With_clauseContext)
	if !ok {
		return
	}

	if withClause.Cte_list() == nil {
		return
	}

	cteList, ok := withClause.Cte_list().(*pg.Cte_listContext)
	if !ok {
		return
	}

	for _, cteExpr := range cteList.AllCommon_table_expr() {
		a.processCTE(cteExpr)
	}
}

// processCTE processes a single Common Table Expression (CTE).
func (a *Analyzer) processCTE(ctx pg.ICommon_table_exprContext) {
	cte, ok := ctx.(*pg.Common_table_exprContext)
	if !ok {
		return
	}

	cteName := ""
	if cte.Name() != nil {
		cteName = a.getIdentifierText(cte.Name())
	}

	a.markTempTable(cteName)

	// Get column list if specified
	var columns []string
	if cte.Opt_name_list() != nil {
		columns = a.extractNameList(cte.Opt_name_list())
	}

	// Process the CTE's subquery in a new scope
	var lineage []model.ColumnRelation
	if cte.Preparablestmt() != nil {
		a.pushScope()
		a.processPreparableStmt(cte.Preparablestmt())
		cteScope := a.popScope()

		outputColumns := cteScope.GetOutputColumns()
		useExplicitColumns := len(columns) > 0 && len(columns) == len(outputColumns)

		// Build lineage for each output column. When the CTE declares an explicit
		// column list with a matching arity, it overrides the inner SELECT output
		// names positionally.
		for i, outputCol := range outputColumns {
			targetColumn := outputCol.Alias
			if useExplicitColumns {
				targetColumn = columns[i]
			}

			for _, sourceCol := range outputCol.SourceColumns {
				resolved, err := cteScope.ResolveColumn(sourceCol)
				if err != nil {
					continue
				}

				// Flatten temporary source lineage if the source is itself a temporary table
				if a.flattenTempSourceLineage(cteScope, resolved, cteName, targetColumn, outputCol.Transform, &lineage) {
					continue
				}

				edge := NewLineageEdge(
					resolved.Schema,
					resolved.Table,
					resolved.Column,
					"",
					cteName,
					targetColumn,
					outputCol.Transform,
					true,
				)

				lineage = append(lineage, edge)
			}
		}
	}

	cteDef := &scope.CTEDefinition{
		Name:          cteName,
		Columns:       columns,
		DefiningScope: a.currentScope(),
		Lineage:       lineage,
	}

	a.currentScope().AddCTE(cteDef)
}

// processPreparableStmt processes a preparable statement (SELECT, INSERT, UPDATE, DELETE).
func (a *Analyzer) processPreparableStmt(ctx pg.IPreparablestmtContext) {
	prepStmt, ok := ctx.(*pg.PreparablestmtContext)
	if !ok {
		return
	}

	if prepStmt.Selectstmt() != nil {
		a.processSelectStmt(prepStmt.Selectstmt())
	} else if prepStmt.Insertstmt() != nil {
		if insertCtx, ok := prepStmt.Insertstmt().(*pg.InsertstmtContext); ok {
			a.processInsertStmt(insertCtx)
		}
	} else if prepStmt.Updatestmt() != nil {
		if updateCtx, ok := prepStmt.Updatestmt().(*pg.UpdatestmtContext); ok {
			a.processUpdateStmt(updateCtx)
		}
	} else if prepStmt.Deletestmt() != nil {
		if deleteCtx, ok := prepStmt.Deletestmt().(*pg.DeletestmtContext); ok {
			a.processDeleteStmt(deleteCtx)
		}
	}
}

// processSelectClause processes a SELECT clause, handling UNION/EXCEPT operations.
func (a *Analyzer) processSelectClause(ctx pg.ISelect_clauseContext) {
	selectClause, ok := ctx.(*pg.Select_clauseContext)
	if !ok {
		return
	}

	// Handle UNION/EXCEPT operations
	allIntersect := selectClause.AllSimple_select_intersect()
	if len(allIntersect) > 1 {
		a.processUnionQueries(allIntersect)
	} else if len(allIntersect) == 1 {
		a.processSimpleSelectIntersect(allIntersect[0])
	}
}

// processUnionQueries processes multiple SELECT queries combined with UNION/EXCEPT operations.
func (a *Analyzer) processUnionQueries(allIntersect []pg.ISimple_select_intersectContext) {
	baseScope := a.currentScope()
	var allOutputColumns [][]scope.OutputColumn

	for i, intersect := range allIntersect {
		if i == 0 {
			// Process first query in the current scope
			a.processSimpleSelectIntersect(intersect)
			allOutputColumns = append(allOutputColumns, baseScope.GetOutputColumns())
		} else {
			// Process subsequent queries in temporary scopes
			tempScope := scope.NewScope(baseScope.Parent())
			for _, cte := range baseScope.GetCTEs() {
				tempScope.AddCTE(cte)
			}

			originalScope := a.currentScope()
			a.scopeStack[len(a.scopeStack)-1] = tempScope

			a.processSimpleSelectIntersect(intersect)

			a.scopeStack[len(a.scopeStack)-1] = originalScope
			allOutputColumns = append(allOutputColumns, tempScope.GetOutputColumns())
		}
	}

	a.mergeUnionOutputColumns(baseScope, allOutputColumns)
}

// mergeUnionOutputColumns merges output columns from multiple UNION queries.
func (*Analyzer) mergeUnionOutputColumns(baseScope *scope.Scope, allOutputColumns [][]scope.OutputColumn) {
	if len(allOutputColumns) == 0 || len(allOutputColumns[0]) == 0 {
		return
	}

	firstQueryOutputs := allOutputColumns[0]

	for colIdx := 0; colIdx < len(firstQueryOutputs); colIdx++ {
		firstCol := firstQueryOutputs[colIdx]

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

		firstCol.SourceColumns = mergedSources
		if hasDerivedTransform && firstCol.Transform == nil {
			firstCol.Transform = []model.Transformation{model.NewUnionTransformation()}
		}
		baseScope.SetOutputColumn(colIdx, firstCol)
	}
}

// processSimpleSelectIntersect processes a simple SELECT with INTERSECT operations.
func (a *Analyzer) processSimpleSelectIntersect(ctx pg.ISimple_select_intersectContext) {
	intersect, ok := ctx.(*pg.Simple_select_intersectContext)
	if !ok {
		return
	}

	allPrimary := intersect.AllSimple_select_pramary()
	if len(allPrimary) > 0 {
		a.processSimpleSelectPrimary(allPrimary[0])
	}
}

// processSimpleSelectPrimary processes a simple SELECT primary expression.
func (a *Analyzer) processSimpleSelectPrimary(ctx pg.ISimple_select_pramaryContext) {
	primary, ok := ctx.(*pg.Simple_select_pramaryContext)
	if !ok {
		return
	}

	scope := a.currentScope()

	// Process FROM clause first
	if primary.From_clause() != nil {
		a.processFromClause(primary.From_clause())
	}

	// Process SELECT list
	if primary.Opt_target_list() != nil {
		a.processTargetList(primary.Opt_target_list(), scope)
	} else if primary.Target_list() != nil {
		a.processTargetList(primary.Target_list(), scope)
	}

	// Handle nested select_with_parens
	if primary.Select_with_parens() != nil {
		a.processSelectWithParens(primary.Select_with_parens())
	}

	// Generate edges from sources to outputs
	a.generateEdges(scope)
}

// processFromClause processes a FROM clause containing table references.
func (a *Analyzer) processFromClause(ctx pg.IFrom_clauseContext) {
	fromClause, ok := ctx.(*pg.From_clauseContext)
	if !ok || fromClause.From_list() == nil {
		return
	}
	a.processFromList(fromClause.From_list())
}

// processFromList processes a list of table references in the FROM clause.
func (a *Analyzer) processFromList(ctx pg.IFrom_listContext) {
	fromList, ok := ctx.(*pg.From_listContext)
	if !ok {
		return
	}

	for _, tableRef := range fromList.AllTable_ref() {
		a.processTableRef(tableRef)
	}
}

// processTableRef processes a single table reference (table, subquery, or JOIN).
func (a *Analyzer) processTableRef(ctx pg.ITable_refContext) {
	if tableRef, ok := ctx.(*pg.Table_refContext); ok {
		// Process relation expression (table reference)
		if tableRef.Relation_expr() != nil {
			a.processRelationExpr(tableRef.Relation_expr(), tableRef.Opt_alias_clause())
		}

		// Process subquery (derived table)
		if tableRef.Select_with_parens() != nil {
			a.processDerivedTable(tableRef.Select_with_parens(), tableRef.Opt_alias_clause())
		}

		// Process JOINs
		for _, joined := range tableRef.AllJoined_table() {
			a.processJoinedTable(joined)
		}
	}
}

func (a *Analyzer) processRelationExpr(ctx pg.IRelation_exprContext, aliasCtx pg.IOpt_alias_clauseContext) {
	if relExpr, ok := ctx.(*pg.Relation_exprContext); ok {
		if relExpr.Qualified_name() != nil {
			tableName, schemaName := a.extractQualifiedName(relExpr.Qualified_name())
			alias := a.extractAlias(aliasCtx)

			// Check if this is a CTE reference
			if cte, ok := a.currentScope().FindCTE(tableName); ok {
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

			ref := &scope.TableRef{
				Schema:     schemaName,
				Table:      tableName,
				Alias:      alias,
				IsSubquery: false,
				IsCTE:      false,
				Columns:    []string{},
			}

			a.currentScope().AddTable(ref)
		}
	}
}

func (a *Analyzer) processDerivedTable(ctx pg.ISelect_with_parensContext, aliasCtx pg.IOpt_alias_clauseContext) {
	alias := a.extractAlias(aliasCtx)
	a.markTempTable(alias)

	a.pushScope()
	a.processSelectWithParens(ctx)
	subqueryScope := a.popScope()

	columns := make([]string, 0)
	lineage := make([]model.ColumnRelation, 0)

	for _, col := range subqueryScope.GetOutputColumns() {
		colName := col.Alias
		if colName == "" {
			colName = "column"
		}
		columns = append(columns, colName)

		for _, sourceCol := range col.SourceColumns {
			resolved, err := subqueryScope.ResolveColumn(sourceCol)
			if err != nil {
				continue
			}

			if a.flattenTempSourceLineage(subqueryScope, resolved, alias, colName, col.Transform, &lineage) {
				continue
			}

			edge := NewLineageEdge(
				resolved.Schema,
				resolved.Table,
				resolved.Column,
				"",
				alias,
				colName,
				col.Transform,
				true,
			)

			lineage = append(lineage, edge)
		}
	}

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

func (a *Analyzer) processJoinedTable(ctx pg.IJoined_tableContext) {
	if joined, ok := ctx.(*pg.Joined_tableContext); ok {
		if joined.Table_ref() != nil {
			a.processTableRef(joined.Table_ref())
		}
	}
}

func (a *Analyzer) processTargetList(ctx antlr.ParserRuleContext, scope *scope.Scope) {
	switch targetList := ctx.(type) {
	case *pg.Opt_target_listContext:
		if targetList.Target_list() != nil {
			a.processTargetList(targetList.Target_list().(*pg.Target_listContext), scope)
		}
	case *pg.Target_listContext:
		for _, targetEl := range targetList.AllTarget_el() {
			a.processTargetEl(targetEl, scope)
		}
	default:
	}
}

func (a *Analyzer) processTargetEl(ctx pg.ITarget_elContext, sp *scope.Scope) {
	switch target := ctx.(type) {
	case *pg.Target_starContext:
		// Handle SELECT *
		tables := sp.GetTables()
		for _, tableRef := range tables {
			if a.catalog != nil && !tableRef.IsSubquery && !tableRef.IsCTE {
				if a.expandWildcardWithCatalog(tableRef, sp) {
					continue
				}
			}

			outputCol := scope.OutputColumn{
				Alias:         wildcardColumn,
				Expression:    wildcardColumn,
				SourceColumns: []scope.ColumnRef{{Schema: tableRef.Schema, Table: tableRef.Table, Column: wildcardColumn}},
				IsDerived:     false,
			}
			sp.AddOutputColumn(outputCol)
		}

	case *pg.Target_columnrefContext:
		// Handle table.* or simple column reference
		if target.Columnref() != nil {
			colRef := a.extractColumnRef(target.Columnref())

			// Check if this is table.*
			if colRef.Column == wildcardColumn {
				if tableRef, ok := sp.FindTable(colRef.Table); ok {
					outputCol := scope.OutputColumn{
						Alias:         wildcardColumn,
						Expression:    colRef.Table + "." + wildcardColumn,
						SourceColumns: []scope.ColumnRef{{Schema: tableRef.Schema, Table: tableRef.Table, Column: wildcardColumn}},
						IsDerived:     false,
					}
					sp.AddOutputColumn(outputCol)
				}
			} else {
				alias := colRef.Column
				outputCol := scope.OutputColumn{
					Alias:         alias,
					Expression:    a.getParseTreeText(target),
					SourceColumns: []scope.ColumnRef{colRef},
					IsDerived:     false,
				}
				sp.AddOutputColumn(outputCol)
			}
		}

	case *pg.Target_labelContext:
		// Handle expression with optional alias
		if target.A_expr() != nil {
			exprText := a.getParseTreeText(target.A_expr())
			alias := ""

			if target.Target_alias() != nil {
				alias = a.extractTargetAlias(target.Target_alias())
			} else {
				alias = a.inferColumnAlias(exprText)
			}

			sourceColumns := a.extractColumnsFromExpr(target.A_expr())
			isDerived := a.isExpressionDerived(target.A_expr())

			if isDerived && len(sourceColumns) == 0 {
				tables := sp.GetTables()
				for _, tableRef := range tables {
					sourceColumns = append(sourceColumns, scope.ColumnRef{
						Schema: tableRef.Schema,
						Table:  tableRef.Table,
						Column: wildcardColumn,
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
				outputCol.Transform = a.analyzeExpressionOperator(target.A_expr())
			}

			sp.AddOutputColumn(outputCol)
		}

	default:
	}
}

func (a *Analyzer) processInsertStmt(ctx *pg.InsertstmtContext) {
	// Process WITH clause if present
	if ctx.Opt_with_clause() != nil {
		a.processOptWithClause(ctx.Opt_with_clause())
	}

	targetTable := ""
	targetSchema := ""
	if ctx.Insert_target() != nil {
		if insertTarget, ok := ctx.Insert_target().(*pg.Insert_targetContext); ok {
			if insertTarget.Qualified_name() != nil {
				targetTable, targetSchema = a.extractQualifiedName(insertTarget.Qualified_name())
			}
		}
	}

	var targetColumns []string
	if ctx.Insert_rest() != nil {
		if insertRest, ok := ctx.Insert_rest().(*pg.Insert_restContext); ok {
			// Extract column list if present
			if insertRest.Insert_column_list() != nil {
				targetColumns = a.extractInsertColumnList(insertRest.Insert_column_list())
			}

			// Process SELECT statement
			if insertRest.Selectstmt() != nil {
				a.inInsertContext = true
				a.processSelectStmt(insertRest.Selectstmt())
				a.inInsertContext = false
			}
		}
	}

	a.generateEdgesForDataModification(targetSchema, targetTable, targetColumns)

	// Handle ON CONFLICT clause
	if ctx.Opt_on_conflict() != nil {
		a.processOnConflict(ctx.Opt_on_conflict(), targetSchema, targetTable)
	}
}

func (a *Analyzer) processOptWithClause(ctx pg.IOpt_with_clauseContext) {
	if optWith, ok := ctx.(*pg.Opt_with_clauseContext); ok {
		if optWith.With_clause() != nil {
			a.processWithClause(optWith.With_clause())
		}
	}
}

func (a *Analyzer) processOnConflict(ctx pg.IOpt_on_conflictContext, targetSchema, targetTable string) {
	if onConflict, ok := ctx.(*pg.Opt_on_conflictContext); ok {
		// Check if there's a SET clause (DO UPDATE)
		if onConflict.Set_clause_list() != nil {
			a.processSetClauseList(onConflict.Set_clause_list(), targetSchema, targetTable)
		}
	}
}

func (a *Analyzer) processUpdateStmt(ctx *pg.UpdatestmtContext) {
	// Process WITH clause if present
	if ctx.Opt_with_clause() != nil {
		a.processOptWithClause(ctx.Opt_with_clause())
	}

	// Process table reference
	if ctx.Relation_expr_opt_alias() != nil {
		a.processRelationExprOptAlias(ctx.Relation_expr_opt_alias())
	}

	// Process FROM clause if present
	if ctx.From_clause() != nil {
		a.processFromClause(ctx.From_clause())
	}

	// Process SET clause
	if ctx.Set_clause_list() != nil {
		targetTable := ""
		targetSchema := ""
		if ctx.Relation_expr_opt_alias() != nil {
			if relExprOptAlias, ok := ctx.Relation_expr_opt_alias().(*pg.Relation_expr_opt_aliasContext); ok {
				if relExprOptAlias.Relation_expr() != nil {
					if relExpr, ok := relExprOptAlias.Relation_expr().(*pg.Relation_exprContext); ok {
						if relExpr.Qualified_name() != nil {
							targetTable, targetSchema = a.extractQualifiedName(relExpr.Qualified_name())
						}
					}
				}
			}
		}
		a.processSetClauseList(ctx.Set_clause_list(), targetSchema, targetTable)
	}
}

func (a *Analyzer) processRelationExprOptAlias(ctx pg.IRelation_expr_opt_aliasContext) {
	if relExprOptAlias, ok := ctx.(*pg.Relation_expr_opt_aliasContext); ok {
		if relExprOptAlias.Relation_expr() != nil {
			if relExpr, ok := relExprOptAlias.Relation_expr().(*pg.Relation_exprContext); ok {
				if relExpr.Qualified_name() != nil {
					tableName, schemaName := a.extractQualifiedName(relExpr.Qualified_name())
					alias := ""
					if relExprOptAlias.Colid() != nil {
						alias = a.getIdentifierText(relExprOptAlias.Colid())
					}

					ref := &scope.TableRef{
						Schema:     schemaName,
						Table:      tableName,
						Alias:      alias,
						IsSubquery: false,
						IsCTE:      false,
						Columns:    []string{},
					}

					a.currentScope().AddTable(ref)
				}
			}
		}
	}
}

func (a *Analyzer) processSetClauseList(ctx pg.ISet_clause_listContext, targetSchema, targetTable string) {
	if setClauseList, ok := ctx.(*pg.Set_clause_listContext); ok {
		currentScope := a.currentScope()

		for _, setClause := range setClauseList.AllSet_clause() {
			if sc, ok := setClause.(*pg.Set_clauseContext); ok {
				// Get target column
				targetColumn := ""
				if sc.Set_target() != nil {
					if setTarget, ok := sc.Set_target().(*pg.Set_targetContext); ok {
						if setTarget.Colid() != nil {
							targetColumn = a.getIdentifierText(setTarget.Colid())
						}
					}
				}

				// Get source columns from the expression
				var sourceColumns []scope.ColumnRef
				var transformInfo []model.Transformation

				if sc.A_expr() != nil {
					sourceColumns = a.extractColumnsFromExpr(sc.A_expr())
					transformInfo = a.analyzeExpressionOperator(sc.A_expr())
				}

				if len(sourceColumns) == 0 {
					sourceColumns = []scope.ColumnRef{{
						Schema: targetSchema,
						Table:  targetTable,
						Column: wildcardColumn,
					}}
				}

				for _, sourceCol := range sourceColumns {
					resolvedSource, err := currentScope.ResolveColumn(sourceCol)
					if err != nil {
						resolvedSource = &sourceCol
					}

					isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetTable)
					relation := NewLineageEdge(
						resolvedSource.Schema, resolvedSource.Table, resolvedSource.Column,
						targetSchema, targetTable, targetColumn,
						transformInfo,
						isTemp,
					)
					a.addRelation(relation)
				}
			}
		}
	}
}

func (a *Analyzer) processDeleteStmt(ctx *pg.DeletestmtContext) {
	// Process WITH clause if present
	if ctx.Opt_with_clause() != nil {
		a.processOptWithClause(ctx.Opt_with_clause())
	}

	// Get target table
	targetTable := ""
	targetSchema := ""
	if ctx.Relation_expr_opt_alias() != nil {
		if relExprOptAlias, ok := ctx.Relation_expr_opt_alias().(*pg.Relation_expr_opt_aliasContext); ok {
			if relExprOptAlias.Relation_expr() != nil {
				if relExpr, ok := relExprOptAlias.Relation_expr().(*pg.Relation_exprContext); ok {
					if relExpr.Qualified_name() != nil {
						targetTable, targetSchema = a.extractQualifiedName(relExpr.Qualified_name())
					}
				}
			}
		}
		a.processRelationExprOptAlias(ctx.Relation_expr_opt_alias())
	}

	// Process USING clause if present
	if ctx.Using_clause() != nil {
		if usingClause, ok := ctx.Using_clause().(*pg.Using_clauseContext); ok {
			if usingClause.From_list() != nil {
				a.processFromList(usingClause.From_list())
			}
		}
	}

	// Process WHERE clause
	if ctx.Where_or_current_clause() != nil {
		if whereClause, ok := ctx.Where_or_current_clause().(*pg.Where_or_current_clauseContext); ok {
			if whereClause.A_expr() != nil {
				conditionColumns := a.extractColumnsFromExpr(whereClause.A_expr())
				scope := a.currentScope()

				for _, condCol := range conditionColumns {
					resolved, err := scope.ResolveColumn(condCol)
					if err != nil {
						resolved = &condCol
					}

					transform := []model.Transformation{
						model.NewDeleteTransformation(normalizeExpressionText(a.getParseTreeText(whereClause.A_expr()))),
					}

					if tableRef, ok := scope.FindTable(resolved.Table); ok && (tableRef.IsCTE || tableRef.IsSubquery) {
						a.traceThroughTableLineageToTarget(tableRef, resolved.Column, targetSchema, targetTable, deletionFieldName, transform)
						continue
					}

					isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetTable)
					relation := NewLineageEdge(
						resolved.Schema, resolved.Table, resolved.Column,
						targetSchema, targetTable, deletionFieldName,
						transform,
						isTemp,
					)
					a.addRelation(relation)
				}
			}
		}
	}
}

func (a *Analyzer) processViewStmt(ctx *pg.ViewstmtContext) {
	targetView := ""
	targetSchema := ""
	if ctx.Qualified_name() != nil {
		targetView, targetSchema = a.extractQualifiedName(ctx.Qualified_name())
	}

	// Extract explicit column names if present
	var explicitColumnNames []string
	if ctx.Opt_column_list() != nil {
		explicitColumnNames = a.extractOptColumnList(ctx.Opt_column_list())
	}
	if ctx.Columnlist() != nil {
		explicitColumnNames = a.extractColumnList(ctx.Columnlist())
	}

	// Process the SELECT query
	if ctx.Selectstmt() != nil {
		a.processSelectStmt(ctx.Selectstmt())
	}

	// Get output columns and generate edges
	scope := a.currentScope()
	outputColumns := scope.GetOutputColumns()

	for i, outputCol := range outputColumns {
		targetColName := outputCol.Alias
		if i < len(explicitColumnNames) {
			targetColName = explicitColumnNames[i]
		}

		for _, sourceCol := range outputCol.SourceColumns {
			resolved, err := scope.ResolveColumn(sourceCol)
			if err != nil {
				continue
			}

			if tableRef, ok := scope.FindTable(resolved.Table); ok && (tableRef.IsCTE || tableRef.IsSubquery) {
				a.traceThroughTableLineageToTarget(tableRef, resolved.Column, targetSchema, targetView, targetColName, outputCol.Transform)
				continue
			}

			isTemp := targetView == resultTableName || a.isTableTempInCurrentScope(targetView)
			relation := NewLineageEdge(
				resolved.Schema, resolved.Table, resolved.Column,
				targetSchema, targetView, targetColName,
				outputCol.Transform,
				isTemp,
			)
			a.addRelation(relation)
		}
	}
}

func (a *Analyzer) processCreateAsStmt(ctx *pg.CreateasstmtContext) {
	targetTable := ""
	targetSchema := ""
	if ctx.Create_as_target() != nil {
		if createAsTarget, ok := ctx.Create_as_target().(*pg.Create_as_targetContext); ok {
			if createAsTarget.Qualified_name() != nil {
				targetTable, targetSchema = a.extractQualifiedName(createAsTarget.Qualified_name())
			}
		}
	}

	// Process the SELECT query
	if ctx.Selectstmt() != nil {
		a.processSelectStmt(ctx.Selectstmt())
	}

	// Get output columns and generate edges
	scope := a.currentScope()
	outputColumns := scope.GetOutputColumns()

	for _, outputCol := range outputColumns {
		targetColName := outputCol.Alias

		for _, sourceCol := range outputCol.SourceColumns {
			resolved, err := scope.ResolveColumn(sourceCol)
			if err != nil {
				continue
			}

			if tableRef, ok := scope.FindTable(resolved.Table); ok && (tableRef.IsCTE || tableRef.IsSubquery) {
				a.traceThroughTableLineageToTarget(tableRef, resolved.Column, targetSchema, targetTable, targetColName, outputCol.Transform)
				continue
			}

			isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetTable)
			relation := NewLineageEdge(
				resolved.Schema, resolved.Table, resolved.Column,
				targetSchema, targetTable, targetColName,
				outputCol.Transform,
				isTemp,
			)
			a.addRelation(relation)
		}
	}
}

// Helper methods for edge generation

// generateEdges generates lineage edges for SELECT query results.
// Only generates edges for the root scope (SELECT statements not in INSERT context).
func (a *Analyzer) generateEdges(scope *scope.Scope) {
	if a.inInsertContext {
		return
	}
	if scope == nil || scope.Parent() != nil {
		return
	}

	for _, outputCol := range scope.GetOutputColumns() {
		for _, sourceCol := range outputCol.SourceColumns {
			a.generateEdgeFromSource(scope, sourceCol, "", resultTableName, outputCol.Alias, outputCol.Transform)
		}
	}
}

// generateEdgesForDataModification generates lineage edges for data modification statements (INSERT, UPDATE, DELETE).
func (a *Analyzer) generateEdgesForDataModification(targetSchema, targetTable string, targetColumns []string) {
	scope := a.currentScope()
	outputColumns := scope.GetOutputColumns()

	for i, outputCol := range outputColumns {
		targetColName := outputCol.Alias
		if i < len(targetColumns) {
			targetColName = targetColumns[i]
		}

		for _, sourceCol := range outputCol.SourceColumns {
			a.generateEdgeFromSource(scope, sourceCol, targetSchema, targetTable, targetColName, outputCol.Transform)
		}
	}
}

// generateEdgeFromSource generates a lineage edge from a source column to a target.
// Handles tracing through temporary tables (CTEs and subqueries).
func (a *Analyzer) generateEdgeFromSource(scope *scope.Scope, sourceCol scope.ColumnRef, targetSchema, targetTable, targetColName string, transform []model.Transformation) {
	resolved, err := scope.ResolveColumn(sourceCol)
	if err != nil {
		return
	}

	// Check if the source is a temporary table (CTE or subquery)
	if tableRef, ok := scope.FindTable(resolved.Table); ok && (tableRef.IsCTE || tableRef.IsSubquery) {
		if targetTable == resultTableName {
			a.traceThroughTableLineage(tableRef, resolved.Column, targetColName, transform)
		} else {
			a.traceThroughTableLineageToTarget(tableRef, resolved.Column, targetSchema, targetTable, targetColName, transform)
		}
		return
	}

	// Create direct relation from source to target
	isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetTable)
	relation := NewLineageEdge(
		resolved.Schema, resolved.Table, resolved.Column,
		targetSchema, targetTable, targetColName,
		transform,
		isTemp,
	)
	a.addRelation(relation)
}

// traceThroughTableLineage traces lineage through a temporary table (CTE/subquery) to the result table.
func (a *Analyzer) traceThroughTableLineage(tableRef *scope.TableRef, columnName string, outputAlias string, transform []model.Transformation) {
	if a.inInsertContext {
		return
	}

	for _, edge := range tableRef.Lineage {
		// Skip if looking for specific column and doesn't match
		if columnName != wildcardColumn && edge.Target.Name != columnName {
			continue
		}

		actualOutput := outputAlias
		// For wildcard expansion, use the actual column name from the edge
		if columnName == wildcardColumn && outputAlias == wildcardColumn {
			actualOutput = edge.Target.Name
		}

		combinedTransform := combineTransformations(edge.Transformation, transform)

		resultRelation := NewLineageEdge(
			edge.Source.Table.Schema,
			edge.Source.Table.Name,
			edge.Source.Name,
			"",
			resultTableName,
			actualOutput,
			combinedTransform,
			true,
		)

		a.addRelation(resultRelation)
	}
}

// traceThroughTableLineageToTarget traces lineage through a temporary table to a specific target table.
func (a *Analyzer) traceThroughTableLineageToTarget(tableRef *scope.TableRef, columnName string, targetSchema string, targetTable string, targetColumn string, transform []model.Transformation) {
	for _, edge := range tableRef.Lineage {
		// Skip if looking for specific column and doesn't match
		if columnName != wildcardColumn && edge.Target.Name != columnName {
			continue
		}

		actualTargetColumn := targetColumn
		// For wildcard expansion, use the actual column name from the edge
		if columnName == wildcardColumn && targetColumn == wildcardColumn {
			actualTargetColumn = edge.Target.Name
		}

		combinedTransform := combineTransformations(edge.Transformation, transform)
		isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetTable)

		resultRelation := NewLineageEdge(
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

// flattenTempSourceLineage flattens lineage edges when the source is a temporary table.
// Returns true if the source was a temporary table and was handled.
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

func (a *Analyzer) appendFlattenedLineage(lineage *[]model.ColumnRelation, scope *scope.Scope, tableRef *scope.TableRef, columnName string, targetTable string, targetColumn string, transform []model.Transformation) {
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

		if nestedRef, ok := scope.FindTable(sourceTableName); ok && (nestedRef.IsCTE || nestedRef.IsSubquery) {
			a.appendFlattenedLineage(lineage, scope, nestedRef, edge.Source.Name, targetTable, targetColumn, combinedTransform)
			continue
		}

		*lineage = append(*lineage, NewLineageEdge(
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

// Extraction helpers

func (a *Analyzer) extractQualifiedName(ctx pg.IQualified_nameContext) (name, schema string) {
	if qn, ok := ctx.(*pg.Qualified_nameContext); ok {
		if qn.Colid() != nil {
			name = a.getIdentifierText(qn.Colid())
		}
		if qn.Indirection() != nil {
			// Handle schema.table notation
			if indirection, ok := qn.Indirection().(*pg.IndirectionContext); ok {
				for _, indEl := range indirection.AllIndirection_el() {
					if el, ok := indEl.(*pg.Indirection_elContext); ok {
						if el.Attr_name() != nil {
							schema = name
							name = a.getIdentifierText(el.Attr_name())
						}
					}
				}
			}
		}
	}
	return name, schema
}

func (a *Analyzer) extractAlias(ctx pg.IOpt_alias_clauseContext) string {
	if ctx == nil {
		return ""
	}
	if optAlias, ok := ctx.(*pg.Opt_alias_clauseContext); ok {
		if optAlias.Table_alias_clause() != nil {
			if tableAliasClause, ok := optAlias.Table_alias_clause().(*pg.Table_alias_clauseContext); ok {
				if tableAliasClause.Table_alias() != nil {
					return a.getIdentifierText(tableAliasClause.Table_alias())
				}
			}
		}
	}
	return ""
}

func (a *Analyzer) extractTargetAlias(ctx pg.ITarget_aliasContext) string {
	if targetAlias, ok := ctx.(*pg.Target_aliasContext); ok {
		if targetAlias.Collabel() != nil {
			return a.getIdentifierText(targetAlias.Collabel())
		}

		return targetAlias.GetText()

		// if targetAlias.Identifier() != nil {
		// 	return targetAlias.Identifier().GetText()
		// }
	}
	return ""
}

func (a *Analyzer) extractColumnRef(ctx pg.IColumnrefContext) scope.ColumnRef {
	colRef := scope.ColumnRef{}
	if columnref, ok := ctx.(*pg.ColumnrefContext); ok {
		if columnref.Colid() != nil {
			colRef.Column = a.getIdentifierText(columnref.Colid())
		}
		if columnref.Indirection() != nil {
			if indirection, ok := columnref.Indirection().(*pg.IndirectionContext); ok {
				for _, indEl := range indirection.AllIndirection_el() {
					if el, ok := indEl.(*pg.Indirection_elContext); ok {
						if el.Attr_name() != nil {
							colRef.Table = colRef.Column
							colRef.Column = a.getIdentifierText(el.Attr_name())
						} else if el.STAR() != nil {
							colRef.Table = colRef.Column
							colRef.Column = wildcardColumn
						}
					}
				}
			}
		}
	}
	return colRef
}

// extractColumnsFromExpr extracts all column references from an expression.
func (a *Analyzer) extractColumnsFromExpr(expr pg.IA_exprContext) []scope.ColumnRef {
	columns := make([]scope.ColumnRef, 0)
	a.visitExprForColumns(expr, &columns)
	return columns
}

// visitExprForColumns recursively visits an expression tree to find column references.
func (a *Analyzer) visitExprForColumns(node antlr.ParseTree, columns *[]scope.ColumnRef) {
	if node == nil {
		return
	}

	if columnref, ok := node.(*pg.ColumnrefContext); ok {
		colRef := a.extractColumnRef(columnref)
		if colRef.Column != "" && colRef.Column != wildcardColumn {
			*columns = append(*columns, colRef)
		}
	}

	if ruleNode, ok := node.(antlr.RuleNode); ok {
		for i := 0; i < ruleNode.GetChildCount(); i++ {
			a.visitExprForColumns(ruleNode.GetChild(i).(antlr.ParseTree), columns)
		}
	}
}

// extractInsertColumnList extracts the list of column names from an INSERT statement.
func (a *Analyzer) extractInsertColumnList(ctx pg.IInsert_column_listContext) []string {
	var columns []string
	colList, ok := ctx.(*pg.Insert_column_listContext)
	if !ok {
		return columns
	}

	for _, colItem := range colList.AllInsert_column_item() {
		item, ok := colItem.(*pg.Insert_column_itemContext)
		if !ok || item.Colid() == nil {
			continue
		}
		columns = append(columns, a.getIdentifierText(item.Colid()))
	}
	return columns
}

// extractNameList extracts a list of names from an optional name list context.
func (a *Analyzer) extractNameList(ctx pg.IOpt_name_listContext) []string {
	var names []string
	optNameList, ok := ctx.(*pg.Opt_name_listContext)
	if !ok || optNameList.Name_list() == nil {
		return names
	}

	nameList, ok := optNameList.Name_list().(*pg.Name_listContext)
	if !ok {
		return names
	}

	for _, name := range nameList.AllName() {
		names = append(names, a.getIdentifierText(name))
	}
	return names
}

// extractOptColumnList extracts an optional column list.
func (a *Analyzer) extractOptColumnList(ctx pg.IOpt_column_listContext) []string {
	var columns []string
	optColList, ok := ctx.(*pg.Opt_column_listContext)
	if !ok || optColList.Columnlist() == nil {
		return columns
	}
	return a.extractColumnList(optColList.Columnlist())
}

// extractColumnList extracts a list of column names from a column list context.
func (a *Analyzer) extractColumnList(ctx pg.IColumnlistContext) []string {
	var columns []string
	colList, ok := ctx.(*pg.ColumnlistContext)
	if !ok {
		return columns
	}

	for _, col := range colList.AllColumnElem() {
		elem, ok := col.(*pg.ColumnElemContext)
		if !ok || elem.Colid() == nil {
			continue
		}
		columns = append(columns, a.getIdentifierText(elem.Colid()))
	}
	return columns
}

// getIdentifierText extracts the text of an identifier, removing surrounding quotes if present.
func (*Analyzer) getIdentifierText(ctx antlr.ParserRuleContext) string {
	if ctx == nil {
		return ""
	}
	text := ctx.GetText()
	// Remove double quotes if present
	if len(text) >= minQuotedIdentifierLength && text[0] == '"' && text[len(text)-1] == '"' {
		return text[1 : len(text)-1]
	}
	return text
}

func (a *Analyzer) getParseTreeText(node antlr.ParseTree) string {
	if node == nil {
		return ""
	}
	if a.tokens != nil {
		text := strings.TrimSpace(a.tokens.GetTextFromInterval(node.GetSourceInterval()))
		if text != "" {
			return text
		}
	}
	return node.GetText()
}

// inferColumnAlias infers a column alias from an expression text.
// For qualified column references (e.g., table.column), returns just the column name.
func (*Analyzer) inferColumnAlias(exprText string) string {
	if strings.Contains(exprText, ".") && !strings.Contains(exprText, "(") {
		parts := strings.Split(exprText, ".")
		return parts[len(parts)-1]
	}
	return exprText
}

// isExpressionDerived checks if an expression involves transformation operations.
func (a *Analyzer) isExpressionDerived(expr pg.IA_exprContext) bool {
	text := a.getParseTreeText(expr)
	upperText := strings.ToUpper(text)

	// Check for function calls
	if strings.Contains(text, "(") {
		return true
	}

	// Check for arithmetic operators
	arithmeticOps := []string{"+", "-", "*", "/"}
	for _, op := range arithmeticOps {
		if strings.Contains(text, op) {
			return true
		}
	}

	// Check for CASE expressions
	return strings.Contains(upperText, "CASE") || strings.Contains(upperText, "WHEN")
}

// analyzeExpressionOperator analyzes an expression to determine its transformation type.
func (a *Analyzer) analyzeExpressionOperator(expr pg.IA_exprContext) []model.Transformation {
	if expr == nil {
		return nil
	}

	exprText := a.getParseTreeText(expr)
	upperText := strings.ToUpper(exprText)

	// Check for aggregate functions
	if fnName := a.findFunctionInExpression(upperText, aggregateFunctions); fnName != "" {
		return []model.Transformation{model.NewAggregateTransformation(fnName, exprText, nil)}
	}

	// Check for window functions
	if fnName := a.findFunctionInExpression(upperText, windowFunctions); fnName != "" {
		if strings.Contains(upperText, "OVER") {
			return []model.Transformation{model.NewWindowTransformation(fnName, exprText, nil, nil)}
		}
	}

	// Check for CASE expressions
	if a.isCaseExpression(upperText) {
		return []model.Transformation{model.NewCaseTransformation(exprText)}
	}

	// Check for function calls
	if strings.Contains(exprText, "(") {
		return []model.Transformation{model.NewFunctionTransformation("", exprText, nil)}
	}

	// Check for arithmetic operators
	if a.containsArithmeticOperator(exprText) {
		return []model.Transformation{model.NewOperatorTransformation("ARITHMETIC", exprText)}
	}

	return []model.Transformation{model.NewProjectTransformation(exprText)}
}

// findFunctionInExpression checks if any function from the list appears in the expression.
func (*Analyzer) findFunctionInExpression(upperText string, functions []string) string {
	for _, fn := range functions {
		if strings.Contains(upperText, fn+"(") {
			return fn
		}
	}
	return ""
}

// isCaseExpression checks if the expression is a CASE expression.
func (*Analyzer) isCaseExpression(upperText string) bool {
	return strings.Contains(upperText, "CASE") && strings.Contains(upperText, "WHEN")
}

// containsArithmeticOperator checks if the expression contains arithmetic operators.
func (*Analyzer) containsArithmeticOperator(text string) bool {
	for _, op := range arithmeticOperators {
		if strings.Contains(text, op) {
			return true
		}
	}
	return false
}

// Scope management

// pushScope creates and pushes a new child scope onto the scope stack.
func (a *Analyzer) pushScope() {
	parent := a.currentScope()
	newScope := scope.NewScope(parent)
	a.scopeStack = append(a.scopeStack, newScope)
}

// popScope removes and returns the current scope from the stack.
func (a *Analyzer) popScope() *scope.Scope {
	if len(a.scopeStack) == 0 {
		return nil
	}
	scope := a.scopeStack[len(a.scopeStack)-1]
	a.scopeStack = a.scopeStack[:len(a.scopeStack)-1]
	return scope
}

// currentScope returns the current scope from the top of the stack.
func (a *Analyzer) currentScope() *scope.Scope {
	if len(a.scopeStack) == 0 {
		return nil
	}
	return a.scopeStack[len(a.scopeStack)-1]
}

// markTempTable marks a table name as temporary (CTE or subquery alias).
func (a *Analyzer) markTempTable(name string) {
	if name == "" {
		return
	}
	a.tempTables[name] = struct{}{}
}

// isTempTable checks if a table name is marked as temporary.
func (a *Analyzer) isTempTable(name string) bool {
	_, ok := a.tempTables[name]
	return ok
}

// isTableTempInCurrentScope checks if a table is a temporary table (CTE or subquery) in any scope.
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

// Edge management

// addRelation adds a column relation to the lineage graph with deduplication.
// Filters out edges where source or target is a temporary table (except __result__).
func (a *Analyzer) addRelation(relation model.ColumnRelation) {
	// Skip if source is a temporary table
	if a.isTempTable(relation.Source.Table.Name) {
		return
	}
	// Skip if target is a temporary table (except for the result table)
	if a.isTempTable(relation.Target.Table.Name) && relation.Target.Table.Name != resultTableName {
		return
	}

	// Create a unique signature for deduplication
	signature := fmt.Sprintf("%s.%s.%s->%s.%s.%s",
		relation.Source.Table.Schema, relation.Source.Table.Name, relation.Source.Name,
		relation.Target.Table.Schema, relation.Target.Table.Name, relation.Target.Name)

	if _, exists := a.edgeSet[signature]; exists {
		return
	}

	a.edgeSet[signature] = struct{}{}
	a.edges = append(a.edges, relation)
}

// expandWildcardWithCatalog expands a SELECT * using the catalog metadata.
// Returns true if expansion was successful.
func (a *Analyzer) expandWildcardWithCatalog(tableRef *scope.TableRef, sp *scope.Scope) bool {
	tableID := model.ObjectIdentifier{
		Schema: tableRef.Schema,
		Name:   tableRef.Table,
	}

	tableMeta, err := a.catalog.GetTable(a.ctx, tableID)
	if err != nil || tableMeta == nil {
		return false
	}

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

// NewLineageEdge creates a new LineageEdge from field-edge parameters.
func NewLineageEdge(fromSchema, fromTable, fromField, toSchema, toTable, toField string, transform []model.Transformation, isTemp bool) model.ColumnRelation {
	relType := determineRelationType(transform)

	return model.ColumnRelation{
		Source: model.Column{
			Table: model.ObjectIdentifier{
				Schema: fromSchema,
				Name:   fromTable,
			},
			Name: fromField,
		},
		Target: model.Column{
			Table: model.ObjectIdentifier{
				Schema: toSchema,
				Name:   toTable,
			},
			Name: toField,
		},
		Transformation: transform,
		RelationType:   relType,
		IsTemp:         isTemp,
	}
}

func determineRelationType(transform []model.Transformation) model.RelationType {
	if len(transform) == 0 {
		return model.RelationTypeDirect
	}

	for _, t := range transform {
		switch t.Operation {
		case model.OperationDelete:
			return model.RelationTypeIndirect
		case model.OperationUnion:
			return model.RelationTypeUnion
		case model.OperationAggregate:
			return model.RelationTypeGroup
		default:
			return model.RelationTypeIndirect
		}
	}

	return model.RelationTypeIndirect
}

func combineTransformations(base, additional []model.Transformation) []model.Transformation {
	if len(base) == 0 {
		return additional
	}
	if len(additional) == 0 {
		return base
	}
	return append(base, additional...)
}

func normalizeExpressionText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
