package model

type Column struct {
	Table ObjectIdentifier
	Name  string
}

type ColumnRelation struct {
	Source         Column
	Target         Column
	Transformation []any
	RelationType   RelationType
	IsTemp         bool //if the target table is not a real table
}

type RelationType int

const (
	RelationTypeDirect RelationType = iota
	RelationTypeIndirect
	RelationTypeJoin
	RelationTypeGroup
	RelationTypeUnion
	RelationTypeIntersect
	RelationTypeExcept
	RelationTypeUnknown
)
