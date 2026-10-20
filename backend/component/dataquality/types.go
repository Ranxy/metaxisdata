package dataquality

import (
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// ColumnKind is the coarse type class a rule needs. It is derived from the
// engine's type string rather than from a per-engine type table because the rules
// only ever need to know which literals the column accepts and how to compare
// them; the engine still decides the exact type.
type ColumnKind int

const (
	// ColumnKindUnknown is a type this build cannot classify. Rules that need a
	// kind reject it at validation time instead of guessing at runtime.
	ColumnKindUnknown ColumnKind = iota
	ColumnKindNumeric
	ColumnKindTemporal
	ColumnKindString
	ColumnKindBoolean
)

// String renders the kind for error messages.
func (k ColumnKind) String() string {
	switch k {
	case ColumnKindNumeric:
		return "numeric"
	case ColumnKindTemporal:
		return "temporal"
	case ColumnKindString:
		return "string"
	case ColumnKindBoolean:
		return "boolean"
	case ColumnKindUnknown:
		return "unknown"
	}
	return "unknown"
}

// numericTypes are the type names whose literals are numeric. The list is
// deliberately prefix-based: engines decorate types with precision and unsigned
// modifiers ("decimal(10,2) unsigned").
var numericTypes = []string{
	"int", "integer", "tinyint", "smallint", "mediumint", "bigint",
	"decimal", "numeric", "float", "double", "real", "serial",
	"smallserial", "bigserial", "number", "money", "smallmoney",
	"dec", "fixed",
	// Integer spellings of the supported engines: MySQL's YEAR and StarRocks/Doris'
	// LARGEINT both compare and aggregate as numbers, so a range or aggregate rule
	// over them is meaningful.
	"year", "largeint",
}

// temporalTypes are the date/time type names. A rule that parses a timestamp from
// a string uses these to know whether the column already is one.
var temporalTypes = []string{
	"date", "datetime", "datetime2", "smalldatetime", "timestamp",
	"timestamptz", "timestamp with time zone", "timestamp without time zone",
	"time", "timetz",
}

// booleanTypes are the engine boolean spellings.
var booleanTypes = []string{
	"bool", "boolean", "bit",
}

// ClassifyColumnType maps an engine type string to its kind. An unrecognized type
// is ColumnKindString: engines spell text types in too many ways to enumerate,
// and treating an unknown type as text is the conservative choice for validation
// (values still get passed as parameters, never concatenated).
func ClassifyColumnType(columnType string) ColumnKind {
	name := columnTypeBaseName(columnType)
	if name == "" {
		return ColumnKindUnknown
	}
	for _, candidate := range booleanTypes {
		if name == candidate {
			return ColumnKindBoolean
		}
	}
	for _, candidate := range numericTypes {
		if name == candidate {
			return ColumnKindNumeric
		}
	}
	for _, candidate := range temporalTypes {
		if strings.HasPrefix(name, candidate) {
			return ColumnKindTemporal
		}
	}
	return ColumnKindString
}

// columnTypeBaseName reduces an engine type string to its base name: precision
// and modifier suffixes are dropped ("decimal(10,2) unsigned" → "decimal",
// "character varying" → "character"). A schema-qualified user-defined type keeps
// its schema ("public.citext"), so a caller can tell it from a built-in name.
func columnTypeBaseName(columnType string) string {
	name := strings.ToLower(strings.TrimSpace(columnType))
	if index := strings.IndexAny(name, "( "); index >= 0 {
		name = name[:index]
	}
	return name
}

// ColumnInfo is one column of the target relation: what a rule names, what type
// the engine reports, and whether NULL is allowed.
type ColumnInfo struct {
	Name     string
	Type     string
	Kind     ColumnKind
	Nullable bool
}

// ResolvedTarget is a target as the registry currently describes it: enough to
// compile a query against it and to say why validation failed.
type ResolvedTarget struct {
	Target
	// Engine is the target instance's engine, which selects the dialect.
	Engine storepb.Engine
	// InstanceResourceID is the instance the target lives in; it is also the first
	// GUID segment, but carrying it here saves every caller a split.
	InstanceResourceID string
	// Database and Schema are the container names used to qualify the table.
	Database string
	Schema   string
	// Table is the object's own name, as the registry spells it.
	Table string
	// Columns are the relation's columns in registry order.
	Columns []ColumnInfo
	// MetaHash is the registry hash of the target when it was read.
	MetaHash []byte
}

// Column returns the named column, and false when the relation has no such
// column.
func (t ResolvedTarget) Column(name string) (ColumnInfo, bool) {
	for _, column := range t.Columns {
		if column.Name == name {
			return column, true
		}
	}
	return ColumnInfo{}, false
}

// ColumnNames returns the relation's column names in registry order.
func (t ResolvedTarget) ColumnNames() []string {
	names := make([]string, 0, len(t.Columns))
	for _, column := range t.Columns {
		names = append(names, column.Name)
	}
	return names
}
