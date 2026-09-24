// Package postgresql provides direct lineage analysis for PostgreSQL queries.
//
// This implementation is built on github.com/bytebase/omni's PostgreSQL parser
// and typed AST (see plan/postgresql_omni_parser_migration_plan.md). It replaced
// the legacy ANTLR implementation, which was parity-verified against this one
// over the golden corpus and then removed.
//
// Expression classification is structural (see
// plan/postgresql_expression_transformation_plan.md), and the analysis mechanism
// this package shares with the other dialects lives in
// backend/plugin/lineage/algorithm (see plan/lineage_transformation_model.md for
// what a Transformation does and does not express).
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
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/algorithm"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"
)

func init() {
	lineage.RegisterAnalyzeRelation(storepb.Engine_POSTGRES, Analyze, splitStatements)
}

// splitStatements splits a script into its individual statements, dropping the
// ones that carry no SQL. The root package feeds them to Analyze one at a time:
// this analyzer is defined for exactly one statement.
func splitStatements(sql string) []string {
	segments := omnipg.Split(sql)
	statements := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment.Empty() {
			continue
		}
		statements = append(statements, segment.Text)
	}
	return statements
}

// Constants for special table/column markers
const (
	resultTableName   = model.ResultTableName
	deletionFieldName = "__deletion__"
	wildcardColumn    = model.WildcardColumn
	// excludedRelationName is PostgreSQL's ON CONFLICT pseudo-relation holding
	// the proposed row. It is not a metadata-registry object, so edges sourced
	// from it can never resolve; its column resolves to the INSERT's own source
	// instead (see processAssignments).
	excludedRelationName = "excluded"
	// fileSourceName is the source marker a data-loading statement's edges carry,
	// shared with the MySQL family and StarRocks so `COPY`, `LOAD DATA` and
	// `COPY INTO` name their file the same way.
	fileSourceName = "__file__"
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
	// edges collects the distinct column relations the analyzed statements
	// produced.
	edges *algorithm.EdgeSet
	// diagnostics collects what the analysis could not represent: a statement
	// shape it does not model, and a reference that resolved to nothing. A
	// non-empty store turns the result into a partial one that says what is
	// missing.
	diagnostics *algorithm.Diagnostics
	// Optional catalog for wildcard expansion and metadata lookup, memoized for
	// the whole analysis so a relation named twice is read once. It is nil when no
	// provider was configured, which is why every use is guarded.
	catalog *catalog.Cache
	// realTarget is set while analyzing the query of a statement that writes to a
	// real object (INSERT, CREATE VIEW, CREATE TABLE AS, CREATE MATERIALIZED
	// VIEW), so the query result edges are not emitted alongside the real target.
	realTarget bool
	// inSetOpArm is set while analyzing one arm of a set operation, so only the
	// merged set-operation result emits edges.
	inSetOpArm bool
	// influences holds the row-set influences collected while a scope was
	// current. They belong to the rows that scope produces and are inherited by
	// whatever consumes them, so a scope whose rows reach no output cannot
	// influence the statement.
	influences *algorithm.Influences
	// namedWindows maps the WINDOW clause in effect to its definitions. A window
	// used as `OVER w` keeps its PARTITION BY and ORDER BY there, where the walk
	// over the expression cannot reach them.
	namedWindows map[string]*pgast.WindowDef
}

func Analyze(ctx context.Context, sql string) ([]model.ColumnRelation, error) {
	analyzer := NewAnalyzer(ctx, sql, lineage.GetCatalogProvide())
	return analyzer.AnalyzeRelations()
}

// NewAnalyzer creates a new PostgreSQL lineage analyzer.
func NewAnalyzer(ctx context.Context, sql string, catalogProvide catalog.Provide) *Analyzer {
	diagnostics := algorithm.NewDiagnostics()
	return &Analyzer{
		ctx:         ctx,
		sql:         sql,
		scopeStack:  []*scope.Scope{scope.NewScope(nil)}, // Root scope
		edges:       algorithm.NewEdgeSet(),
		diagnostics: diagnostics,
		catalog:     catalog.NewCache(catalogProvide),
		influences:  algorithm.NewInfluences(diagnostics),
	}
}

// AnalyzeRelations parses the SQL and returns column relations.
//
// Parse errors are a hard failure: no partial result is returned. A statement
// that parses but that the analyzer cannot model (MERGE) is not one — it is
// reported as a gap beside the edges found, so a caller records it without
// discarding real lineage.
//
// The analyzer is defined for exactly one statement: the caller splits a script
// and feeds the statements in one at a time. A shared analyzer state across
// statements made an unqualified column in a later statement resolve against an
// earlier statement's relations, and the engines disagreed about whether a
// multi-statement script was accepted at all.
func (a *Analyzer) AnalyzeRelations() ([]model.ColumnRelation, error) {
	stmts, err := omnipg.Parse(a.sql)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse PostgreSQL SQL")
	}

	only, count := pgast.Node(nil), 0
	for _, stmt := range stmts {
		if stmt.Empty() {
			continue
		}
		only, count = stmt.AST, count+1
	}
	if count != 1 {
		return nil, errors.Errorf("expected exactly 1 statement, got %d", count)
	}
	// A cancelled analysis yields no result rather than a partial one: the caller
	// re-runs the whole object, and a truncated edge set would be persisted as if
	// it were complete.
	if err := a.ctx.Err(); err != nil {
		return nil, errors.Wrap(err, "analysis cancelled")
	}
	a.processStmt(only)

	// What the analysis could not represent is reported beside the edges it did
	// find: the caller stores both, so a gap never reads as "no lineage here".
	if messages := a.diagnostics.Messages(); len(messages) > 0 {
		return a.edges.Edges(), &lineage.UnsupportedStatementError{
			Message: errors.Errorf("analysis errors: %s", strings.Join(messages, "; ")).Error(),
		}
	}

	return a.edges.Edges(), nil
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
	case *pgast.CopyStmt:
		a.processCopyStmt(stmt)
	case *pgast.MergeStmt:
		// MERGE carries column lineage this analyzer does not model yet. Failing
		// loudly keeps it from being read as a statement with none, which is what
		// the StarRocks analyzer does for the same statement.
		a.diagnostics.NotModelled("MERGE")
	default:
		// Statement kinds that carry no lineage are ignored, as in the MySQL
		// analyzer.
	}
}

// processCopyStmt processes COPY. `COPY <table> FROM ...` loads a file into a
// table, the shape MySQL's LOAD DATA and StarRocks' COPY INTO record as a file
// source; `COPY ... TO` reads a table or a query out, which is not lineage. The
// file's columns are positional, so a declared column list maps them one by one
// and no list means the whole table.
func (a *Analyzer) processCopyStmt(stmt *pgast.CopyStmt) {
	if stmt == nil || !stmt.IsFrom || stmt.Relation == nil {
		return
	}
	targetSchema := stmt.Relation.Schemaname
	targetTable := stmt.Relation.Relname
	isTemp := targetTable == resultTableName

	targetColumns := stringList(stmt.Attlist)
	if len(targetColumns) == 0 {
		a.addRelation(scope.NewSchemaLineageEdge(
			"", fileSourceName, wildcardColumn,
			targetSchema, targetTable, wildcardColumn,
			nil,
			isTemp,
		))
		return
	}
	for i, column := range targetColumns {
		a.addRelation(scope.NewSchemaLineageEdge(
			"", fileSourceName, fmt.Sprintf("col%d", i+1),
			targetSchema, targetTable, column,
			nil,
			isTemp,
		))
	}
}

// ---------------------------------------------------------------------------
// SELECT
// ---------------------------------------------------------------------------

// processSelectStmt processes a SELECT. A SELECT ... INTO names the object it
// creates, so it is dispatched to the same target path as CREATE TABLE AS; any
// other SELECT is a query whose result is the statement's.
func (a *Analyzer) processSelectStmt(stmt *pgast.SelectStmt) {
	if stmt == nil {
		return
	}
	if stmt.IntoClause != nil {
		a.processQueryToTarget(stmt, stmt.IntoClause)
		return
	}
	a.processSelectQuery(stmt)
}

// processSelectQuery processes a SELECT that produces a query result, handling
// its WITH clause and set operations before the leaf SELECT core.
func (a *Analyzer) processSelectQuery(stmt *pgast.SelectStmt) {
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

	// A window written as `OVER w` names a definition in this query's WINDOW
	// clause, so the definitions have to be in reach while its clauses are read.
	previousWindows := a.namedWindows
	a.namedWindows = namedWindowsOf(stmt.WindowClause)
	defer func() { a.namedWindows = previousWindows }()

	if stmt.FromClause != nil {
		a.processFromClause(stmt.FromClause)
	}

	// A WHERE or HAVING predicate decides which rows the query emits without its
	// value reaching any output column, so it is recorded as an influence on the
	// statement's target rows rather than on a column.
	if stmt.WhereClause != nil {
		a.collectPredicates(stmt.WhereClause, sp, model.NewFilterTransformation(a.exprTextOf(stmt.WhereClause)), false)
	}

	// A VALUES list is the query's whole row source: it has no FROM clause and no
	// target list, and every row derives the same columns.
	if stmt.ValuesLists != nil {
		a.processValuesLists(stmt.ValuesLists, sp)
	}

	if stmt.TargetList != nil {
		a.processTargetList(stmt.TargetList, sp, a.groupByKeys(stmt.GroupClause))
	}

	// HAVING is the one clause that may name a select-list alias, so it resolves
	// one before falling back to the scope.
	if stmt.HavingClause != nil {
		a.collectPredicates(stmt.HavingClause, sp, model.NewFilterTransformation(a.exprTextOf(stmt.HavingClause)), true)
	}

	a.generateEdges(sp)
}

// setOpArm is one leaf SELECT of a set-operation tree together with the chain of
// set-operation transformations that combine it into the statement's result,
// outermost first. The chain is kept because PostgreSQL's set operations do not
// all bind equally: INTERSECT binds tighter than UNION and EXCEPT, so
// `a UNION b INTERSECT c` groups as `a UNION (b INTERSECT c)`. Flattening the
// tree without the chain would label every arm with the outermost operation and
// lose the inner one.
type setOpArm struct {
	stmt  *pgast.SelectStmt
	chain []model.Transformation
}

// flattenSetOpArms flattens a UNION/INTERSECT/EXCEPT tree into its leaf SELECTs
// in order, so every arm of every set operation contributes lineage.
func flattenSetOpArms(stmt *pgast.SelectStmt, chain []model.Transformation) []setOpArm {
	if stmt == nil {
		return nil
	}
	if stmt.Op == pgast.SETOP_NONE {
		return []setOpArm{{stmt: stmt, chain: chain}}
	}
	// stmt.Op combines the two subtrees, so it is inner to everything already on
	// the chain and is appended after it.
	inner := chain
	if transform, ok := setOpTransformation(stmt.Op, stmt.All); ok {
		inner = algorithm.ArmChain(chain, transform)
	}
	return append(flattenSetOpArms(stmt.Larg, inner), flattenSetOpArms(stmt.Rarg, inner)...)
}

// processSetOperation processes UNION/INTERSECT/EXCEPT: the first operand is
// analyzed in the base scope, every later operand in a temporary scope that
// resolves no relation of the base scope's but still reads the CTEs the
// statement declared.
func (a *Analyzer) processSetOperation(stmt *pgast.SelectStmt) {
	arms := flattenSetOpArms(stmt, nil)
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

		// The arm starts a relation namespace of its own — the first arm's
		// relations are not visible in it — while still reading the CTEs the
		// statement declared.
		tempScope := scope.NewScopeWithDefinitions(baseScope.Parent(), baseScope)

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

// setOpTransformation maps a PostgreSQL set-operation kind to its transformation.
func setOpTransformation(setOp pgast.SetOperation, all bool) (model.Transformation, bool) {
	switch setOp {
	case pgast.SETOP_UNION:
		return model.NewUnionTransformation(all), true
	case pgast.SETOP_INTERSECT:
		return model.NewIntersectTransformation(all), true
	case pgast.SETOP_EXCEPT:
		return model.NewExceptTransformation(all), true
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
			a.processCTE(cte, withClause.Recursive)
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

// processCTE processes a single Common Table Expression (CTE). recursive is the
// WITH clause's RECURSIVE flag, which is what makes a reference to the CTE's own
// name inside its body a self-reference rather than a same-named base table.
func (a *Analyzer) processCTE(cte *pgast.CommonTableExpr, recursive bool) {
	if cte == nil {
		return
	}

	cteName := cte.Ctename
	columns := stringList(cte.Aliascolnames)
	definition := &scope.CTEDefinition{Name: cteName, Columns: columns}

	if cte.Ctequery != nil {
		a.pushScope()
		if recursive {
			// In a recursive CTE the name refers to the rows the CTE has produced
			// so far, not to a stored relation. Registering it with no lineage —
			// the fixpoint is not modelled — makes every reference to it inside
			// its own body opaque, so neither a source nor a predicate resolves to
			// a base table that merely shares the name.
			a.currentScope().AddCTE(&scope.CTEDefinition{Name: cteName, Columns: columns})
		}
		a.processPreparableStmt(cte.Ctequery)
		cteScope := a.popScope()

		// The body's influences belong to the CTE's rows, so they are held
		// against the definition and reach the statement only if it references
		// the CTE. An unreferenced CTE filters rows nobody reads.
		a.influences.BindCTE(definition, cteScope)

		outputColumns := cteScope.GetOutputColumns()
		// A declared column list renames the body's outputs positionally, and only
		// when its arity matches the body: a mismatch is a statement PostgreSQL
		// rejects. The definition keeps the validated form, so a reference to the
		// CTE resolves columns against names the body really exposes.
		definition.Columns = exposedColumnNames(columns, outputColumns)
		useExplicitColumns := len(columns) > 0 && len(columns) == len(outputColumns)

		// Build lineage for each output column. When the CTE declares an explicit
		// column list with a matching arity, it overrides the inner SELECT output
		// names positionally.
		for i, outputCol := range outputColumns {
			targetColumn := outputCol.Alias
			if useExplicitColumns {
				targetColumn = columns[i]
			}

			for _, source := range outputCol.Sources {
				resolutions, err := cteScope.ResolveColumnRefs(source.Ref)
				if err != nil {
					a.diagnostics.Unresolved("a CTE body", source.Ref)
					continue
				}
				for _, res := range resolutions {
					if a.flattenTempSourceLineage(cteScope, res.Relation, res.Ref.Column, cteName, targetColumn, source.Transform, &definition.Lineage) {
						continue
					}

					definition.Lineage = append(definition.Lineage, scope.NewSchemaLineageEdge(
						res.Ref.Schema,
						res.Ref.Table,
						res.Ref.Column,
						"",
						cteName,
						targetColumn,
						source.Transform,
						true,
					))
				}
			}
		}
	}

	a.currentScope().AddCTE(definition)
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
		if a.ctx.Err() != nil {
			return
		}
		a.processTableExpr(item)
		// The relations have to be in scope before a join condition's columns
		// can be resolved.
		a.collectJoinPredicates(item)
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
	case *pgast.RangeTableSample:
		// TABLESAMPLE wraps a relation: the sample changes which rows are read,
		// not which relation they come from, so the relation is processed as if the
		// clause were not there. Leaving it unhandled dropped the relation and the
		// whole statement produced no edges.
		a.processTableExpr(tableExpr.Relation)
	case *pgast.RangeFunction:
		a.processRangeFunction(tableExpr)
	case *pgast.RangeTableFunc:
		a.processRangeTableFunc(tableExpr)
	default:
		// Other FROM items were ignored by this analyzer as well.
	}
}

// processRangeFunction registers a set-returning function in FROM as a
// query-local relation. The function names no stored relation, but it produces
// its rows from its arguments, so each output column comes from the columns the
// argument expression reads: `SELECT * FROM t, LATERAL unnest(t.arr) u` reports
// `t.arr`, which used to be dropped with the whole function.
func (a *Analyzer) processRangeFunction(fn *pgast.RangeFunction) {
	if fn == nil {
		return
	}

	alias := ""
	var declared []string
	if fn.Alias != nil {
		alias = fn.Alias.Aliasname
		declared = stringList(fn.Alias.Colnames)
	}
	if alias == "" {
		// An unaliased function is addressed by its own name.
		alias = rangeFunctionName(fn)
		if alias == "" {
			return
		}
	}

	sp := a.currentScope()
	var (
		columns []string
		lineage = make([]model.ColumnRelation, 0)
	)
	for i, item := range fn.Functions.Items {
		call := rangeFunctionCall(item)
		if call == nil {
			continue
		}
		column := rangeFunctionOutputName(call, declared, i)
		columns = append(columns, column)
		for _, argument := range call.Args.Items {
			for _, ref := range a.extractColumnsFromNode(argument, sp) {
				resolutions, err := sp.ResolveColumnRefs(ref)
				if err != nil {
					a.diagnostics.Unresolved("a range function argument", ref)
					continue
				}
				for _, res := range resolutions {
					if a.flattenTempSourceLineage(sp, res.Relation, res.Ref.Column, alias, column, nil, &lineage) {
						continue
					}
					lineage = append(lineage, scope.NewSchemaLineageEdge(
						res.Ref.Schema, res.Ref.Table, res.Ref.Column,
						"", alias, column,
						nil,
						true,
					))
				}
			}
		}
	}
	// WITH ORDINALITY adds a row number of its own, which no column produces.
	// A declared column list already names it.
	if fn.Ordinality && len(declared) <= len(columns) {
		columns = append(columns, "ordinality")
	}

	tableRef := &scope.TableRef{
		Table:      alias,
		Alias:      alias,
		IsSubquery: true,
		Lineage:    lineage,
	}
	attachTempColumnLookup(tableRef, columns)
	sp.AddTable(tableRef)
}

// processRangeTableFunc registers XMLTABLE in FROM as a query-local relation. Its
// columns are the ones its COLUMNS clause declares, and every extracted value comes
// out of the document expression the PASSING clause names, so that expression's
// columns are the source of each of them: `SELECT * FROM t, XMLTABLE('/a' PASSING
// t.doc COLUMNS x int PATH 'x') q` reports `t.doc`. The whole FROM item used to be
// ignored, which dropped the relation and the source with it.
func (a *Analyzer) processRangeTableFunc(fn *pgast.RangeTableFunc) {
	if fn == nil {
		return
	}

	alias := ""
	var declared []string
	if fn.Alias != nil {
		alias = fn.Alias.Aliasname
		declared = stringList(fn.Alias.Colnames)
	}
	if alias == "" {
		// An unaliased XMLTABLE is addressed by its own name.
		alias = "xmltable"
	}

	type funcColumn struct {
		name       string
		ordinality bool
	}
	sp := a.currentScope()
	columns := make([]funcColumn, 0)
	// The row expression and each column's PATH and DEFAULT expressions may read a
	// column too; the document expression decides the value of every extracted
	// column, so it is a source of each of them.
	sourceNodes := []pgast.Node{fn.Docexpr, fn.Rowexpr}
	if fn.Columns != nil {
		for i, item := range fn.Columns.Items {
			col, ok := item.(*pgast.RangeTableFuncCol)
			if !ok {
				continue
			}
			name := col.Colname
			if i < len(declared) && declared[i] != "" {
				name = declared[i]
			}
			columns = append(columns, funcColumn{name: name, ordinality: col.ForOrdinality})
			if !col.ForOrdinality {
				sourceNodes = append(sourceNodes, col.Colexpr, col.Coldefexpr)
			}
		}
	}

	lineage := make([]model.ColumnRelation, 0)
	for _, column := range columns {
		// FOR ORDINALITY numbers the rows of the document and reads no column.
		if column.ordinality {
			continue
		}
		for _, node := range sourceNodes {
			if node == nil {
				continue
			}
			for _, ref := range a.extractColumnsFromNode(node, sp) {
				resolutions, err := sp.ResolveColumnRefs(ref)
				if err != nil {
					a.diagnostics.Unresolved("an XMLTABLE expression", ref)
					continue
				}
				for _, res := range resolutions {
					if a.flattenTempSourceLineage(sp, res.Relation, res.Ref.Column, alias, column.name, nil, &lineage) {
						continue
					}
					lineage = append(lineage, scope.NewSchemaLineageEdge(
						res.Ref.Schema, res.Ref.Table, res.Ref.Column,
						"", alias, column.name,
						nil,
						true,
					))
				}
			}
		}
	}

	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, column.name)
	}
	tableRef := &scope.TableRef{
		Table:      alias,
		Alias:      alias,
		IsSubquery: true,
		Lineage:    lineage,
	}
	attachTempColumnLookup(tableRef, names)
	sp.AddTable(tableRef)
}

// rangeFunctionCall extracts the call from one entry of a RangeFunction's list.
// omni wraps each entry in a one-element list, and a ROWS FROM entry holds a
// call as well.
func rangeFunctionCall(item pgast.Node) *pgast.FuncCall {
	switch t := item.(type) {
	case *pgast.FuncCall:
		return t
	case *pgast.List:
		if len(t.Items) == 1 {
			if call, ok := t.Items[0].(*pgast.FuncCall); ok {
				return call
			}
		}
	default:
		// A ROWS FROM entry of any other shape names no call to trace.
	}
	return nil
}

// rangeFunctionName is the name an unaliased function in FROM is addressed by.
func rangeFunctionName(fn *pgast.RangeFunction) string {
	for _, item := range fn.Functions.Items {
		if call := rangeFunctionCall(item); call != nil {
			if names := stringList(call.Funcname); len(names) > 0 {
				return names[len(names)-1]
			}
		}
	}
	return ""
}

// rangeFunctionOutputName names one output column of a function in FROM: the
// declared column list renames it positionally, otherwise the function names it.
func rangeFunctionOutputName(call *pgast.FuncCall, declared []string, index int) string {
	if index < len(declared) {
		return declared[index]
	}
	if names := stringList(call.Funcname); len(names) > 0 {
		return names[len(names)-1]
	}
	return ""
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
			// Reading the CTE's rows carries the predicates that shaped them into
			// this query.
			a.influences.InheritCTE(a.currentScope(), cte)
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

// exposedColumnNames lists the names a temporary relation exposes, in the order
// the relation offers them. A declared list — a CTE's `WITH c (a, b)` or a
// derived table's `AS d (a, b)` — renames the body's output positionally, so both
// the relation's own lineage and the columns the scope resolves against have to
// use the renamed names. The list is honoured only when its arity matches the
// body, because a mismatch is a statement PostgreSQL rejects.
func exposedColumnNames(declared []string, cols []scope.OutputColumn) []string {
	if len(declared) > 0 && len(declared) == len(cols) {
		return declared
	}
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
		meta := a.catalogTable(tableRef)
		if meta == nil {
			return nil
		}
		names = make([]string, 0, len(meta.Columns))
		for _, col := range meta.Columns {
			names = append(names, col.Name)
		}
		return names
	})
}

// catalogTable looks up a base table's metadata, reading the catalog at most once
// per analysis. A lookup that fails leaves the relation's columns unknown — the
// analysis falls back to a wildcard edge — so the failure is reported as a gap:
// swallowing it made a catalog outage produce a result indistinguishable from a
// complete one.
func (a *Analyzer) catalogTable(tableRef *scope.TableRef) *catalog.TableMeta {
	if a.catalog == nil {
		return nil
	}
	id := model.ObjectIdentifier{Schema: tableRef.Schema, Name: tableRef.Table}
	meta, err := a.catalog.GetTable(a.ctx, id)
	if err != nil {
		a.diagnostics.CatalogUnavailable(id.FullName(), err)
		return nil
	}
	return meta
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
	var declaredColumns []string
	if sub.Alias != nil {
		alias = sub.Alias.Aliasname
		declaredColumns = stringList(sub.Alias.Colnames)
	}

	query, ok := sub.Subquery.(*pgast.SelectStmt)
	if !ok {
		return
	}

	a.pushScope()
	a.processSelectStmt(query)
	subqueryScope := a.popScope()
	// The derived table's rows feed the query that reads it, so the predicates
	// that shaped them reach that query's output.
	a.influences.Inherit(subqueryScope, a.currentScope())

	lineage := make([]model.ColumnRelation, 0)

	outputColumns := subqueryScope.GetOutputColumns()
	names := exposedColumnNames(declaredColumns, outputColumns)
	for i, col := range outputColumns {
		colName := names[i]
		if colName == "" {
			colName = "column"
		}

		for _, source := range col.Sources {
			resolutions, err := subqueryScope.ResolveColumnRefs(source.Ref)
			if err != nil {
				a.diagnostics.Unresolved("a derived table", source.Ref)
				continue
			}
			for _, res := range resolutions {
				if a.flattenTempSourceLineage(subqueryScope, res.Relation, res.Ref.Column, alias, colName, source.Transform, &lineage) {
					continue
				}

				lineage = append(lineage, scope.NewSchemaLineageEdge(
					res.Ref.Schema,
					res.Ref.Table,
					res.Ref.Column,
					"",
					alias,
					colName,
					source.Transform,
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
	attachTempColumnLookup(tableRef, names)
	a.currentScope().AddTable(tableRef)
}

// ---------------------------------------------------------------------------
// Target list
// ---------------------------------------------------------------------------

// processValuesLists records what a VALUES list writes into each column of the row
// it produces. Every row is one derivation of the same columns, so the sources of
// each row's nth expression merge into the nth column, the way a set operation
// merges its arms. PostgreSQL groups the rows this way too: it evaluates each row
// and returns them together.
//
// The common `VALUES (1, 2)` is all literals and so records nothing, which is what
// it did before. A row holding a subquery is the shape that used to be dropped
// whole: `INSERT INTO t (a) VALUES ((SELECT max(x) FROM other))` wrote other.x into
// t.a and reported no lineage at all.
func (a *Analyzer) processValuesLists(lists *pgast.List, sp *scope.Scope) {
	var columns []scope.OutputColumn
	for _, row := range lists.Items {
		items, ok := row.(*pgast.List)
		if !ok || items == nil {
			continue
		}
		// PostgreSQL requires every row to have the same arity; the widest one
		// defines the shape here so no source is dropped if one does not.
		for len(columns) < len(items.Items) {
			// The name PostgreSQL gives an unnamed VALUES column is positional.
			columns = append(columns, scope.OutputColumn{Alias: fmt.Sprintf("column%d", len(columns)+1)})
		}
		for i, item := range items.Items {
			var transform []model.Transformation
			if classification, ok := a.classifyExpression(item); ok {
				transform = []model.Transformation{classification}
			}
			sources := scope.NewColumnSources(a.extractColumnsFromNode(item, sp), transform)
			if len(sources) == 0 {
				continue
			}
			columns[i].Sources = append(columns[i].Sources, sources...)
			columns[i].IsDerived = isExpressionDerived(item)
		}
	}
	sp.SetOutputColumns(columns)
}

// processTargetList processes the SELECT target list.
func (a *Analyzer) processTargetList(targets *pgast.List, sp *scope.Scope, groupKeys []string) {
	if targets == nil {
		return
	}
	for _, item := range targets.Items {
		if a.ctx.Err() != nil {
			return
		}
		rt, ok := item.(*pgast.ResTarget)
		if !ok {
			continue
		}
		a.processResTarget(rt, sp, groupKeys)
	}
}

// processResTarget processes one target element.
func (a *Analyzer) processResTarget(rt *pgast.ResTarget, sp *scope.Scope, groupKeys []string) {
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
				Alias:     colRef.Column,
				Sources:   scope.NewColumnSources([]scope.ColumnRef{colRef}, nil),
				IsDerived: false,
			})
			return
		}
	}

	a.processExpressionTarget(rt, sp, groupKeys)
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
			Alias:     wildcardColumn,
			Sources:   scope.NewColumnSources([]scope.ColumnRef{wildcardSourceRef(tableRef)}, nil),
			IsDerived: false,
		})
	}
}

// processTableStar expands `table.*` against a single relation in scope.
func (a *Analyzer) processTableStar(cr *pgast.ColumnRef, sp *scope.Scope) {
	colRef := a.columnRefFromFields(cr.Fields)
	tableRef, ok := sp.FindRelation(scope.RelationKey{Qualifier: colRef.Schema, Name: colRef.Table})
	if !ok {
		// PostgreSQL rejects a wildcard whose qualifier names no relation
		// ("missing FROM-clause entry"), so there is no source to expand; the note
		// is what keeps the empty result from reading as a complete one.
		a.diagnostics.UnresolvedQualifier("a wildcard qualifier", colRef.Table)
		return
	}
	if a.catalog != nil && !tableRef.IsSubquery && !tableRef.IsCTE {
		if a.expandWildcardWithCatalog(tableRef, sp) {
			return
		}
	}
	sp.AddOutputColumn(scope.OutputColumn{
		Alias:     wildcardColumn,
		Sources:   scope.NewColumnSources([]scope.ColumnRef{wildcardSourceRef(tableRef)}, nil),
		IsDerived: false,
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
func (a *Analyzer) processExpressionTarget(rt *pgast.ResTarget, sp *scope.Scope, groupKeys []string) {
	alias := rt.Name
	if alias == "" {
		alias = a.inferColumnAlias(rt.Val)
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
		Alias:     alias,
		Sources:   scope.NewColumnSources(sourceColumns, nil),
		IsDerived: isDerived,
	}

	if isDerived {
		if transform, ok := a.classifyExpression(rt.Val); ok {
			// The keys are attached to every transformation of a select item that
			// contains a group aggregate, so the field never claims a column was
			// aggregated when it was only projected.
			if len(groupKeys) > 0 && containsGroupAggregate(rt.Val) {
				transform.GroupKeys = groupKeys
			}
			outputCol.SetTransform([]model.Transformation{transform})
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

	sourceScope := a.processInsertSource(stmt)

	a.generateEdgesForDataModification(sourceScope, targetSchema, targetTable, targetColumns)

	if stmt.OnConflictClause != nil {
		a.processOnConflict(stmt.OnConflictClause, stmt.Relation, a.insertSourceMap(sourceScope, targetColumns))
	}

	// Recorded after the insert's own edges: the returned columns are the
	// statement's output, not another set of source columns for the target.
	a.processReturning(stmt.ReturningList, stmt.Relation)
}

// processInsertSource analyzes the query an insert writes from in a scope of its
// own and returns that scope. PostgreSQL analyzes the query as a subquery of the
// INSERT, so its relations belong to a query level of their own: they are the
// sources of the write, and neither the RETURNING list nor the conflict clause
// can name them (verified on 16.15). Leaving them in the statement's scope is what
// used to leak them into both clauses. A statement with no source query (DEFAULT
// VALUES) still gets a scope, which is simply empty. The scope is returned rather
// than left on the stack because the write's edges and its insert-source map are
// both read from it, while the clauses that follow belong to the statement's own.
func (a *Analyzer) processInsertSource(stmt *pgast.InsertStmt) *scope.Scope {
	previous := a.realTarget
	a.realTarget = true
	a.pushScope()
	if sel, ok := stmt.SelectStmt.(*pgast.SelectStmt); ok {
		a.processSelectStmt(sel)
	}
	a.realTarget = previous
	return a.popScope()
}

// insertSourceMap maps each written target column to the sources the INSERT
// writes into it, resolved in the scope the query ran in. EXCLUDED.col names the
// value the insert proposed for col, so the conflict clause resolves through this
// map the way MySQL's VALUES(col) does.
func (a *Analyzer) insertSourceMap(sp *scope.Scope, targetColumns []string) map[string][]scope.ColumnRef {
	out := make(map[string][]scope.ColumnRef)
	for i, col := range a.resolveOutputColumns(sp, sp.GetOutputColumns()) {
		if len(targetColumns) > 0 && i >= len(targetColumns) {
			break
		}
		name := col.Alias
		if i < len(targetColumns) {
			name = targetColumns[i]
		}
		out[name] = append(out[name], scope.Refs(col.Sources)...)
	}
	return out
}

// processOnConflict processes an ON CONFLICT DO UPDATE SET list. The clause is
// analyzed in the scope PostgreSQL gives it: the target relation and the
// EXCLUDED pseudo-relation, and nothing else. Its own scope is what lets the
// target resolve at all, so `SET quantity = inventory.quantity + …` records the
// self-reference instead of being dropped or attributed to the INSERT source. A
// reference to one of the relations the query writes from is "missing FROM-clause
// entry" (verified on 16.15); those relations are no longer in the statement's
// scope at all (see processInsertSource), so that part is now structural.
//
// The scope is still detached from the statement's rather than parented to it, and
// for a reason of its own: a CTE name is not a usable qualifier anywhere in
// PostgreSQL, and the parent chain is the path that resolves one (see
// scope.findCTEQualifier). `SET b = c.y` must stay unresolved while a subquery in
// the clause still reads c from its own FROM list.
//
// The statement's CTE definitions therefore stay reachable even though its
// relations do not. A subquery written in the clause is a query level of its own,
// so its FROM clause resolves against them exactly as it would anywhere else —
// `ON CONFLICT (id) DO UPDATE SET a = (SELECT a FROM c)` reads c's lineage through
// the CTE, which is what PostgreSQL executes (verified on 16.15). Without the
// definitions link the name fell through to the base-table branch and the edge
// named a table `c` that does not exist.
func (a *Analyzer) processOnConflict(onConflict *pgast.OnConflictClause, target *pgast.RangeVar, insertSources map[string][]scope.ColumnRef) {
	if onConflict == nil || onConflict.TargetList == nil {
		return
	}
	targetSchema := ""
	targetTable := ""
	if target != nil {
		targetSchema = target.Schemaname
		targetTable = target.Relname
	}

	a.scopeStack = append(a.scopeStack, scope.NewScopeWithDefinitions(nil, a.currentScope()))
	a.addTargetRelation(target)
	a.processAssignments(onConflict.TargetList, targetSchema, targetTable, insertSources)
	a.popScope()
}

// processUpdateStmt processes an UPDATE statement.
//
// The statement records the columns each assignment reads, but not the WHERE and
// FROM predicates that decide which rows are written. That is the shared
// MySQL-family rule — predicate influence edges describe a SELECT-based
// statement's row set, and only DELETE models a modification's row set, as
// `__deletion__` — so no emitter runs for this statement's influences and the
// ones its clauses collect are deliberately dropped with the statement. The
// clauses are still walked because their subqueries can carry lineage of their
// own.
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

	a.processReturning(stmt.ReturningList, stmt.Relation)
}

// processReturning records the columns a data-modifying statement returns as the
// statement's output, which is what a data-modifying CTE hands to the query that
// reads it (`WITH moved AS (DELETE ... RETURNING *) INSERT ... SELECT * FROM
// moved`). The expressions read the target relation's own row, so they are
// resolved in a scope holding just that relation, and the resulting references
// are marked resolved before they leave it. An insert's source is a query level of
// its own (see processInsertSource), so the clause cannot name its relations either
// — which is what PostgreSQL enforces.
//
// The returned columns replace the scope's output columns rather than joining
// them: they are the statement's whole output, while the columns the query it
// writes from produced are its input. Appending them made a data-modifying CTE
// expose the INSERT's own source columns beside the returned ones, so a name the
// two shared was attributed to a table the CTE never returns from. A statement
// without RETURNING exposes nothing, which is what PostgreSQL requires:
// referencing such a CTE is "WITH query ... does not have a RETURNING clause"
// (verified on 16.15).
//
// A top-level data-modifying statement emits no result edges from them: the rows
// it returns are a client-facing result, and the lineage it records is the write
// it performs (and, for DELETE, the rows that write removes).
func (a *Analyzer) processReturning(returning *pgast.List, target *pgast.RangeVar) {
	if target == nil {
		return
	}
	if returning == nil {
		a.currentScope().SetOutputColumns(nil)
		return
	}

	a.pushScope()
	a.addTargetRelation(target)
	returningScope := a.currentScope()
	a.processTargetList(returning, returningScope, nil)
	columns := a.resolveOutputColumns(returningScope, returningScope.GetOutputColumns())
	a.popScope()

	a.currentScope().SetOutputColumns(columns)
}

// processSetClauseList processes UPDATE SET assignment targets.
func (a *Analyzer) processSetClauseList(assignments *pgast.List, targetSchema, targetTable string) {
	a.processAssignments(assignments, targetSchema, targetTable, nil)
}

// processAssignments resolves assignment targets. When insertSources is set, a
// source column qualified by the EXCLUDED pseudo-relation is replaced by the
// sources the INSERT writes to that column, because EXCLUDED names the row the
// insert proposed; the pseudo-relation itself can never resolve.
func (a *Analyzer) processAssignments(assignments *pgast.List, targetSchema, targetTable string, insertSources map[string][]scope.ColumnRef) {
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
		switch val := rt.Val.(type) {
		case nil:
		case *pgast.MultiAssignRef:
			// `SET (a, b) = (SELECT x, y FROM s)` gives one ResTarget per target
			// column, each naming its own column of the same row expression, so the
			// columns map positionally. Reading the row's merged sources instead
			// would attribute every source to every target.
			sourceColumns = a.multiAssignSources(val, currentScope)
		default:
			sourceColumns = a.extractColumnsFromNode(rt.Val, currentScope)
			if insertSources != nil {
				sourceColumns = replaceExcluded(sourceColumns, insertSources)
			}
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
			resolutions, err := currentScope.ResolveColumnRefs(sourceCol)
			if err != nil {
				a.diagnostics.Unresolved("an assignment", sourceCol)
				continue
			}

			isTemp := targetTable == resultTableName
			for _, res := range resolutions {
				// A CTE or derived table contributes the lineage of its own columns:
				// there is no stored relation to point an edge at, so the assignment
				// has to be traced through it the way every other emitter traces a
				// temporary source.
				if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
					a.traceThroughTableLineageToTarget(res.Relation, res.Ref.Column, targetSchema, targetTable, targetColumn, transformInfo)
					continue
				}
				a.addRelation(scope.NewSchemaLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					targetSchema, targetTable, targetColumn,
					transformInfo,
					isTemp,
				))
			}
		}
	}
}

// replaceExcluded replaces a reference to the EXCLUDED pseudo-relation with the
// sources the INSERT writes to that column. A reference to a column the insert
// does not write (a literal, or a column outside its target list) contributes
// nothing.
func replaceExcluded(refs []scope.ColumnRef, insertSources map[string][]scope.ColumnRef) []scope.ColumnRef {
	out := make([]scope.ColumnRef, 0, len(refs))
	for _, ref := range refs {
		if !strings.EqualFold(ref.Table, excludedRelationName) {
			out = append(out, ref)
			continue
		}
		out = append(out, insertSources[ref.Column]...)
	}
	return out
}

// multiAssignSources returns the sources of one column of a multi-column
// assignment's row expression. The expression is analyzed in its own scope, like
// any other subquery, and its output columns keep their order so the assignment's
// column number selects one.
func (a *Analyzer) multiAssignSources(ref *pgast.MultiAssignRef, sp *scope.Scope) []scope.ColumnRef {
	sub, ok := ref.Source.(*pgast.SubLink)
	if !ok {
		return nil
	}
	sel, ok := sub.Subselect.(*pgast.SelectStmt)
	if !ok {
		return nil
	}

	a.scopeStack = append(a.scopeStack, scope.NewScope(sp))
	a.processSelectStmt(sel)
	subScope := a.popScope()
	if subScope == nil {
		return nil
	}
	// The row expression's rows decide the value written, so its predicates
	// belong to the query that writes it.
	a.influences.Inherit(subScope, sp)

	columns := a.resolveOutputColumns(subScope, subScope.GetOutputColumns())
	index := ref.Colno - 1
	if index < 0 || index >= len(columns) {
		return nil
	}
	return scope.Refs(columns[index].Sources)
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
				a.diagnostics.Unresolved("a DELETE condition", condCol)
				continue
			}

			transform := []model.Transformation{
				model.NewDeleteTransformation(conditionText),
			}

			isTemp := targetTable == resultTableName
			for _, res := range resolutions {
				if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
					a.traceThroughTableLineageToTarget(res.Relation, res.Ref.Column, targetSchema, targetTable, deletionFieldName, transform)
					continue
				}
				a.addRelation(scope.NewSchemaLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					targetSchema, targetTable, deletionFieldName,
					transform,
					isTemp,
				))
			}
		}
	}

	a.processReturning(stmt.ReturningList, stmt.Relation)
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
		a.processSelectQuery(sel)
		a.realTarget = previous
	}

	a.emitOutputColumnsToTarget(targetSchema, targetView, explicitColumnNames)
}

// emitOutputColumnsToTarget maps the current scope's output columns onto the
// columns of the object the statement creates, positionally. The current scope's
// own predicates are emitted against the same object, because they decided which
// rows reached it.
func (a *Analyzer) emitOutputColumnsToTarget(targetSchema, targetTable string, explicitColumnNames []string) {
	sp := a.currentScope()

	for i, outputCol := range sp.GetOutputColumns() {
		targetColName := outputCol.Alias
		if i < len(explicitColumnNames) {
			targetColName = explicitColumnNames[i]
		}

		for _, source := range outputCol.Sources {
			resolutions, err := sp.ResolveColumnRefs(source.Ref)
			if err != nil {
				a.diagnostics.Unresolved("an output target", source.Ref)
				continue
			}
			isTemp := targetTable == resultTableName
			for _, res := range resolutions {
				if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
					a.traceThroughTableLineageToTarget(res.Relation, res.Ref.Column, targetSchema, targetTable, targetColName, source.Transform)
					continue
				}
				a.addRelation(scope.NewSchemaLineageEdge(
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

// processCreateTableAsStmt processes CREATE TABLE AS, CREATE MATERIALIZED VIEW and
// SELECT INTO. All three write the query's output into the object they name; the
// SELECT INTO spelling reaches the analyzer as a plain SelectStmt with an INTO
// clause and is dispatched from processSelectStmt.
func (a *Analyzer) processCreateTableAsStmt(stmt *pgast.CreateTableAsStmt) {
	if stmt == nil {
		return
	}

	if stmt.Into == nil || stmt.Into.Rel == nil {
		// No target to write into, so the query's result is the statement's.
		if sel, ok := stmt.Query.(*pgast.SelectStmt); ok {
			a.processSelectQuery(sel)
		}
		return
	}

	a.processQueryToTarget(stmt.Query, stmt.Into)
}

// processQueryToTarget analyzes a query whose output is written into the object
// an INTO clause names: CREATE TABLE AS, CREATE MATERIALIZED VIEW and
// SELECT ... INTO. A column list on the clause renames the query's output
// positionally, which is what the created object's synced column list holds.
func (a *Analyzer) processQueryToTarget(query pgast.Node, into *pgast.IntoClause) {
	targetTable := ""
	targetSchema := ""
	var explicitColumnNames []string
	if into != nil {
		if into.Rel != nil {
			targetTable = into.Rel.Relname
			targetSchema = into.Rel.Schemaname
		}
		explicitColumnNames = stringList(into.ColNames)
	}

	if sel, ok := query.(*pgast.SelectStmt); ok {
		previous := a.realTarget
		a.realTarget = true
		a.processSelectQuery(sel)
		a.realTarget = previous
	}

	a.emitOutputColumnsToTarget(targetSchema, targetTable, explicitColumnNames)
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
		for _, source := range outputCol.Sources {
			a.generateEdgeFromSource(sp, source.Ref, "", resultTableName, outputCol.Alias, source.Transform)
		}
	}
	a.emitPredicateInfluences(sp, "", resultTableName)
}

// generateEdgesForDataModification generates lineage edges for an INSERT: the
// columns the query it writes from produced, written into the target positionally.
// That query's scope is passed in because it is not the statement's: an insert's
// source is analyzed as a subquery, so both the sources and the row predicates are
// read from the scope that query ran in.
func (a *Analyzer) generateEdgesForDataModification(sourceScope *scope.Scope, targetSchema, targetTable string, targetColumns []string) {
	outputColumns := sourceScope.GetOutputColumns()

	for i, outputCol := range outputColumns {
		// The column list declares exactly which targets the query fills;
		// PostgreSQL rejects a query that produces more columns than the list, so
		// an extra output has no target and must not be given one.
		if len(targetColumns) > 0 && i >= len(targetColumns) {
			break
		}
		targetColName := outputCol.Alias
		if i < len(targetColumns) {
			targetColName = targetColumns[i]
		}

		for _, source := range outputCol.Sources {
			a.generateEdgeFromSource(sourceScope, source.Ref, targetSchema, targetTable, targetColName, source.Transform)
		}
	}
	a.emitPredicateInfluences(sourceScope, targetSchema, targetTable)
}

// generateEdgeFromSource generates a lineage edge from a source column to a target.
// Handles tracing through temporary tables (CTEs and subqueries).
func (a *Analyzer) generateEdgeFromSource(sp *scope.Scope, sourceCol scope.ColumnRef, targetSchema, targetTable, targetColName string, transform []model.Transformation) {
	resolutions, err := sp.ResolveColumnRefs(sourceCol)
	if err != nil {
		a.diagnostics.Unresolved("an output column", sourceCol)
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
		isTemp := targetTable == resultTableName
		relation := scope.NewSchemaLineageEdge(
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
	algorithm.TraceThroughTableLineage(scope.NewSchemaLineageEdge, a.addRelation, tableRef, columnName, outputAlias, transform)
}

// traceThroughTableLineageToTarget traces lineage through a temporary table to a specific target table.
func (a *Analyzer) traceThroughTableLineageToTarget(tableRef *scope.TableRef, columnName string, targetSchema string, targetTable string, targetColumn string, transform []model.Transformation) {
	algorithm.TraceThroughTableLineageToTarget(scope.NewSchemaLineageEdge, a.addRelation, tableRef, columnName, targetSchema, targetTable, targetColumn, transform)
}

// flattenTempSourceLineage flattens lineage edges when the source is a temporary table.
// Returns true if the source was a temporary table and was handled.
func (*Analyzer) flattenTempSourceLineage(sp *scope.Scope, relation *scope.TableRef, columnName, targetTable string, targetColumn string, transform []model.Transformation, lineage *[]model.ColumnRelation) bool {
	return algorithm.FlattenTempSourceLineage(scope.NewSchemaLineageEdge, sp, relation, columnName, targetTable, targetColumn, transform, lineage)
}

// flattenTempSources replaces a source that resolved to a query-local relation
// with the stored relations that relation's own lineage came from.
func (*Analyzer) flattenTempSources(sp *scope.Scope, ref scope.ColumnRef, relation *scope.TableRef, transform []model.Transformation) []scope.ColumnSource {
	return algorithm.FlattenTempSources(sp, ref, relation, transform)
}

// ---------------------------------------------------------------------------
// Scope management
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Edge management
// ---------------------------------------------------------------------------

// addRelation adds a column relation to the lineage graph with deduplication.
// A query-local relation (a CTE or a derived table) is never an endpoint here:
// every path that resolves one traces through its own lineage first, so the edge
// already names the stored relation the column came from.
func (a *Analyzer) addRelation(relation model.ColumnRelation) {
	a.edges.Add(relation)
}

// expandWildcardWithCatalog expands a SELECT * using the catalog metadata.
// Returns true if expansion was successful.
func (a *Analyzer) expandWildcardWithCatalog(tableRef *scope.TableRef, sp *scope.Scope) bool {
	tableMeta := a.catalogTable(tableRef)
	if tableMeta == nil {
		return false
	}

	for _, colMeta := range tableMeta.Columns {
		sp.AddOutputColumn(scope.OutputColumn{
			Alias: colMeta.Name,
			Sources: scope.NewColumnSources([]scope.ColumnRef{{
				Schema: tableRef.Schema,
				Table:  tableRef.Table,
				Column: colMeta.Name,
				// The catalog identified the real column, so the reference must not
				// be rebound by name (which fails for an aliased relation).
				Resolved: true,
			}}, nil),
			IsDerived: false,
		})
	}

	return true
}

func normalizeExpressionText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
