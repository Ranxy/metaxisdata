// Package resource names the objects an authorization decision is about.
//
// It is pure: it parses metadata GUIDs into their ancestor chain and answers
// scope questions, but it never reads the store. A resource whose parent is not
// encoded in its identifier (a quality rule's target, for example) needs a
// resolver in the authorization layer.
package resource

import (
	"github.com/Ranxy/metaxisdata/backend/common"
)

// Type is the kind of object an authorization decision names. The value is
// stored in a grant as its stable string, so a rename needs a migration.
type Type int

// The resource types.
const (
	// TypeUnspecified is the zero value and never names a resource.
	TypeUnspecified Type = iota
	// TypeWorkspace is the single-workspace root.
	TypeWorkspace
	// TypeInstance is a database instance.
	TypeInstance
	// TypeDatabase is a database inside an instance.
	TypeDatabase
	// TypeSchema is a schema inside a database.
	TypeSchema
	// TypeTable is a table.
	TypeTable
	// TypeView is a view.
	TypeView
	// TypeExternalTable is an external table.
	TypeExternalTable
	// TypeColumn is a column of a relation.
	TypeColumn
	// TypeQualityRule is one quality rule.
	TypeQualityRule
	// TypeQualityExecution is one rule execution.
	TypeQualityExecution
	// TypeQualityIssue is one quality issue.
	TypeQualityIssue
	// TypeQualityContract is one target's quality contract.
	TypeQualityContract
	// TypeQualitySuggestion is one queued suggestion.
	TypeQualitySuggestion
	// TypeColumnProfile is one column's cached profile.
	TypeColumnProfile
	// TypeColumnLabel is one column's semantic label.
	TypeColumnLabel
	// TypeManualSQL is one manual SQL definition.
	TypeManualSQL
	// TypeApplication is one connected application.
	TypeApplication
	// TypeApplicationSQL is one SQL statement an application reported.
	TypeApplicationSQL
	// TypeQualityAgent is one built-in quality agent.
	TypeQualityAgent
)

var typeNames = map[Type]string{
	TypeWorkspace:         "workspace",
	TypeInstance:          "instance",
	TypeDatabase:          "database",
	TypeSchema:            "schema",
	TypeTable:             "table",
	TypeView:              "view",
	TypeExternalTable:     "external_table",
	TypeColumn:            "column",
	TypeQualityRule:       "quality_rule",
	TypeQualityExecution:  "quality_execution",
	TypeQualityIssue:      "quality_issue",
	TypeQualityContract:   "quality_contract",
	TypeQualitySuggestion: "quality_suggestion",
	TypeColumnProfile:     "column_profile",
	TypeColumnLabel:       "column_label",
	TypeManualSQL:         "manual_sql",
	TypeApplication:       "application",
	TypeApplicationSQL:    "application_sql",
	TypeQualityAgent:      "quality_agent",
}

// String returns the stable name of the type. An unknown value is "unspecified".
func (t Type) String() string {
	if name, ok := typeNames[t]; ok {
		return name
	}
	return "unspecified"
}

// ParseType resolves a stored type name. The empty string is unspecified.
func ParseType(name string) (Type, bool) {
	if name == "" {
		return TypeUnspecified, true
	}
	for t, n := range typeNames {
		if n == name {
			return t, true
		}
	}
	return TypeUnspecified, false
}

// IsMetadataType reports whether an object's parent chain is encoded in its
// metadata GUID.
func IsMetadataType(t Type) bool {
	switch t {
	case TypeInstance, TypeDatabase, TypeSchema, TypeTable, TypeView, TypeExternalTable, TypeColumn:
		return true
	default:
		return false
	}
}

// Ref identifies one resource. ID is the metadata GUID for a metadata object,
// the resource id for a product resource, and empty for the workspace.
type Ref struct {
	Type Type
	ID   string
}

// NewRef builds one resource reference.
func NewRef(t Type, id string) Ref {
	return Ref{Type: t, ID: id}
}

// WorkspaceRef names the workspace root.
func WorkspaceRef() Ref {
	return Ref{Type: TypeWorkspace}
}

// IsZero reports whether the reference names nothing.
func (r Ref) IsZero() bool {
	return r.Type == TypeUnspecified
}

// Valid reports whether the reference could name a resource: a known type, an
// empty id only for the workspace, and a non-empty id otherwise.
func (r Ref) Valid() bool {
	switch {
	case r.Type == TypeUnspecified:
		return false
	case r.Type == TypeWorkspace:
		return r.ID == ""
	case r.ID == "":
		return false
	default:
		return true
	}
}

// Equal reports whether two references name the same resource.
func (r Ref) Equal(other Ref) bool {
	return r.Type == other.Type && r.ID == other.ID
}

// String renders the reference for logs and test failures.
func (r Ref) String() string {
	if r.Type == TypeWorkspace {
		return "workspace"
	}
	return r.Type.String() + ":" + r.ID
}

// MetadataAncestors returns ref, then its parent, and so on up to the workspace.
// It returns nil when ref is not a metadata object or its GUID does not have the
// segment count its type requires.
//
// A column's relation ancestor is reported as a table: the registry does not
// carry the relation kind in the column GUID, and scope matching compares
// metadata ids, not kinds.
func MetadataAncestors(ref Ref) []Ref {
	if !IsMetadataType(ref.Type) {
		return nil
	}
	segments := common.SplitMetaGUID(ref.ID)
	chain := []Ref{ref}
	add := func(t Type, count int) {
		chain = append(chain, NewRef(t, common.BuildMetaGUID(segments[:count]...)))
	}
	switch ref.Type {
	case TypeInstance:
		if len(segments) != 1 {
			return nil
		}
	case TypeDatabase:
		if len(segments) != 2 {
			return nil
		}
		add(TypeInstance, 1)
	case TypeSchema:
		if len(segments) != 3 {
			return nil
		}
		add(TypeDatabase, 2)
		add(TypeInstance, 1)
	case TypeTable, TypeView, TypeExternalTable:
		switch len(segments) {
		case 3:
			add(TypeDatabase, 2)
			add(TypeInstance, 1)
		case 4:
			add(TypeSchema, 3)
			add(TypeDatabase, 2)
			add(TypeInstance, 1)
		default:
			return nil
		}
	case TypeColumn:
		switch len(segments) {
		case 4:
			add(TypeTable, 3)
			add(TypeDatabase, 2)
			add(TypeInstance, 1)
		case 5:
			add(TypeTable, 4)
			add(TypeSchema, 3)
			add(TypeDatabase, 2)
			add(TypeInstance, 1)
		default:
			return nil
		}
	default:
		return nil
	}
	return append(chain, WorkspaceRef())
}
