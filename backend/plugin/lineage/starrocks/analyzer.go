// Package starrocks provides direct lineage analysis for StarRocks queries.
//
// This implementation is built on github.com/bytebase/omni's StarRocks parser
// and typed AST, mirroring backend/plugin/lineage/mysql. It is a port rather
// than a dialect copy: omni's StarRocks AST differs structurally from its MySQL
// AST (set operations are a separate node, the select list is []*SelectItem,
// and derived tables, CTAS and expression subqueries keep their query as raw
// text).
//
// Engine registration is deliberately scoped to STARROCKS. DORIS keeps
// resolving to lineage.ErrorEngineNotSupported, which the runner records as a
// deliberate per-object skip; see plan/starrocks_lineage_plan.md.
//
// A statement kind the analyzer cannot model yet (MERGE) returns an explicit
// error instead of a partial or empty result.
package starrocks

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/algorithm"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"

	nodes "github.com/bytebase/omni/starrocks/ast"
	starrocksparser "github.com/bytebase/omni/starrocks/parser"
)

// Special table/column markers shared with the other analyzers.
const (
	resultTableName   = model.ResultTableName
	deletionFieldName = "__deletion__"
	wildcardColumn    = model.WildcardColumn
	fileSourceMarker  = "__file__" // source marker for COPY INTO / LOAD
)

func init() {
	lineage.RegisterAnalyzeRelation(storepb.Engine_STARROCKS, Analyze)
}

// Analyzer performs direct lineage analysis on one StarRocks statement.
type Analyzer struct {
	ctx context.Context
	sql string
	// sources is the text/token stack for the statement being analyzed; see
	// rawsql.go.
	sources []source
	// scopeStack tracks lexical scopes, innermost last.
	scopeStack []*scope.Scope
	// edges is the accumulated, deduplicated lineage.
	edges *algorithm.EdgeSet
	// errors collects analysis failures. A non-empty list fails the whole
	// analysis: a partial result is never returned.
	errors []string
	// catalog optionally expands wildcards and resolves metadata.
	catalog catalog.Provide
	// inTargetContext is set while the query body of a statement that maps its
	// output columns onto an explicit object (CREATE VIEW / MATERIALIZED VIEW /
	// TABLE ... AS, INSERT ... SELECT) is analyzed. Those columns must not also
	// be emitted against __result__.
	inTargetContext bool
	// inSetOpArm is set while analyzing one arm of a set operation, so only the
	// merged set-operation result emits edges.
	inSetOpArm bool
	// influences holds the row-set influences collected while a scope was
	// current. They belong to the rows that scope produces and are inherited by
	// whatever consumes them, so a scope whose rows reach no output cannot
	// influence the statement's result.
	influences *algorithm.Influences
}

// Analyze parses a single StarRocks statement and returns its column relations.
func Analyze(ctx context.Context, sql string) ([]model.ColumnRelation, error) {
	return NewAnalyzer(ctx, sql, lineage.GetCatalogProvide()).AnalyzeRelations()
}

// NewAnalyzer creates a StarRocks lineage analyzer for a single statement.
func NewAnalyzer(ctx context.Context, sql string, catalogProvide catalog.Provide) *Analyzer {
	return &Analyzer{
		ctx:        ctx,
		sql:        sql,
		sources:    []source{newSource(sql)},
		scopeStack: []*scope.Scope{scope.NewScope(nil)}, // root scope
		edges:      algorithm.NewEdgeSet(),
		errors:     make([]string, 0),
		catalog:    catalogProvide,
		influences: algorithm.NewInfluences(),
	}
}

// AnalyzeRelations parses the SQL and returns its column relations.
//
// Parsing is strict: a statement omni cannot parse is an error, never a partial
// result. Multi-statement input is rejected because the analyzer is defined for
// exactly one statement.
func (a *Analyzer) AnalyzeRelations() ([]model.ColumnRelation, error) {
	if segments := starrocksparser.Split(a.sql); len(segments) != 1 {
		return nil, errors.Errorf("expected exactly 1 statement, got %d", len(segments))
	}

	file, errs := starrocksparser.Parse(a.sql)
	if len(errs) > 0 {
		// omni's CREATE VIEW grammar accepts only a plain SELECT body (finding
		// F2). A WITH / set-operation / parenthesized body is analyzed from the
		// extracted query instead; anything else is still a hard failure.
		if ddl, ok := extractViewDDL(a.sql); ok {
			a.processViewDDL(ddl)
			if len(a.errors) > 0 {
				return nil, errors.Errorf("analysis errors: %s", strings.Join(a.errors, "; "))
			}
			return a.edges.Edges(), nil
		}
		return nil, errors.Wrap(&errs[0], "failed to parse StarRocks SQL")
	}
	if file == nil || len(file.Stmts) != 1 {
		return nil, errors.New("expected exactly 1 statement")
	}

	a.dispatch(file.Stmts[0])

	if len(a.errors) > 0 {
		return nil, errors.Errorf("analysis errors: %s", strings.Join(a.errors, "; "))
	}
	return a.edges.Edges(), nil
}

// dispatch routes a parsed statement to its analyzer.
func (a *Analyzer) dispatch(stmt nodes.Node) {
	switch s := stmt.(type) {
	case *nodes.SelectStmt:
		a.processSelectStatement(s)
	case *nodes.SetOpStmt:
		a.processSetOperation(s)
	case *nodes.ParenSelect:
		a.processQueryNode(s)
	case *nodes.CreateViewStmt:
		a.processDDLTarget(s.Name, viewColumnNames(s.Columns), s.Query)
	case *nodes.AlterViewStmt:
		a.processDDLTarget(s.Name, viewColumnNames(s.Columns), s.Query)
	case *nodes.CreateMTMVStmt:
		a.processDDLTarget(s.Name, viewColumnNames(s.Columns), s.Query)
	case *nodes.CreateTableStmt:
		a.processCreateTable(s)
	case *nodes.InsertStmt:
		a.processInsertStatement(s)
	case *nodes.UpdateStmt:
		a.processUpdateStatement(s)
	case *nodes.DeleteStmt:
		a.processDeleteStatement(s)
	case *nodes.CopyIntoStmt:
		a.processCopyInto(s)
	case *nodes.LoadDataStmt:
		a.processLoadStatement(s)
	case *nodes.MergeStmt:
		// MERGE carries column lineage this analyzer does not model yet;
		// failing loudly keeps it from looking like a statement with none.
		a.unsupported("MERGE")
	default:
		// Statement kinds that carry no lineage are ignored, as in the MySQL
		// analyzer.
	}
}

// unsupported records a statement kind this phase cannot analyze yet. It is an
// error rather than a silent empty result, so an unimplemented shape is never
// mistaken for "this statement has no lineage".
func (a *Analyzer) unsupported(what string) {
	a.errors = append(a.errors, fmt.Sprintf("%s analysis is not implemented yet", what))
}

// ---------------------------------------------------------------------------
// SELECT
// ---------------------------------------------------------------------------

// processSelectStatement processes a SELECT statement.
func (a *Analyzer) processSelectStatement(stmt *nodes.SelectStmt) {
	if stmt == nil {
		return
	}
	if stmt.With != nil {
		a.processCTEs(stmt.With)
	}
	a.processQuerySpecification(stmt)
}

// processQueryNode dispatches a query expression: a SELECT, a set operation or
// a parenthesized query. Unlike the MySQL analyzer, omni models set operations
// and parenthesized queries as their own nodes rather than as fields of
// SelectStmt.
func (a *Analyzer) processQueryNode(node nodes.Node) {
	switch n := node.(type) {
	case *nodes.SelectStmt:
		a.processSelectStatement(n)
	case *nodes.SetOpStmt:
		a.processSetOperation(n)
	case *nodes.ParenSelect:
		a.processQueryNode(n.Sel)
	default:
		a.unsupported("query expression")
	}
}

// processQuerySpecification processes FROM, then the SELECT list, then emits
// edges.
func (a *Analyzer) processQuerySpecification(stmt *nodes.SelectStmt) {
	sp := a.currentScope()
	a.processFromClause(stmt.From)
	// A WHERE or HAVING predicate decides which rows the query emits without its
	// value reaching any output column, so it is recorded as an influence on the
	// statement's target rows rather than on a column.
	if stmt.Where != nil {
		a.collectPredicates(stmt.Where, sp, model.NewFilterTransformation(a.exprTextOf(stmt.Where)), false)
	}
	a.processSelectItemList(stmt.Items, sp, a.groupByKeys(stmt.GroupBy))
	// HAVING is the one clause that may name a select-list alias, so it resolves
	// one before falling back to the scope.
	if stmt.Having != nil {
		a.collectPredicates(stmt.Having, sp, model.NewFilterTransformation(a.exprTextOf(stmt.Having)), true)
	}
	a.generateEdges(sp)
}

// ---------------------------------------------------------------------------
// FROM clause
// ---------------------------------------------------------------------------

// processFromClause processes the FROM clause.
func (a *Analyzer) processFromClause(from []nodes.Node) {
	for _, te := range from {
		a.processTableExpr(te)
		// The relations have to be in scope before a join condition's columns
		// can be resolved.
		a.collectJoinPredicates(te)
	}
}

// processTableExpr processes a table reference or join.
func (a *Analyzer) processTableExpr(te nodes.Node) {
	switch t := te.(type) {
	case *nodes.TableRef:
		a.processSingleTableRef(t)
	case *nodes.JoinClause:
		a.processTableExpr(t.Left)
		a.processTableExpr(t.Right)
	default:
		// Inline tables and table functions reference no physical table.
	}
}

// processSingleTableRef adds a base table reference to the current scope.
func (a *Analyzer) processSingleTableRef(ref *nodes.TableRef) {
	if ref == nil {
		return
	}
	if ref.Subquery != nil {
		a.processDerivedTable(ref)
		return
	}
	schema, table, ok := tableRefFromObjectName(ref.Name)
	if !ok {
		return
	}
	alias := ref.Alias
	if alias == "" {
		alias = table
	}
	if schema == "" {
		if cte, ok := a.currentScope().FindCTE(table); ok {
			tableRef := &scope.TableRef{
				Table:   table,
				Alias:   alias,
				IsCTE:   true,
				Lineage: cte.Lineage,
			}
			attachTempColumnLookup(tableRef, cte.Columns)
			a.currentScope().AddTable(tableRef)
			// Reading the CTE's rows carries the predicates that shaped them.
			a.influences.InheritCTE(a.currentScope(), cte)
			return
		}
	}
	a.addBaseTable(ref.Name, ref.Alias)
}

// addBaseTable registers a base-table reference in the current scope, keyed by
// alias (or by its own name when there is none). An unusable name is ignored.
func (a *Analyzer) addBaseTable(name *nodes.ObjectName, alias string) {
	schema, table, ok := tableRefFromObjectName(name)
	if !ok {
		return
	}
	if alias == "" {
		alias = table
	}
	tableRef := &scope.TableRef{
		Schema: schema,
		Table:  table,
		Alias:  alias,
	}
	a.attachColumnLookup(tableRef)
	a.currentScope().AddTable(tableRef)
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
		meta, err := a.catalog.GetTable(a.ctx, model.ObjectIdentifier{Database: tableRef.Schema, Name: tableRef.Table})
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

// processDerivedTable processes a derived table (a subquery in FROM). omni
// keeps the subquery body as raw text, so it is re-parsed with a matching
// source pushed; the alias is registered as a temporary table carrying the
// subquery's lineage.
func (a *Analyzer) processDerivedTable(ref *nodes.TableRef) {
	sub := ref.Subquery
	if sub == nil || strings.TrimSpace(sub.RawText) == "" {
		return
	}
	alias := ref.Alias

	subqueryScope := a.analyzeRawQueryScope(sub.RawText, fmt.Sprintf("derived table %q", alias))
	if subqueryScope == nil {
		return
	}
	// The derived table's rows are part of the enclosing query's rows, so the
	// predicates that shaped them reach its output.
	a.influences.Inherit(subqueryScope, a.currentScope())

	lineage := a.tempTableLineage(subqueryScope, alias)
	tableRef := &scope.TableRef{
		Table:      alias,
		Alias:      alias,
		IsSubquery: true,
		Lineage:    lineage,
	}
	attachTempColumnLookup(tableRef, outputColumnAliases(subqueryScope.GetOutputColumns()))
	a.currentScope().AddTable(tableRef)
}

// analyzeRawQueryScope parses and analyzes a query body that omni exposes only
// as text (a derived table, a CTAS body or an expression subquery) in a fresh
// scope and source. It returns nil, after recording the failure, when the body
// does not parse.
func (a *Analyzer) analyzeRawQueryScope(raw string, what string) *scope.Scope {
	a.pushSource(newSource(raw))
	query, err := parseRawQuery(raw)
	if err != nil {
		a.popSource()
		a.errors = append(a.errors, fmt.Sprintf("%s: %v", what, err))
		return nil
	}
	a.pushScope()
	a.processQueryNode(query)
	subScope := a.popScope()
	a.popSource()
	return subScope
}

// tempTableLineage builds the base-table lineage a temporary table (a CTE or
// derived table) exposes under targetName.
func (a *Analyzer) tempTableLineage(sp *scope.Scope, targetName string) []model.ColumnRelation {
	lineage := make([]model.ColumnRelation, 0)
	for _, col := range sp.GetOutputColumns() {
		colName := col.Alias
		if colName == "" {
			colName = "column"
		}
		for _, source := range col.Sources {
			resolutions, err := sp.ResolveColumnRefs(source.Ref)
			if err != nil {
				continue
			}
			for _, res := range resolutions {
				if a.flattenTempSourceLineage(sp, res.Relation, res.Ref.Column, targetName, colName, source.Transform, &lineage) {
					continue
				}
				lineage = append(lineage, scope.NewLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					"", targetName, colName,
					source.Transform,
					true, // the temporary table is not a real object
				))
			}
		}
	}
	return lineage
}

// ---------------------------------------------------------------------------
// DDL targets
// ---------------------------------------------------------------------------

// processDDLTarget analyzes the query body of a CREATE/ALTER VIEW or CREATE
// MATERIALIZED VIEW and maps its output columns onto the created object.
func (a *Analyzer) processDDLTarget(name *nodes.ObjectName, columns []string, query nodes.Node) {
	if name == nil || query == nil {
		return
	}
	schema, table, ok := tableRefFromObjectName(name)
	if !ok {
		return
	}
	a.inTargetContext = true
	a.processQueryNode(query)
	a.inTargetContext = false

	a.generateEdgesForTarget(a.currentScope(), schema, table, columns)
}

// processViewDDL is processDDLTarget for a view statement whose body omni
// could not parse in place; the body was extracted by extractViewDDL and is
// analyzed as a top-level query in its own scope.
func (a *Analyzer) processViewDDL(ddl *viewDDL) {
	sp := a.analyzeRawQueryScope(ddl.body, "view query")
	if sp == nil {
		return
	}
	a.generateEdgesForTarget(sp, ddl.schema, ddl.name, ddl.columns)
}

// processCreateTable processes CREATE TABLE ... AS SELECT. omni keeps the query
// as raw text, so it is re-parsed like a derived table's body.
func (a *Analyzer) processCreateTable(stmt *nodes.CreateTableStmt) {
	if stmt == nil || stmt.AsSelect == nil {
		return
	}
	schema, table, ok := tableRefFromObjectName(stmt.Name)
	if !ok {
		return
	}
	raw := strings.TrimSpace(stmt.AsSelect.RawText)
	if raw == "" {
		return
	}
	sp := a.analyzeRawQueryScope(raw, "CREATE TABLE AS SELECT")
	if sp == nil {
		return
	}
	a.generateEdgesForTarget(sp, schema, table, stmt.CTASColumns)
}

// generateEdgesForTarget maps a scope's output columns onto the columns of the
// object being created. A column list declared on the statement wins over the
// query's own output alias.
func (a *Analyzer) generateEdgesForTarget(sp *scope.Scope, targetSchema, targetTable string, targetColumns []string) {
	if sp == nil {
		return
	}
	for i, outputCol := range sp.GetOutputColumns() {
		targetColName := outputCol.Alias
		if i < len(targetColumns) {
			targetColName = targetColumns[i]
		}
		for _, source := range outputCol.Sources {
			resolutions, err := sp.ResolveColumnRefs(source.Ref)
			if err != nil {
				continue
			}
			isTemp := targetTable == resultTableName
			for _, res := range resolutions {
				if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
					a.traceThroughTableLineageToTarget(res.Relation, res.Ref.Column, targetSchema, targetTable, targetColName, source.Transform)
					continue
				}
				a.addRelation(scope.NewLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					targetSchema, targetTable, targetColName,
					source.Transform,
					isTemp,
				))
			}
		}
	}
	a.emitPredicateInfluences(sp, targetSchema, targetTable)
}

// viewColumnNames extracts the declared column names of a view or materialized
// view statement.
func viewColumnNames(cols []*nodes.ViewColumn) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		if c != nil && c.Name != "" {
			out = append(out, c.Name)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// INSERT / UPDATE / DELETE
// ---------------------------------------------------------------------------

// processInsertStatement processes INSERT [OVERWRITE] INTO ... SELECT and
// INSERT ... BY NAME. The VALUES form carries only literals, so it has no
// lineage.
func (a *Analyzer) processInsertStatement(stmt *nodes.InsertStmt) {
	if stmt == nil || stmt.Target == nil {
		return
	}
	schema, table, ok := tableRefFromObjectName(stmt.Target)
	if !ok {
		return
	}

	targetColumns := stmt.Columns
	if stmt.ByName {
		// BY NAME matches the source column names to the target's; without the
		// target's catalog metadata the source alias is the closest available
		// approximation, so the positional list is dropped.
		targetColumns = nil
	}

	if stmt.Query != nil {
		a.inTargetContext = true
		a.processQueryNode(stmt.Query)
		a.inTargetContext = false
	}

	a.generateEdgesForTarget(a.currentScope(), schema, table, targetColumns)
}

// processUpdateStatement processes UPDATE ... SET. The target table is also a
// readable relation, because an assignment may read the row it writes.
func (a *Analyzer) processUpdateStatement(stmt *nodes.UpdateStmt) {
	if stmt == nil || stmt.Target == nil {
		return
	}
	schema, table, ok := tableRefFromObjectName(stmt.Target)
	if !ok {
		return
	}
	if stmt.With != nil {
		a.processCTEs(stmt.With)
	}
	a.addBaseTable(stmt.Target, stmt.TargetAlias)
	for _, te := range stmt.From {
		a.processTableExpr(te)
	}
	a.processUpdateList(stmt.Assignments, schema, table)
}

// processUpdateList processes the SET clause of an UPDATE. Every assignment
// writes a column of the updated table: StarRocks does not accept a qualified SET
// target (`UPDATE t SET s.a = …` is a syntax error, verified on 4.1), so the target
// comes from the statement rather than from the scope — resolving the name would
// pick whichever relation the FROM clause introduced.
func (a *Analyzer) processUpdateList(assignments []*nodes.Assignment, targetSchema, targetTable string) {
	sp := a.currentScope()
	isTemp := targetTable == resultTableName
	for _, elem := range assignments {
		if elem == nil || elem.Column == nil {
			continue
		}
		targetCol, ok := columnRefFromObjectName(elem.Column)
		if !ok {
			continue
		}
		// The assigned column still has to belong to a relation in scope:
		// StarRocks rejects `UPDATE t SET nosuchcol = …` with "Column 'nosuchcol'
		// cannot be resolved" (verified on 4.1). With the updated table registered
		// that check is the scope's; the target itself is the statement's, below.
		if _, err := sp.ResolveColumn(targetCol); err != nil {
			continue
		}

		var sourceColumns []scope.ColumnRef
		var transformInfo []model.Transformation
		if elem.Value != nil {
			sourceColumns = collectColumns(elem.Value)
			sourceColumns = append(sourceColumns, a.expressionSubquerySources(elem.Value, sp)...)
			exprText := normalizeExpressionText(a.exprTextOf(elem.Value))
			isDerived := len(sourceColumns) != 1 || exprText != targetCol.Column
			if isDerived && exprText != "" {
				transformInfo = a.analyzeExpressionOperator(elem.Value)
			}
		}
		if len(sourceColumns) == 0 {
			// A literal assignment still depends on the row it overwrites.
			sourceColumns = []scope.ColumnRef{{
				Schema: targetSchema, Table: targetTable, Column: wildcardColumn, Resolved: true,
			}}
			if elem.Value != nil {
				transformInfo = a.analyzeExpressionOperator(elem.Value)
			}
		}

		for _, sourceCol := range sourceColumns {
			resolutions, err := sp.ResolveColumnRefs(sourceCol)
			if err != nil {
				continue
			}
			for _, res := range resolutions {
				if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
					a.traceThroughTableLineageToTarget(res.Relation, res.Ref.Column, targetSchema, targetTable, targetCol.Column, transformInfo)
					continue
				}
				a.addRelation(scope.NewLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					targetSchema, targetTable, targetCol.Column,
					transformInfo,
					isTemp,
				))
			}
		}
	}
}

// processDeleteStatement processes DELETE ... [USING ...] WHERE. The WHERE
// columns are what determines which rows are removed, so they become
// __deletion__ edges on the target table.
func (a *Analyzer) processDeleteStatement(stmt *nodes.DeleteStmt) {
	if stmt == nil || stmt.Target == nil {
		return
	}
	if stmt.With != nil {
		a.processCTEs(stmt.With)
	}
	for _, te := range stmt.Using {
		a.processTableExpr(te)
	}
	a.addBaseTable(stmt.Target, stmt.TargetAlias)

	if stmt.Where == nil {
		return
	}
	schema, table, ok := tableRefFromObjectName(stmt.Target)
	if !ok {
		return
	}
	// A USING clause may re-introduce the target under an alias; the alias is
	// then the identity the WHERE resolves through.
	if stmt.TargetAlias != "" {
		if ref, ok := a.currentScope().FindTable(stmt.TargetAlias); ok {
			schema, table = ref.Schema, ref.Table
		}
	}

	sp := a.currentScope()
	conditionColumns := collectColumns(stmt.Where)
	conditionColumns = append(conditionColumns, a.expressionSubquerySources(stmt.Where, sp)...)
	transform := []model.Transformation{model.NewDeleteTransformation(normalizeExpressionText(a.exprTextOf(stmt.Where)))}

	for _, condCol := range conditionColumns {
		resolutions, err := sp.ResolveColumnRefs(condCol)
		if err != nil {
			resolutions = []scope.ResolvedColumn{{Ref: condCol}}
		}
		isTemp := table == resultTableName
		for _, res := range resolutions {
			if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
				a.traceThroughTableLineageToTarget(res.Relation, res.Ref.Column, schema, table, deletionFieldName, transform)
				continue
			}
			a.addRelation(scope.NewLineageEdge(
				res.Ref.Schema, res.Ref.Table, res.Ref.Column,
				schema, table, deletionFieldName,
				transform,
				isTemp,
			))
		}
	}
}

// processCopyInto processes COPY INTO <table> FROM <stage>. The staged files'
// shape is not described by the statement, so the whole file is the source.
func (a *Analyzer) processCopyInto(stmt *nodes.CopyIntoStmt) {
	if stmt == nil || stmt.Target == nil {
		return
	}
	schema, table, ok := tableRefFromObjectName(stmt.Target)
	if !ok {
		return
	}
	isTemp := table == resultTableName
	a.addRelation(scope.NewLineageEdge(
		"", fileSourceMarker, wildcardColumn,
		schema, table, wildcardColumn,
		nil,
		isTemp,
	))
}

// processLoadStatement processes LOAD LABEL ... (DATA INFILE ... INTO TABLE).
// Each data description names its own target and optional column list; a
// description without one loads the whole file into the table. The SET clause
// is captured only as raw text by omni, so it contributes no column lineage.
func (a *Analyzer) processLoadStatement(stmt *nodes.LoadDataStmt) {
	if stmt == nil {
		return
	}
	for _, desc := range stmt.DataDescs {
		if desc == nil || desc.Target == nil {
			continue
		}
		schema, table, ok := tableRefFromObjectName(desc.Target)
		if !ok {
			continue
		}
		isTemp := table == resultTableName
		if len(desc.ColumnList) == 0 {
			a.addRelation(scope.NewLineageEdge(
				"", fileSourceMarker, wildcardColumn,
				schema, table, wildcardColumn,
				nil,
				isTemp,
			))
			continue
		}
		for i, colName := range desc.ColumnList {
			a.addRelation(scope.NewLineageEdge(
				"", fileSourceMarker, fmt.Sprintf("col%d", i+1),
				schema, table, colName,
				nil,
				isTemp,
			))
		}
	}
}

// ---------------------------------------------------------------------------
// SELECT list
// ---------------------------------------------------------------------------

// processSelectItemList processes the SELECT item list.
func (a *Analyzer) processSelectItemList(items []*nodes.SelectItem, sp *scope.Scope, groupKeys []string) {
	for _, item := range items {
		if item == nil {
			continue
		}
		switch {
		case item.Star && item.TableName == nil:
			a.processStar(sp, item.ExceptColumns)
		case item.Star:
			a.processTableWildcard(item, sp)
		default:
			a.processSelectExpr(item.Expr, item.Alias, item.Aliased, sp, groupKeys)
		}
	}
}

// processStar expands SELECT * against every table in scope.
func (a *Analyzer) processStar(sp *scope.Scope, except []string) {
	for _, tableRef := range sp.Tables() {
		if a.catalog != nil && !tableRef.IsSubquery && !tableRef.IsCTE {
			if a.expandWildcardWithCatalog(tableRef, sp, except) {
				continue
			}
		}
		sp.AddOutputColumn(scope.OutputColumn{
			Alias:   wildcardColumn,
			Sources: scope.NewColumnSources([]scope.ColumnRef{wildcardSourceRef(tableRef)}, nil),
		})
	}
}

// processTableWildcard expands table.* against the named table.
func (a *Analyzer) processTableWildcard(item *nodes.SelectItem, sp *scope.Scope) {
	if item.TableName == nil || len(item.TableName.Parts) == 0 {
		return
	}
	schema, tableName, ok := tableRefFromObjectName(item.TableName)
	if !ok {
		return
	}
	tableRef, ok := sp.FindRelation(scope.RelationKey{Qualifier: schema, Name: tableName})
	if !ok {
		return
	}
	if a.catalog != nil && !tableRef.IsSubquery && !tableRef.IsCTE {
		if a.expandWildcardWithCatalog(tableRef, sp, item.ExceptColumns) {
			return
		}
	}
	sp.AddOutputColumn(scope.OutputColumn{
		Alias:   wildcardColumn,
		Sources: scope.NewColumnSources([]scope.ColumnRef{wildcardSourceRef(tableRef)}, nil),
	})
}

// wildcardSourceRef builds the source reference for a `*` expansion of one FROM
// relation.
//
// A base table is already fully identified here, so the reference is marked
// resolved: looking it up again by name would fail whenever the relation has an
// alias, which silently produced no lineage at all for `SELECT * FROM t x`.
// (The MySQL analyzer still has that gap.) A CTE or derived table is instead
// looked up by its scope key so the temp-table trace can find it.
func wildcardSourceRef(tableRef *scope.TableRef) scope.ColumnRef {
	if tableRef.IsSubquery || tableRef.IsCTE {
		key := tableRef.Alias
		if key == "" {
			key = tableRef.Table
		}
		return scope.ColumnRef{Table: key, Column: wildcardColumn}
	}
	return scope.ColumnRef{Schema: tableRef.Schema, Table: tableRef.Table, Column: wildcardColumn, Resolved: true}
}

// processSelectExpr turns one select expression into an output column. aliased
// records whether the SQL carried an explicit alias, so an explicit empty alias
// is not replaced by an inferred one.
func (a *Analyzer) processSelectExpr(expr nodes.Node, alias string, aliased bool, sp *scope.Scope, groupKeys []string) {
	if expr == nil {
		return
	}
	exprText := a.exprTextOf(expr)
	if !aliased && alias == "" {
		alias = inferColumnAlias(exprText)
	}
	sourceColumns := collectColumns(expr)
	sourceColumns = append(sourceColumns, a.expressionSubquerySources(expr, sp)...)
	isDerived := !isPlainColumnRef(expr)

	// A table-wide aggregate such as COUNT(*) depends on the rows of every
	// relation in scope even though it names no column. Any other source-less
	// expression (a literal, NOW(), a cast of a constant) depends on no column
	// at all and must not invent a dependency.
	if isDerived && len(sourceColumns) == 0 && containsAggregateCall(expr) {
		for _, tableRef := range sp.Tables() {
			sourceColumns = append(sourceColumns, wildcardSourceRef(tableRef))
		}
	}

	outputCol := scope.OutputColumn{
		Alias:     alias,
		Sources:   scope.NewColumnSources(sourceColumns, nil),
		IsDerived: isDerived,
	}
	if isDerived {
		transform := a.analyzeExpressionOperator(expr)
		// The keys are attached to every transformation of a select item that
		// contains a group aggregate, so the field never claims a column was
		// aggregated when it was only projected.
		if len(groupKeys) > 0 && containsGroupAggregate(expr) {
			for i := range transform {
				transform[i].GroupKeys = groupKeys
			}
		}
		outputCol.SetTransform(transform)
	}
	sp.AddOutputColumn(outputCol)
}

// expressionSubquerySources analyzes the subqueries embedded in a select
// expression. omni models a scalar / IN / EXISTS subquery as a leaf carrying
// only raw text, so each is re-parsed in its own scope and flattened to
// base-table references. Those references are marked resolved, because the
// table they resolved to lives in the subquery's scope and the enclosing query
// resolves its output columns in a different one.
func (a *Analyzer) expressionSubquerySources(expr nodes.Node, sp *scope.Scope) []scope.ColumnRef {
	var subqueries []*nodes.SubqueryExpr
	nodes.Inspect(expr, func(n nodes.Node) bool {
		sub, ok := n.(*nodes.SubqueryExpr)
		if !ok {
			return true
		}
		subqueries = append(subqueries, sub)
		return false // a SubqueryExpr is a leaf; its body is raw text
	})
	if len(subqueries) == 0 {
		return nil
	}

	refs := make([]scope.ColumnRef, 0)
	for i, sub := range subqueries {
		if strings.TrimSpace(sub.RawText) == "" {
			continue
		}
		subScope := a.analyzeRawQueryScope(sub.RawText, "expression subquery")
		if subScope == nil {
			continue
		}
		// The subquery's rows decide which rows the enclosing expression sees,
		// so its influences belong to the enclosing scope.
		a.influences.Inherit(subScope, sp)
		synthetic := fmt.Sprintf("__subquery_%d__", i)
		lineage := a.tempTableLineage(subScope, synthetic)
		for _, edge := range lineage {
			refs = append(refs, scope.ColumnRef{
				Schema:   edge.Source.Table.Database,
				Table:    edge.Source.Table.Name,
				Column:   edge.Source.Name,
				Resolved: true,
			})
		}
	}
	return refs
}

// ---------------------------------------------------------------------------
// Edge generation
// ---------------------------------------------------------------------------

// generateEdges creates ColumnRelation objects from the scope's output columns.
// Only the root query emits edges to the final result; a DDL body does not (its
// columns are mapped onto the created object instead) and neither does a set
// operation arm (the merged operation emits them once).
func (a *Analyzer) generateEdges(sp *scope.Scope) {
	if a.inTargetContext || a.inSetOpArm || sp == nil || sp.Parent() != nil {
		return
	}
	for _, outputCol := range sp.GetOutputColumns() {
		for _, source := range outputCol.Sources {
			resolutions, err := sp.ResolveColumnRefs(source.Ref)
			if err != nil {
				continue
			}
			for _, res := range resolutions {
				if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
					a.traceThroughTableLineage(res.Relation, res.Ref.Column, outputCol.Alias, source.Transform)
					continue
				}
				a.addRelation(scope.NewLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					"", resultTableName, outputCol.Alias,
					source.Transform,
					true, // __result__ is always temporary
				))
			}
		}
	}
	a.emitPredicateInfluences(sp, "", resultTableName)
}

// addRelation adds a column relation, dropping exact duplicates. A query-local
// relation (a CTE or a derived table) is never an endpoint here: every path that
// resolves one traces through its own lineage first, so the edge already names
// the stored relation the column came from.
func (a *Analyzer) addRelation(relation model.ColumnRelation) {
	a.edges.Add(relation)
}

// expandWildcardWithCatalog expands a wildcard using catalog metadata, omitting
// any column named in except. It reports false when no metadata is available so
// the caller falls back to a bulk wildcard edge.
func (a *Analyzer) expandWildcardWithCatalog(tableRef *scope.TableRef, sp *scope.Scope, except []string) bool {
	tableID := model.ObjectIdentifier{
		Database: tableRef.Schema,
		Name:     tableRef.Table,
	}
	tableMeta, err := a.catalog.GetTable(a.ctx, tableID)
	if err != nil || tableMeta == nil {
		return false
	}
	excluded := make(map[string]struct{}, len(except))
	for _, name := range except {
		excluded[strings.ToLower(name)] = struct{}{}
	}
	for _, colMeta := range tableMeta.Columns {
		if _, ok := excluded[strings.ToLower(colMeta.Name)]; ok {
			continue
		}
		sp.AddOutputColumn(scope.OutputColumn{
			Alias: colMeta.Name,
			Sources: scope.NewColumnSources([]scope.ColumnRef{{
				Schema: tableRef.Schema,
				Table:  tableRef.Table,
				Column: colMeta.Name,
				// The catalog identified the real column, so the reference must
				// not be rebound by name (which fails for an aliased relation).
				Resolved: true,
			}}, nil),
		})
	}
	return true
}

// ---------------------------------------------------------------------------
// Scope stack and temp-table tracking
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

// currentScope returns the current (innermost) scope.
func (a *Analyzer) currentScope() *scope.Scope {
	if len(a.scopeStack) == 0 {
		return nil
	}
	return a.scopeStack[len(a.scopeStack)-1]
}

// ---------------------------------------------------------------------------
// CTE
// ---------------------------------------------------------------------------

// processCTEs processes a WITH clause.
func (a *Analyzer) processCTEs(with *nodes.WithClause) {
	if with == nil {
		return
	}
	for _, cte := range with.CTEs {
		a.processCTE(cte, with.Recursive)
	}
}

// processCTE processes a single CTE: its body is analyzed in a nested scope,
// and its output columns are flattened into a lineage the outer query can
// trace through. recursive is the WITH clause's RECURSIVE flag, which is what
// makes a reference to the CTE's own name inside its body a self-reference
// rather than a same-named base table.
func (a *Analyzer) processCTE(cte *nodes.CTE, recursive bool) {
	if cte == nil {
		return
	}
	cteName := cte.Name

	lineage := make([]model.ColumnRelation, 0)
	definition := &scope.CTEDefinition{Name: cteName, Columns: cte.Columns}
	if cte.Query != nil {
		a.pushScope()
		if recursive {
			// In a recursive CTE the name refers to the rows the CTE has produced
			// so far, not to a stored relation. Registering it with no lineage —
			// the fixpoint is not modelled — makes every reference to it inside
			// its own body opaque, so neither a source nor a predicate resolves to
			// a base table that merely shares the name.
			a.currentScope().AddCTE(definition)
		}
		a.processQueryNode(cte.Query)
		cteScope := a.popScope()

		// The body's influences belong to the CTE's rows, so they are held
		// against the definition and reach the statement only if it references
		// the CTE. An unreferenced CTE filters rows nobody reads.
		a.influences.BindCTE(definition, cteScope)

		for i, outputCol := range cteScope.GetOutputColumns() {
			// A CTE may rename its output columns: WITH c (a, b) AS (SELECT id,
			// name ...). The MySQL analyzer ignores that list and emits no
			// lineage at all for this shape; mapping it is strictly more
			// correct.
			targetName := outputCol.Alias
			if i < len(cte.Columns) {
				targetName = cte.Columns[i]
			}
			for _, source := range outputCol.Sources {
				resolutions, err := cteScope.ResolveColumnRefs(source.Ref)
				if err != nil {
					continue
				}
				for _, res := range resolutions {
					if a.flattenTempSourceLineage(cteScope, res.Relation, res.Ref.Column, cteName, targetName, source.Transform, &lineage) {
						continue
					}
					lineage = append(lineage, scope.NewLineageEdge(
						res.Ref.Schema, res.Ref.Table, res.Ref.Column,
						"", cteName, targetName,
						source.Transform,
						true, // a CTE is temporary
					))
				}
			}
		}
	}

	definition.Lineage = lineage
	a.currentScope().AddCTE(definition)
}

// ---------------------------------------------------------------------------
// Set operations
// ---------------------------------------------------------------------------

// setOpArm is one leaf SELECT of a set-operation tree together with the chain of
// set-operation transformations that combine it into the statement's result,
// outermost first, so a nested operation is not labelled with only the outermost
// one.
type setOpArm struct {
	stmt  *nodes.SelectStmt
	chain []model.Transformation
}

// flattenSetOpArms flattens a set-operation tree into its leaf SELECTs in order,
// so every arm of every set operation contributes lineage.
func flattenSetOpArms(node nodes.Node, chain []model.Transformation) []setOpArm {
	switch n := node.(type) {
	case *nodes.SelectStmt:
		return []setOpArm{{stmt: n, chain: chain}}
	case *nodes.ParenSelect:
		return flattenSetOpArms(n.Sel, chain)
	case *nodes.SetOpStmt:
		inner := chain
		if transform, ok := setOpTransformation(n.Op); ok {
			inner = algorithm.ArmChain(chain, transform)
		}
		return append(flattenSetOpArms(n.Left, inner), flattenSetOpArms(n.Right, inner)...)
	default:
		return nil
	}
}

// processSetOperation handles UNION/INTERSECT/EXCEPT by processing every arm
// and merging their output columns positionally. Each arm is analyzed in its
// own scope so one arm's FROM relations cannot leak into the next, and only the
// merged operation emits result edges, so the set-operation relation type is
// recorded once. A set operation nested in a CTE or derived table contributes
// the merged columns to its parent instead.
func (a *Analyzer) processSetOperation(stmt *nodes.SetOpStmt) {
	arms := flattenSetOpArms(stmt, nil)
	if len(arms) == 0 {
		return
	}

	baseScope := a.currentScope()
	var (
		allOutputColumns [][]scope.OutputColumn
		armTransforms    [][]model.Transformation
	)

	for i, arm := range arms {
		armTransforms = append(armTransforms, arm.chain)
		if i == 0 {
			a.processSetOpArm(arm.stmt)
			allOutputColumns = append(allOutputColumns, a.resolveOutputColumns(baseScope, baseScope.GetOutputColumns()))
			continue
		}
		tempScope := scope.NewScope(baseScope.Parent())
		for _, cte := range baseScope.CTEs() {
			tempScope.AddCTE(cte)
		}
		originalScope := a.currentScope()
		a.scopeStack[len(a.scopeStack)-1] = tempScope
		a.processSetOpArm(arm.stmt)
		a.scopeStack[len(a.scopeStack)-1] = originalScope
		// The arm's rows are part of the merged result, so its predicates reach
		// the operation's output.
		a.influences.Inherit(tempScope, baseScope)
		allOutputColumns = append(allOutputColumns, a.resolveOutputColumns(tempScope, tempScope.GetOutputColumns()))
	}

	algorithm.MergeSetOpColumns(baseScope, allOutputColumns, armTransforms)
	a.generateEdges(baseScope)
}

// processSetOpArm processes one leaf arm of a set operation, suppressing the
// arm's own result edges so the merged operation emits them once.
func (a *Analyzer) processSetOpArm(arm *nodes.SelectStmt) {
	previous := a.inSetOpArm
	a.inSetOpArm = true
	a.processSelectStatement(arm)
	a.inSetOpArm = previous
}

// resolveOutputColumns resolves each output column's source references against
// the scope the arm was analyzed in, marking them resolved and replacing a
// query-local relation with the stored relations behind it.
//
// The merge below runs after every arm's own scope is gone, and the merged
// references are later resolved again in the enclosing scope. Without this
// step an unqualified reference from a non-first arm would bind to the first
// arm's table, and a reference that resolved to a derived table would bind to an
// unrelated real relation that happens to share its name.
func (*Analyzer) resolveOutputColumns(sp *scope.Scope, cols []scope.OutputColumn) []scope.OutputColumn {
	out := make([]scope.OutputColumn, len(cols))
	copy(out, cols)
	for i := range out {
		if len(out[i].Sources) == 0 {
			continue
		}
		resolved := make([]scope.ColumnSource, 0, len(out[i].Sources))
		for _, source := range out[i].Sources {
			resolutions, err := sp.ResolveColumnRefs(source.Ref)
			if err != nil {
				resolved = append(resolved, source)
				continue
			}
			for _, res := range resolutions {
				if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
					resolved = append(resolved, algorithm.FlattenTempSources(sp, res.Ref, res.Relation, source.Transform)...)
					continue
				}
				columnRef := res.Ref
				columnRef.Resolved = true
				resolved = append(resolved, scope.ColumnSource{Ref: columnRef, Transform: source.Transform})
			}
		}
		out[i].Sources = resolved
	}
	return out
}

// setOpTransformation maps a StarRocks set operator to its transformation.
func setOpTransformation(setOp nodes.SetOperator) (model.Transformation, bool) {
	switch setOp {
	case nodes.SetUnion:
		return model.NewUnionTransformation(), true
	case nodes.SetIntersect:
		return model.NewIntersectTransformation(), true
	case nodes.SetExcept:
		return model.NewExceptTransformation(), true
	default:
		return model.Transformation{}, false
	}
}

// ---------------------------------------------------------------------------
// Temporary-table lineage flattening
// ---------------------------------------------------------------------------

// traceThroughTableLineage traces lineage through a CTE or derived table to the
// result table.
func (a *Analyzer) traceThroughTableLineage(tableRef *scope.TableRef, columnName string, outputAlias string, transform []model.Transformation) {
	algorithm.TraceThroughTableLineage(scope.NewLineageEdge, a.addRelation, tableRef, columnName, outputAlias, transform)
}

// traceThroughTableLineageToTarget traces lineage through a CTE or derived
// table to a specific target column on a real object.
func (a *Analyzer) traceThroughTableLineageToTarget(tableRef *scope.TableRef, columnName string, targetSchema string, targetTable string, targetColumn string, transform []model.Transformation) {
	algorithm.TraceThroughTableLineageToTarget(scope.NewLineageEdge, a.addRelation, tableRef, columnName, targetSchema, targetTable, targetColumn, transform)
}

// flattenTempSourceLineage resolves a column from a temporary table into base
// table lineage. It reports whether the source was handled.
func (*Analyzer) flattenTempSourceLineage(sp *scope.Scope, relation *scope.TableRef, columnName, targetTable string, targetColumn string, transform []model.Transformation, lineage *[]model.ColumnRelation) bool {
	return algorithm.FlattenTempSourceLineage(scope.NewLineageEdge, sp, relation, columnName, targetTable, targetColumn, transform, lineage)
}
