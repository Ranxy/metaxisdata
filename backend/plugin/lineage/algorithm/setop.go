package algorithm

import (
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"
)

// ArmChain appends the operation that combines a subtree to the chain of
// operations already combining it, outermost first.
//
// The chain is kept because a set-operation tree does not bind flatly:
// INTERSECT binds tighter than UNION and EXCEPT, so `a UNION b INTERSECT c`
// groups as `a UNION (b INTERSECT c)`. Flattening the tree without the chain
// labelled every arm with the outermost operation and lost the inner one.
//
// A left-deep tree repeats the same operation (`a UNION b UNION c` nests as
// `(a UNION b) UNION c`), and that repetition describes one union of three arms
// rather than a stack of them, so a step already on the end of the chain is not
// recorded twice.
func ArmChain(chain []model.Transformation, op model.Transformation) []model.Transformation {
	if n := len(chain); n > 0 && chain[n-1].Equal(op) {
		return chain
	}
	out := make([]model.Transformation, 0, len(chain)+1)
	out = append(out, chain...)
	return append(out, op)
}

// MergeSetOpColumns merges output columns from multiple set-operation arms
// positionally, replacing the base scope's columns with the merged ones, and
// records the operations that produced each arm as the leading transformations
// of that arm's own sources. That leading transformation is what makes the
// relation type union/intersect/except instead of direct.
//
// The transformations stay attached to the source they came from: one
// transformation for the whole merged column would attribute the first arm's
// expression to every other arm's sources, which is lineage the query does not
// have.
//
// The first arm's columns are expected to be the base scope's own; the merged
// list replaces them.
func MergeSetOpColumns(baseScope *scope.Scope, armColumns [][]scope.OutputColumn, armTransforms [][]model.Transformation) {
	if len(armColumns) == 0 || len(armColumns[0]) == 0 {
		return
	}

	firstArmColumns := armColumns[0]
	merged := make([]scope.OutputColumn, 0, len(firstArmColumns))
	for colIdx := range firstArmColumns {
		firstColumn := firstArmColumns[colIdx]

		var mergedSources []scope.ColumnSource
		for armIdx := range armColumns {
			if colIdx >= len(armColumns[armIdx]) {
				continue
			}
			var armTransform []model.Transformation
			if armIdx < len(armTransforms) {
				armTransform = armTransforms[armIdx]
			}
			for _, source := range armColumns[armIdx][colIdx].Sources {
				source.Transform = model.CombineTransformations(armTransform, source.Transform)
				mergedSources = append(mergedSources, source)
			}
		}

		firstColumn.Sources = mergedSources
		merged = append(merged, firstColumn)
	}

	baseScope.SetOutputColumns(merged)
}
