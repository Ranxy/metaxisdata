// Package mysql provides direct lineage analysis for MySQL queries.
//
// This implementation is built on github.com/bytebase/omni's MySQL parser and
// typed AST (see plan/mysql_omni_parser_migration_plan.md). It replaced the
// legacy ANTLR implementation, which was parity-verified against this one over
// the golden corpus and then removed.
package mysql

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"

	nodes "github.com/bytebase/omni/mysql/ast"
	mysqlparser "github.com/bytebase/omni/mysql/parser"
)

// Constants for special table/column markers.
const (
	resultTableName   = "__result__"
	deletionFieldName = "__deletion__"
	wildcardColumn    = "*"
	fileSourceMarker  = "__file__" // Special marker for LOAD DATA source
)

func init() {
	// MariaDB, TiDB and OceanBase are intentionally not registered here. They are
	// migrated separately (see plan/mysql_omni_parser_migration_plan.md), and
	// pointing omni's MySQL parser at their SQL is the divergence risk this
	// migration exists to avoid. Until those plans land the runner records a
	// deliberate "no lineage analyzer" skip for them.
	lineage.RegisterAnalyzeRelation(storepb.Engine_MYSQL, Analyze)
}

// Analyzer performs direct lineage analysis on MySQL queries.
type Analyzer struct {
	ctx context.Context
	sql string
	// tokens backs whitespace-free expression text reconstruction so that
	// transformation metadata matches the legacy ANTLR GetText() output.
	tokens []mysqlparser.Token
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
	inInsertReplaceContext bool
	// Track temporary table names (CTEs, subqueries) to filter intermediate results
	tempTables map[string]struct{}
}

// Analyze parses a single MySQL statement and returns its column relations.
func Analyze(ctx context.Context, sql string) ([]model.ColumnRelation, error) {
	return NewAnalyzer(ctx, sql, lineage.CatelogProvide).AnalyzeRelations()
}

// NewAnalyzer creates a new MySQL lineage analyzer.
func NewAnalyzer(ctx context.Context, sql string, catalogProvide catalog.Provide) *Analyzer {
	return &Analyzer{
		ctx:        ctx,
		sql:        sql,
		tokens:     mysqlparser.Tokenize(sql),
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
// Parsing is strict: a statement omni cannot parse is an error, never a partial
// result. Multi-statement input is rejected because the analyzer is defined for
// exactly one statement.
func (a *Analyzer) AnalyzeRelations() ([]model.ColumnRelation, error) {
	list, err := mysqlparser.Parse(a.sql)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse MySQL SQL")
	}
	if list.Len() != 1 {
		return nil, errors.Errorf("expected exactly 1 statement, got %d", list.Len())
	}

	switch stmt := list.Items[0].(type) {
	case *nodes.SelectStmt:
		a.processSelectStatement(stmt)
	case *nodes.InsertStmt:
		if stmt.IsReplace {
			a.processReplaceStatement(stmt)
		} else {
			a.processInsertStatement(stmt)
		}
	case *nodes.CreateTableStmt:
		a.processCreateTable(stmt)
	case *nodes.CreateViewStmt:
		a.processCreateView(stmt)
	case *nodes.UpdateStmt:
		a.processUpdateStatement(stmt)
	case *nodes.DeleteStmt:
		a.processDeleteStatement(stmt)
	case *nodes.LoadDataStmt:
		a.processLoadStatement(stmt)
	default:
		// Unsupported statement kinds produce no lineage, matching the legacy
		// analyzer which silently ignored them.
	}

	if len(a.errors) > 0 {
		return nil, errors.Errorf("analysis errors: %s", strings.Join(a.errors, "; "))
	}
	return a.edges, nil
}

// ---------------------------------------------------------------------------
// Source text
// ---------------------------------------------------------------------------

// nodeLoc reads the Loc field every omni AST node carries. omni exposes no
// generic location interface (Loc is a named field, not an embedded one), so
// reflection is the only option short of an exhaustive type switch.
func nodeLoc(n nodes.Node) nodes.Loc {
	if n == nil {
		return nodes.Loc{}
	}
	v := reflect.ValueOf(n)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nodes.Loc{}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nodes.Loc{}
	}
	f := v.FieldByName("Loc")
	if !f.IsValid() || f.Type() != reflect.TypeOf(nodes.Loc{}) {
		return nodes.Loc{}
	}
	loc, ok := f.Interface().(nodes.Loc)
	if !ok {
		return nodes.Loc{}
	}
	return loc
}

// exprText reconstructs an expression's source text with inter-token whitespace
// removed, mirroring ANTLR's GetText() token concatenation. Token slicing keeps
// whitespace inside string literals intact.
func (a *Analyzer) exprText(loc nodes.Loc) string {
	if loc.Start < 0 || loc.End > len(a.sql) || loc.Start >= loc.End {
		return ""
	}
	var b strings.Builder
	for i := range a.tokens {
		t := a.tokens[i]
		if t.Loc >= loc.End {
			break
		}
		if t.Loc >= loc.Start && t.End <= loc.End {
			_, _ = b.WriteString(a.sql[t.Loc:t.End])
		}
	}
	if b.Len() == 0 {
		return a.sql[loc.Start:loc.End]
	}
	return b.String()
}

// exprTextOf returns the reconstructed source text for a node.
func (a *Analyzer) exprTextOf(n nodes.Node) string {
	return a.exprText(nodeLoc(n))
}

// ---------------------------------------------------------------------------
// Temporary table tracking
// ---------------------------------------------------------------------------

// isTableTempInCurrentScope checks if a table is a CTE or subquery.
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

// ---------------------------------------------------------------------------
// SELECT
// ---------------------------------------------------------------------------

// processSelectStatement processes a SELECT statement, set operation or
// parenthesized query.
func (a *Analyzer) processSelectStatement(stmt *nodes.SelectStmt) {
	if stmt == nil {
		return
	}
	if len(stmt.CTEs) > 0 {
		a.processCTEs(stmt.CTEs)
	}
	switch {
	case stmt.SetOp != nodes.SetOpNone:
		a.processSetOperation(stmt)
	case stmt.ParenSource != nil:
		a.processSelectStatement(stmt.ParenSource)
	default:
		a.processQuerySpecification(stmt)
	}
}

// processQuerySpecification processes FROM, the SELECT list, then emits edges.
func (a *Analyzer) processQuerySpecification(stmt *nodes.SelectStmt) {
	sp := a.currentScope()
	a.processFromClause(stmt.From)
	a.processSelectItemList(stmt.TargetList, sp)
	a.generateEdges(sp)
}

// processCTEs processes a WITH clause.
func (a *Analyzer) processCTEs(ctes []*nodes.CommonTableExpr) {
	for _, cte := range ctes {
		a.processCTE(cte)
	}
}

// processCTE processes a single CTE.
func (a *Analyzer) processCTE(cte *nodes.CommonTableExpr) {
	if cte == nil {
		return
	}
	cteName := cte.Name
	a.markTempTable(cteName)

	var lineage []model.ColumnRelation
	if cte.Select != nil {
		a.pushScope()
		a.processSelectStatement(cte.Select)
		cteScope := a.popScope()

		for _, outputCol := range cteScope.GetOutputColumns() {
			for _, sourceCol := range outputCol.SourceColumns {
				resolved, err := cteScope.ResolveColumn(sourceCol)
				if err != nil {
					continue
				}
				if a.flattenTempSourceLineage(cteScope, resolved, cteName, outputCol.Alias, outputCol.Transform, &lineage) {
					continue
				}
				lineage = append(lineage, scope.NewLineageEdge(
					resolved.Schema, resolved.Table, resolved.Column,
					"", cteName, outputCol.Alias,
					outputCol.Transform,
					true, // CTE is temporary
				))
			}
		}
	}

	a.currentScope().AddCTE(&scope.CTEDefinition{
		Name:          cteName,
		Columns:       cte.Columns,
		DefiningScope: a.currentScope(),
		Lineage:       lineage,
	})
}

// flattenSetOpArms flattens a set-operation tree into its leaf SELECTs in order.
func flattenSetOpArms(stmt *nodes.SelectStmt) []*nodes.SelectStmt {
	if stmt == nil {
		return nil
	}
	if stmt.SetOp != nodes.SetOpNone {
		var out []*nodes.SelectStmt
		out = append(out, flattenSetOpArms(stmt.Left)...)
		out = append(out, flattenSetOpArms(stmt.Right)...)
		return out
	}
	if stmt.ParenSource != nil {
		return flattenSetOpArms(stmt.ParenSource)
	}
	return []*nodes.SelectStmt{stmt}
}

// processSetOperation handles UNION/INTERSECT/EXCEPT by processing every arm and
// merging their output columns positionally.
func (a *Analyzer) processSetOperation(stmt *nodes.SelectStmt) {
	arms := flattenSetOpArms(stmt)
	if len(arms) == 0 {
		return
	}

	baseScope := a.currentScope()
	var allOutputColumns [][]scope.OutputColumn

	for i, arm := range arms {
		if i == 0 {
			a.processSelectStatement(arm)
			allOutputColumns = append(allOutputColumns, baseScope.GetOutputColumns())
			continue
		}
		tempScope := scope.NewScope(baseScope.Parent())
		for _, cte := range baseScope.GetCTEs() {
			tempScope.AddCTE(cte)
		}
		originalScope := a.currentScope()
		a.scopeStack[len(a.scopeStack)-1] = tempScope
		a.processSelectStatement(arm)
		a.scopeStack[len(a.scopeStack)-1] = originalScope
		allOutputColumns = append(allOutputColumns, tempScope.GetOutputColumns())
	}

	a.mergeUnionOutputColumns(baseScope, allOutputColumns)
}

// mergeUnionOutputColumns merges output columns from multiple set-operation arms.
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
// FROM clause
// ---------------------------------------------------------------------------

// processFromClause processes the FROM clause.
func (a *Analyzer) processFromClause(from []nodes.TableExpr) {
	for _, te := range from {
		a.processTableExpr(te)
	}
}

// processTableExpr processes a table reference, join or derived table.
func (a *Analyzer) processTableExpr(te nodes.TableExpr) {
	switch t := te.(type) {
	case *nodes.TableRef:
		a.processSingleTableRef(t)
	case *nodes.JoinClause:
		a.processTableExpr(t.Left)
		a.processTableExpr(t.Right)
	case *nodes.SubqueryExpr:
		a.processDerivedTable(t)
	default:
		// Function-in-FROM and other table expressions carry no lineage.
	}
}

// processSingleTableRef adds a base table or CTE reference to the current scope.
func (a *Analyzer) processSingleTableRef(ref *nodes.TableRef) {
	if ref == nil {
		return
	}
	tableName := ref.Name
	alias := ref.Alias

	if cte, ok := a.currentScope().FindCTE(tableName); ok {
		a.currentScope().AddTable(&scope.TableRef{
			Table:   tableName,
			Alias:   alias,
			IsCTE:   true,
			Columns: cte.Columns,
			Lineage: cte.Lineage,
		})
		return
	}

	if alias == "" {
		alias = tableName
	}
	a.currentScope().AddTable(&scope.TableRef{
		Schema:  ref.Schema,
		Table:   tableName,
		Alias:   alias,
		Columns: []string{},
	})
}

// processDerivedTable processes a derived table (subquery in FROM).
func (a *Analyzer) processDerivedTable(sub *nodes.SubqueryExpr) {
	if sub == nil || sub.Select == nil {
		return
	}
	alias := sub.Alias
	a.markTempTable(alias)

	a.pushScope()
	a.processSelectStatement(sub.Select)
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
			lineage = append(lineage, scope.NewLineageEdge(
				resolved.Schema, resolved.Table, resolved.Column,
				"", alias, colName,
				col.Transform,
				true, // Subquery is temporary
			))
		}
	}

	a.currentScope().AddTable(&scope.TableRef{
		Table:      alias,
		Alias:      alias,
		IsSubquery: true,
		Columns:    columns,
		Lineage:    lineage,
	})
}

// ---------------------------------------------------------------------------
// SELECT list
// ---------------------------------------------------------------------------

// processSelectItemList processes the SELECT item list.
func (a *Analyzer) processSelectItemList(items []nodes.ExprNode, sp *scope.Scope) {
	for _, item := range items {
		switch it := item.(type) {
		case *nodes.StarExpr:
			a.processStar(sp)
		case *nodes.ResTarget:
			a.processSelectExpr(it.Val, it.Name, sp)
		case *nodes.ColumnRef:
			if it.Star {
				a.processTableWildcard(it, sp)
				continue
			}
			a.processSelectExpr(it, "", sp)
		default:
			a.processSelectExpr(item, "", sp)
		}
	}
}

// processStar expands SELECT * against every table in scope.
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
		})
	}
}

// processTableWildcard expands table.* against the named table.
func (a *Analyzer) processTableWildcard(cr *nodes.ColumnRef, sp *scope.Scope) {
	tableName := cr.Table
	if tableRef, ok := sp.FindTable(tableName); ok {
		if a.catalog != nil && !tableRef.IsSubquery && !tableRef.IsCTE {
			if a.expandWildcardWithCatalog(tableRef, sp) {
				return
			}
		}
		sp.AddOutputColumn(scope.OutputColumn{
			Alias:         wildcardColumn,
			Expression:    tableName + "." + wildcardColumn,
			SourceColumns: []scope.ColumnRef{{Schema: tableRef.Schema, Table: tableRef.Table, Column: wildcardColumn}},
		})
	}
}

// processSelectExpr turns one select expression into an output column.
func (a *Analyzer) processSelectExpr(expr nodes.ExprNode, alias string, sp *scope.Scope) {
	if expr == nil {
		return
	}
	exprText := a.exprTextOf(expr)
	if alias == "" {
		alias = inferColumnAlias(exprText)
	}
	sourceColumns := collectColumns(expr)
	isDerived := isExpressionDerivedText(exprText)

	// Special case: a derived expression with no column references (e.g. COUNT(*))
	// contributes a synthetic reference to every table in scope.
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
		outputCol.Transform = a.analyzeExpressionOperator(expr)
	}
	sp.AddOutputColumn(outputCol)
}

// ---------------------------------------------------------------------------
// Edge generation
// ---------------------------------------------------------------------------

// generateEdges creates ColumnRelation objects from the scope's output columns.
func (a *Analyzer) generateEdges(sp *scope.Scope) {
	if a.inInsertReplaceContext {
		return
	}
	// Only the root query emits edges to the final result; subqueries/CTEs rely
	// on their lineage being traced when the parent references them.
	if sp == nil || sp.Parent() != nil {
		return
	}

	for _, outputCol := range sp.GetOutputColumns() {
		for _, sourceCol := range outputCol.SourceColumns {
			resolved, err := sp.ResolveColumn(sourceCol)
			if err != nil {
				continue
			}
			if tableRef, ok := sp.FindTable(resolved.Table); ok && (tableRef.IsCTE || tableRef.IsSubquery) {
				a.traceThroughTableLineage(tableRef, resolved.Column, outputCol.Alias, outputCol.Transform)
				continue
			}
			a.addRelation(scope.NewLineageEdge(
				resolved.Schema, resolved.Table, resolved.Column,
				"", resultTableName, outputCol.Alias,
				outputCol.Transform,
				true, // __result__ is always temporary
			))
		}
	}
}

// generateEdgesForDataModification maps SELECT output columns onto the target
// table columns of an INSERT/REPLACE.
func (a *Analyzer) generateEdgesForDataModification(targetSchema, targetTable string, targetColumns []string) {
	sp := a.currentScope()
	for i, outputCol := range sp.GetOutputColumns() {
		targetColName := outputCol.Alias
		if i < len(targetColumns) {
			targetColName = targetColumns[i]
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
			a.addRelation(scope.NewLineageEdge(
				resolved.Schema, resolved.Table, resolved.Column,
				targetSchema, targetTable, targetColName,
				outputCol.Transform,
				isTemp,
			))
		}
	}
}

// traceThroughTableLineage traces lineage through a CTE or subquery to the final result.
func (a *Analyzer) traceThroughTableLineage(tableRef *scope.TableRef, columnName string, outputAlias string, transform []model.Transformation) {
	if a.inInsertReplaceContext {
		return
	}
	for _, edge := range tableRef.Lineage {
		if columnName != wildcardColumn && edge.Target.Name != columnName {
			continue
		}
		actualOutput := outputAlias
		if columnName == wildcardColumn && outputAlias == wildcardColumn {
			actualOutput = edge.Target.Name
		}
		a.addRelation(scope.NewLineageEdge(
			edge.Source.Table.Database, edge.Source.Table.Name, edge.Source.Name,
			"", resultTableName, actualOutput,
			combineTransformations(edge.Transformation, transform),
			true,
		))
	}
}

// traceThroughTableLineageToTarget traces lineage through a CTE or subquery to a specific target.
func (a *Analyzer) traceThroughTableLineageToTarget(tableRef *scope.TableRef, columnName string, targetSchema string, targetTable string, targetColumn string, transform []model.Transformation) {
	for _, edge := range tableRef.Lineage {
		if columnName != wildcardColumn && edge.Target.Name != columnName {
			continue
		}
		actualTargetColumn := targetColumn
		if columnName == wildcardColumn && targetColumn == wildcardColumn {
			actualTargetColumn = edge.Target.Name
		}
		isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetTable)
		a.addRelation(scope.NewLineageEdge(
			edge.Source.Table.Database, edge.Source.Table.Name, edge.Source.Name,
			targetSchema, targetTable, actualTargetColumn,
			combineTransformations(edge.Transformation, transform),
			isTemp,
		))
	}
}

// appendFlattenedLineage traces through nested temporary tables to real tables.
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
			a.appendFlattenedLineage(lineage, sp, nestedRef, edge.Source.Name, targetTable, actualTarget, combinedTransform)
			continue
		}
		*lineage = append(*lineage, scope.NewLineageEdge(
			edge.Source.Table.Database, sourceTableName, edge.Source.Name,
			"", targetTable, actualTarget,
			combinedTransform,
			true,
		))
	}
}

// flattenTempSourceLineage resolves a column from a temporary table into base
// table lineage. Returns true if handled.
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
			Table:   cte.Name,
			Alias:   cte.Name,
			IsCTE:   true,
			Columns: cte.Columns,
			Lineage: cte.Lineage,
		}
		a.appendFlattenedLineage(lineage, sp, tempRef, resolved.Column, targetTable, targetColumn, transform)
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// INSERT / REPLACE
// ---------------------------------------------------------------------------

// processInsertStatement processes INSERT statements.
func (a *Analyzer) processInsertStatement(stmt *nodes.InsertStmt) {
	if stmt == nil || stmt.Table == nil {
		return
	}
	targetTable := stmt.Table.Name
	targetSchema := stmt.Table.Schema
	targetColumns := columnNames(stmt.Columns)

	if stmt.Select != nil {
		a.inInsertReplaceContext = true
		a.processSelectStatement(stmt.Select)
		a.inInsertReplaceContext = false
	}

	a.generateEdgesForDataModification(targetSchema, targetTable, targetColumns)

	if len(stmt.OnDuplicateKey) > 0 {
		a.processInsertUpdateList(stmt.OnDuplicateKey, targetSchema, targetTable)
	}
}

// processReplaceStatement processes REPLACE statements, which behave like INSERT
// for lineage purposes.
func (a *Analyzer) processReplaceStatement(stmt *nodes.InsertStmt) {
	if stmt == nil || stmt.Table == nil {
		return
	}
	targetTable := stmt.Table.Name
	targetSchema := stmt.Table.Schema
	targetColumns := columnNames(stmt.Columns)

	if stmt.Select != nil {
		a.inInsertReplaceContext = true
		a.processSelectStatement(stmt.Select)
		a.inInsertReplaceContext = false
	}

	a.generateEdgesForDataModification(targetSchema, targetTable, targetColumns)
}

// processInsertUpdateList processes the ON DUPLICATE KEY UPDATE clause.
func (a *Analyzer) processInsertUpdateList(assignments []*nodes.Assignment, targetSchema, targetTable string) {
	sp := a.currentScope()
	for _, elem := range assignments {
		if elem == nil || elem.Column == nil {
			continue
		}
		targetCol := scope.ColumnRef{Table: elem.Column.Table, Column: elem.Column.Column}
		if targetCol.Table == "" {
			targetCol.Table = targetTable
		}

		var sourceColumns []scope.ColumnRef
		var transformInfo []model.Transformation
		if elem.Value != nil {
			sourceColumns = collectColumns(elem.Value)
			exprText := a.exprTextOf(elem.Value)
			if strings.Contains(strings.ToUpper(exprText), "VALUES(") || len(sourceColumns) > 0 {
				transformInfo = a.analyzeExpressionOperator(elem.Value)
			}
		}
		if len(sourceColumns) == 0 {
			sourceColumns = []scope.ColumnRef{{
				Schema: targetSchema,
				Table:  targetTable,
				Column: wildcardColumn,
			}}
			if elem.Value != nil {
				transformInfo = a.analyzeExpressionOperator(elem.Value)
			}
		}

		for _, sourceCol := range sourceColumns {
			resolvedSource := &sourceCol
			if sourceCol.Table == "" || sourceCol.Column != wildcardColumn {
				if resolved, err := sp.ResolveColumn(sourceCol); err == nil {
					resolvedSource = resolved
				}
			}
			isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetTable)
			a.addRelation(scope.NewLineageEdge(
				resolvedSource.Schema, resolvedSource.Table, resolvedSource.Column,
				targetSchema, targetTable, targetCol.Column,
				transformInfo,
				isTemp,
			))
		}
	}
}

// ---------------------------------------------------------------------------
// CREATE TABLE / VIEW
// ---------------------------------------------------------------------------

// processCreateTable processes CREATE TABLE ... AS SELECT.
func (a *Analyzer) processCreateTable(stmt *nodes.CreateTableStmt) {
	if stmt == nil || stmt.Table == nil || stmt.Select == nil {
		return
	}
	targetTable := stmt.Table.Name
	targetSchema := stmt.Table.Schema

	a.processSelectStatement(stmt.Select)

	sp := a.currentScope()
	for _, outputCol := range sp.GetOutputColumns() {
		targetColName := outputCol.Alias
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
			a.addRelation(scope.NewLineageEdge(
				resolved.Schema, resolved.Table, resolved.Column,
				targetSchema, targetTable, targetColName,
				outputCol.Transform,
				isTemp,
			))
		}
	}
}

// processCreateView processes CREATE VIEW.
func (a *Analyzer) processCreateView(stmt *nodes.CreateViewStmt) {
	if stmt == nil || stmt.Name == nil || stmt.Select == nil {
		return
	}
	targetView := stmt.Name.Name
	targetSchema := stmt.Name.Schema
	explicitColumnNames := stmt.Columns

	a.processSelectStatement(stmt.Select)

	sp := a.currentScope()
	for i, outputCol := range sp.GetOutputColumns() {
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
			a.addRelation(scope.NewLineageEdge(
				resolved.Schema, resolved.Table, resolved.Column,
				targetSchema, targetView, targetColName,
				outputCol.Transform,
				isTemp,
			))
		}
	}
}

// ---------------------------------------------------------------------------
// UPDATE
// ---------------------------------------------------------------------------

// processUpdateStatement processes UPDATE statements.
func (a *Analyzer) processUpdateStatement(stmt *nodes.UpdateStmt) {
	if stmt == nil {
		return
	}
	for _, te := range stmt.Tables {
		a.processTableExpr(te)
	}
	a.processUpdateList(stmt.SetList)
}

// processUpdateList processes the SET clause in UPDATE statements.
func (a *Analyzer) processUpdateList(assignments []*nodes.Assignment) {
	sp := a.currentScope()
	for _, elem := range assignments {
		if elem == nil || elem.Column == nil {
			continue
		}
		targetCol := scope.ColumnRef{Table: elem.Column.Table, Column: elem.Column.Column}
		resolved, err := sp.ResolveColumn(targetCol)
		if err != nil {
			continue
		}

		var sourceColumns []scope.ColumnRef
		var isDerived bool
		var transformInfo []model.Transformation
		if elem.Value != nil {
			sourceColumns = collectColumns(elem.Value)
			exprText := normalizeExpressionText(a.exprTextOf(elem.Value))
			isDerived = len(sourceColumns) != 1 || exprText != targetCol.Column
			if isDerived && exprText != "" {
				transformInfo = a.analyzeExpressionOperator(elem.Value)
			}
		}
		if len(sourceColumns) == 0 {
			sourceColumns = []scope.ColumnRef{{
				Schema: resolved.Schema,
				Table:  resolved.Table,
				Column: wildcardColumn,
			}}
			if elem.Value != nil {
				transformInfo = a.analyzeExpressionOperator(elem.Value)
			}
		}

		for _, sourceCol := range sourceColumns {
			resolvedSource, err := sp.ResolveColumn(sourceCol)
			if err != nil {
				resolvedSource = &sourceCol
			}
			isTemp := resolved.Table == resultTableName || a.isTableTempInCurrentScope(resolved.Table)
			a.addRelation(scope.NewLineageEdge(
				resolvedSource.Schema, resolvedSource.Table, resolvedSource.Column,
				resolved.Schema, resolved.Table, resolved.Column,
				transformInfo,
				isTemp,
			))
		}
	}
}

// ---------------------------------------------------------------------------
// DELETE
// ---------------------------------------------------------------------------

// processDeleteStatement processes DELETE statements (single and multi-table).
func (a *Analyzer) processDeleteStatement(stmt *nodes.DeleteStmt) {
	if stmt == nil {
		return
	}
	multi := len(stmt.Using) > 0
	for _, te := range stmt.Using {
		a.processTableExpr(te)
	}

	var targetTables []scope.TableRef
	for _, te := range stmt.Tables {
		tr, ok := te.(*nodes.TableRef)
		if !ok {
			continue
		}
		alias := tr.Alias
		if alias == "" && multi {
			// In a multi-table DELETE the target entry is the alias to delete.
			alias = tr.Name
		}
		targetTables = append(targetTables, scope.TableRef{Schema: tr.Schema, Table: tr.Name, Alias: alias})
	}

	if len(targetTables) == 0 {
		for _, tableRef := range a.currentScope().GetTables() {
			targetTables = append(targetTables, *tableRef)
			break
		}
	}

	if stmt.Where == nil {
		return
	}
	conditionColumns := collectColumns(stmt.Where)
	sp := a.currentScope()
	whereText := normalizeExpressionText(a.exprTextOf(stmt.Where))

	for _, targetTable := range targetTables {
		actualTargetTable := targetTable
		if targetTable.Alias != "" {
			if foundTable, ok := sp.FindTable(targetTable.Alias); ok {
				actualTargetTable = *foundTable
			}
		}
		for _, condCol := range conditionColumns {
			resolved, err := sp.ResolveColumn(condCol)
			if err != nil {
				resolved = &condCol
			}
			transform := []model.Transformation{
				model.NewDeleteTransformation(whereText),
			}
			if tableRef, ok := sp.FindTable(resolved.Table); ok && (tableRef.IsCTE || tableRef.IsSubquery) {
				a.traceThroughTableLineageToTarget(tableRef, resolved.Column, actualTargetTable.Schema, actualTargetTable.Table, deletionFieldName, transform)
				continue
			}
			if cte, ok := sp.FindCTE(resolved.Table); ok {
				tempRef := &scope.TableRef{
					Table:   cte.Name,
					Alias:   cte.Name,
					IsCTE:   true,
					Columns: cte.Columns,
					Lineage: cte.Lineage,
				}
				a.traceThroughTableLineageToTarget(tempRef, resolved.Column, actualTargetTable.Schema, actualTargetTable.Table, deletionFieldName, transform)
				continue
			}
			isTemp := actualTargetTable.Table == resultTableName || a.isTableTempInCurrentScope(actualTargetTable.Table)
			a.addRelation(scope.NewLineageEdge(
				resolved.Schema, resolved.Table, resolved.Column,
				actualTargetTable.Schema, actualTargetTable.Table, deletionFieldName,
				transform,
				isTemp,
			))
		}
	}
}

// ---------------------------------------------------------------------------
// LOAD DATA
// ---------------------------------------------------------------------------

// processLoadStatement processes LOAD DATA INFILE statements.
func (a *Analyzer) processLoadStatement(stmt *nodes.LoadDataStmt) {
	if stmt == nil || stmt.Table == nil {
		return
	}
	targetTable := stmt.Table.Name
	targetSchema := stmt.Table.Schema
	sourceFile := fileSourceMarker

	targetColumns := loadDataTargetColumns(stmt.Columns)
	if len(stmt.SetList) > 0 {
		a.processLoadDataSetClause(stmt.SetList, targetSchema, targetTable, targetColumns, sourceFile)
	}

	isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetTable)
	if len(targetColumns) == 0 {
		a.addRelation(scope.NewLineageEdge(
			"", sourceFile, wildcardColumn,
			targetSchema, targetTable, wildcardColumn,
			nil,
			isTemp,
		))
		return
	}
	for i, colName := range targetColumns {
		a.addRelation(scope.NewLineageEdge(
			"", sourceFile, fmt.Sprintf("col%d", i+1),
			targetSchema, targetTable, colName,
			nil,
			isTemp,
		))
	}
}

// processLoadDataSetClause processes the SET clause in LOAD DATA statements.
func (a *Analyzer) processLoadDataSetClause(assignments []*nodes.Assignment, targetSchema, targetTable string, loadedColumns []string, sourceFile string) {
	sp := a.currentScope()
	for _, elem := range assignments {
		if elem == nil || elem.Column == nil {
			continue
		}
		targetCol := scope.ColumnRef{Table: elem.Column.Table, Column: elem.Column.Column}

		var sourceColumns []scope.ColumnRef
		var transformInfo []model.Transformation
		if elem.Value != nil {
			sourceColumns = collectColumns(elem.Value)
			transformInfo = a.analyzeExpressionOperator(elem.Value)
		}
		if len(sourceColumns) == 0 {
			sourceColumns = []scope.ColumnRef{{
				Schema: "",
				Table:  sourceFile,
				Column: wildcardColumn,
			}}
		}

		for _, sourceCol := range sourceColumns {
			isFromFile := false
			for _, loadedCol := range loadedColumns {
				if sourceCol.Column == loadedCol {
					isFromFile = true
					break
				}
			}
			var fromSchema, fromTable, fromField string
			if isFromFile {
				fromTable = sourceFile
				fromField = sourceCol.Column
			} else if resolved, err := sp.ResolveColumn(sourceCol); err != nil {
				fromTable = sourceFile
				fromField = sourceCol.Column
			} else {
				fromSchema = resolved.Schema
				fromTable = resolved.Table
				fromField = resolved.Column
			}
			isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetTable)
			a.addRelation(scope.NewLineageEdge(
				fromSchema, fromTable, fromField,
				targetSchema, targetTable, targetCol.Column,
				transformInfo,
				isTemp,
			))
		}
	}
}

// ---------------------------------------------------------------------------
// Expressions
// ---------------------------------------------------------------------------

// collectColumns recursively collects column references from an expression.
func collectColumns(expr nodes.ExprNode) []scope.ColumnRef {
	columns := make([]scope.ColumnRef, 0)
	if expr == nil {
		return columns
	}
	nodes.Inspect(expr, func(n nodes.Node) bool {
		if cr, ok := n.(*nodes.ColumnRef); ok && cr.Column != "" {
			columns = append(columns, scope.ColumnRef{Table: cr.Table, Column: cr.Column})
		}
		return true
	})
	return columns
}

// isExpressionDerivedText reports whether an expression text implies a transformation.
func isExpressionDerivedText(text string) bool {
	upperText := strings.ToUpper(text)
	return strings.Contains(text, "(") ||
		strings.Contains(text, "+") ||
		strings.Contains(text, "-") ||
		strings.Contains(text, "*") ||
		strings.Contains(text, "/") ||
		strings.Contains(upperText, "CASE") ||
		strings.Contains(upperText, "WHEN")
}

// analyzeExpressionOperator identifies the operation kind of an expression and
// returns its transformation metadata.
func (a *Analyzer) analyzeExpressionOperator(expr nodes.ExprNode) []model.Transformation {
	if expr == nil {
		return nil
	}
	exprText := a.exprTextOf(expr)

	if aggInfo, ok := a.detectAggregateFunction(expr, exprText); ok {
		return []model.Transformation{aggInfo}
	}
	if windowInfo, ok := a.detectWindowFunction(expr, exprText); ok {
		return []model.Transformation{windowInfo}
	}
	if funcInfo, ok := a.detectFunctionCall(expr, exprText); ok {
		return []model.Transformation{funcInfo}
	}
	if caseInfo, ok := detectCaseExpression(exprText); ok {
		return []model.Transformation{caseInfo}
	}
	if opInfo, ok := detectOperatorExpression(exprText); ok {
		return []model.Transformation{opInfo}
	}
	return []model.Transformation{model.NewProjectTransformation(exprText)}
}

// firstFuncCall returns the first function call in pre-order, matching the legacy
// detector's depth-first search.
func firstFuncCall(expr nodes.ExprNode) *nodes.FuncCallExpr {
	var found *nodes.FuncCallExpr
	if expr == nil {
		return nil
	}
	nodes.Inspect(expr, func(n nodes.Node) bool {
		if found != nil {
			return false
		}
		if fc, ok := n.(*nodes.FuncCallExpr); ok {
			found = fc
			return false
		}
		return true
	})
	return found
}

// Common aggregate functions.
var aggregateFunctions = map[string]bool{
	"COUNT": true, "SUM": true, "AVG": true, "MAX": true, "MIN": true,
	"GROUP_CONCAT": true, "STD": true, "STDDEV": true, "STDDEV_POP": true,
	"STDDEV_SAMP": true, "VAR_POP": true, "VAR_SAMP": true, "VARIANCE": true,
}

// Common window functions.
var windowFunctions = map[string]bool{
	"ROW_NUMBER": true, "RANK": true, "DENSE_RANK": true, "NTILE": true,
	"LEAD": true, "LAG": true, "FIRST_VALUE": true, "LAST_VALUE": true,
	"NTH_VALUE": true, "CUME_DIST": true, "PERCENT_RANK": true,
}

// detectAggregateFunction checks if an expression is an aggregate function call.
func (*Analyzer) detectAggregateFunction(expr nodes.ExprNode, exprText string) (model.Transformation, bool) {
	fc := firstFuncCall(expr)
	if fc == nil {
		return model.Transformation{}, false
	}
	name := strings.ToUpper(fc.Name)
	if aggregateFunctions[name] {
		return createAggregateOperatorInfo(name, exprText, nil), true
	}
	return model.Transformation{}, false
}

// detectWindowFunction checks if an expression is a window function call.
func (a *Analyzer) detectWindowFunction(expr nodes.ExprNode, exprText string) (model.Transformation, bool) {
	fc := firstFuncCall(expr)
	if fc == nil || fc.Over == nil {
		return model.Transformation{}, false
	}
	name := strings.ToUpper(fc.Name)
	if windowFunctions[name] || aggregateFunctions[name] {
		partitionBy, orderBy := a.extractWindowClauses(fc)
		return createWindowOperatorInfo(name, exprText, partitionBy, orderBy), true
	}
	return model.Transformation{}, false
}

// detectFunctionCall checks if an expression is a scalar function call.
func (a *Analyzer) detectFunctionCall(expr nodes.ExprNode, exprText string) (model.Transformation, bool) {
	if strings.Contains(strings.ToUpper(exprText), "OVER") {
		return model.Transformation{}, false
	}
	fc := firstFuncCall(expr)
	if fc == nil || fc.Name == "" {
		return model.Transformation{}, false
	}
	args := make([]string, 0, len(fc.Args))
	for _, arg := range fc.Args {
		args = append(args, a.exprTextOf(arg))
	}
	return createFunctionOperatorInfo(fc.Name, exprText, args), true
}

// detectCaseExpression checks if an expression is a CASE expression.
func detectCaseExpression(exprText string) (model.Transformation, bool) {
	upper := strings.ToUpper(exprText)
	if strings.Contains(upper, "CASE") && strings.Contains(upper, "WHEN") {
		return createCaseOperatorInfo(exprText), true
	}
	return model.Transformation{}, false
}

// detectOperatorExpression checks if an expression uses arithmetic or comparison operators.
func detectOperatorExpression(exprText string) (model.Transformation, bool) {
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

// extractWindowClauses extracts PARTITION BY and ORDER BY from a window function.
func (a *Analyzer) extractWindowClauses(fc *nodes.FuncCallExpr) (partitionBy []string, orderBy []string) {
	if fc == nil || fc.Over == nil {
		return nil, nil
	}
	for _, expr := range fc.Over.PartitionBy {
		partitionBy = append(partitionBy, a.exprTextOf(expr))
	}
	for _, item := range fc.Over.OrderBy {
		if item != nil && item.Expr != nil {
			orderBy = append(orderBy, a.exprTextOf(item.Expr))
		}
	}
	return partitionBy, orderBy
}

// ---------------------------------------------------------------------------
// Transformation builders
// ---------------------------------------------------------------------------

// combineTransformations merges two transformation chains.
func combineTransformations(base, additional []model.Transformation) []model.Transformation {
	if len(base) == 0 {
		return additional
	}
	if len(additional) == 0 {
		return base
	}
	combined := make([]model.Transformation, len(base)+len(additional))
	copy(combined, base)
	copy(combined[len(base):], additional)
	return combined
}

func createFunctionOperatorInfo(functionName string, exprText string, args []string) model.Transformation {
	return model.NewFunctionTransformation(functionName, exprText, args)
}

func createAggregateOperatorInfo(functionName string, exprText string, groupKeys []string) model.Transformation {
	return model.NewAggregateTransformation(functionName, exprText, groupKeys)
}

func createOperatorExprInfo(opType string, exprText string) model.Transformation {
	return model.NewOperatorTransformation(opType, exprText)
}

func createCaseOperatorInfo(exprText string) model.Transformation {
	return model.NewCaseTransformation(exprText)
}

func createWindowOperatorInfo(functionName string, exprText string, partitionBy []string, orderBy []string) model.Transformation {
	return model.NewWindowTransformation(functionName, exprText, partitionBy, orderBy)
}

// ---------------------------------------------------------------------------
// Identifier helpers
// ---------------------------------------------------------------------------

// normalizeExpressionText removes spaces from expression text for consistency.
func normalizeExpressionText(text string) string {
	return strings.ReplaceAll(text, " ", "")
}

// normalizeIdentifier strips surrounding backticks or double quotes.
func normalizeIdentifier(text string) string {
	text = strings.TrimSpace(text)
	if len(text) < 2 {
		return text
	}
	quote := text[0]
	if (quote != '`' && quote != '"') || text[len(text)-1] != quote {
		return text
	}
	inner := text[1 : len(text)-1]
	escapedQuote := strings.Repeat(string(quote), 2)
	return strings.ReplaceAll(inner, escapedQuote, string(quote))
}

// splitQualifiedIdentifier splits a dotted identifier into normalized parts.
func splitQualifiedIdentifier(fullText string) []string {
	parts := strings.Split(fullText, ".")
	for i, part := range parts {
		parts[i] = normalizeIdentifier(part)
	}
	return parts
}

// inferColumnAlias infers an alias from an expression text.
func inferColumnAlias(exprText string) string {
	if strings.Contains(exprText, ".") && !strings.Contains(exprText, "(") {
		parts := splitQualifiedIdentifier(exprText)
		return parts[len(parts)-1]
	}
	if !strings.ContainsAny(exprText, "() +-*/%<>=,!?") {
		return normalizeIdentifier(exprText)
	}
	return exprText
}

// columnNames extracts column names from a column reference list.
func columnNames(refs []*nodes.ColumnRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if r != nil && r.Column != "" {
			out = append(out, r.Column)
		}
	}
	return out
}

// loadDataTargetColumns extracts LOAD DATA target columns. omni surfaces user
// variables (`@name`) as ColumnRefs in the target list; the legacy analyzer
// skipped them because the grammar keeps them in a separate rule, so they are
// filtered out to preserve behavior.
func loadDataTargetColumns(refs []*nodes.ColumnRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if r != nil && r.Column != "" && !strings.HasPrefix(r.Column, "@") {
			out = append(out, r.Column)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Scope stack and edge accumulation
// ---------------------------------------------------------------------------

// pushScope creates and pushes a new scope onto the stack.
func (a *Analyzer) pushScope() {
	a.scopeStack = append(a.scopeStack, scope.NewScope(a.currentScope()))
}

// popScope removes and returns the top scope from the stack.
func (a *Analyzer) popScope() *scope.Scope {
	if len(a.scopeStack) == 0 {
		return nil
	}
	top := a.scopeStack[len(a.scopeStack)-1]
	a.scopeStack = a.scopeStack[:len(a.scopeStack)-1]
	return top
}

// currentScope returns the current (top) scope.
func (a *Analyzer) currentScope() *scope.Scope {
	if len(a.scopeStack) == 0 {
		return nil
	}
	return a.scopeStack[len(a.scopeStack)-1]
}

// addRelation adds a column relation, avoiding duplicates.
func (a *Analyzer) addRelation(relation model.ColumnRelation) {
	if a.isTempTable(relation.Source.Table.Name) {
		return
	}
	if a.isTempTable(relation.Target.Table.Name) && relation.Target.Table.Name != resultTableName {
		return
	}
	signature := fmt.Sprintf("%s.%s.%s->%s.%s.%s",
		relation.Source.Table.Schema, relation.Source.Table.Name, relation.Source.Name,
		relation.Target.Table.Schema, relation.Target.Table.Name, relation.Target.Name)
	if _, exists := a.edgeSet[signature]; exists {
		return
	}
	a.edgeSet[signature] = struct{}{}
	a.edges = append(a.edges, relation)
}

// expandWildcardWithCatalog expands a wildcard using catalog metadata.
func (a *Analyzer) expandWildcardWithCatalog(tableRef *scope.TableRef, sp *scope.Scope) bool {
	tableID := model.ObjectIdentifier{
		Database: tableRef.Schema,
		Name:     tableRef.Table,
	}
	tableMeta, err := a.catalog.GetTable(a.ctx, tableID)
	if err != nil || tableMeta == nil {
		return false
	}
	for _, colMeta := range tableMeta.Columns {
		sp.AddOutputColumn(scope.OutputColumn{
			Alias:      colMeta.Name,
			Expression: tableRef.Table + "." + colMeta.Name,
			SourceColumns: []scope.ColumnRef{{
				Schema: tableRef.Schema,
				Table:  tableRef.Table,
				Column: colMeta.Name,
			}},
		})
	}
	return true
}
