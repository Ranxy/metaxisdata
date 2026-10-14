// Package mssql provides direct lineage analysis for T-SQL (Microsoft SQL Server)
// queries.
//
// It is built on github.com/bytebase/omni's T-SQL parser and typed AST, and is a
// port rather than a dialect copy: a set operation is not a node of its own but
// fields on SelectStmt (Op/All/Larg/Rarg), a relation's qualifier is its schema, and
// every query body — a derived table, an expression subquery, a CTE — arrives typed,
// so there is no raw text to re-parse the way StarRocks has to.
//
// Three upstream AST facts shape the traversal and are recorded in
// .agents/docs/mssql-lineage.md: `SelectStmt.All` doubles as SELECT ALL, so it means
// "keeps duplicates" only when Op is set; a scalar subquery, an aliased table
// reference and every declared name list carry no end offset; and two keywords are
// read as aliases (`FROM t1 NATURAL JOIN t2`, `MERGE INTO tgt USING src`), which is
// why relationAlias confirms an alias against the source text. No issue, patch or
// report is filed against the upstream module; see
// .agents/docs/omni-upstream-defects.md.
//
// A statement kind the analyzer cannot represent is reported as a structured gap
// beside the edges it did find, so a coverage gap never reads as "no lineage" and
// never discards edges that are right.
package mssql

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	omnimssql "github.com/bytebase/omni/mssql"
	nodes "github.com/bytebase/omni/mssql/ast"
	mssqlparser "github.com/bytebase/omni/mssql/parser"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/algorithm"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"
)

// Special table/column markers shared with the other analyzers.
const (
	resultTableName   = model.ResultTableName
	deletionFieldName = model.DeletionColumnName
	wildcardColumn    = model.WildcardColumn
	fileSourceMarker  = model.FileSourceName
)

// Registration binds MSSQL to the analyzer this package provides. The process
// assembles the registered engines where it is built, so this package does not
// register itself into package state.
func Registration() lineage.EngineRegistration {
	return lineage.EngineRegistration{
		Engine:              storepb.Engine_MSSQL,
		Analyze:             Analyze,
		Split:               SplitStatements,
		FoldIdentifierNames: identifierNamesAreCaseInsensitive,
	}
}

// identifierNamesAreCaseInsensitive is the dialect's one statement of a fact two layers
// obey. T-SQL binds an identifier by the database collation, which is case-insensitive by
// default, so `SELECT T.a FROM t` and a CTE named in another case than it was written all
// name the relation they were written as; the scope resolves names by this rule, and the
// catalog layer folds its registry lookups by it, so both agree on which object a
// statement named. Registration exports it so a caller outside this package can ask too.
const identifierNamesAreCaseInsensitive = true

// Analyzer analyzes one T-SQL statement.
type Analyzer struct {
	ctx        context.Context
	sql        string
	scopeStack []*scope.Scope
	edges      *algorithm.EdgeSet
	errors     []string
	catalog    *catalog.Cache

	influences  *algorithm.Influences
	diagnostics *algorithm.Diagnostics

	// contextDatabase is the database of the object being analyzed, read from the
	// analysis context. A three-part name's database qualifier is compared against
	// it, because this analyzer records a schema and the registry fills the database
	// in from the context: only a name that agrees with the context can be recorded
	// as the object it names.
	contextDatabase string

	// namedWindows holds the statement's WINDOW clause while its query is processed,
	// so `OVER w` can read the keys the window's own definition carries.
	namedWindows map[string]*nodes.WindowDef

	// inTargetContext suppresses result edges while a statement's query is analyzed:
	// its columns are mapped onto the object the statement writes instead.
	inTargetContext bool
	// inSetOpArm suppresses an arm's own result edges: the merged operation emits
	// them once.
	inSetOpArm bool
}

// Analyze parses a single T-SQL statement and returns its column relations.
func Analyze(ctx context.Context, sql string, cat catalog.Provide) ([]model.ColumnRelation, error) {
	return NewAnalyzer(ctx, sql, cat).AnalyzeRelations()
}

// NewAnalyzer creates a T-SQL lineage analyzer for a single statement.
func NewAnalyzer(ctx context.Context, sql string, catalogProvide catalog.Provide) *Analyzer {
	diagnostics := algorithm.NewDiagnostics()
	// A T-SQL identifier is bound by the database's collation, which is case-insensitive by
	// default, so `SELECT T.a FROM t`, `SELECT X.a FROM t AS x` and a CTE named in another case
	// than it was written all name the relation they were written as. The scope resolves names
	// byte for byte unless a dialect says otherwise, and this is the dialect that does; every
	// child scope inherits it. The same fact reaches the catalog layer, which folds the
	// registry lookups it makes for this analysis: without it a name the statement spelled in
	// another case than the sync registered would find no metadata at all.
	if identifierNamesAreCaseInsensitive {
		ctx = catalog.WithIdentifierFolding(ctx)
	}
	root := scope.NewScope(nil)
	root.SetFoldNames(identifierNamesAreCaseInsensitive)
	analyzer := &Analyzer{
		ctx:         ctx,
		sql:         sql,
		scopeStack:  []*scope.Scope{root},
		edges:       algorithm.NewEdgeSet(),
		catalog:     catalog.NewCache(catalogProvide),
		influences:  algorithm.NewInfluences(diagnostics),
		diagnostics: diagnostics,
	}
	if analysisContext, ok := catalog.GetAnalysisContext(ctx); ok {
		analyzer.contextDatabase = analysisContext.Database
	}
	return analyzer
}

// AnalyzeRelations parses the SQL and returns its column relations.
//
// Parsing is strict: a statement omni cannot parse is an error, never a partial
// result. Multi-statement input is rejected because the analyzer is defined for
// exactly one statement; the caller splits a script and analyzes it statement by
// statement.
func (a *Analyzer) AnalyzeRelations() ([]model.ColumnRelation, error) {
	statements, err := omnimssql.Parse(a.sql)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse MSSQL SQL")
	}
	// A WITH clause in front of a data-modification statement is not attached to it:
	// omni reads the CTE list, finds no SELECT to attach it to, and leaves an empty
	// statement followed by the statement it belonged to. The list is recovered from the
	// source text below, before the statement is analyzed; see withclause.go.
	cteCarrier := len(statements) > 1 && nodeOrNil(statements[0].AST) == nil && isDataModification(statements[1].AST)

	parsed := make([]nodes.Node, 0, len(statements))
	for _, statement := range statements {
		if nodeOrNil(statement.AST) == nil || isBatchSeparator(statement.AST) {
			continue
		}
		parsed = append(parsed, statement.AST)
	}
	if len(parsed) != 1 {
		return nil, errors.Errorf("expected exactly 1 statement, got %d", len(parsed))
	}

	if cteCarrier {
		// The parser dropped the CTE list, so it is recovered from the source text before the
		// statement is analyzed: a reference anywhere in it may name a definition that is not
		// in the tree, and an edge from a statement that lost part of its text cannot be
		// trusted. A recovery that fails is still a hard failure, not a gap.
		withClause, recovered := recoverWithClause(a.sql)
		if !recovered || !attachWithClause(parsed[0], withClause) {
			return nil, errors.New("analysis errors: a WITH clause in front of a data-modification statement is dropped by the T-SQL parser")
		}
	}

	a.dispatch(parsed[0])

	return a.partialOrComplete()
}

// partialOrComplete returns the edges with what could not be represented beside
// them. A statement the analyzer cannot analyze at all fails whole — its edges would
// be wrong, so they are discarded — while a shape it does not model and a reference
// it could not resolve keep them and are reported instead.
func (a *Analyzer) partialOrComplete() ([]model.ColumnRelation, error) {
	if len(a.errors) > 0 {
		return nil, errors.Errorf("analysis errors: %s", strings.Join(a.diagnostics.AppendTo(a.errors), "; "))
	}
	if notes, omitted := a.diagnostics.Notes(); len(notes) > 0 {
		return a.edges.Edges(), &lineage.UnsupportedStatementError{Diagnostics: notes, Omitted: omitted}
	}
	return a.edges.Edges(), nil
}

// dispatch routes a parsed statement to its analyzer.
func (a *Analyzer) dispatch(stmt nodes.Node) {
	switch s := stmt.(type) {
	case *nodes.SelectStmt:
		a.processSelectStatement(s)
	case *nodes.CreateViewStmt:
		// omni parses ALTER VIEW into the same node, so one handler covers both, and
		// SQL Server stores either statement as a CREATE VIEW in
		// sys.sql_modules.definition.
		a.processCreateView(s)
	case *nodes.InsertStmt:
		a.processInsertStatement(s)
	case *nodes.UpdateStmt:
		a.processUpdateStatement(s)
	case *nodes.DeleteStmt:
		a.processDeleteStatement(s)
	case *nodes.MergeStmt:
		a.processMergeStatement(s)
	case *nodes.BulkInsertStmt:
		a.processBulkInsert(s)
	case *nodes.ExecStmt:
		// EXEC runs a statement that is not in the text being analyzed: dynamic SQL through a
		// literal (`EXEC('…')`, `sp_executesql`) as well as a procedure whose body lives in the
		// instance. An INSERT … EXEC reports the gap where its source belongs; on its own the
		// statement has no edge to record, and saying so is what keeps `EXEC …` from reading as
		// "this statement has no lineage".
		a.diagnostics.NotModelled("a statement the analyzer cannot see", "EXEC")
	case *nodes.CreateProcedureStmt:
		a.diagnostics.NotModelled("a routine body", "CREATE PROCEDURE")
	case *nodes.CreateFunctionStmt:
		a.diagnostics.NotModelled("a routine body", "CREATE FUNCTION")
	default:
		// Statement kinds that carry no column lineage are ignored, as in the other
		// analyzers: DDL other than a view, permissions, transactions, batches,
		// cursors and the like.
	}
}

// ---------------------------------------------------------------------------
// SELECT
// ---------------------------------------------------------------------------

// processSelectStatement processes a query expression: a plain query, a set
// operation, or a query whose rows are written into a new table.
func (a *Analyzer) processSelectStatement(stmt *nodes.SelectStmt) {
	if stmt == nil {
		return
	}
	if stmt.Op != nodes.SetOpNone {
		a.processSetOperation(stmt)
		return
	}
	if stmt.WithClause != nil {
		a.processCTEs(stmt.WithClause)
	}
	if stmt.IntoTable != nil {
		a.processSelectInto(stmt)
		return
	}
	a.processQuerySpecification(stmt)
}

// processQueryNode dispatches a query expression: a plain query or a set operation.
// omni hangs a set operation on the same SelectStmt, so both arrive as one node; the
// guard is there so a query shape this analyzer does not model is reported rather
// than ignored.
func (a *Analyzer) processQueryNode(node nodes.Node) {
	node = nodeOrNil(node)
	if node == nil {
		return
	}
	stmt, ok := node.(*nodes.SelectStmt)
	if !ok {
		a.diagnostics.NotModelled("a query expression", "")
		return
	}
	a.processSelectStatement(stmt)
}

// reportTargetWithoutIdentity records why a statement's target cannot be named in the
// registry, so a statement that writes nowhere the graph can follow says so instead of
// emitting nothing. A table variable and a `#temp` table are session-local state the
// registry never holds; a name in another database is reported where it is read
// (targetAddress), because that one has a statement-written name to point at.
func (a *Analyzer) reportTargetWithoutIdentity(target nodes.Node) {
	switch t := nodeOrNil(target).(type) {
	case *nodes.TableVarRef:
		a.diagnostics.NotModelled("a target the registry cannot address", "the table variable "+t.Name)
	case *nodes.TableRef:
		if strings.HasPrefix(t.Object, "#") {
			a.diagnostics.NotModelled("a target the registry cannot address", "the temporary table "+t.Object)
		}
	default:
	}
}

// processSelectInto processes SELECT ... INTO, which creates the table it names and
// fills it: the created table is the target and the query's columns map onto it
// positionally, exactly as CREATE VIEW maps its own.
func (a *Analyzer) processSelectInto(stmt *nodes.SelectStmt) {
	sp := a.currentScope()
	a.processFromClause(stmt.FromClause)
	if stmt.WhereClause != nil {
		a.collectPredicates(stmt.WhereClause, sp, model.NewFilterTransformation(a.exprTextOf(stmt.WhereClause)), false)
	}
	a.processTargetList(stmt.TargetList, sp, a.groupByKeys(stmt.GroupByClause))

	schema, table, ok := a.targetAddress(stmt.IntoTable)
	if !ok {
		// The rows are written into something the registry has no identity for, so the reads
		// are not re-pointed at the statement's own result either: SELECT ... INTO returns no
		// rows, and an edge to __result__ would say it does.
		a.reportTargetWithoutIdentity(stmt.IntoTable)
		return
	}
	a.generateEdgesForTarget(sp, schema, table, nil, false)
}

// processQuerySpecification processes FROM, then the SELECT list, then emits edges.
func (a *Analyzer) processQuerySpecification(stmt *nodes.SelectStmt) {
	sp := a.currentScope()
	previousWindows := a.namedWindows
	a.namedWindows = windowDefinitions(stmt.WindowClause)

	a.processFromClause(stmt.FromClause)
	// A WHERE predicate decides which rows the query emits without its value
	// reaching any output column, so it is recorded as an influence on the
	// statement's rows rather than on a column.
	if stmt.WhereClause != nil {
		a.collectPredicates(stmt.WhereClause, sp, model.NewFilterTransformation(a.exprTextOf(stmt.WhereClause)), false)
	}

	a.processTargetList(stmt.TargetList, sp, a.groupByKeys(stmt.GroupByClause))

	// HAVING is the one clause that may name a select-list alias, so it resolves one
	// before falling back to the scope.
	if stmt.HavingClause != nil {
		a.collectPredicates(stmt.HavingClause, sp, model.NewFilterTransformation(a.exprTextOf(stmt.HavingClause)), true)
	}

	a.namedWindows = previousWindows
	a.generateEdges(sp)
}

// windowDefinitions indexes a statement's named window definitions, which `OVER w`
// names. A definition is part of the query it appears in, so it is only in scope
// while that query is processed.
func windowDefinitions(list *nodes.List) map[string]*nodes.WindowDef {
	if list == nil {
		return nil
	}
	out := make(map[string]*nodes.WindowDef, len(list.Items))
	for _, item := range list.Items {
		if definition, ok := item.(*nodes.WindowDef); ok && definition != nil && definition.Name != "" {
			out[definition.Name] = definition
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// FROM clause
// ---------------------------------------------------------------------------

// processFromClause processes the relations a FROM clause introduces.
func (a *Analyzer) processFromClause(from *nodes.List) {
	if from == nil {
		return
	}
	for _, item := range from.Items {
		if item == nil {
			continue
		}
		a.processTableExpr(item)
		// The relations have to be in scope before a join condition's columns can be
		// resolved.
		a.collectJoinPredicates(item)
	}
}

// processTableExpr processes one table source.
func (a *Analyzer) processTableExpr(te nodes.Node) {
	switch t := nodeOrNil(te).(type) {
	case *nodes.TableRef:
		a.addBaseTable(t)
	case *nodes.AliasedTableRef:
		a.processAliasedTableRef(t)
	case *nodes.JoinClause:
		a.processTableExpr(t.Left)
		a.processTableExpr(t.Right)
	case *nodes.SubqueryExpr:
		a.processDerivedTable(t, "", nil)
	case *nodes.ValuesClause:
		a.processInlineValues(t, "values", nil)
	case *nodes.FuncCallExpr:
		a.processTableFunction(t, "", nil)
	case *nodes.PivotExpr:
		a.processPivot(t, "")
	case *nodes.UnpivotExpr:
		a.processUnpivot(t, "")
	case *nodes.FullTextTableRef:
		// CONTAINSTABLE / FREETEXTTABLE search a real table, so the statement still
		// reads it; the columns the search itself reads are not modelled.
		if t != nil {
			a.addBaseTable(t.Table)
		}
	case *nodes.TableVarRef, *nodes.TableVarMethodCallRef:
		// A table variable is query-local state, not a registry object.
	case *nodes.SemanticTableRef:
		a.diagnostics.NotModelled("a semantic model table", "SEMANTIC")
	default:
		// A source that names no stored relation.
	}
}

// processAliasedTableRef processes a table source with an alias, which is how omni
// wraps a derived table, a table function and an inline table.
func (a *Analyzer) processAliasedTableRef(alias *nodes.AliasedTableRef) {
	if alias == nil {
		return
	}
	name := alias.Alias
	columns := columnList(alias.Columns)
	switch inner := alias.Table.(type) {
	case *nodes.SubqueryExpr:
		a.processDerivedTable(inner, name, columns)
	case *nodes.TableRef:
		// The wrapper's alias is the one the statement wrote; an alias the inner
		// reference already carries is the parser's, and a keyword among them is a
		// clause rather than a name (see relationAlias).
		if name != "" {
			inner.Alias = name
		}
		a.addBaseTable(inner)
	case *nodes.FuncCallExpr:
		a.processTableFunction(inner, name, columns)
	case *nodes.ValuesClause:
		a.processInlineValues(inner, name, columns)
	case *nodes.PivotExpr:
		a.processPivot(inner, name)
	case *nodes.UnpivotExpr:
		a.processUnpivot(inner, name)
	default:
		a.processTableExpr(alias.Table)
	}
}

// addBaseTable registers a stored relation in the current scope, or the CTE it
// names when it names one.
func (a *Analyzer) addBaseTable(ref *nodes.TableRef) {
	if ref == nil || ref.Object == "" {
		return
	}
	if !a.databaseMatches(ref.Database) {
		a.diagnostics.UnresolvedQualifier("a relation", objectName(ref.Database, ref.Schema, ref.Object))
		return
	}
	// A `#temp` table is session-local state rather than a registry object. Registering it
	// anyway made every reference to it an edge endpoint naming an object no registry holds;
	// leaving it out reports each reference as unresolved instead.
	if strings.HasPrefix(ref.Object, "#") {
		a.diagnostics.NotModelled("a relation the registry cannot address", "the temporary table "+ref.Object)
		return
	}
	alias := a.relationAlias(ref)

	// A CTE is query-local and is only ever named from a FROM clause, and a qualified
	// name always names a stored relation: `s.t` must not be read as a CTE named t.
	// A linked server is a stored relation's source too, so only an unqualified name
	// can be a CTE.
	if ref.Server == "" && ref.Database == "" && ref.Schema == "" {
		if cte, ok := a.currentScope().FindCTE(ref.Object); ok {
			// Reading the CTE's rows carries the predicates that shaped them into
			// this query.
			a.influences.InheritCTE(a.currentScope(), cte)
			tableRef := &scope.TableRef{Table: ref.Object, Alias: alias, IsCTE: true, Lineage: cte.Lineage}
			scope.AttachTempColumnLookup(tableRef, cte.Columns)
			a.currentScope().AddTable(tableRef)
			return
		}
	}

	tableRef := &scope.TableRef{Schema: ref.Schema, Table: ref.Object, Alias: alias}
	a.canonicalRelation(tableRef)
	a.attachColumnLookup(tableRef)
	a.currentScope().AddTable(tableRef)
}

// relationAlias returns the name a SQL reference addresses a relation by, or an empty string
// when the statement gave it none.
//
// omni's T-SQL parser reads a clause keyword as a table alias in two shapes — `FROM t1 NATURAL
// JOIN t2` makes NATURAL t1's alias, and `MERGE INTO tgt USING src` makes USING the target's
// alias. Both keywords are only ODBC and future reserved words rather than T-SQL reserved ones,
// so the statement may also have written one as a genuine alias (`FROM t1 NATURAL`); what tells
// the two apart is the token that follows. Omni is not patched or reported to; see
// .agents/docs/omni-upstream-defects.md section 10.2.
func (a *Analyzer) relationAlias(ref *nodes.TableRef) string {
	if ref.Alias == "" {
		return ""
	}
	if clauseKeywordAliases[strings.ToUpper(ref.Alias)] && a.aliasIsFollowingClause(ref) {
		return ""
	}
	return ref.Alias
}

// clauseKeywordAliases are the keywords the parser can consume as an alias while the statement
// has none, and the token that proves the alias is really the clause: NATURAL is followed by
// JOIN in the shape the parser mis-reads, and USING by the row source it introduces.
var clauseKeywordAliases = map[string]bool{"NATURAL": true, "USING": true}

// fromItemEnd reports whether a token ends a FROM item rather than starting the row source a
// USING clause introduces. It is a deny list because the row source may be a name, a bracketed
// name, a function call or a parenthesised query.
func fromItemEnd(text string) bool {
	switch text {
	case ",", ";", ")", "JOIN", "INNER", "LEFT", "RIGHT", "FULL", "CROSS", "OUTER",
		"WHERE", "GROUP", "ORDER", "HAVING", "UNION", "EXCEPT", "INTERSECT",
		"ON", "WHEN", "OPTION", "FOR", "PIVOT", "UNPIVOT", "WITH", "TABLESAMPLE":
		return true
	default:
		return false
	}
}

// aliasIsFollowingClause reports whether the alias the parser recorded is really the clause
// that follows the relation's own name.
//
// The recorded alias sits either inside the relation's own span (`t1 NATURAL`, whose Loc covers
// both) or in the token after it (`tgt USING`, whose Loc covers only the name), so the decision
// is made on the token stream: comments are not tokens, and a table hint between the name and
// the clause is skipped with its parentheses, so `MERGE INTO tgt /* c */ WITH (READPAST) USING
// src` reads the same as the plain form. An `AS` before the token means the statement wrote the
// alias.
func (a *Analyzer) aliasIsFollowingClause(ref *nodes.TableRef) bool {
	loc := ref.Loc
	if !loc.IsValid() || loc.Start < 0 || loc.End > len(a.sql) {
		return false
	}
	alias := strings.ToUpper(ref.Alias)
	text := strings.ToUpper(normalizeExpressionText(a.sql[loc.Start:loc.End]))
	switch {
	case strings.HasSuffix(text, " AS "+alias), strings.HasSuffix(text, "["+alias+"]"):
		// The statement wrote the alias; it is one however much it looks like a clause.
		return false
	default:
	}

	tokens := mssqlparser.Tokenize(a.sql)
	index := firstTokenAtOrAfter(tokens, loc.End)
	if index < 0 {
		return false
	}
	if tokenText(a.sql, tokens[index]) == "AS" {
		return false
	}
	index = skipTableHint(a.sql, tokens, index)
	if index < 0 {
		return false
	}
	if tokenText(a.sql, tokens[index]) == alias {
		// The parser recorded the keyword as the next token rather than inside the span.
		index = skipTableHint(a.sql, tokens, index+1)
		if index < 0 {
			return false
		}
	}
	switch alias {
	case "NATURAL":
		return tokenText(a.sql, tokens[index]) == "JOIN"
	case "USING":
		return !fromItemEnd(tokenText(a.sql, tokens[index]))
	default:
		return false
	}
}

// firstTokenAtOrAfter returns the index of the first token starting at or after offset.
func firstTokenAtOrAfter(tokens []mssqlparser.Token, offset int) int {
	for i, token := range tokens {
		if token.Loc >= offset {
			return i
		}
	}
	return -1
}

// skipTableHint returns the index of the first token after a `WITH (…)` table hint, or index
// itself when the tokens there are not a hint.
func skipTableHint(sql string, tokens []mssqlparser.Token, index int) int {
	if index >= 0 && index < len(tokens) && tokenText(sql, tokens[index]) == "WITH" {
		depth := 0
		for ; index < len(tokens); index++ {
			switch tokens[index].Type {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					return index + 1
				}
			default:
			}
		}
		return -1
	}
	return index
}

// tokenText returns a token's source text, uppercased, with any trailing space the lexer
// swallowed removed.
func tokenText(sql string, token mssqlparser.Token) string {
	return strings.ToUpper(sql[token.Loc:trimEnd(sql, token.End)])
}

// attachColumnLookup lets the scope resolver disambiguate an unqualified column with
// catalog metadata for this base table, wherever the reference is resolved: the
// statement itself, a CTE body, a derived table or an expression subquery.
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

// registeredColumn returns a DML target's column under the spelling the registry gives it, for
// the same reason a source column takes it: a statement may spell one object's field two ways
// (`INSERT INTO Users (id) SELECT id FROM Users`), and the graph walks a field trail on
// (object, column) pairs. A column the target does not have keeps the statement's spelling, and
// so does every column of a target the registry does not know.
func (a *Analyzer) registeredColumn(schema, table, column string) string {
	if a.catalog == nil || column == "" {
		return column
	}
	meta := a.catalogTable(&scope.TableRef{Schema: schema, Table: table})
	if meta == nil {
		return column
	}
	names := make([]string, 0, len(meta.Columns))
	for _, col := range meta.Columns {
		names = append(names, col.Name)
	}
	return spellingIn(names, column)
}

// targetAddress reads the schema and object a data-modification statement writes,
// reporting the database qualifier as a gap when it names another database.
func (a *Analyzer) targetAddress(te nodes.Node) (schema, table string, ok bool) {
	ref, isTable := te.(*nodes.TableRef)
	if !isTable || ref == nil || ref.Object == "" {
		return "", "", false
	}
	if !a.databaseMatches(ref.Database) {
		a.diagnostics.UnresolvedQualifier("a target relation", objectName(ref.Database, ref.Schema, ref.Object))
		return "", "", false
	}
	// A `#temp` table lives in tempdb for the session and is never a registry object, so a
	// statement writing into one has no target the graph can follow.
	if strings.HasPrefix(ref.Object, "#") {
		return "", "", false
	}
	tableRef := &scope.TableRef{Schema: ref.Schema, Table: ref.Object}
	a.canonicalRelation(tableRef)
	return tableRef.Schema, tableRef.Table, true
}

// canonicalRelation rewrites a stored relation's address to the identity the registry knows it
// by, which is the spelling the sync registered rather than the one the statement wrote.
//
// This dialect binds identifiers case-insensitively, so `FROM users` and the registered `Users`
// are one object — but an edge records a name, and the registry is addressed by a meta GUID that
// is compared byte for byte. Recording the statement's spelling therefore named an object the
// registry does not have, and the graph drew a node beside the object instead of the object. The
// two registration points for stored relations — a FROM item and a data-modification target —
// are where the address is settled, and every edge reads it from there afterwards.
//
// A relation the registry does not know keeps the statement's spelling, and so does one whose
// lookup failed: a name nobody could resolve is better recorded as written than dropped, and the
// failed lookup is already reported as a gap by catalogTable.
func (a *Analyzer) canonicalRelation(tableRef *scope.TableRef) {
	if a.catalog == nil || tableRef == nil || tableRef.Table == "" {
		return
	}
	meta := a.catalogTable(tableRef)
	if meta == nil || meta.ID.Name == "" {
		return
	}
	tableRef.Schema, tableRef.Table = meta.ID.Schema, meta.ID.Name
}

// registerTargetRelation registers a data-modification target in the current scope,
// so that an assignment's own right-hand side resolves against the row being
// written.
func (a *Analyzer) registerTargetRelation(te nodes.Node) (schema, table string, ok bool) {
	ref, isTable := te.(*nodes.TableRef)
	if !isTable {
		return "", "", false
	}
	schema, table, ok = a.targetAddress(ref)
	if !ok {
		return "", "", false
	}
	tableRef := &scope.TableRef{Schema: schema, Table: table, Alias: a.relationAlias(ref)}
	a.attachColumnLookup(tableRef)
	a.currentScope().AddTable(tableRef)
	return schema, table, true
}

// databaseMatches reports whether a name's database qualifier can be recorded. A
// three-part name is the only way T-SQL names a database, and this analyzer records
// a relation's schema: the database is filled in from the analysis context, so a
// qualifier that disagrees with it names an object the edge would point at wrongly.
// An unqualified name, or one written while no context is known, is accepted.
func (a *Analyzer) databaseMatches(database string) bool {
	return database == "" || a.contextDatabase == "" || strings.EqualFold(database, a.contextDatabase)
}

// objectName renders a three-part name for a diagnostic.
func objectName(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, ".")
}

// ---------------------------------------------------------------------------
// Query-local relations
// ---------------------------------------------------------------------------

// processDerivedTable processes a derived table: its body is analyzed in a scope of
// its own, and the alias is registered as a temporary relation carrying the body's
// lineage.
func (a *Analyzer) processDerivedTable(sub *nodes.SubqueryExpr, alias string, declared []string) {
	if sub == nil || sub.Query == nil {
		return
	}
	a.pushScope()
	a.processQueryNode(sub.Query)
	subqueryScope := a.popScope()
	if subqueryScope == nil {
		return
	}
	// The derived table's rows are part of the enclosing query's rows, so the
	// predicates that shaped them reach its output.
	a.influences.Inherit(subqueryScope, a.currentScope())

	if alias == "" {
		// An unaliased derived table is one T-SQL rejects, so a name is only needed
		// so that its lineage has somewhere to point.
		alias = "derived"
	}
	// A declared column list renames the body's output columns, which is what the
	// statement that reads the table addresses; without one the body's own names are
	// what it offers. ExposedColumnNames keeps the two in step when the declaration
	// names fewer columns than the body produces.
	names := scope.ExposedColumnNames(declared, subqueryScope.GetOutputColumns())
	tableRef := &scope.TableRef{
		Table:      alias,
		Alias:      alias,
		IsSubquery: true,
		Lineage:    a.tempTableLineage(subqueryScope, names, alias),
	}
	scope.AttachTempColumnLookup(tableRef, names)
	a.currentScope().AddTable(tableRef)
}

// processTableFunction registers a table-valued function as a query-local relation.
// The function names no stored relation, but its arguments are what it builds its
// rows from, so the columns they read are the sources of its columns.
func (a *Analyzer) processTableFunction(call *nodes.FuncCallExpr, alias string, declared []string) {
	if call == nil {
		return
	}
	name := funcCallName(call)
	if alias == "" {
		alias = name
	}
	if alias == "" {
		return
	}
	columns := declared
	if len(columns) == 0 {
		columns = []string{name}
	}

	sp := a.currentScope()
	var sources []scope.ColumnRef
	for _, argument := range listNodes(call.Args) {
		sources = append(sources, a.extractColumnsFromNode(argument, sp)...)
	}
	lineage := make([]model.ColumnRelation, 0, len(columns)*len(sources))
	for _, column := range columns {
		for _, source := range sources {
			resolutions, err := sp.ResolveColumnRefs(source)
			if err != nil {
				a.diagnostics.Unresolved("a table function argument", source)
				continue
			}
			for _, res := range resolutions {
				res = a.canonicalResolution(res)
				if a.flattenTempSourceLineage(sp, res.Relation, res.Ref.Column, alias, column, nil, &lineage) {
					continue
				}
				lineage = append(lineage, scope.NewSchemaLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					"", alias, column,
					nil,
					true, // the function's rows are not a stored object
				))
			}
		}
	}

	tableRef := &scope.TableRef{Table: alias, Alias: alias, IsSubquery: true, Lineage: lineage}
	scope.AttachTempColumnLookup(tableRef, columns)
	sp.AddTable(tableRef)
}

// processInlineValues registers an inline table — `(VALUES (…), (…)) v (x)` — as a
// query-local relation. A row of literals has no source; a row that holds a subquery
// does, and it is a source of the column in its position.
func (a *Analyzer) processInlineValues(values *nodes.ValuesClause, alias string, declared []string) {
	if values == nil || alias == "" {
		return
	}
	rows := valueRows(values)
	if len(rows) == 0 {
		return
	}
	names := declared
	if len(names) == 0 {
		names = a.inferredValuesColumns(rows[0])
	}

	sp := a.currentScope()
	lineage := make([]model.ColumnRelation, 0)
	for _, row := range rows {
		for i, expr := range row {
			if i >= len(names) {
				break
			}
			for _, source := range a.extractColumnsFromNode(expr, sp) {
				resolutions, err := sp.ResolveColumnRefs(source)
				if err != nil {
					a.diagnostics.Unresolved("an inline table row", source)
					continue
				}
				for _, res := range resolutions {
					res = a.canonicalResolution(res)
					if a.flattenTempSourceLineage(sp, res.Relation, res.Ref.Column, alias, names[i], nil, &lineage) {
						continue
					}
					lineage = append(lineage, scope.NewSchemaLineageEdge(
						res.Ref.Schema, res.Ref.Table, res.Ref.Column,
						"", alias, names[i],
						nil,
						true,
					))
				}
			}
		}
	}

	tableRef := &scope.TableRef{Table: alias, Alias: alias, IsSubquery: true, Lineage: lineage}
	scope.AttachTempColumnLookup(tableRef, names)
	sp.AddTable(tableRef)
}

// inferredValuesColumns names the columns of an inline table whose alias declares no
// column list, which SQL Server does not accept anyway: the names only have to be
// distinct.
func (a *Analyzer) inferredValuesColumns(row []nodes.Node) []string {
	names := make([]string, 0, len(row))
	for i, expr := range row {
		name := a.exprTextOf(expr)
		if name == "" || !isPlainColumnRef(expr) {
			name = fmt.Sprintf("column%d", i+1)
		}
		names = append(names, name)
	}
	return names
}

// processPivot registers a PIVOT relation. A pivot turns one column's values into columns:
// SQL Server materialises one output column per value in the IN list, each aggregating the
// aggregate function's argument over the rows the FOR column groups.
//
// The relation also keeps the source's columns that neither the aggregate nor the FOR column
// consumes, as grouping columns. The statement never names them — which ones they are is a
// catalog question — so they are read from the source relation's column list when it is
// known, and the relation exposes the pivot columns alone when it is not.
//
// The source is analyzed in a scope of its own, because it is internal to the pivot: left in
// the enclosing scope it is read as a second row source by a `SELECT *` beside the pivot.
func (a *Analyzer) processPivot(pivot *nodes.PivotExpr, alias string) {
	if pivot == nil {
		return
	}
	if alias == "" {
		alias = pivot.Alias
	}
	if alias == "" {
		alias = "pivot"
	}
	values := columnList(pivot.InValues)
	if len(values) == 0 {
		a.processTableExpr(pivot.Source)
		return
	}

	a.pushScope()
	a.processTableExpr(pivot.Source)
	sourceScope := a.currentScope()
	var transform []model.Transformation
	if call, ok := pivot.AggFunc.(*nodes.FuncCallExpr); ok {
		transform = []model.Transformation{model.NewAggregateTransformation(
			strings.ToUpper(funcCallName(call)),
			a.exprTextOf(pivot.AggFunc),
			[]string{pivot.ForCol},
		)}
	} else {
		transform = []model.Transformation{model.NewProjectTransformation(a.exprTextOf(pivot))}
	}
	sources := a.extractColumnsFromNode(pivot.AggFunc, sourceScope)

	lineage := make([]model.ColumnRelation, 0, len(values)*len(sources))
	for _, value := range values {
		for _, source := range sources {
			resolutions, err := sourceScope.ResolveColumnRefs(source)
			if err != nil {
				a.diagnostics.Unresolved("a PIVOT aggregate", source)
				continue
			}
			for _, res := range resolutions {
				res = a.canonicalResolution(res)
				if a.flattenTempSourceLineage(sourceScope, res.Relation, res.Ref.Column, alias, value, transform, &lineage) {
					continue
				}
				lineage = append(lineage, scope.NewSchemaLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					"", alias, value,
					transform,
					true,
				))
			}
		}
	}

	// A grouping column's value is carried through unchanged, so its edge has no
	// transformation: calling it an aggregate would say the value was computed.
	grouping, groupingLineage := a.pivotGroupingColumns(pivot, alias, sourceScope.Tables(), values, sourceScope)
	lineage = append(lineage, groupingLineage...)

	// The rows a pivot emits are shaped by the predicates that shaped its source's rows, so
	// those influences belong to the statement rather than to the scope being discarded.
	a.influences.Inherit(sourceScope, a.currentScope())
	a.popScope()

	tableRef := &scope.TableRef{Table: alias, Alias: alias, IsSubquery: true, Lineage: lineage}
	scope.AttachTempColumnLookup(tableRef, append(grouping, values...))
	a.currentScope().AddTable(tableRef)
}

// pivotGroupingColumns lists the source columns a pivot carries through, and the lineage that
// says so. A column is carried through when neither the aggregate nor the FOR column consumes
// it, and only a source whose column list the catalog knows can be enumerated: an unknown
// relation contributes nothing rather than a guess.
func (a *Analyzer) pivotGroupingColumns(pivot *nodes.PivotExpr, alias string, sourceRelations []*scope.TableRef, values []string, sp *scope.Scope) ([]string, []model.ColumnRelation) {
	consumed := make(map[string]struct{}, len(values))
	consumed[strings.ToLower(pivot.ForCol)] = struct{}{}
	for _, source := range a.extractColumnsFromNode(pivot.AggFunc, sp) {
		consumed[strings.ToLower(source.Column)] = struct{}{}
	}
	for _, value := range values {
		consumed[strings.ToLower(value)] = struct{}{}
	}

	var (
		names   []string
		lineage []model.ColumnRelation
	)
	for _, relation := range sourceRelations {
		if relation.IsSubquery {
			// A derived table's columns are its own query's, and its lineage already says
			// where they came from; carrying them here would name a relation the statement
			// never wrote.
			continue
		}
		for _, name := range relation.ColumnNames() {
			if _, ok := consumed[strings.ToLower(name)]; ok {
				continue
			}
			consumed[strings.ToLower(name)] = struct{}{}
			names = append(names, name)
			if a.flattenTempSourceLineage(sp, relation, name, alias, name, nil, &lineage) {
				continue
			}
			lineage = append(lineage, scope.NewSchemaLineageEdge(
				relation.Schema, relation.Table, name,
				"", alias, name,
				nil,
				true,
			))
		}
	}
	return names, lineage
}

// processUnpivot registers an UNPIVOT relation: every column in its IN list becomes a row
// whose value column holds that column's value, so each of them is a source of the value
// column and the pivot column is a name the statement itself constructs.
//
// The source is analyzed in a scope of its own, for the same reason a PIVOT's is: it is
// internal to the relation being registered.
func (a *Analyzer) processUnpivot(pivot *nodes.UnpivotExpr, alias string) {
	if pivot == nil {
		return
	}
	if alias == "" {
		alias = pivot.Alias
	}
	if alias == "" {
		alias = "unpivot"
	}
	columns := []string{pivot.ValueCol}
	if pivot.ForCol != "" {
		columns = append(columns, pivot.ForCol)
	}
	unpivoted := columnList(pivot.InCols)

	a.pushScope()
	a.processTableExpr(pivot.Source)
	sourceScope := a.currentScope()
	transform := []model.Transformation{model.NewProjectTransformation("UNPIVOT(" + strings.Join(unpivoted, ",") + ")")}
	lineage := make([]model.ColumnRelation, 0, len(unpivoted))
	for _, name := range unpivoted {
		// The column is read from the source relation, whichever relation that is.
		resolutions, err := sourceScope.ResolveColumnRefs(scope.ColumnRef{Column: name})
		if err != nil {
			a.diagnostics.Unresolved("an UNPIVOT column", scope.ColumnRef{Column: name})
			continue
		}
		for _, res := range resolutions {
			res = a.canonicalResolution(res)
			if a.flattenTempSourceLineage(sourceScope, res.Relation, res.Ref.Column, alias, pivot.ValueCol, transform, &lineage) {
				continue
			}
			lineage = append(lineage, scope.NewSchemaLineageEdge(
				res.Ref.Schema, res.Ref.Table, res.Ref.Column,
				"", alias, pivot.ValueCol,
				transform,
				true,
			))
		}
	}
	a.influences.Inherit(sourceScope, a.currentScope())
	a.popScope()

	tableRef := &scope.TableRef{Table: alias, Alias: alias, IsSubquery: true, Lineage: lineage}
	scope.AttachTempColumnLookup(tableRef, columns)
	a.currentScope().AddTable(tableRef)
}

// tempTableLineage builds the base-table lineage a temporary relation — a CTE, a
// derived table, a table function — exposes under targetName, naming its columns from
// names positionally.
func (a *Analyzer) tempTableLineage(sp *scope.Scope, names []string, targetName string) []model.ColumnRelation {
	lineage := make([]model.ColumnRelation, 0)
	for i, col := range sp.GetOutputColumns() {
		colName := col.Alias
		if i < len(names) {
			colName = names[i]
		}
		if colName == "" {
			colName = "column"
		}
		for _, source := range col.Sources {
			resolutions, err := sp.ResolveColumnRefs(source.Ref)
			if err != nil {
				a.diagnostics.Unresolved("a temporary relation", source.Ref)
				continue
			}
			for _, res := range resolutions {
				res = a.canonicalResolution(res)
				if a.flattenTempSourceLineage(sp, res.Relation, res.Ref.Column, targetName, colName, source.Transform, &lineage) {
					continue
				}
				lineage = append(lineage, scope.NewSchemaLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					"", targetName, colName,
					source.Transform,
					true, // the temporary relation is not a real object
				))
			}
		}
	}
	return lineage
}

// ---------------------------------------------------------------------------
// CTE
// ---------------------------------------------------------------------------

// processCTEs processes a WITH clause.
func (a *Analyzer) processCTEs(with *nodes.WithClause) {
	if with == nil {
		return
	}
	for _, item := range with.CTEs.Items {
		cte, ok := item.(*nodes.CommonTableExpr)
		if !ok || cte == nil {
			continue
		}
		a.processCTE(cte)
	}
}

// processCTE processes a single CTE: its body is analyzed in a nested scope, and its
// output columns are flattened into a lineage the outer query traces through.
//
// The definition is registered in the body's own scope before the body is analyzed,
// with no lineage yet. T-SQL has no RECURSIVE keyword, so a recursive CTE cannot be
// told from a plain one, and a name that refers to the rows produced so far is not
// modelled: registering it makes the self-reference opaque, so neither a source nor
// a predicate resolves to a base table that merely shares the name. The fixpoint is
// not modelled by any dialect here.
func (a *Analyzer) processCTE(cte *nodes.CommonTableExpr) {
	if cte == nil || cte.Name == "" {
		return
	}
	definition := &scope.CTEDefinition{Name: cte.Name, Columns: columnList(cte.Columns)}
	if cte.Query == nil {
		a.currentScope().AddCTE(definition)
		return
	}

	a.pushScope()
	a.currentScope().AddCTE(definition)
	a.processQueryNode(cte.Query)
	cteScope := a.popScope()

	// The body's influences belong to the CTE's rows, so they are held against the
	// definition and reach the statement only if it references the CTE. An
	// unreferenced CTE filters rows nobody reads.
	a.influences.BindCTE(definition, cteScope)
	definition.Lineage = a.tempTableLineage(cteScope, definition.Columns, cte.Name)
	a.currentScope().AddCTE(definition)
}

// ---------------------------------------------------------------------------
// Set operations
// ---------------------------------------------------------------------------

// setOpArm is one leaf SELECT of a set-operation tree together with the chain of
// set-operation transformations that combine it into the statement's result,
// outermost first, so a nested operation is not labelled with only the outermost one.
type setOpArm struct {
	stmt  *nodes.SelectStmt
	chain []model.Transformation
}

// flattenSetOpArms flattens a set-operation tree into its leaf SELECTs in order, so
// every arm of every set operation contributes lineage. omni hangs the operation on
// the node that combines the two arms rather than on a node of its own.
func flattenSetOpArms(stmt *nodes.SelectStmt, chain []model.Transformation) []setOpArm {
	if stmt == nil {
		return nil
	}
	if stmt.Op == nodes.SetOpNone {
		return []setOpArm{{stmt: stmt, chain: chain}}
	}
	inner := chain
	if transform, ok := setOpTransformation(stmt.Op, stmt.All); ok {
		inner = algorithm.ArmChain(chain, transform)
	}
	return append(flattenSetOpArms(stmt.Larg, inner), flattenSetOpArms(stmt.Rarg, inner)...)
}

// processSetOperation handles UNION / INTERSECT / EXCEPT by processing every arm and
// merging their output columns positionally. Each arm is analyzed in its own scope so
// one arm's FROM relations cannot leak into the next, and only the merged operation
// emits result edges, so the set-operation relation type is recorded once. A set
// operation nested in a CTE or derived table contributes the merged columns to its
// parent instead.
func (a *Analyzer) processSetOperation(stmt *nodes.SelectStmt) {
	arms := flattenSetOpArms(stmt, nil)
	if len(arms) == 0 {
		return
	}
	if stmt.WithClause != nil {
		a.processCTEs(stmt.WithClause)
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
			allOutputColumns = append(allOutputColumns, algorithm.ResolveOutputColumns(baseScope, baseScope.GetOutputColumns()))
			continue
		}
		// The arm starts a relation namespace of its own — the first arm's relations
		// are not visible in it — while still reading the CTEs the statement declared.
		tempScope := scope.NewScopeWithDefinitions(baseScope.Parent(), baseScope)
		originalScope := a.currentScope()
		a.scopeStack[len(a.scopeStack)-1] = tempScope
		a.processSetOpArm(arm.stmt)
		a.scopeStack[len(a.scopeStack)-1] = originalScope
		// The arm's rows are part of the merged result, so its predicates reach the
		// operation's output.
		a.influences.Inherit(tempScope, baseScope)
		allOutputColumns = append(allOutputColumns, algorithm.ResolveOutputColumns(tempScope, tempScope.GetOutputColumns()))
	}

	algorithm.MergeSetOpColumns(baseScope, allOutputColumns, armTransforms)
	a.generateEdges(baseScope)
}

// processSetOpArm processes one leaf arm of a set operation, suppressing the arm's
// own result edges so the merged operation emits them once.
func (a *Analyzer) processSetOpArm(arm *nodes.SelectStmt) {
	previous := a.inSetOpArm
	a.inSetOpArm = true
	a.processSelectStatement(arm)
	a.inSetOpArm = previous
}

// setOpTransformation maps a T-SQL set operator onto its transformation. The ALL bit
// is read only here: on a plain SELECT the same field means SELECT ALL.
func setOpTransformation(setOp nodes.SetOperation, all bool) (model.Transformation, bool) {
	switch setOp {
	case nodes.SetOpUnion:
		return model.NewUnionTransformation(all), true
	case nodes.SetOpIntersect:
		return model.NewIntersectTransformation(all), true
	case nodes.SetOpExcept:
		return model.NewExceptTransformation(all), true
	default:
		return model.Transformation{}, false
	}
}

// ---------------------------------------------------------------------------
// DDL targets
// ---------------------------------------------------------------------------

// processCreateView analyzes the query body of a CREATE / ALTER VIEW and maps its
// output columns onto the view. The sync reads the whole CREATE VIEW statement out of
// sys.sql_modules.definition, and the lineage runner passes it through unchanged
// because it is already a complete statement.
func (a *Analyzer) processCreateView(stmt *nodes.CreateViewStmt) {
	if stmt == nil || stmt.Query == nil {
		return
	}
	schema, table, ok := a.targetAddress(stmt.Name)
	if !ok {
		return
	}
	a.inTargetContext = true
	a.processQueryNode(stmt.Query)
	a.inTargetContext = false

	a.generateEdgesForTarget(a.currentScope(), schema, table, columnList(stmt.Columns), false)
}

// ---------------------------------------------------------------------------
// INSERT / UPDATE / DELETE / MERGE
// ---------------------------------------------------------------------------

// processInsertStatement processes INSERT ... SELECT / VALUES / EXEC.
func (a *Analyzer) processInsertStatement(stmt *nodes.InsertStmt) {
	if stmt == nil {
		return
	}
	if stmt.WithClause != nil {
		a.processCTEs(stmt.WithClause)
	}
	targetColumns := columnList(stmt.Cols)
	schema, table, ok := a.targetAddress(stmt.Relation)

	a.inTargetContext = true
	switch source := stmt.Source.(type) {
	case nil:
		// DEFAULT VALUES writes a row with no source at all.
	case *nodes.SelectStmt:
		a.processQueryNode(source)
	case *nodes.ValuesClause:
		a.processValuesColumns(source, targetColumns)
	case *nodes.ExecStmt:
		// EXEC / EXECUTE runs a statement the analyzer cannot see; INSERT ... EXEC
		// is the one shape whose source is a stored procedure's result set.
		a.diagnostics.NotModelled("INSERT ... EXEC", "")
	default:
		a.diagnostics.NotModelled("an INSERT source", "")
	}
	a.inTargetContext = false

	if !ok {
		// The rows are written into something the registry has no identity for — a table
		// variable, a temp table, or a table in another database — so there is no target to
		// record them against. The statement's reads are not re-pointed at its own result
		// either: an INSERT returns no rows, and an edge to __result__ would say it does.
		a.reportTargetWithoutIdentity(stmt.Relation)
		return
	}
	a.generateEdgesForTarget(a.currentScope(), schema, table, targetColumns, true)
}

// processValuesColumns adds one output column per position of a VALUES clause. Every
// row derives the same columns, so they merge positionally: the expression in a
// position of each row is a source of that column.
func (a *Analyzer) processValuesColumns(values *nodes.ValuesClause, targetColumns []string) {
	rows := valueRows(values)
	if len(rows) == 0 {
		return
	}
	sp := a.currentScope()
	for i, expr := range rows[0] {
		name := ""
		if i < len(targetColumns) {
			name = targetColumns[i]
		}
		merged := a.outputColumnFor(expr, name, name != "", sp, nil)
		for _, row := range rows[1:] {
			if i >= len(row) {
				continue
			}
			merged.Sources = append(merged.Sources, a.outputColumnFor(row[i], name, true, sp, nil).Sources...)
		}
		sp.AddOutputColumn(merged)
	}
}

// processUpdateStatement processes UPDATE ... SET, including its FROM clause. The
// target relation is registered in scope so that an assignment's right-hand side
// resolves against the row being written.
func (a *Analyzer) processUpdateStatement(stmt *nodes.UpdateStmt) {
	if stmt == nil {
		return
	}
	if stmt.WithClause != nil {
		a.processCTEs(stmt.WithClause)
	}
	schema, table, ok := a.registerTargetRelation(stmt.Relation)
	a.processFromClause(stmt.FromClause)
	if !ok {
		a.reportTargetWithoutIdentity(stmt.Relation)
		return
	}
	a.processSetClause(stmt.SetClause, schema, table)
}

// processSetClause resolves the assignments of an UPDATE or of a MERGE WHEN MATCHED
// action.
func (a *Analyzer) processSetClause(assignments *nodes.List, targetSchema, targetTable string) {
	if assignments == nil {
		return
	}
	sp := a.currentScope()
	for _, item := range assignments.Items {
		assignment, ok := item.(*nodes.SetExpr)
		if !ok || assignment == nil {
			continue
		}
		// `SET @v = col = expr` writes one column and one variable; the column is the
		// target either way.
		targetColumn := ""
		switch {
		case assignment.Column != nil:
			targetColumn = assignment.Column.Column
		case assignment.VarColumn != nil:
			targetColumn = assignment.VarColumn.Column
		default:
		}
		if targetColumn == "" {
			continue
		}
		targetColumn = a.registeredColumn(targetSchema, targetTable, targetColumn)

		sourceColumns := a.extractColumnsFromNode(assignment.Value, sp)
		if len(sourceColumns) == 0 {
			// A constant assignment rewrites the row without reading a column: the row itself is
			// the source, which is the same rule the MySQL family and PostgreSQL record. The
			// reference is marked resolved because the target is already identified here: looking
			// it up by name would fail whenever the statement addressed it by an alias, as a
			// MERGE target may (`MERGE INTO tgt AS x`).
			sourceColumns = []scope.ColumnRef{{Schema: targetSchema, Table: targetTable, Column: wildcardColumn, Resolved: true}}
		}
		var transform []model.Transformation
		if isExpressionDerived(assignment.Value) {
			transform = []model.Transformation{a.classifyExpression(assignment.Value)}
		}

		for _, source := range sourceColumns {
			resolutions, err := sp.ResolveColumnRefs(source)
			if err != nil {
				a.diagnostics.Unresolved("an assignment", source)
				continue
			}
			for _, res := range resolutions {
				res = a.canonicalResolution(res)
				if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
					a.traceThroughTableLineageToTarget(res.Relation, res.Ref.Column, targetSchema, targetTable, targetColumn, transform)
					continue
				}
				a.addRelation(scope.NewSchemaLineageEdge(
					res.Ref.Schema, res.Ref.Table, res.Ref.Column,
					targetSchema, targetTable, targetColumn,
					transform,
					targetTable == resultTableName,
				))
			}
		}
	}
}

// processDeleteStatement processes DELETE. A DELETE's WHERE decides which rows are
// removed, so every column it names is recorded as a source of the row set the
// statement deletes, the way PostgreSQL and the MySQL family record it. UPDATE's
// WHERE is deliberately not recorded: it decides which rows are rewritten, not which
// are removed, and no dialect here records an influence for it.
func (a *Analyzer) processDeleteStatement(stmt *nodes.DeleteStmt) {
	if stmt == nil {
		return
	}
	if stmt.WithClause != nil {
		a.processCTEs(stmt.WithClause)
	}
	schema, table, ok := a.registerTargetRelation(stmt.Relation)
	a.processFromClause(stmt.FromClause)
	if !ok {
		a.reportTargetWithoutIdentity(stmt.Relation)
		return
	}
	if stmt.WhereClause == nil {
		// Without a WHERE, the row set a DELETE removes is decided by its FROM
		// clause's joins, so those predicates are recorded as influences on the
		// target's rows. A statement with no condition at all at least says so.
		a.emitPredicateInfluences(a.currentScope(), schema, table)
		return
	}
	sp := a.currentScope()
	conditionText := a.exprTextOf(stmt.WhereClause)
	for _, conditionColumn := range a.extractColumnsFromNode(stmt.WhereClause, sp) {
		resolutions, err := sp.ResolveColumnRefs(conditionColumn)
		if err != nil {
			a.diagnostics.Unresolved("a DELETE condition", conditionColumn)
			continue
		}
		transform := []model.Transformation{model.NewDeleteTransformation(conditionText)}
		for _, res := range resolutions {
			res = a.canonicalResolution(res)
			if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
				a.traceThroughTableLineageToTarget(res.Relation, res.Ref.Column, schema, table, deletionFieldName, transform)
				continue
			}
			a.addRelation(scope.NewSchemaLineageEdge(
				res.Ref.Schema, res.Ref.Table, res.Ref.Column,
				schema, table, deletionFieldName,
				transform,
				table == resultTableName,
			))
		}
	}
}

// processMergeStatement processes MERGE, T-SQL's upsert and the shape most ETL
// scripts are written in. The target and source are one relation namespace — which is
// why the ON condition resolves against both and is recorded as a join influence —
// and each WHEN clause writes the target the way the statement it stands for would.
func (a *Analyzer) processMergeStatement(stmt *nodes.MergeStmt) {
	if stmt == nil {
		return
	}
	if stmt.WithClause != nil {
		a.processCTEs(stmt.WithClause)
	}
	schema, table, ok := a.registerTargetRelation(stmt.Target)
	a.processTableExpr(stmt.Source)
	if !ok {
		// Every WHEN clause writes the target, so a target the registry cannot address leaves
		// nothing to write to: running the clauses anyway recorded edges whose target had no
		// schema and no name at all, and the runner persists what the analyzer returns.
		a.reportTargetWithoutIdentity(stmt.Target)
		return
	}

	sp := a.currentScope()
	if stmt.OnCondition != nil {
		a.collectPredicates(stmt.OnCondition, sp, model.NewJoinTransformation(a.exprTextOf(stmt.OnCondition)), false)
	}
	if stmt.WhenClauses != nil {
		for _, item := range stmt.WhenClauses.Items {
			a.processMergeWhenClause(item, schema, table)
		}
	}
	a.emitPredicateInfluences(sp, schema, table)
}

// processMergeWhenClause processes one WHEN clause of a MERGE.
func (a *Analyzer) processMergeWhenClause(item nodes.Node, targetSchema, targetTable string) {
	clause, ok := item.(*nodes.MergeWhenClause)
	if !ok || clause == nil {
		return
	}
	// The clause's own AND condition decides which of the rows the ON condition matched this
	// action writes, so the columns it names are a row-set influence on the target, exactly as
	// the ON condition is. Both are emitted together, once, by processMergeStatement.
	if clause.Condition != nil {
		a.collectPredicates(clause.Condition, a.currentScope(), model.NewFilterTransformation(a.exprTextOf(clause.Condition)), false)
	}
	switch action := clause.Action.(type) {
	case *nodes.MergeUpdateAction:
		a.processSetClause(action.SetClause, targetSchema, targetTable)
	case *nodes.MergeInsertAction:
		if action.DefaultValues {
			return
		}
		// A MERGE insert holds one row of expressions, which maps positionally onto
		// the columns it declares exactly as a VALUES row does.
		columns := columnList(action.Cols)
		sp := a.currentScope()
		for i, expr := range listNodes(action.Values) {
			name := ""
			if i < len(columns) {
				name = columns[i]
			}
			sp.AddOutputColumn(a.outputColumnFor(expr, name, name != "", sp, nil))
		}
		a.generateEdgesForTarget(sp, targetSchema, targetTable, columns, true)
	case *nodes.MergeDeleteAction:
		// The rows a MERGE deletes are the target's own rows, selected by the ON
		// condition and this clause's own condition — both recorded as influences on
		// the target — so the row set is attributed to the relation itself.
		a.addRelation(scope.NewSchemaLineageEdge(
			targetSchema, targetTable, wildcardColumn,
			targetSchema, targetTable, deletionFieldName,
			[]model.Transformation{model.NewDeleteTransformation(a.exprTextOf(clause.Condition))},
			targetTable == resultTableName,
		))
	default:
		a.diagnostics.NotModelled("a MERGE WHEN action", "")
	}
}

// processBulkInsert processes BULK INSERT, which loads a file. The file's shape is
// not described by the statement, so the whole file is the source, as StarRocks'
// COPY INTO records it.
func (a *Analyzer) processBulkInsert(stmt *nodes.BulkInsertStmt) {
	if stmt == nil {
		return
	}
	schema, table, ok := a.targetAddress(stmt.Table)
	if !ok {
		return
	}
	a.addRelation(scope.NewSchemaLineageEdge(
		"", fileSourceMarker, wildcardColumn,
		schema, table, wildcardColumn,
		nil,
		table == resultTableName,
	))
}

// ---------------------------------------------------------------------------
// SELECT list
// ---------------------------------------------------------------------------

// processTargetList processes a SELECT's target list.
func (a *Analyzer) processTargetList(list *nodes.List, sp *scope.Scope, groupKeys []string) {
	if list == nil {
		return
	}
	for _, item := range list.Items {
		switch x := item.(type) {
		case *nodes.ResTarget:
			a.processResTarget(x, sp, groupKeys)
		case *nodes.StarExpr:
			a.processStar(x, sp)
		default:
			// A target the parser did not wrap in a ResTarget contributes no column.
		}
	}
}

// processResTarget turns one target-list item into an output column.
func (a *Analyzer) processResTarget(target *nodes.ResTarget, sp *scope.Scope, groupKeys []string) {
	if target == nil || nodeOrNil(target.Val) == nil {
		return
	}
	switch val := target.Val.(type) {
	case *nodes.StarExpr:
		a.processStar(val, sp)
	case *nodes.SelectAssign:
		// `SELECT @v = expr` assigns a variable. The expression is read but the item
		// is not a column of the result set, so it produces no output column — the
		// same reason PostgreSQL records nothing for a data-modifying statement's
		// RETURNING.
	default:
		sp.AddOutputColumn(a.outputColumnFor(val, target.Name, target.Name != "", sp, groupKeys))
	}
}

// processStar expands `*` against every relation in scope.
func (a *Analyzer) processStar(star *nodes.StarExpr, sp *scope.Scope) {
	if star != nil && star.Qualifier != "" {
		a.processTableWildcard(star, sp)
		return
	}
	for _, tableRef := range sp.Tables() {
		if a.expandWildcardWithCatalog(tableRef, sp) {
			continue
		}
		sp.AddOutputColumn(scope.OutputColumn{
			Alias:   wildcardColumn,
			Sources: scope.NewColumnSources([]scope.ColumnRef{queryLocalWildcardSourceRef(tableRef)}, nil),
		})
	}
}

// processTableWildcard expands `t.*` against the named relation.
func (a *Analyzer) processTableWildcard(star *nodes.StarExpr, sp *scope.Scope) {
	name := star.Qualifier
	// A wildcard's qualifier is a relation's own address, so a schema-qualified one
	// (`dbo.t.*`) is addressed by its last segment, which is the name SQL uses.
	if index := strings.LastIndexByte(name, '.'); index >= 0 {
		name = strings.Trim(name[index+1:], "[]")
	}
	tableRef, ok := sp.FindTable(name)
	if !ok {
		// A wildcard whose qualifier names no relation has no source to expand; the
		// note is what keeps the empty result from reading as a complete one.
		a.diagnostics.UnresolvedQualifier("a wildcard qualifier", name)
		return
	}
	if a.expandWildcardWithCatalog(tableRef, sp) {
		return
	}
	sp.AddOutputColumn(scope.OutputColumn{
		Alias:   wildcardColumn,
		Sources: scope.NewColumnSources([]scope.ColumnRef{queryLocalWildcardSourceRef(tableRef)}, nil),
	})
}

// queryLocalWildcardSourceRef builds the source reference for a `*` expansion of one
// FROM relation.
//
// A base table is already fully identified here, so the reference is marked resolved:
// looking it up again by name would fail whenever the relation has an alias. A CTE or
// derived table is where T-SQL parts company: its reference stays unresolved so the
// temporary-relation trace can find it by its scope key.
func queryLocalWildcardSourceRef(tableRef *scope.TableRef) scope.ColumnRef {
	if tableRef.IsSubquery || tableRef.IsCTE {
		key := tableRef.Alias
		if key == "" {
			key = tableRef.Table
		}
		return scope.ColumnRef{Table: key, Column: wildcardColumn}
	}
	return scope.WildcardSourceRef(tableRef)
}

// outputColumnFor builds the output column one expression produces.
func (a *Analyzer) outputColumnFor(expr nodes.Node, alias string, aliased bool, sp *scope.Scope, groupKeys []string) scope.OutputColumn {
	if nodeOrNil(expr) == nil {
		return scope.OutputColumn{Alias: alias}
	}
	exprText := a.exprTextOf(expr)
	if !aliased && alias == "" {
		alias = a.inferredColumnAlias(expr, exprText)
	}
	sourceColumns := a.extractColumnsFromNode(expr, sp)
	isDerived := isExpressionDerived(expr)

	// A table-wide aggregate such as COUNT(*) depends on the rows of every relation
	// in scope even though it names no column. Any other source-less expression (a
	// literal, GETDATE(), a cast of a constant) depends on no column at all and must
	// not invent a dependency.
	if isDerived && len(sourceColumns) == 0 && containsAggregate(expr) {
		for _, tableRef := range sp.Tables() {
			sourceColumns = append(sourceColumns, queryLocalWildcardSourceRef(tableRef))
		}
	}

	outputColumn := scope.OutputColumn{
		Alias:     alias,
		Sources:   scope.NewColumnSources(sourceColumns, nil),
		IsDerived: isDerived,
	}
	if isDerived {
		transform := []model.Transformation{a.classifyExpression(expr)}
		// The keys are attached to a select item that contains a group aggregate, so
		// the field never claims a column was aggregated when it was only projected.
		if len(groupKeys) > 0 && containsGroupAggregate(expr) {
			for i := range transform {
				transform[i].GroupKeys = groupKeys
			}
		}
		outputColumn.SetTransform(transform)
	}
	return outputColumn
}

// ---------------------------------------------------------------------------
// Edge generation
// ---------------------------------------------------------------------------

// generateEdges creates column relations from the scope's output columns. Only the
// root query emits edges to the result: a DDL body does not (its columns are mapped
// onto the created object instead) and neither does a set-operation arm (the merged
// operation emits them once).
func (a *Analyzer) generateEdges(sp *scope.Scope) {
	if a.inTargetContext || a.inSetOpArm || sp == nil || sp.Parent() != nil {
		return
	}
	for _, outputCol := range sp.GetOutputColumns() {
		for _, source := range outputCol.Sources {
			resolutions, err := sp.ResolveColumnRefs(source.Ref)
			if err != nil {
				a.diagnostics.Unresolved("an output column", source.Ref)
				continue
			}
			for _, res := range resolutions {
				res = a.canonicalResolution(res)
				if res.Relation != nil && (res.Relation.IsCTE || res.Relation.IsSubquery) {
					a.traceThroughTableLineage(res.Relation, res.Ref.Column, outputCol.Alias, source.Transform)
					continue
				}
				a.addRelation(scope.NewSchemaLineageEdge(
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

// generateEdgesForTarget maps a scope's output columns onto the columns of the object
// a statement writes. A column list declared on the statement wins over the query's
// own output name, which is what makes `CREATE VIEW v (a, b) AS SELECT x, y` and
// `INSERT INTO t (a, b) SELECT x, y` name the right targets.
func (a *Analyzer) generateEdgesForTarget(sp *scope.Scope, targetSchema, targetTable string, targetColumns []string, registeredTarget bool) {
	if sp == nil {
		return
	}
	for i, outputCol := range sp.GetOutputColumns() {
		targetColName := outputCol.Alias
		if i < len(targetColumns) {
			targetColName = targetColumns[i]
		}
		if registeredTarget {
			// The target is a stored object, so its columns are the registry's; a statement that
			// spells one differently would otherwise record one field under two names.
			targetColName = a.registeredColumn(targetSchema, targetTable, targetColName)
		}
		for _, source := range outputCol.Sources {
			resolutions, err := sp.ResolveColumnRefs(source.Ref)
			if err != nil {
				a.diagnostics.Unresolved("an output target", source.Ref)
				continue
			}
			isTemp := targetTable == resultTableName
			for _, res := range resolutions {
				res = a.canonicalResolution(res)
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

// addRelation adds a column relation, dropping exact duplicates. A query-local
// relation is never an endpoint here: every path that resolves one traces through its
// own lineage first, so the edge already names the stored relation the column came
// from.
func (a *Analyzer) addRelation(relation model.ColumnRelation) {
	a.edges.Add(relation)
}

// expandWildcardWithCatalog expands a wildcard using catalog metadata. It reports
// false when no metadata is available, so the caller falls back to a bulk wildcard
// edge rather than expanding to nothing.
func (a *Analyzer) expandWildcardWithCatalog(tableRef *scope.TableRef, sp *scope.Scope) bool {
	if a.catalog == nil || tableRef.IsSubquery || tableRef.IsCTE {
		return false
	}
	tableMeta := a.catalogTable(tableRef)
	if tableMeta == nil {
		return false
	}
	for _, colMeta := range tableMeta.Columns {
		sp.AddOutputColumn(scope.OutputColumn{
			Alias:   colMeta.Name,
			Sources: scope.NewColumnSources([]scope.ColumnRef{scope.WildcardSourceRef(tableRef)}, nil),
		})
	}
	return len(tableMeta.Columns) > 0
}

// ---------------------------------------------------------------------------
// Scope stack and temporary-relation tracing
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

// traceThroughTableLineage traces lineage through a CTE or derived table to the
// result table.
func (a *Analyzer) traceThroughTableLineage(tableRef *scope.TableRef, columnName, outputAlias string, transform []model.Transformation) {
	if !a.queryLocalAnswers(tableRef, columnName, "an output column") {
		return
	}
	algorithm.TraceThroughTableLineage(scope.NewSchemaLineageEdge, a.addRelation, tableRef, columnName, outputAlias, transform)
}

// traceThroughTableLineageToTarget traces lineage through a CTE or derived table to a
// specific target column on a real object.
func (a *Analyzer) traceThroughTableLineageToTarget(tableRef *scope.TableRef, columnName, targetSchema, targetTable, targetColumn string, transform []model.Transformation) {
	// The clause is read off the target the trace was asked for, which is what tells the three
	// callers apart: a predicate influence carries no target column, a DELETE records the target
	// it removes rows from, and everything else writes a column of the target.
	var clause string
	switch targetColumn {
	case "":
		clause = "a predicate"
	case deletionFieldName:
		clause = "a DELETE target"
	default:
		clause = "an output target"
	}
	if !a.queryLocalAnswers(tableRef, columnName, clause) {
		return
	}
	algorithm.TraceThroughTableLineageToTarget(scope.NewSchemaLineageEdge, a.addRelation, tableRef, columnName, targetSchema, targetTable, targetColumn, transform)
}

// flattenTempSourceLineage resolves a column from a temporary relation into base-table
// lineage. It reports whether the source was handled, which is also the case when the
// relation cannot answer the column: the source was query-local, and what it could not
// answer is reported rather than silently dropped.
func (a *Analyzer) flattenTempSourceLineage(sp *scope.Scope, relation *scope.TableRef, columnName, targetTable, targetColumn string, transform []model.Transformation, lineage *[]model.ColumnRelation) bool {
	if relation == nil || (!relation.IsSubquery && !relation.IsCTE) {
		return false
	}
	if !a.queryLocalAnswers(relation, columnName, "a temporary relation") {
		return true
	}
	return algorithm.FlattenTempSourceLineage(scope.NewSchemaLineageEdge, sp, relation, columnName, targetTable, targetColumn, transform, lineage)
}

// queryLocalAnswers reports whether a query-local relation can answer a reference to column, and
// records the gap when it cannot.
//
// A relation answers a name only if it offers the name at all: a CTE, a derived table, a table
// function and a pivot expose what their bodies produced, and a reference to a column they never
// produced is a gap — the relation was found, so nothing else reports it, and tracing it records
// no edge at all, which is what a statement with no lineage looks like. A column the relation does
// offer, but whose own lineage names no source, is not a gap: it is a column built from constants,
// and it has no lineage to record.
func (a *Analyzer) queryLocalAnswers(relation *scope.TableRef, columnName, clause string) bool {
	if relation == nil {
		return false
	}
	// A star is not a name the relation has to offer: it is every column it has, and the trace
	// forwards whatever the body's lineage recorded for it.
	if columnName != model.WildcardColumn {
		if names := relation.ColumnNames(); names != nil && !offersColumn(names, columnName) {
			a.diagnostics.Unresolved(clause, scope.ColumnRef{Table: relation.Key().Name, Column: columnName})
			return false
		}
	}
	return len(model.AnsweringLineage(relation.Lineage, columnName)) > 0
}

// offersColumn reports whether a column list exposes a name, compared the way this dialect binds an
// identifier: a T-SQL identifier is case-insensitive under the default collation.
func offersColumn(names []string, column string) bool {
	for _, name := range names {
		if strings.EqualFold(name, column) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// List helpers
// ---------------------------------------------------------------------------

// listNodes returns the items of a node list.
func listNodes(list *nodes.List) []nodes.Node {
	if list == nil {
		return nil
	}
	return list.Items
}

// valueRows returns the rows of a VALUES clause, each row being its own expression
// list.
func valueRows(values *nodes.ValuesClause) [][]nodes.Node {
	if values == nil || values.Rows == nil {
		return nil
	}
	rows := make([][]nodes.Node, 0, len(values.Rows.Items))
	for _, item := range values.Rows.Items {
		row, ok := item.(*nodes.List)
		if !ok || row == nil {
			continue
		}
		rows = append(rows, row.Items)
	}
	return rows
}

// containsAggregate reports whether an expression calls an aggregate function, and
// therefore depends on the rows of its relations even when it names no column.
func containsAggregate(expr nodes.Node) bool {
	return findAggregate(expr, false)
}

// containsGroupAggregate reports whether an expression contains an aggregate call
// that GROUP BY governs, i.e. one without an OVER clause: a windowed aggregate follows
// its OVER clause instead. Traversal stops at a subquery, whose aggregates are grouped
// by that query's own GROUP BY.
func containsGroupAggregate(expr nodes.Node) bool {
	return findAggregate(expr, true)
}

func findAggregate(expr nodes.Node, groupOnly bool) bool {
	if expr == nil {
		return false
	}
	found := false
	nodes.Inspect(expr, func(n nodes.Node) bool {
		if found {
			return false
		}
		switch x := n.(type) {
		case *nodes.SubqueryExpr, *nodes.ExistsExpr, *nodes.SubqueryComparisonExpr:
			return false
		case *nodes.FuncCallExpr:
			if aggregateFunctions[strings.ToUpper(funcCallName(x))] && (!groupOnly || x.Over == nil) {
				found = true
				return false
			}
		default:
		}
		return true
	})
	return found
}
