package dataquality

import (
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// ColumnKind decides how a rule's literal is read and which comparison a rule
// may use, but it collapses every type this build does not recognize into
// "string". That is right for binding a parameter (the engine casts it) and wrong
// for choosing SQL operators: PostgreSQL has no regular-expression operator or
// character length for jsonb, uuid or bytea, and no equality at all for json and
// xml. These capability predicates are what validation and compilation consult,
// so a rule the engine cannot run is refused at save time instead of failing on
// every execution.
//
// The type lists below are deny-lists on purpose. An unrecognized type keeps its
// historical allowance — rules bind values as parameters rather than
// concatenating them — so a user-defined text type such as citext does not lose
// the text rules it supports. A missing entry is the status quo, not a regression.

// supportsText reports whether the engine defines text operations on the
// column's values: the regex match operator and the character-length function.
// The MySQL wire family converts JSON and spatial values to text for both, so it
// is permissive; PostgreSQL and SQL Server are strict.
func supportsText(column ColumnInfo, engine storepb.Engine) bool {
	if column.Kind != ColumnKindString {
		return false
	}
	name := columnTypeBaseName(column.Type)
	switch engine {
	case storepb.Engine_POSTGRES:
		return postgresTextType(name)
	case storepb.Engine_MSSQL:
		_, unsupported := mssqlTextUnsupportedTypes[name]
		return !unsupported
	default:
		return true
	}
}

// postgresTextType decides one PostgreSQL base type name. An array is stored as
// its element's udt name ("_int4"), never as a text type. A user-defined type is
// stored schema-qualified: citext is the one text type with regex operators and a
// length, while enums, hstore, composites and PostGIS types are not text.
func postgresTextType(name string) bool {
	if strings.HasPrefix(name, "_") {
		return false
	}
	if index := strings.LastIndexByte(name, '.'); index >= 0 {
		return name[index+1:] == "citext"
	}
	_, unsupported := postgresTextUnsupportedTypes[name]
	return !unsupported
}

// supportsEquality reports whether the engine can compare the column's values
// for equality, which is what uniqueness, grouping, filters and accepted-value
// checks need. PostgreSQL has no = operator for json, xml or its geometric
// types, so those statements fail at parse time.
func supportsEquality(column ColumnInfo, engine storepb.Engine) bool {
	switch column.Kind {
	case ColumnKindNumeric, ColumnKindTemporal, ColumnKindBoolean:
		return true
	case ColumnKindString:
		name := columnTypeBaseName(column.Type)
		switch engine {
		case storepb.Engine_POSTGRES:
			return postgresEquatableType(name)
		case storepb.Engine_MSSQL:
			_, unsupported := mssqlEqualityUnsupportedTypes[name]
			return !unsupported
		default:
			return true
		}
	case ColumnKindUnknown:
		return false
	default:
		return false
	}
}

// postgresEquatableType decides one PostgreSQL base type name for equality. An
// array compares element-wise; a user-defined type compares unless it is one of
// the PostGIS geometry types, which define no = operator.
func postgresEquatableType(name string) bool {
	if strings.HasPrefix(name, "_") {
		return true
	}
	if index := strings.LastIndexByte(name, '.'); index >= 0 {
		element := name[index+1:]
		return element != "geometry" && element != "geography"
	}
	_, unsupported := postgresEqualityUnsupportedTypes[name]
	return !unsupported
}

// postgresTextUnsupportedTypes are the PostgreSQL types without a regex operator
// or character-length function.
var postgresTextUnsupportedTypes = map[string]struct{}{
	"json": {}, "jsonb": {}, "xml": {}, "uuid": {}, "bytea": {}, "interval": {},
	"inet": {}, "cidr": {}, "macaddr": {}, "macaddr8": {},
	"tsvector": {}, "tsquery": {}, "oid": {}, "pg_lsn": {},
	"refcursor": {}, "txid_snapshot": {},
	"point": {}, "line": {}, "lseg": {}, "box": {}, "path": {}, "polygon": {},
	"circle": {}, "geometry": {}, "geography": {},
}

// postgresEqualityUnsupportedTypes are the PostgreSQL types without an equality
// operator. The geometric types have no comparison operators at all for the
// built-in point family, and PostGIS geometry defines ST_Equals rather than =.
var postgresEqualityUnsupportedTypes = map[string]struct{}{
	"json": {}, "xml": {}, "refcursor": {}, "txid_snapshot": {},
	"point": {}, "line": {}, "lseg": {}, "box": {}, "path": {}, "polygon": {},
	"circle": {}, "geometry": {}, "geography": {},
}

// mssqlTextUnsupportedTypes are the SQL Server types without a character length
// (LEN) or regex surface. SQL Server has no regex operator at all, so this list
// only decides the string-length rule.
var mssqlTextUnsupportedTypes = map[string]struct{}{
	"xml": {}, "image": {}, "binary": {}, "varbinary": {},
	"uniqueidentifier": {}, "geometry": {}, "geography": {}, "hierarchyid": {},
	"sql_variant": {},
}

// mssqlEqualityUnsupportedTypes are the SQL Server types that cannot be compared
// or grouped. text, ntext and image cannot be compared or sorted at all, xml is
// not comparable, and the spatial types define STEquals rather than =.
var mssqlEqualityUnsupportedTypes = map[string]struct{}{
	"text": {}, "ntext": {}, "image": {}, "xml": {},
	"geometry": {}, "geography": {},
}

// requireTextColumn resolves a column and requires text operations on its type.
func requireTextColumn(target ResolvedTarget, name string) (ColumnInfo, error) {
	column, err := requireColumn(target, name)
	if err != nil {
		return ColumnInfo{}, err
	}
	if column.Kind == ColumnKindUnknown {
		return ColumnInfo{}, invalidConfig("column %q has a type this build cannot classify", column.Name)
	}
	if !supportsText(column, target.Engine) {
		return ColumnInfo{}, invalidConfig("column %q is %s; this rule needs a text column", column.Name, column.Type)
	}
	return column, nil
}

// requireEqualityColumn resolves a column and requires the engine to compare its
// values for equality. Uniqueness, grouping, filters and accepted-value checks all
// express a comparison, so a type without equality would fail their statement.
func requireEqualityColumn(target ResolvedTarget, name string) (ColumnInfo, error) {
	column, err := requireColumn(target, name)
	if err != nil {
		return ColumnInfo{}, err
	}
	if column.Kind == ColumnKindUnknown {
		return ColumnInfo{}, invalidConfig("column %q has a type this build cannot classify", column.Name)
	}
	if !supportsEquality(column, target.Engine) {
		return ColumnInfo{}, invalidConfig("column %q is %s; the engine cannot compare or group values of this type", column.Name, column.Type)
	}
	return column, nil
}

// comparableColumnTypes reports whether two columns of the same kind can be
// compared directly. Text pairs compare when both sides support text operations
// (any text spelling); any other pair must name the same base type. Kind equality
// alone is too coarse for the "string" kind, which holds jsonb, uuid, bytea and
// user-defined types alike.
func comparableColumnTypes(left, right ColumnInfo, engine storepb.Engine) bool {
	if left.Kind != right.Kind {
		return false
	}
	if left.Kind != ColumnKindString {
		return true
	}
	if supportsText(left, engine) && supportsText(right, engine) {
		return true
	}
	return columnTypeBaseName(left.Type) == columnTypeBaseName(right.Type)
}
