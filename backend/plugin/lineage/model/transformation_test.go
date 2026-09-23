package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A combined list must not be backed by either argument's array. Appending to
// the first argument returns a list sharing that array, so the second edge built
// from the same base overwrites the first one's added transformations.
func TestCombineTransformationsDoesNotShareItsArgumentsArrays(t *testing.T) {
	t.Parallel()

	base := make([]Transformation, 1, 4)
	base[0] = NewUnionTransformation(false)

	first := CombineTransformations(base, []Transformation{NewProjectTransformation("a")})
	second := CombineTransformations(base, []Transformation{NewProjectTransformation("b")})

	require.Len(t, first, 2)
	require.Equal(t, "a", first[1].Expression, "the second combination overwrote the first one's transformation")
	require.Equal(t, "b", second[1].Expression)
	require.Len(t, base, 1)
	require.Equal(t, OperationUnion, base[0].Operation)
}

func TestCombineTransformationsHandlesAnEmptySide(t *testing.T) {
	t.Parallel()

	project := []Transformation{NewProjectTransformation("a")}
	require.Nil(t, CombineTransformations(nil, nil))
	require.Equal(t, project, CombineTransformations(nil, project))
	require.Equal(t, project, CombineTransformations(project, nil))

	// An empty combination still returns its own list, so appending to the
	// result cannot reach the argument.
	combined := CombineTransformations(nil, project)
	_ = append(combined, NewCaseTransformation("c"))
	require.Len(t, project, 1)
}

// A set operation's ALL flag is part of the transformation, so UNION and UNION
// ALL are different transformations and, since a transformation is part of an
// edge's identity, different edges.
func TestSetOperationsDistinguishAll(t *testing.T) {
	t.Parallel()

	require.False(t, NewUnionTransformation(false).Equal(NewUnionTransformation(true)))
	require.True(t, NewUnionTransformation(true).Equal(NewUnionTransformation(true)))
	require.False(t, NewIntersectTransformation(false).Equal(NewIntersectTransformation(true)))
	require.False(t, NewExceptTransformation(false).Equal(NewExceptTransformation(true)))
	require.False(t, SameTransformations(
		[]Transformation{NewUnionTransformation(false)},
		[]Transformation{NewUnionTransformation(true)}))
}
