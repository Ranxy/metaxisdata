// Package postgresql provides direct lineage analysis for PostgreSQL queries.
//
// This implementation is built on github.com/bytebase/omni's PostgreSQL parser
// and typed AST (see plan/postgresql_omni_parser_migration_plan.md). It replaced
// the legacy ANTLR implementation, which was parity-verified against this one
// over the golden corpus and then removed.
package postgresql

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	omnipg "github.com/bytebase/omni/pg"
	pgast "github.com/bytebase/omni/pg/ast"

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

// PostgreSQL aggregate functions
var aggregateFunctions = []string{"COUNT", "SUM", "AVG", "MAX", "MIN", "ARRAY_AGG", "STRING_AGG"}

// PostgreSQL window functions
var windowFunctions = []string{"ROW_NUMBER", "RANK", "DENSE_RANK", "LEAD", "LAG", "FIRST_VALUE", "LAST_VALUE"}

// Arithmetic operators
var arithmeticOperators = []string{"+", "-", "*", "/"}

// Analyzer performs direct lineage analysis on PostgreSQL queries.
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
	return &Analyzer{
		ctx:        ctx,
		sql:        sql,
		scopeStack: []*scope.Scope{scope.NewScope(nil)}, // Root scope
		edges:      make([]model.ColumnRelation, 0),
		edgeSet:    make(map[string]struct{}),
		errors:     make([]string, 0),
		catalog:    catalogProvide,
		tempTables: make(map[string]struct{}),
	}
}

// AnalyzeRelations parses the SQL and returns column relations.
//
// Parse errors are a hard failure: no partial result is returned. Every
// well-formed statement in a multi-statement input is analyzed in order on the
// shared analyzer state, matching the legacy stmtmulti behavior (notably for
// MANUAL_SQL).
func (a *Analyzer) AnalyzeRelations() ([]model.ColumnRelation, error) {
	stmts, err := omnipg.Parse(a.sql)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse PostgreSQL SQL")
	}

	for _, stmt := range stmts {
		if stmt.Empty() {
			continue
		}
		a.processStmt(stmt.AST)
	}

	if len(a.errors) > 0 {
		return nil, errors.Errorf("analysis errors: %s", strings.Join(a.errors, "; "))
	}

	return a.edges, nil
}

// processStmt dispatches one parsed statement.
func (a *Analyzer) processStmt(node pgast.Node) {
	switch stmt := node.(type) {
	case *pgast.SelectStmt:
		a.processSelectStmt(stmt)
	case *pgast.InsertStmt:
		a.processInsertStmt(stmt)
	case *pgast.UpdateStmt:
		a.processUpdateStmt(stmt)
	case *pgast.DeleteStmt:
		a.processDeleteStmt(stmt)
	case *pgast.ViewStmt:
		a.processViewStmt(stmt)
	case *pgast.CreateTableAsStmt:
		a.processCreateTableAsStmt(stmt)
	default:
		// Unsupported statement kinds produce no lineage, matching the legacy
		// analyzer which silently ignored them.
	}
}

// ---------------------------------------------------------------------------
// SELECT
// ---------------------------------------------------------------------------

// processSelectStmt processes a SELECT, handling its WITH clause and set
// operations before the leaf SELECT core.
func (a *Analyzer) processSelectStmt(stmt *pgast.SelectStmt) {
	if stmt == nil {
		return
	}

	if stmt.WithClause != nil {
		a.processWithClause(stmt.WithClause)
	}

	if stmt.Op != pgast.SETOP_NONE {
		a.processSetOperation(stmt)
		return
	}

	a.processSelectCore(stmt)
}

// processSelectCore processes a leaf SELECT: FROM, then the target list, then
// the resulting edges.
func (a *Analyzer) processSelectCore(stmt *pgast.SelectStmt) {
	sp := a.currentScope()

	if stmt.FromClause != nil {
		a.processFromClause(stmt.FromClause)
	}

	if stmt.TargetList != nil {
		a.processTargetList(stmt.TargetList, sp)
	}

	a.generateEdges(sp)
}

// flattenSetOpArms flattens a UNION/EXCEPT tree into its operands, left to
// right. INTERSECT binds tighter and is left as one operand so the legacy
// "first primary only" behavior can be preserved.
func flattenSetOpArms(stmt *pgast.SelectStmt) []*pgast.SelectStmt {
	if stmt == nil {
		return nil
	}
	if stmt.Op == pgast.SETOP_UNION || stmt.Op == pgast.SETOP_EXCEPT {
		return append(flattenSetOpArms(stmt.Larg), flattenSetOpArms(stmt.Rarg)...)
	}
	return []*pgast.SelectStmt{stmt}
}

// processSetOperation processes UNION/INTERSECT/EXCEPT, mirroring the legacy
// scope juggling: the first operand is processed in the base scope, every later
// operand in a temporary scope parented to the base scope's parent.
func (a *Analyzer) processSetOperation(stmt *pgast.SelectStmt) {
	arms := flattenSetOpArms(stmt)
	baseScope := a.currentScope()
	var allOutputColumns [][]scope.OutputColumn

	for i, arm := range arms {
		if i == 0 {
			a.processSetOpArm(arm)
			allOutputColumns = append(allOutputColumns, baseScope.GetOutputColumns())
			continue
		}

		tempScope := scope.NewScope(baseScope.Parent())
		for _, cte := range baseScope.GetCTEs() {
			tempScope.AddCTE(cte)
		}

		originalScope := a.currentScope()
		a.scopeStack[len(a.scopeStack)-1] = tempScope
		a.processSetOpArm(arm)
		a.scopeStack[len(a.scopeStack)-1] = originalScope

		allOutputColumns = append(allOutputColumns, tempScope.GetOutputColumns())
	}

	a.mergeUnionOutputColumns(baseScope, allOutputColumns)
}

// processSetOpArm processes a single set-operation operand. An INTERSECT group
// keeps the legacy behavior of inspecting only its first primary.
func (a *Analyzer) processSetOpArm(arm *pgast.SelectStmt) {
	for arm != nil && arm.Op == pgast.SETOP_INTERSECT {
		arm = arm.Larg
	}
	if arm == nil {
		return
	}
	if arm.Op == pgast.SETOP_UNION || arm.Op == pgast.SETOP_EXCEPT {
		a.processSetOperation(arm)
		return
	}
	if arm.WithClause != nil {
		a.processWithClause(arm.WithClause)
	}
	a.processSelectCore(arm)
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

// ---------------------------------------------------------------------------
// CTEs
// ---------------------------------------------------------------------------

// processWithClause processes a WITH (CTE) clause.
func (a *Analyzer) processWithClause(withClause *pgast.WithClause) {
	if withClause == nil || withClause.Ctes == nil {
		return
	}
	for _, item := range withClause.Ctes.Items {
		if cte, ok := item.(*pgast.CommonTableExpr); ok {
			a.processCTE(cte)
		}
	}
}

// processPreparableStmt processes a preparable statement (SELECT, INSERT, UPDATE, DELETE).
func (a *Analyzer) processPreparableStmt(node pgast.Node) {
	switch stmt := node.(type) {
	case *pgast.SelectStmt:
		a.processSelectStmt(stmt)
	case *pgast.InsertStmt:
		a.processInsertStmt(stmt)
	case *pgast.UpdateStmt:
		a.processUpdateStmt(stmt)
	case *pgast.DeleteStmt:
		a.processDeleteStmt(stmt)
	default:
	}
}

// processCTE processes a single Common Table Expression (CTE).
func (a *Analyzer) processCTE(cte *pgast.CommonTableExpr) {
	if cte == nil {
		return
	}

	cteName := cte.Ctename
	a.markTempTable(cteName)

	columns := stringList(cte.Aliascolnames)

	var lineage []model.ColumnRelation
	if cte.Ctequery != nil {
		a.pushScope()
		a.processPreparableStmt(cte.Ctequery)
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

				if a.flattenTempSourceLineage(cteScope, resolved, cteName, targetColumn, outputCol.Transform, &lineage) {
					continue
				}

				lineage = append(lineage, NewLineageEdge(
					resolved.Schema,
					resolved.Table,
					resolved.Column,
					"",
					cteName,
					targetColumn,
					outputCol.Transform,
					true,
				))
			}
		}
	}

	a.currentScope().AddCTE(&scope.CTEDefinition{
		Name:          cteName,
		Columns:       columns,
		DefiningScope: a.currentScope(),
		Lineage:       lineage,
	})
}

// ---------------------------------------------------------------------------
// FROM
// ---------------------------------------------------------------------------

// processFromClause processes a FROM clause containing table references.
func (a *Analyzer) processFromClause(from *pgast.List) {
	if from == nil {
		return
	}
	for _, item := range from.Items {
		a.processTableExpr(item)
	}
}

// processTableExpr processes one FROM item: a relation, a derived table, or a
// join tree.
func (a *Analyzer) processTableExpr(node pgast.Node) {
	switch tableExpr := node.(type) {
	case *pgast.RangeVar:
		a.processRangeVar(tableExpr)
	case *pgast.RangeSubselect:
		a.processRangeSubselect(tableExpr)
	case *pgast.JoinExpr:
		a.processTableExpr(tableExpr.Larg)
		a.processTableExpr(tableExpr.Rarg)
	default:
		// RangeFunction/RangeTableSample and other FROM items were ignored by
		// the legacy analyzer as well.
	}
}

// processRangeVar registers a plain relation (or CTE reference) in the scope.
func (a *Analyzer) processRangeVar(rangeVar *pgast.RangeVar) {
	if rangeVar == nil {
		return
	}

	tableName := rangeVar.Relname
	alias := ""
	if rangeVar.Alias != nil {
		alias = rangeVar.Alias.Aliasname
	}

	if cte, ok := a.currentScope().FindCTE(tableName); ok {
		a.currentScope().AddTable(&scope.TableRef{
			Schema:     "",
			Table:      tableName,
			Alias:      alias,
			IsSubquery: false,
			IsCTE:      true,
			Columns:    cte.Columns,
			Lineage:    cte.Lineage,
		})
		return
	}

	a.currentScope().AddTable(&scope.TableRef{
		Schema:     rangeVar.Schemaname,
		Table:      tableName,
		Alias:      alias,
		IsSubquery: false,
		IsCTE:      false,
		Columns:    []string{},
	})
}

// addTargetRelation registers a data-modification target relation in the scope.
// Unlike processRangeVar it does not resolve CTEs, matching the legacy
// processRelationExprOptAlias.
func (a *Analyzer) addTargetRelation(rangeVar *pgast.RangeVar) {
	if rangeVar == nil {
		return
	}
	alias := ""
	if rangeVar.Alias != nil {
		alias = rangeVar.Alias.Aliasname
	}
	a.currentScope().AddTable(&scope.TableRef{
		Schema:     rangeVar.Schemaname,
		Table:      rangeVar.Relname,
		Alias:      alias,
		IsSubquery: false,
		IsCTE:      false,
		Columns:    []string{},
	})
}

// processRangeSubselect processes a derived table (subquery in FROM).
func (a *Analyzer) processRangeSubselect(sub *pgast.RangeSubselect) {
	if sub == nil {
		return
	}

	alias := ""
	if sub.Alias != nil {
		alias = sub.Alias.Aliasname
	}
	a.markTempTable(alias)

	query, ok := sub.Subquery.(*pgast.SelectStmt)
	if !ok {
		return
	}

	a.pushScope()
	a.processSelectStmt(query)
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

			lineage = append(lineage, NewLineageEdge(
				resolved.Schema,
				resolved.Table,
				resolved.Column,
				"",
				alias,
				colName,
				col.Transform,
				true,
			))
		}
	}

	a.currentScope().AddTable(&scope.TableRef{
		Schema:     "",
		Table:      alias,
		Alias:      alias,
		IsSubquery: true,
		IsCTE:      false,
		Columns:    columns,
		Lineage:    lineage,
	})
}

// ---------------------------------------------------------------------------
// Target list
// ---------------------------------------------------------------------------

// processTargetList processes the SELECT target list.
func (a *Analyzer) processTargetList(targets *pgast.List, sp *scope.Scope) {
	if targets == nil {
		return
	}
	for _, item := range targets.Items {
		rt, ok := item.(*pgast.ResTarget)
		if !ok {
			continue
		}
		a.processResTarget(rt, sp)
	}
}

// processResTarget processes one target element.
func (a *Analyzer) processResTarget(rt *pgast.ResTarget, sp *scope.Scope) {
	if rt == nil {
		return
	}

	if cr, ok := rt.Val.(*pgast.ColumnRef); ok {
		if isBareStar(cr) {
			a.processStar(sp)
			return
		}
		if isStarColumnRef(cr) {
			a.processTableStar(cr, sp)
			return
		}
		if rt.Name == "" {
			// Bare column reference: legacy Target_columnref.
			colRef := a.columnRefFromFields(cr.Fields)
			sp.AddOutputColumn(scope.OutputColumn{
				Alias:         colRef.Column,
				Expression:    a.exprTextOf(rt.Val),
				SourceColumns: []scope.ColumnRef{colRef},
				IsDerived:     false,
			})
			return
		}
	}

	a.processExpressionTarget(rt, sp)
}

// processStar expands `SELECT *` against every table in scope.
func (a *Analyzer) processStar(sp *scope.Scope) {
	for _, tableRef := range sp.GetTables() {
		if a.catalog != nil && !tableRef.IsSubquery && !tableRef.IsCTE {
			if a.expandWildcardWithCatalog(tableRef, sp) {
				continue
			}
		}

		sp.AddOutputColumn(scope.OutputColumn{
			Alias:         wildcardColumn,
			Expression:    wildcardColumn,
			SourceColumns: []scope.ColumnRef{{Schema: tableRef.Schema, Table: tableRef.Table, Column: wildcardColumn}},
			IsDerived:     false,
		})
	}
}

// processTableStar expands `table.*` against a single relation in scope.
func (a *Analyzer) processTableStar(cr *pgast.ColumnRef, sp *scope.Scope) {
	colRef := a.columnRefFromFields(cr.Fields)
	if tableRef, ok := sp.FindTable(colRef.Table); ok {
		sp.AddOutputColumn(scope.OutputColumn{
			Alias:         wildcardColumn,
			Expression:    colRef.Table + "." + wildcardColumn,
			SourceColumns: []scope.ColumnRef{{Schema: tableRef.Schema, Table: tableRef.Table, Column: wildcardColumn}},
			IsDerived:     false,
		})
	}
}

// processExpressionTarget processes an expression/aliased target element
// (legacy Target_label).
func (a *Analyzer) processExpressionTarget(rt *pgast.ResTarget, sp *scope.Scope) {
	exprText := a.exprTextOf(rt.Val)
	alias := rt.Name
	if alias == "" {
		alias = a.inferColumnAlias(exprText)
	}

	sourceColumns := a.extractColumnsFromNode(rt.Val)
	isDerived := a.isExpressionDerivedText(exprText)

	if isDerived && len(sourceColumns) == 0 {
		for _, tableRef := range sp.GetTables() {
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
		outputCol.Transform = a.analyzeExpressionOperator(rt.Val)
	}

	sp.AddOutputColumn(outputCol)
}

// ---------------------------------------------------------------------------
// DML
// ---------------------------------------------------------------------------

// processInsertStmt processes an INSERT statement.
func (a *Analyzer) processInsertStmt(stmt *pgast.InsertStmt) {
	if stmt == nil {
		return
	}

	if stmt.WithClause != nil {
		a.processWithClause(stmt.WithClause)
	}

	targetTable := ""
	targetSchema := ""
	if stmt.Relation != nil {
		targetTable = stmt.Relation.Relname
		targetSchema = stmt.Relation.Schemaname
	}

	var targetColumns []string
	if stmt.Cols != nil {
		for _, item := range stmt.Cols.Items {
			if rt, ok := item.(*pgast.ResTarget); ok {
				targetColumns = append(targetColumns, rt.Name)
			}
		}
	}

	if stmt.SelectStmt != nil {
		a.inInsertContext = true
		if sel, ok := stmt.SelectStmt.(*pgast.SelectStmt); ok {
			a.processSelectStmt(sel)
		}
		a.inInsertContext = false
	}

	a.generateEdgesForDataModification(targetSchema, targetTable, targetColumns)

	if stmt.OnConflictClause != nil {
		a.processOnConflict(stmt.OnConflictClause, targetSchema, targetTable)
	}
}

// processOnConflict processes an ON CONFLICT DO UPDATE SET list.
func (a *Analyzer) processOnConflict(onConflict *pgast.OnConflictClause, targetSchema, targetTable string) {
	if onConflict == nil {
		return
	}
	if onConflict.TargetList != nil {
		a.processSetClauseList(onConflict.TargetList, targetSchema, targetTable)
	}
}

// processUpdateStmt processes an UPDATE statement.
func (a *Analyzer) processUpdateStmt(stmt *pgast.UpdateStmt) {
	if stmt == nil {
		return
	}

	if stmt.WithClause != nil {
		a.processWithClause(stmt.WithClause)
	}

	targetTable := ""
	targetSchema := ""
	if stmt.Relation != nil {
		targetTable = stmt.Relation.Relname
		targetSchema = stmt.Relation.Schemaname
	}
	a.addTargetRelation(stmt.Relation)

	if stmt.FromClause != nil {
		a.processFromClause(stmt.FromClause)
	}

	if stmt.TargetList != nil {
		a.processSetClauseList(stmt.TargetList, targetSchema, targetTable)
	}
}

// processSetClauseList processes assignment targets (UPDATE SET / ON CONFLICT DO UPDATE SET).
func (a *Analyzer) processSetClauseList(assignments *pgast.List, targetSchema, targetTable string) {
	if assignments == nil {
		return
	}

	currentScope := a.currentScope()

	for _, item := range assignments.Items {
		rt, ok := item.(*pgast.ResTarget)
		if !ok {
			continue
		}

		targetColumn := rt.Name

		var sourceColumns []scope.ColumnRef
		var transformInfo []model.Transformation
		if rt.Val != nil {
			sourceColumns = a.extractColumnsFromNode(rt.Val)
			transformInfo = a.analyzeExpressionOperator(rt.Val)
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

// processDeleteStmt processes a DELETE statement.
func (a *Analyzer) processDeleteStmt(stmt *pgast.DeleteStmt) {
	if stmt == nil {
		return
	}

	if stmt.WithClause != nil {
		a.processWithClause(stmt.WithClause)
	}

	targetTable := ""
	targetSchema := ""
	if stmt.Relation != nil {
		targetTable = stmt.Relation.Relname
		targetSchema = stmt.Relation.Schemaname
	}
	a.addTargetRelation(stmt.Relation)

	if stmt.UsingClause != nil {
		a.processFromClause(stmt.UsingClause)
	}

	if stmt.WhereClause != nil {
		conditionColumns := a.extractColumnsFromNode(stmt.WhereClause)
		conditionText := normalizeExpressionText(a.exprTextOf(stmt.WhereClause))
		sp := a.currentScope()

		for _, condCol := range conditionColumns {
			resolved, err := sp.ResolveColumn(condCol)
			if err != nil {
				resolved = &condCol
			}

			transform := []model.Transformation{
				model.NewDeleteTransformation(conditionText),
			}

			if tableRef, ok := sp.FindTable(resolved.Table); ok && (tableRef.IsCTE || tableRef.IsSubquery) {
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

// ---------------------------------------------------------------------------
// DDL targets
// ---------------------------------------------------------------------------

// processViewStmt processes a CREATE VIEW statement.
func (a *Analyzer) processViewStmt(stmt *pgast.ViewStmt) {
	if stmt == nil {
		return
	}

	targetView := ""
	targetSchema := ""
	if stmt.View != nil {
		targetView = stmt.View.Relname
		targetSchema = stmt.View.Schemaname
	}

	explicitColumnNames := stringList(stmt.Aliases)

	if sel, ok := stmt.Query.(*pgast.SelectStmt); ok {
		a.processSelectStmt(sel)
	}

	sp := a.currentScope()
	outputColumns := sp.GetOutputColumns()

	for i, outputCol := range outputColumns {
		targetColName := outputCol.Alias
		if i < len(explicitColumnNames) {
			targetColName = explicitColumnNames[i]
		}

		for _, sourceCol := range outputCol.SourceColumns {
			resolved, err := sp.ResolveColumn(sourceCol)
			if err != nil {
				continue
			}

			if tableRef, ok := sp.FindTable(resolved.Table); ok && (tableRef.IsCTE || tableRef.IsSubquery) {
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

// processCreateTableAsStmt processes CREATE TABLE AS, CREATE MATERIALIZED VIEW and
// SELECT INTO. SELECT INTO behaves like a SELECT (legacy parity).
func (a *Analyzer) processCreateTableAsStmt(stmt *pgast.CreateTableAsStmt) {
	if stmt == nil {
		return
	}

	if stmt.IsSelectInto {
		if sel, ok := stmt.Query.(*pgast.SelectStmt); ok {
			a.processSelectStmt(sel)
		}
		return
	}

	targetTable := ""
	targetSchema := ""
	var explicitColumnNames []string
	if stmt.Into != nil {
		if stmt.Into.Rel != nil {
			targetTable = stmt.Into.Rel.Relname
			targetSchema = stmt.Into.Rel.Schemaname
		}
		// Only the legacy materialized-view path honored an explicit column list.
		if stmt.Objtype == pgast.OBJECT_MATVIEW {
			explicitColumnNames = stringList(stmt.Into.ColNames)
		}
	}

	if sel, ok := stmt.Query.(*pgast.SelectStmt); ok {
		a.processSelectStmt(sel)
	}

	sp := a.currentScope()
	outputColumns := sp.GetOutputColumns()

	for i, outputCol := range outputColumns {
		targetColName := outputCol.Alias
		if i < len(explicitColumnNames) {
			targetColName = explicitColumnNames[i]
		}

		for _, sourceCol := range outputCol.SourceColumns {
			resolved, err := sp.ResolveColumn(sourceCol)
			if err != nil {
				continue
			}

			if tableRef, ok := sp.FindTable(resolved.Table); ok && (tableRef.IsCTE || tableRef.IsSubquery) {
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

// ---------------------------------------------------------------------------
// Edge generation
// ---------------------------------------------------------------------------

// generateEdges generates lineage edges for SELECT query results.
// Only generates edges for the root scope (SELECT statements not in INSERT context).
func (a *Analyzer) generateEdges(sp *scope.Scope) {
	if a.inInsertContext {
		return
	}
	if sp == nil || sp.Parent() != nil {
		return
	}

	for _, outputCol := range sp.GetOutputColumns() {
		for _, sourceCol := range outputCol.SourceColumns {
			a.generateEdgeFromSource(sp, sourceCol, "", resultTableName, outputCol.Alias, outputCol.Transform)
		}
	}
}

// generateEdgesForDataModification generates lineage edges for data modification statements (INSERT, UPDATE, DELETE).
func (a *Analyzer) generateEdgesForDataModification(targetSchema, targetTable string, targetColumns []string) {
	sp := a.currentScope()
	outputColumns := sp.GetOutputColumns()

	for i, outputCol := range outputColumns {
		targetColName := outputCol.Alias
		if i < len(targetColumns) {
			targetColName = targetColumns[i]
		}

		for _, sourceCol := range outputCol.SourceColumns {
			a.generateEdgeFromSource(sp, sourceCol, targetSchema, targetTable, targetColName, outputCol.Transform)
		}
	}
}

// generateEdgeFromSource generates a lineage edge from a source column to a target.
// Handles tracing through temporary tables (CTEs and subqueries).
func (a *Analyzer) generateEdgeFromSource(sp *scope.Scope, sourceCol scope.ColumnRef, targetSchema, targetTable, targetColName string, transform []model.Transformation) {
	resolved, err := sp.ResolveColumn(sourceCol)
	if err != nil {
		return
	}

	// Check if the source is a temporary table (CTE or subquery)
	if tableRef, ok := sp.FindTable(resolved.Table); ok && (tableRef.IsCTE || tableRef.IsSubquery) {
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

		if nestedRef, ok := sp.FindTable(sourceTableName); ok && (nestedRef.IsCTE || nestedRef.IsSubquery) {
			a.appendFlattenedLineage(lineage, sp, nestedRef, edge.Source.Name, targetTable, targetColumn, combinedTransform)
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

// ---------------------------------------------------------------------------
// Scope management
// ---------------------------------------------------------------------------

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
	currentScope := a.scopeStack[len(a.scopeStack)-1]
	a.scopeStack = a.scopeStack[:len(a.scopeStack)-1]
	return currentScope
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
	for _, sp := range a.scopeStack {
		if _, ok := sp.FindCTE(tableName); ok {
			return true
		}
		if tableRef, ok := sp.FindTable(tableName); ok {
			return tableRef.IsSubquery || tableRef.IsCTE
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Edge management
// ---------------------------------------------------------------------------

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
