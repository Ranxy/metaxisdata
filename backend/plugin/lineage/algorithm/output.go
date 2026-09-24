package algorithm

import "github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"

// ResolveOutputColumns resolves each output column's source references against
// the scope they were collected in, marking them resolved and replacing a
// query-local relation with the stored relations behind it. Without this, an
// arm's reference would be resolved again in the enclosing scope, where the
// arm's tables no longer exist, and the arm's lineage would be silently dropped.
func ResolveOutputColumns(sp *scope.Scope, cols []scope.OutputColumn) []scope.OutputColumn {
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
					resolved = append(resolved, FlattenTempSources(sp, res.Ref, res.Relation, source.Transform)...)
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
