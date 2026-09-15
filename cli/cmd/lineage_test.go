package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

func relation(source, target string, isTemp bool) *v1pb.AnalyzeSQLRelation {
	return &v1pb.AnalyzeSQLRelation{SourceGuid: source, TargetGuid: target, IsTemp: isTemp}
}

// A statement that writes somewhere real never reaches the CLI with temporary
// relations, because the server drops them as duplicates of the real target.
func TestVisibleRelationsHidesTemporaryOnesByDefault(t *testing.T) {
	t.Parallel()

	relations := []*v1pb.AnalyzeSQLRelation{
		relation("1;shop;;orders", "1;shop;;daily", false),
		relation("1;shop;;orders", "", true),
	}

	visible, hidden := visibleRelations(relations, false)
	require.Len(t, visible, 1)
	require.False(t, visible[0].GetIsTemp())
	require.Equal(t, 1, hidden)

	all, hidden := visibleRelations(relations, true)
	require.Len(t, all, 2)
	require.Zero(t, hidden)
}

// Hiding everything is allowed, but the count has to come back so the caller
// can say the scope is not empty, it is filtered.
func TestVisibleRelationsReportsAFullyHiddenScope(t *testing.T) {
	t.Parallel()

	relations := []*v1pb.AnalyzeSQLRelation{
		relation("1;shop;;orders", "", true),
		relation("1;shop;;users", "", true),
	}

	visible, hidden := visibleRelations(relations, false)
	require.Empty(t, visible)
	require.Equal(t, 2, hidden)
}

func TestVisibleRelationsOnAnEmptyScope(t *testing.T) {
	t.Parallel()

	visible, hidden := visibleRelations(nil, false)
	require.Empty(t, visible)
	require.Zero(t, hidden)
}
