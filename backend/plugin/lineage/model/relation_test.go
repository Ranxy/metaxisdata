package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func lineageEdge(source, target string) ColumnRelation {
	return ColumnRelation{
		Source: Column{Table: ObjectIdentifier{Name: "t"}, Name: source},
		Target: Column{Table: ObjectIdentifier{Name: "d"}, Name: target},
	}
}

// An entry that names the column answers it, and the wildcard entry does not
// answer for it: the named entry is the precise answer.
func TestAnsweringLineage_PrefersTheNamedTarget(t *testing.T) {
	lineage := []ColumnRelation{lineageEdge("id", "id"), lineageEdge(WildcardColumn, WildcardColumn)}

	answering := AnsweringLineage(lineage, "id")
	require.Len(t, answering, 1)
	require.Equal(t, "id", answering[0].Target.Name)
}

// A body that never expanded its star forwards every column of its source, so a
// name the lineage does not carry is answered by the wildcard entry. The column
// is unknown, the source table is not.
func TestAnsweringLineage_WildcardForwardsAnUnnamedColumn(t *testing.T) {
	lineage := []ColumnRelation{lineageEdge(WildcardColumn, WildcardColumn)}

	answering := AnsweringLineage(lineage, "x")
	require.Len(t, answering, 1)
	require.Equal(t, WildcardColumn, answering[0].Source.Name)
	require.Equal(t, WildcardColumn, answering[0].Target.Name)
}

// A star reference takes the whole lineage, which is how a wildcard expands.
func TestAnsweringLineage_WildcardRequestTakesEverything(t *testing.T) {
	lineage := []ColumnRelation{lineageEdge("id", "id"), lineageEdge(WildcardColumn, WildcardColumn)}

	require.Equal(t, lineage, AnsweringLineage(lineage, WildcardColumn))
}

// Nothing answers a name the lineage neither names nor forwards.
func TestAnsweringLineage_NothingAnswers(t *testing.T) {
	lineage := []ColumnRelation{lineageEdge("id", "id")}

	require.Empty(t, AnsweringLineage(lineage, "x"))
	require.Empty(t, AnsweringLineage(nil, "x"))
}
