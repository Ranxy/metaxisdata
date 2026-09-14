// Package starrocks provides direct lineage analysis for StarRocks queries.
//
// This implementation is built on github.com/bytebase/omni's StarRocks parser
// and typed AST, mirroring backend/plugin/lineage/mysql. It is a port rather
// than a dialect copy: omni's StarRocks AST differs structurally from its MySQL
// AST (set operations are a separate node, the select list is []*SelectItem,
// and derived tables, CTAS and expression subqueries keep their query as raw
// text).
//
// Engine registration lands with the DDL/DML phases (see
// plan/starrocks_lineage_plan.md); until then the package is exercised by its
// own hermetic tests only, and statement kinds that are not implemented yet
// return an explicit error instead of a partial result.
package starrocks

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"

	nodes "github.com/bytebase/omni/starrocks/ast"
	starrocksparser "github.com/bytebase/omni/starrocks/parser"
)

// Special table/column markers shared with the other analyzers.
const (
	resultTableName = "__result__"
	wildcardColumn  = "*"
)

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
	edges []model.ColumnRelation
	// edgeSet deduplicates edges by their source->target signature.
	edgeSet map[string]struct{}
	// errors collects analysis failures. A non-empty list fails the whole
	// analysis: a partial result is never returned.
	errors []string
	// catalog optionally expands wildcards and resolves metadata.
	catalog catalog.Provide
	// tempTables tracks CTE and subquery names so intermediate edges can be
	// filtered out.
	tempTables map[string]struct{}
}

// Analyze parses a single StarRocks statement and returns its column relations.
func Analyze(ctx context.Context, sql string) ([]model.ColumnRelation, error) {
	return NewAnalyzer(ctx, sql, lineage.CatelogProvide).AnalyzeRelations()
}

// NewAnalyzer creates a StarRocks lineage analyzer for a single statement.
func NewAnalyzer(ctx context.Context, sql string, catalogProvide catalog.Provide) *Analyzer {
	return &Analyzer{
		ctx:        ctx,
		sql:        sql,
		sources:    []source{newSource(sql)},
		scopeStack: []*scope.Scope{scope.NewScope(nil)}, // root scope
		edges:      make([]model.ColumnRelation, 0),
		edgeSet:    make(map[string]struct{}),
		errors:     make([]string, 0),
		catalog:    catalogProvide,
		tempTables: make(map[string]struct{}),
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
		return nil, errors.Wrap(&errs[0], "failed to parse StarRocks SQL")
	}
	if file == nil || len(file.Stmts) != 1 {
		return nil, errors.New("expected exactly 1 statement")
	}

	a.dispatch(file.Stmts[0])

	if len(a.errors) > 0 {
		return nil, errors.Errorf("analysis errors: %s", strings.Join(a.errors, "; "))
	}
	return a.edges, nil
}

// dispatch routes a parsed statement to its analyzer.
func (a *Analyzer) dispatch(stmt nodes.Node) {
	switch s := stmt.(type) {
	case *nodes.SelectStmt:
		a.processSelectStatement(s)
	case *nodes.SetOpStmt:
		a.unsupported("set operation (UNION/INTERSECT/EXCEPT)")
	case *nodes.ParenSelect:
		a.unsupported("parenthesized query")
	case *nodes.CreateViewStmt:
		a.unsupported("CREATE VIEW")
	case *nodes.AlterViewStmt:
		a.unsupported("ALTER VIEW")
	case *nodes.CreateMTMVStmt:
		a.unsupported("CREATE MATERIALIZED VIEW")
	case *nodes.CreateTableStmt:
		a.unsupported("CREATE TABLE")
	case *nodes.InsertStmt:
		a.unsupported("INSERT")
	case *nodes.UpdateStmt:
		a.unsupported("UPDATE")
	case *nodes.DeleteStmt:
		a.unsupported("DELETE")
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
		a.unsupported("CTE (WITH)")
		return
	}
	a.processQuerySpecification(stmt)
}

// processQuerySpecification processes FROM, then the SELECT list, then emits
// edges.
func (a *Analyzer) processQuerySpecification(stmt *nodes.SelectStmt) {
	sp := a.currentScope()
	a.processFromClause(stmt.From)
	a.processSelectItemList(stmt.Items, sp)
	a.generateEdges(sp)
}

// ---------------------------------------------------------------------------
// FROM clause
// ---------------------------------------------------------------------------

// processFromClause processes the FROM clause.
func (a *Analyzer) processFromClause(from []nodes.Node) {
	for _, te := range from {
		a.processTableExpr(te)
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
		// omni keeps a derived table's query as raw text; re-parsing it lands
		// with the subquery phase.
		a.unsupported("derived table")
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
	a.currentScope().AddTable(&scope.TableRef{
		Schema:  schema,
		Table:   table,
		Alias:   alias,
		Columns: []string{},
	})
}

// ---------------------------------------------------------------------------
// SELECT list
// ---------------------------------------------------------------------------

// processSelectItemList processes the SELECT item list.
func (a *Analyzer) processSelectItemList(items []*nodes.SelectItem, sp *scope.Scope) {
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
			a.processSelectExpr(item.Expr, item.Alias, item.Aliased, sp)
		}
	}
}

// processStar expands SELECT * against every table in scope.
func (a *Analyzer) processStar(sp *scope.Scope, except []string) {
	for _, tableRef := range sp.GetTables() {
		if a.catalog != nil && !tableRef.IsSubquery && !tableRef.IsCTE {
			if a.expandWildcardWithCatalog(tableRef, sp, except) {
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
func (a *Analyzer) processTableWildcard(item *nodes.SelectItem, sp *scope.Scope) {
	if item.TableName == nil || len(item.TableName.Parts) == 0 {
		return
	}
	tableName := item.TableName.Parts[len(item.TableName.Parts)-1]
	tableRef, ok := sp.FindTable(tableName)
	if !ok {
		return
	}
	if a.catalog != nil && !tableRef.IsSubquery && !tableRef.IsCTE {
		if a.expandWildcardWithCatalog(tableRef, sp, item.ExceptColumns) {
			return
		}
	}
	sp.AddOutputColumn(scope.OutputColumn{
		Alias:         wildcardColumn,
		Expression:    tableName + "." + wildcardColumn,
		SourceColumns: []scope.ColumnRef{{Schema: tableRef.Schema, Table: tableRef.Table, Column: wildcardColumn}},
	})
}

// processSelectExpr turns one select expression into an output column. aliased
// records whether the SQL carried an explicit alias, so an explicit empty alias
// is not replaced by an inferred one.
func (a *Analyzer) processSelectExpr(expr nodes.Node, alias string, aliased bool, sp *scope.Scope) {
	if expr == nil {
		return
	}
	exprText := a.exprTextOf(expr)
	if !aliased && alias == "" {
		alias = inferColumnAlias(exprText)
	}
	sourceColumns := collectColumns(expr)
	isDerived := isExpressionDerivedText(exprText)

	// A derived expression with no column reference (for example COUNT(*))
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
// Only the root query emits edges to the final result.
func (a *Analyzer) generateEdges(sp *scope.Scope) {
	if sp == nil || sp.Parent() != nil {
		return
	}
	for _, outputCol := range sp.GetOutputColumns() {
		for _, sourceCol := range outputCol.SourceColumns {
			resolved, err := sp.ResolveColumn(sourceCol)
			if err != nil {
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

// addRelation adds a column relation, skipping temp-table endpoints and
// duplicate signatures.
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

// ---------------------------------------------------------------------------
// Scope stack and temp-table tracking
// ---------------------------------------------------------------------------

// currentScope returns the current (innermost) scope.
func (a *Analyzer) currentScope() *scope.Scope {
	if len(a.scopeStack) == 0 {
		return nil
	}
	return a.scopeStack[len(a.scopeStack)-1]
}

// isTempTable checks whether a table name was marked as temporary.
func (a *Analyzer) isTempTable(name string) bool {
	_, ok := a.tempTables[name]
	return ok
}
