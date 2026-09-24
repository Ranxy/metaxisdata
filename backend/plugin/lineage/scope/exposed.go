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

// ExposedColumnNames lists the names a temporary relation exposes, in the order
// the relation offers them. A declared list — a CTE's `WITH c (a, b)` or a
// derived table's `AS d (a, b)` — renames the body's output positionally, so both
// the relation's own lineage and the columns the scope resolves against have to
// use the renamed names. The list is honoured only when its arity matches the
// body, because a mismatch is a statement MySQL rejects.
func ExposedColumnNames(declared []string, cols []OutputColumn) []string {
	if len(declared) > 0 && len(declared) == len(cols) {
		return declared
	}
	names := make([]string, 0, len(cols))
	for _, col := range cols {
		names = append(names, col.Alias)
	}
	return names
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
