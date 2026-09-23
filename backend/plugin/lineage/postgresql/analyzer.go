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
	// excludedRelationName is PostgreSQL's ON CONFLICT pseudo-relation holding
	// the proposed row. It is not a metadata-registry object, so edges sourced
	// from it can never resolve; the real lineage is already emitted from the
	// INSERT source.
	excludedRelationName = "excluded"
)

// PostgreSQL aggregate functions. Window detection is structural (FuncCall.Over),
// so no window-function name set is needed.
var aggregateFunctions = map[string]bool{
	"COUNT": true, "SUM": true, "AVG": true, "MAX": true, "MIN": true,
	"ARRAY_AGG": true, "STRING_AGG": true,
}

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
	// realTarget is set while analyzing the query of a statement that writes to a
	// real object (INSERT, CREATE VIEW, CREATE TABLE AS, CREATE MATERIALIZED
	// VIEW), so the query result edges are not emitted alongside the real target.
	realTarget bool
	// inSetOpArm is set while analyzing one arm of a set operation, so only the
	// merged set-operation result emits edges.
	inSetOpArm bool
	// Track temporary table names (CTEs, subqueries) to filter intermediate results
	tempTables map[string]struct{}
}

func Analyze(ctx context.Context, sql string) ([]model.ColumnRelation, error) {
	analyzer := NewAnalyzer(ctx, sql, lineage.GetCatalogProvide())
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
// shared analyzer state, so a multi-statement script is not treated as one query (notably for
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
		// Unsupported statement kinds produce no lineage.
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

// flattenSetOpArms flattens a UNION/INTERSECT/EXCEPT tree into its leaf SELECTs
// in order, so every arm of every set operation contributes lineage.
func flattenSetOpArms(stmt *pgast.SelectStmt) []*pgast.SelectStmt {
	if stmt == nil {
		return nil
	}
	if stmt.Op != pgast.SETOP_NONE {
		return append(flattenSetOpArms(stmt.Larg), flattenSetOpArms(stmt.Rarg)...)
	}
	return []*pgast.SelectStmt{stmt}
}

// processSetOperation processes UNION/INTERSECT/EXCEPT: the first operand is
// analyzed in the base scope, every later operand in a temporary scope parented
// to the base scope's parent.
func (a *Analyzer) processSetOperation(stmt *pgast.SelectStmt) {
	arms := flattenSetOpArms(stmt)
	baseScope := a.currentScope()
	var allOutputColumns [][]scope.OutputColumn

	for i, arm := range arms {
		if i == 0 {
			a.processSetOpArm(arm)
			allOutputColumns = append(allOutputColumns, resolveOutputColumns(baseScope, baseScope.GetOutputColumns()))
			continue
		}

		tempScope := scope.NewScope(baseScope.Parent())
		for _, cte := range baseScope.CTEs() {
			tempScope.AddCTE(cte)
		}

		originalScope := a.currentScope()
		a.scopeStack[len(a.scopeStack)-1] = tempScope
		a.processSetOpArm(arm)
		a.scopeStack[len(a.scopeStack)-1] = originalScope

		allOutputColumns = append(allOutputColumns, resolveOutputColumns(tempScope, tempScope.GetOutputColumns()))
	}

	a.mergeUnionOutputColumns(baseScope, allOutputColumns, stmt.Op)
	// An arm does not emit result edges on its own; the merged operation does.
	a.generateEdges(baseScope)
}

// processSetOpArm processes a single set-operation operand.
func (a *Analyzer) processSetOpArm(arm *pgast.SelectStmt) {
	previous := a.inSetOpArm
	a.inSetOpArm = true
	defer func() { a.inSetOpArm = previous }()

	if arm == nil {
		return
	}
	if arm.WithClause != nil {
		a.processWithClause(arm.WithClause)
	}
	a.processSelectCore(arm)
}

// mergeUnionOutputColumns merges output columns from multiple set-operation arms
// positionally and records the set operation as the leading transformation of
// every merged column, which is what makes the relation type union/intersect/
// except instead of direct.
func (*Analyzer) mergeUnionOutputColumns(baseScope *scope.Scope, allOutputColumns [][]scope.OutputColumn, setOp pgast.SetOperation) {
	if len(allOutputColumns) == 0 || len(allOutputColumns[0]) == 0 {
		return
	}

	transform, hasTransform := setOpTransformation(setOp)
	firstQueryOutputs := allOutputColumns[0]

	for colIdx := 0; colIdx < len(firstQueryOutputs); colIdx++ {
		firstCol := firstQueryOutputs[colIdx]

		var mergedSources []scope.ColumnRef
		for queryIdx := 0; queryIdx < len(allOutputColumns); queryIdx++ {
			if colIdx < len(allOutputColumns[queryIdx]) {
				mergedSources = append(mergedSources, allOutputColumns[queryIdx][colIdx].SourceColumns...)
			}
		}

		firstCol.SourceColumns = mergedSources
		if hasTransform {
			firstCol.Transform = append([]model.Transformation{transform}, firstCol.Transform...)
		}
		baseScope.SetOutputColumn(colIdx, firstCol)
	}
}

// setOpTransformation maps a PostgreSQL set-operation kind to its transformation.
func setOpTransformation(setOp pgast.SetOperation) (model.Transformation, bool) {
	switch setOp {
	case pgast.SETOP_UNION:
		return model.NewUnionTransformation(), true
	case pgast.SETOP_INTERSECT:
		return model.NewIntersectTransformation(), true
	case pgast.SETOP_EXCEPT:
		return model.NewExceptTransformation(), true
	default:
		return model.Transformation{}, false
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
				resolutions, err := cteScope.ResolveColumnRefs(sourceCol)
				if err != nil {
					continue
				}
				for _, res := range resolutions {
					if a.flattenTempSourceLineage(cteScope, res.Relation, res.Ref.Column, cteName, targetColumn, outputCol.Transform, &lineage) {
						continue
					}

					lineage = append(lineage, NewLineageEdge(
						res.Ref.Schema,
						res.Ref.Table,
						res.Ref.Column,
						"",
						cteName,
						targetColumn,
						outputCol.Transform,
						true,
					))
				}
			}
		}
	}

	a.currentScope().AddCTE(&scope.CTEDefinition{
		Name:    cteName,
		Columns: columns,
		Lineage: lineage,
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
		// this analyzer as well.
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

	// A CTE is query-local and is never qualified, so a qualified reference
	// always names a real relation: `s1.t` must not be mistaken for a CTE named t.
	if rangeVar.Schemaname == "" {
		if cte, ok := a.currentScope().FindCTE(tableName); ok {
			tableRef := &scope.TableRef{
				Schema:     "",
				Table:      tableName,
				Alias:      alias,
				IsSubquery: false,
				IsCTE:      true,
				Lineage:    cte.Lineage,
			}
			attachTempColumnLookup(tableRef, cte.Columns)
			a.currentScope().AddTable(tableRef)
			return
		}
	}

	tableRef := &scope.TableRef{
		Schema:     rangeVar.Schemaname,
		Table:      tableName,
		Alias:      alias,
		IsSubquery: false,
		IsCTE:      false,
	}
	a.attachColumnLookup(tableRef)
	a.currentScope().AddTable(tableRef)
}

// attachTempColumnLookup lets the scope resolver treat a CTE or derived table as
// a relation whose columns are known, so an unqualified name it owns resolves
// through its own lineage instead of being guessed from name order. declared is
// the CTE's column list when the query wrote one, or the names the query's output
// exposes; a nil list falls back to the targets the lineage carries.
func attachTempColumnLookup(tableRef *scope.TableRef, declared []string) {
	if names := tempColumnNames(declared, tableRef.Lineage); names != nil {
		tableRef.SetColumnLookup(func() []string { return names })
	}
}

// tempColumnNames reports the columns a temporary relation exposes, or nil when
// one of them cannot be named. A wildcard the catalog did not expand, and an
// output without a name, both leave the list incomplete; an incomplete list is
// reported as unknown so the resolver keeps its fallback for the relation.
func tempColumnNames(declared []string, lineage []model.ColumnRelation) []string {
	names := declared
	if len(names) == 0 {
		seen := make(map[string]struct{}, len(lineage))
		for _, edge := range lineage {
			if _, ok := seen[edge.Target.Name]; ok {
				continue
			}
			seen[edge.Target.Name] = struct{}{}
			names = append(names, edge.Target.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	for _, name := range names {
		if name == "" || name == wildcardColumn {
			return nil
		}
	}
	return names
}

// outputColumnAliases lists the names a query's output exposes, which is the
// column list a derived table offers to the statement that selects from it.
func outputColumnAliases(cols []scope.OutputColumn) []string {
	names := make([]string, 0, len(cols))
	for _, col := range cols {
		names = append(names, col.Alias)
	}
	return names
}

// attachColumnLookup lets the scope resolver disambiguate an unqualified column
// with catalog metadata for this base table, wherever the reference is resolved:
// the statement itself, a CTE body, a derived table or an expression subquery.
func (a *Analyzer) attachColumnLookup(tableRef *scope.TableRef) {
	if a.catalog == nil {
		return
	}
	var (
		names  []string
		loaded bool
	)
	tableRef.SetColumnLookup(func() []string {
		if loaded {
			return names
		}
		loaded = true
		meta, err := a.catalog.GetTable(a.ctx, model.ObjectIdentifier{Schema: tableRef.Schema, Name: tableRef.Table})
		if err != nil || meta == nil {
			return nil
		}
		names = make([]string, 0, len(meta.Columns))
		for _, col := range meta.Columns {
			names = append(names, col.Name)
		}
		return names
	})
}

// addTargetRelation registers a data-modification target relation in the scope.
// Unlike processRangeVar it does not resolve CTEs.
func (a *Analyzer) addTargetRelation(rangeVar *pgast.RangeVar) {
	if rangeVar == nil {
		return
	}
	alias := ""
	if rangeVar.Alias != nil {
		alias = rangeVar.Alias.Aliasname
	}
	tableRef := &scope.TableRef{
		Schema:     rangeVar.Schemaname,
		Table:      rangeVar.Relname,
		Alias:      alias,
		IsSubquery: false,
		IsCTE:      false,
	}
	a.attachColumnLookup(tableRef)
	a.currentScope().AddTable(tableRef)
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

	lineage := make([]model.ColumnRelation, 0)

	for _, col := range subqueryScope.GetOutputColumns() {
		colName := col.Alias
		if colName == "" {
			colName = "column"
		}

		for _, sourceCol := range col.SourceColumns {
			resolutions, err := subqueryScope.ResolveColumnRefs(sourceCol)
			if err != nil {
				continue
			}
			for _, res := range resolutions {
				if a.flattenTempSourceLineage(subqueryScope, res.Relation, res.Ref.Column, alias, colName, col.Transform, &lineage) {
					continue
				}

				lineage = append(lineage, NewLineageEdge(
					res.Ref.Schema,
					res.Ref.Table,
					res.Ref.Column,
					"",
					alias,
					colName,
					col.Transform,
					true,
				))
			}
		}
	}

	tableRef := &scope.TableRef{
		Schema:     "",
		Table:      alias,
		Alias:      alias,
		IsSubquery: true,
		IsCTE:      false,
		Lineage:    lineage,
	}
	attachTempColumnLookup(tableRef, outputColumnAliases(subqueryScope.GetOutputColumns()))
	a.currentScope().AddTable(tableRef)
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
			// A bare column reference is a direct projection.
			colRef := a.columnRefFromFields(cr.Fields)
			sp.AddOutputColumn(scope.OutputColumn{
				Alias:         colRef.Column,
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
	for _, tableRef := range sp.Tables() {
		if a.catalog != nil && !tableRef.IsSubquery && !tableRef.IsCTE {
			if a.expandWildcardWithCatalog(tableRef, sp) {
				continue
			}
		}

		sp.AddOutputColumn(scope.OutputColumn{
			Alias:         wildcardColumn,
			SourceColumns: []scope.ColumnRef{wildcardSourceRef(tableRef)},
			IsDerived:     false,
		})
	}
}

// processTableStar expands `table.*` against a single relation in scope.
func (a *Analyzer) processTableStar(cr *pgast.ColumnRef, sp *scope.Scope) {
	colRef := a.columnRefFromFields(cr.Fields)
	tableRef, ok := sp.FindRelation(scope.RelationKey{Qualifier: colRef.Schema, Name: colRef.Table})
	if !ok {
		return
	}
	if a.catalog != nil && !tableRef.IsSubquery && !tableRef.IsCTE {
		if a.expandWildcardWithCatalog(tableRef, sp) {
			return
		}
	}
	sp.AddOutputColumn(scope.OutputColumn{
		Alias:         wildcardColumn,
		SourceColumns: []scope.ColumnRef{wildcardSourceRef(tableRef)},
		IsDerived:     false,
	})
}

// wildcardSourceRef builds the source reference for a wildcard. It is marked
// resolved because the scope is keyed by alias while the reference carries the
// real table name, so resolving it again by name would fail and drop the edge.
func wildcardSourceRef(tableRef *scope.TableRef) scope.ColumnRef {
	return scope.ColumnRef{
		Schema:   tableRef.Schema,
		Table:    tableRef.Table,
		Column:   wildcardColumn,
		Resolved: true,
	}
}

// processExpressionTarget processes an expression/aliased target element
// (an aliased or derived projection).
func (a *Analyzer) processExpressionTarget(rt *pgast.ResTarget, sp *scope.Scope) {
	exprText := a.exprTextOf(rt.Val)
	alias := rt.Name
	if alias == "" {
		alias = a.inferColumnAlias(exprText)
	}

	sourceColumns := a.extractColumnsFromNode(rt.Val, sp)
	isDerived := isExpressionDerived(rt.Val)

	// A source-less expression is attributed to the whole relation only when it is
	// a function call (COUNT(*), now(), …). Casts/arrays/rows of constants have no
	// source table, so they must not fabricate a `table.*` edge.
	if isDerived && len(sourceColumns) == 0 && isTableWideExpression(rt.Val) {
		for _, tableRef := range sp.Tables() {
			sourceColumns = append(sourceColumns, wildcardSourceRef(tableRef))
		}
	}

	outputCol := scope.OutputColumn{
		Alias:         alias,
		SourceColumns: sourceColumns,
		IsDerived:     isDerived,
	}

	if isDerived {
		if transform, ok := a.classifyExpression(rt.Val); ok {
			outputCol.Transform = []model.Transformation{transform}
		}
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
		previous := a.realTarget
		a.realTarget = true
		if sel, ok := stmt.SelectStmt.(*pgast.SelectStmt); ok {
			a.processSelectStmt(sel)
		}
		a.realTarget = previous
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
		a.processOnConflictSetList(onConflict.TargetList, targetSchema, targetTable)
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

// processSetClauseList processes UPDATE SET assignment targets.
func (a *Analyzer) processSetClauseList(assignments *pgast.List, targetSchema, targetTable string) {
	a.processAssignments(assignments, targetSchema, targetTable, false)
}

// processOnConflictSetList processes ON CONFLICT DO UPDATE SET assignment
// targets, dropping columns sourced from the EXCLUDED pseudo-relation (see
// excludedRelationName).
func (a *Analyzer) processOnConflictSetList(assignments *pgast.List, targetSchema, targetTable string) {
	a.processAssignments(assignments, targetSchema, targetTable, true)
}

// processAssignments resolves assignment targets. When skipExcluded is set,
// source columns qualified by the EXCLUDED pseudo-relation are dropped.
func (a *Analyzer) processAssignments(assignments *pgast.List, targetSchema, targetTable string, skipExcluded bool) {
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
			sourceColumns = a.extractColumnsFromNode(rt.Val, currentScope)
			if transform, ok := a.classifyExpression(rt.Val); ok {
				transformInfo = []model.Transformation{transform}
			}
		}

		if len(sourceColumns) == 0 {
			sourceColumns = []scope.ColumnRef{{
				Schema: targetSchema,
				Table:  targetTable,
				Column: wildcardColumn,
			}}
		}

		for _, sourceCol := range sourceColumns {
			if skipExcluded && strings.EqualFold(sourceCol.Table, excludedRelationName) {
				continue
			}

			resolutions, err := currentScope.ResolveColumnRefs(sourceCol)
			if err != nil {
				resolutions = []scope.ResolvedColumn{{Ref: sourceCol}}
			}

			isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetSchema, targetTable)
			for _, res := range resolutions {
				a.addRelation(NewLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					targetSchema, targetTable, targetColumn,
					transformInfo,
					isTemp,
				))
			}
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
		conditionColumns := a.extractColumnsFromNode(stmt.WhereClause, a.currentScope())
		conditionText := normalizeExpressionText(a.exprTextOf(stmt.WhereClause))
		sp := a.currentScope()

		for _, condCol := range conditionColumns {
			resolutions, err := sp.ResolveColumnRefs(condCol)
			if err != nil {
				resolutions = []scope.ResolvedColumn{{Ref: condCol}}
			}

			transform := []model.Transformation{
				model.NewDeleteTransformation(conditionText),
			}

			isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetSchema, targetTable)
			for _, res := range resolutions {
				if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
					a.traceThroughTableLineageToTarget(res.Relation, res.Ref.Column, targetSchema, targetTable, deletionFieldName, transform)
					continue
				}
				a.addRelation(NewLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					targetSchema, targetTable, deletionFieldName,
					transform,
					isTemp,
				))
			}
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
		previous := a.realTarget
		a.realTarget = true
		a.processSelectStmt(sel)
		a.realTarget = previous
	}

	sp := a.currentScope()
	outputColumns := sp.GetOutputColumns()

	for i, outputCol := range outputColumns {
		targetColName := outputCol.Alias
		if i < len(explicitColumnNames) {
			targetColName = explicitColumnNames[i]
		}

		for _, sourceCol := range outputCol.SourceColumns {
			resolutions, err := sp.ResolveColumnRefs(sourceCol)
			if err != nil {
				continue
			}
			isTemp := targetView == resultTableName || a.isTableTempInCurrentScope(targetSchema, targetView)
			for _, res := range resolutions {
				if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
					a.traceThroughTableLineageToTarget(res.Relation, res.Ref.Column, targetSchema, targetView, targetColName, outputCol.Transform)
					continue
				}
				a.addRelation(NewLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					targetSchema, targetView, targetColName,
					outputCol.Transform,
					isTemp,
				))
			}
		}
	}
}

// processCreateTableAsStmt processes CREATE TABLE AS, CREATE MATERIALIZED VIEW and
// SELECT INTO. SELECT INTO is analyzed as a bare SELECT: its target object is
// not resolved, so the query's output columns are reported against the result.
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
		// Only a materialized view honors an explicit column list here.
		if stmt.Objtype == pgast.OBJECT_MATVIEW {
			explicitColumnNames = stringList(stmt.Into.ColNames)
		}
	}

	if sel, ok := stmt.Query.(*pgast.SelectStmt); ok {
		previous := a.realTarget
		a.realTarget = true
		a.processSelectStmt(sel)
		a.realTarget = previous
	}

	sp := a.currentScope()
	outputColumns := sp.GetOutputColumns()

	for i, outputCol := range outputColumns {
		targetColName := outputCol.Alias
		if i < len(explicitColumnNames) {
			targetColName = explicitColumnNames[i]
		}

		for _, sourceCol := range outputCol.SourceColumns {
			resolutions, err := sp.ResolveColumnRefs(sourceCol)
			if err != nil {
				continue
			}
			isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetSchema, targetTable)
			for _, res := range resolutions {
				if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
					a.traceThroughTableLineageToTarget(res.Relation, res.Ref.Column, targetSchema, targetTable, targetColName, outputCol.Transform)
					continue
				}
				a.addRelation(NewLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					targetSchema, targetTable, targetColName,
					outputCol.Transform,
					isTemp,
				))
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Edge generation
// ---------------------------------------------------------------------------

// generateEdges generates lineage edges for SELECT query results.
// Only generates edges for the root scope of a SELECT that has no real target
// and is not an arm of a set operation.
func (a *Analyzer) generateEdges(sp *scope.Scope) {
	if a.realTarget || a.inSetOpArm {
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
	resolutions, err := sp.ResolveColumnRefs(sourceCol)
	if err != nil {
		return
	}

	for _, res := range resolutions {
		// A CTE or derived table contributes the lineage of its own columns:
		// there is no stored relation to point an edge at.
		if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
			if targetTable == resultTableName {
				a.traceThroughTableLineage(res.Relation, res.Ref.Column, targetColName, transform)
			} else {
				a.traceThroughTableLineageToTarget(res.Relation, res.Ref.Column, targetSchema, targetTable, targetColName, transform)
			}
			continue
		}

		// Create direct relation from source to target
		isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetSchema, targetTable)
		relation := NewLineageEdge(
			res.Ref.Schema, res.Ref.Table, res.Ref.Column,
			targetSchema, targetTable, targetColName,
			transform,
			isTemp,
		)
		a.addRelation(relation)
	}
}

// traceThroughTableLineage traces lineage through a temporary table (CTE/subquery) to the result table.
func (a *Analyzer) traceThroughTableLineage(tableRef *scope.TableRef, columnName string, outputAlias string, transform []model.Transformation) {
	if a.realTarget {
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
		isTemp := targetTable == resultTableName || a.isTableTempInCurrentScope(targetSchema, targetTable)

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
func (a *Analyzer) flattenTempSourceLineage(sp *scope.Scope, relation *scope.TableRef, columnName, targetTable string, targetColumn string, transform []model.Transformation, lineage *[]model.ColumnRelation) bool {
	if relation == nil || (!relation.IsSubquery && !relation.IsCTE) {
		return false
	}
	a.appendFlattenedLineage(lineage, sp, relation, columnName, targetTable, targetColumn, transform)
	return true
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

		if nestedRef, ok := sp.FindRelation(scope.RelationKeyOf(edge.Source.Table)); ok && (nestedRef.IsCTE || nestedRef.IsSubquery) {
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

// isTempRelation reports whether an endpoint names a query-local relation (a CTE
// or a derived table). Those are never qualified, so a qualified reference to a
// real relation of the same name is not one.
func (a *Analyzer) isTempRelation(id model.ObjectIdentifier) bool {
	key := scope.RelationKeyOf(id)
	if key.Qualifier != "" {
		return false
	}
	_, ok := a.tempTables[key.Name]
	return ok
}

// isTableTempInCurrentScope checks if a relation is a temporary table (CTE or
// subquery) in any scope. A qualified relation is a real one.
func (a *Analyzer) isTableTempInCurrentScope(qualifier, tableName string) bool {
	if qualifier != "" {
		return false
	}
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
	if a.isTempRelation(relation.Source.Table) {
		return
	}
	// Skip if target is a temporary table (except for the result table)
	if a.isTempRelation(relation.Target.Table) && relation.Target.Table.Name != resultTableName {
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
			Alias: colMeta.Name,
			SourceColumns: []scope.ColumnRef{{
				Schema: tableRef.Schema,
				Table:  tableRef.Table,
				Column: colMeta.Name,
				// The catalog identified the real column, so the reference must not
				// be rebound by name (which fails for an aliased relation).
				Resolved: true,
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
