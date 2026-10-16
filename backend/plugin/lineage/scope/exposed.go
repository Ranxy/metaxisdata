package scope

import "github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"

// AttachTempColumnLookup lets the scope resolver treat a CTE or derived table as
// a relation whose columns are known, so an unqualified name it owns resolves
// through its own lineage instead of being guessed from name order. declared is
// the CTE's column list when the query wrote one, or the names the query's output
// exposes; a nil list falls back to the targets the lineage carries.
func AttachTempColumnLookup(tableRef *TableRef, declared []string) {
	if names := TempColumnNames(declared, tableRef.Lineage); names != nil {
		tableRef.SetColumnLookup(func() []string { return names })
	}
}

// TempColumnNames reports the columns a temporary relation exposes, or nil when
// one of them cannot be named. A wildcard the catalog did not expand, and an
// output without a name, both leave the list incomplete; an incomplete list is
// reported as unknown so the resolver keeps its fallback for the relation.
func TempColumnNames(declared []string, lineage []model.ColumnRelation) []string {
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
		if name == "" || name == model.WildcardColumn {
			return nil
		}
	}
	return names
}

// ExposedColumnNames lists the names a temporary relation exposes to whatever reads it, in
// the order it offers them. It is OutputColumnNames — a declared list renames the body's
// outputs from the left, the outputs it does not reach keep their own names — with one
// exception: a declared list longer than the modelled body is kept when that body carries a
// wildcard output column, because the count is then a lower bound rather than the body's
// arity. `WITH c (a, b) AS (SELECT * FROM undescribed) SELECT * FROM c` is engine-valid
// whenever `undescribed` really has two columns — all five engines were asked, and each
// exposes `(a, b)` for an equal-arity declaration — while discarding it left the relation
// undescribed, so the two edges the names carry became one wildcard edge, an edge a declared
// target list expected went missing, and a name the relation does not have resolved
// silently. The statement's own list is the only account of those columns there is.
//
// The result may be longer than cols, so a caller indexes outputs with it only up to
// len(cols) and uses it in full for the lookup a relation's references resolve against.
func ExposedColumnNames(declared []string, cols []OutputColumn) []string {
	if len(declared) > len(cols) && hasWildcardOutput(cols) {
		return declared
	}
	return OutputColumnNames(declared, cols)
}

// OutputColumnNames lists the names the analyzer's modelled outputs carry, in the order it
// modelled them. A declared list — a CTE's `WITH c (a, b)` or a derived table's `AS d (a,
// b)` — renames them from the left, so the relation's own lineage edges are named the same
// way. The outputs the list does not reach keep their own names, because that is what the
// engines do: PostgreSQL 16 and Doris 4.1.4 accept a list shorter than the body and expose
// `(a, name)` for `WITH c (a) AS (SELECT k, name FROM t) SELECT …`, while MySQL 8.3, TiDB,
// MariaDB, StarRocks 4.1 and MSSQL 2019 accept a declared list only when its arity matches
// the body, which makes this rule and an equality rule agree on every statement they
// execute. A list longer than the body is a statement every one of them rejects, so the
// declaration is discarded and the body names the columns.
//
// The rule is about the body's outputs, never about the names its lineage happens to
// carry: an output column with no source — a literal, an aggregate — is a column of the
// relation all the same, and leaving it out let a scope holding another relation deny the
// name and shifted a declared target list behind a wildcard over the relation.
func OutputColumnNames(declared []string, cols []OutputColumn) []string {
	renames := len(declared) <= len(cols)
	names := make([]string, 0, len(cols))
	for i, col := range cols {
		name := col.Alias
		if renames && i < len(declared) {
			name = declared[i]
		}
		names = append(names, name)
	}
	return names
}

// hasWildcardOutput reports whether any modelled output column is the wildcard the
// analyzer records for a relation it could not enumerate.
func hasWildcardOutput(cols []OutputColumn) bool {
	for _, col := range cols {
		if col.Alias == model.WildcardColumn {
			return true
		}
	}
	return false
}

// WildcardSourceRef is the source a `*` contributes for one relation: a wildcard
// reference to that relation, marked resolved because the relation it names
// cannot be rebound by name later. StarRocks keeps its own rule for a
// query-local relation, where the reference has to stay rebindable; see its
// queryLocalWildcardSourceRef.
func WildcardSourceRef(tableRef *TableRef) ColumnRef {
	return ColumnRef{
		Schema:   tableRef.Schema,
		Table:    tableRef.Table,
		Column:   model.WildcardColumn,
		Resolved: true,
	}
}

// QueryLocalColumnRef is the source one column of a query-local relation
// contributes. The relation's name is an alias the statement invented, so the
// reference stays unresolved for the scope resolver to bind by the relation's
// scope key — which is what attaches the relation and lets the temporary-relation
// tracing flatten it. Marking it resolved instead made COUNT(*) over a CTE emit an
// edge naming the CTE, which is not a stored object at all.
func QueryLocalColumnRef(tableRef *TableRef, column string) ColumnRef {
	key := tableRef.Alias
	if key == "" {
		key = tableRef.Table
	}
	return ColumnRef{Table: key, Column: column}
}
