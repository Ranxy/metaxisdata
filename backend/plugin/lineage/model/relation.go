package model

type Column struct {
	Table ObjectIdentifier
	Name  string
}

type ColumnRelation struct {
	Source         Column
	Target         Column
	Transformation []Transformation
	RelationType   RelationType
	IsTemp         bool // if the target table is not a real table
}

// WildcardColumn is the column marker a lineage edge carries when the column
// itself is unknown because a wildcard was never expanded. A temporary relation's
// entry with this target forwards every column of its source.
const WildcardColumn = "*"

// AnsweringLineage returns the entries of a temporary relation's lineage that
// answer a reference to column, in their original order. An entry whose target
// names the column answers it; when none does, the entries whose target is
// WildcardColumn do, because a body that never expanded its star forwards every
// column of its sources — the column behind the wildcard is unknown, the source
// table is not. An empty result means the lineage says nothing about the name.
func AnsweringLineage(lineage []ColumnRelation, column string) []ColumnRelation {
	if column == WildcardColumn {
		return lineage
	}
	var named, forwarded []ColumnRelation
	for _, edge := range lineage {
		if edge.Target.Name == column {
			named = append(named, edge)
			continue
		}
		if edge.Target.Name == WildcardColumn {
			forwarded = append(forwarded, edge)
		}
	}
	if len(named) > 0 {
		return named
	}
	return forwarded
}

type RelationType int

const (
	RelationTypeDirect RelationType = iota + 1
	RelationTypeIndirect
	RelationTypeJoin
	RelationTypeGroup
	RelationTypeUnion
	RelationTypeIntersect
	RelationTypeExcept
	RelationTypeUnknown
)
