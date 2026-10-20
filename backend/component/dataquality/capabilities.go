package dataquality

import (
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// columnCapabilities is the SQL surface of one column type: which operations the
// engine defines for its values. ColumnKind only decides how a bound literal is
// read; it collapses every type this build does not recognize into "string",
// which is right for parameter binding and wrong for choosing operators. These
// predicates are what validation, compilation and profiling consult.
//
// Each field is a separate question because engines disagree in every direction:
// PostgreSQL jsonb supports equality but no regex or character length; StarRocks
// JSON supports comparison but rejects GROUP BY; Doris JSON accepts GROUP BY but
// rejects COUNT(DISTINCT) and comparison predicates; SQL Server accepts LEN on
// varbinary but not on its legacy text type. Conflating them produced rules the
// engine could never run.
//
// A type absent from an engine's table keeps the historical refusal (the zero
// value): this build only allows an operation it has a reason to believe in. An
// unknown spelling is therefore conservative — a rejected rule at save time,
// never a query that fails on every execution.
type columnCapabilities struct {
	// comparison: =, <>, IN and joins on the column.
	comparison bool
	// grouping: GROUP BY, which uniqueness and group checks use.
	grouping bool
	// countDistinct: COUNT(DISTINCT col), which profiling uses for exact
	// distinct counts. An engine may support grouping without it (Doris JSON).
	countDistinct bool
	// regex: the regular-expression match operator.
	regex bool
	// length: a character-length function.
	length bool
	// extrema: the MIN/MAX aggregates.
	extrema bool
	// text: values render as human text, so the profile's value shapes are
	// meaningful. This is deliberately not the same as "text operators exist":
	// binary values have a length but are not words.
	text bool
}

// textCapabilities is a column rendered as text: every text rule applies.
var textCapabilities = columnCapabilities{
	comparison: true, grouping: true, countDistinct: true,
	regex: true, length: true, extrema: true, text: true,
}

// scalarCapabilities is a number or a date/time: comparable, groupable and
// ordered, with no text operators.
var scalarCapabilities = columnCapabilities{
	comparison: true, grouping: true, countDistinct: true, extrema: true,
}

// capabilitiesFor reports the SQL surface of one column's type.
func capabilitiesFor(engine storepb.Engine, column ColumnInfo) columnCapabilities {
	switch column.Kind {
	case ColumnKindNumeric, ColumnKindTemporal:
		return scalarCapabilities
	case ColumnKindBoolean:
		return columnCapabilities{comparison: true, grouping: true, countDistinct: true}
	case ColumnKindString:
		return stringCapabilities(engine, columnTypeBaseName(column.Type))
	case ColumnKindUnknown:
		return columnCapabilities{}
	}
	return columnCapabilities{}
}

// stringCapabilities dispatches one engine type spelling to its engine family.
func stringCapabilities(engine storepb.Engine, name string) columnCapabilities {
	switch engine {
	case storepb.Engine_POSTGRES:
		return postgresStringCapabilities(name)
	case storepb.Engine_MSSQL:
		return mssqlStringCapabilities(name)
	case storepb.Engine_MYSQL, storepb.Engine_TIDB, storepb.Engine_MARIADB,
		storepb.Engine_OCEANBASE:
		return mysqlStringCapabilities(name)
	case storepb.Engine_STARROCKS:
		return starrocksStringCapabilities(name)
	case storepb.Engine_DORIS:
		return dorisStringCapabilities(name)
	default:
		// An engine without a dialect is refused before a rule compiles.
		return columnCapabilities{}
	}
}

// supportsComparison reports whether the column can be compared by value, which
// filters, accepted values, custom null values and referential joins need.
func supportsComparison(column ColumnInfo, engine storepb.Engine) bool {
	return capabilitiesFor(engine, column).comparison
}

// supportsGrouping reports whether the column can be a GROUP BY key, which
// uniqueness and group-check rules need.
func supportsGrouping(column ColumnInfo, engine storepb.Engine) bool {
	return capabilitiesFor(engine, column).grouping
}

// supportsRegex reports whether the engine has a regular-expression operator for
// the column's values.
func supportsRegex(column ColumnInfo, engine storepb.Engine) bool {
	return capabilitiesFor(engine, column).regex
}

// supportsLength reports whether the engine has a character-length function for
// the column's values.
func supportsLength(column ColumnInfo, engine storepb.Engine) bool {
	return capabilitiesFor(engine, column).length
}

// postgresStringCapabilities decides one PostgreSQL base type name. A user-defined
// type is stored schema-qualified: citext is the one text type with every text
// operator, PostGIS geometry has none, and the remaining user-defined types (enums,
// hstore, composites) are comparable and groupable but not text.
func postgresStringCapabilities(name string) columnCapabilities {
	if strings.HasPrefix(name, "_") {
		return postgresArrayCapabilities(name[1:])
	}
	if index := strings.LastIndexByte(name, '.'); index >= 0 {
		switch name[index+1:] {
		case "citext":
			return textCapabilities
		case "geometry", "geography":
			return columnCapabilities{}
		default:
			return columnCapabilities{comparison: true, grouping: true, countDistinct: true}
		}
	}
	if capabilities, ok := postgresTypeCapabilities[name]; ok {
		return capabilities
	}
	return columnCapabilities{}
}

// postgresTypeCapabilities is the PostgreSQL type table. The entries are the
// information_schema data_type spellings the syncer stores, plus the internal names
// arrays use as their element's udt name.
var postgresTypeCapabilities = map[string]columnCapabilities{
	// Text.
	"character": textCapabilities,
	"varchar":   textCapabilities,
	"text":      textCapabilities,
	"name":      textCapabilities,
	// Equality but no ordering aggregates or text operators.
	"jsonb": {comparison: true, grouping: true, countDistinct: true},
	"uuid":  {comparison: true, grouping: true, countDistinct: true},
	"bytea": {comparison: true, grouping: true, countDistinct: true},
	// Equality, ordering and MIN/MAX, but no text operators.
	"interval": {comparison: true, grouping: true, countDistinct: true, extrema: true},
	"inet":     {comparison: true, grouping: true, countDistinct: true, extrema: true},
	"cidr":     {comparison: true, grouping: true, countDistinct: true, extrema: true},
	"macaddr":  {comparison: true, grouping: true, countDistinct: true, extrema: true},
	"macaddr8": {comparison: true, grouping: true, countDistinct: true, extrema: true},
	// No equality operator at all.
	"json": {}, "xml": {}, "point": {}, "polygon": {},
	"geometry": {}, "geography": {}, "refcursor": {}, "txid_snapshot": {},
	// Comparable but not groupable, and with no MIN/MAX: PostgreSQL defines no
	// ordering aggregate for the built-in geometric types.
	"box":      {comparison: true},
	"line":     {comparison: true},
	"lseg":     {comparison: true},
	"path":     {comparison: true},
	"circle":   {comparison: true},
	"tsvector": {comparison: true, grouping: true, countDistinct: true},
	"tsquery":  {comparison: true, grouping: true, countDistinct: true},
	"oid":      {comparison: true, grouping: true, countDistinct: true},
	"pg_lsn":   {comparison: true, grouping: true, countDistinct: true},
}

// postgresArrayCapabilities derives an array's capabilities from its element. An
// array compares only when its element does; GROUP BY and COUNT(DISTINCT) need the
// element to be groupable too. Arrays have no text operators, and MIN/MAX on an
// array is only safe when the element has an ordering aggregate.
func postgresArrayCapabilities(element string) columnCapabilities {
	elementCaps, ok := postgresArrayElementCapabilities[element]
	if !ok {
		return columnCapabilities{}
	}
	capabilities := columnCapabilities{comparison: elementCaps.comparison}
	if elementCaps.comparison && elementCaps.grouping {
		capabilities.grouping = true
		capabilities.countDistinct = true
	}
	capabilities.extrema = elementCaps.extrema
	return capabilities
}

// postgresArrayElementCapabilities is the capability table for an array's element,
// keyed by the udt_name PostgreSQL stores for the array type ("_int4" arrives here
// as "int4").
var postgresArrayElementCapabilities = map[string]columnCapabilities{
	"int2":        scalarCapabilities,
	"int4":        scalarCapabilities,
	"int8":        scalarCapabilities,
	"float4":      scalarCapabilities,
	"float8":      scalarCapabilities,
	"numeric":     scalarCapabilities,
	"money":       scalarCapabilities,
	"text":        textCapabilities,
	"varchar":     textCapabilities,
	"bpchar":      textCapabilities,
	"bool":        {comparison: true, grouping: true, countDistinct: true},
	"uuid":        {comparison: true, grouping: true, countDistinct: true},
	"bytea":       {comparison: true, grouping: true, countDistinct: true},
	"jsonb":       {comparison: true, grouping: true, countDistinct: true},
	"timestamp":   {comparison: true, grouping: true, countDistinct: true, extrema: true},
	"timestamptz": {comparison: true, grouping: true, countDistinct: true, extrema: true},
	"date":        {comparison: true, grouping: true, countDistinct: true, extrema: true},
	"time":        {comparison: true, grouping: true, countDistinct: true, extrema: true},
	"timetz":      {comparison: true, grouping: true, countDistinct: true, extrema: true},
	"interval":    {comparison: true, grouping: true, countDistinct: true, extrema: true},
	"json":        {}, "xml": {}, "point": {}, "polygon": {},
}

// mssqlStringCapabilities decides one SQL Server base type name. The legacy text,
// ntext and image types cannot be compared, grouped or measured, and COUNT(col) is
// invalid for them; text and ntext still shape the profile's sample values.
func mssqlStringCapabilities(name string) columnCapabilities {
	if capabilities, ok := mssqlTypeCapabilities[name]; ok {
		return capabilities
	}
	return columnCapabilities{}
}

var mssqlTypeCapabilities = map[string]columnCapabilities{
	// SQL Server has no regex operator, so text types carry every other text fact.
	"char":             {comparison: true, grouping: true, countDistinct: true, length: true, extrema: true, text: true},
	"varchar":          {comparison: true, grouping: true, countDistinct: true, length: true, extrema: true, text: true},
	"nchar":            {comparison: true, grouping: true, countDistinct: true, length: true, extrema: true, text: true},
	"nvarchar":         {comparison: true, grouping: true, countDistinct: true, length: true, extrema: true, text: true},
	"text":             {text: true},
	"ntext":            {text: true},
	"image":            {},
	"xml":              {},
	"binary":           {comparison: true, grouping: true, countDistinct: true, length: true, extrema: true},
	"varbinary":        {comparison: true, grouping: true, countDistinct: true, length: true, extrema: true},
	"uniqueidentifier": {comparison: true, grouping: true, countDistinct: true, length: true, extrema: true},
	// sql_variant compares by precedence and orders, but has no LEN.
	"sql_variant": {comparison: true, grouping: true, countDistinct: true, extrema: true},
	// Spatial and CLR types have no comparison, grouping or aggregation.
	"geometry": {}, "geography": {}, "hierarchyid": {},
}

// mysqlStringCapabilities decides one MySQL-wire base type name. MySQL-family
// engines coerce JSON, spatial and binary values to text for REGEXP and
// CHAR_LENGTH, and all of them can be grouped. JSON comparison is deliberately
// denied: a bound string never compares equal to a JSON document, so an
// accepted-values or filter rule would mark every JSON row as a violation.
func mysqlStringCapabilities(name string) columnCapabilities {
	if capabilities, ok := mysqlTypeCapabilities[name]; ok {
		return capabilities
	}
	return columnCapabilities{}
}

var mysqlTypeCapabilities = map[string]columnCapabilities{
	"char": textCapabilities, "varchar": textCapabilities,
	"tinytext": textCapabilities, "text": textCapabilities,
	"mediumtext": textCapabilities, "longtext": textCapabilities,
	"enum": textCapabilities, "set": textCapabilities,
	// JSON: groupable, ordered, measurable and matchable, but a bound string never
	// compares equal to a JSON document.
	"json": {grouping: true, countDistinct: true, regex: true, length: true, extrema: true},
	// Spatial types convert to text, but MIN is invalid and value comparison is not
	// a data quality check the rule author means.
	"geometry":           {grouping: true, countDistinct: true, regex: true, length: true},
	"point":              {grouping: true, countDistinct: true, regex: true, length: true},
	"linestring":         {grouping: true, countDistinct: true, regex: true, length: true},
	"polygon":            {grouping: true, countDistinct: true, regex: true, length: true},
	"multipoint":         {grouping: true, countDistinct: true, regex: true, length: true},
	"multilinestring":    {grouping: true, countDistinct: true, regex: true, length: true},
	"multipolygon":       {grouping: true, countDistinct: true, regex: true, length: true},
	"geometrycollection": {grouping: true, countDistinct: true, regex: true, length: true},
	// Binary and blob values compare, group, order and measure by byte length.
	"binary":     {comparison: true, grouping: true, countDistinct: true, regex: true, length: true, extrema: true},
	"varbinary":  {comparison: true, grouping: true, countDistinct: true, regex: true, length: true, extrema: true},
	"tinyblob":   {comparison: true, grouping: true, countDistinct: true, regex: true, length: true, extrema: true},
	"blob":       {comparison: true, grouping: true, countDistinct: true, regex: true, length: true, extrema: true},
	"mediumblob": {comparison: true, grouping: true, countDistinct: true, regex: true, length: true, extrema: true},
	"longblob":   {comparison: true, grouping: true, countDistinct: true, regex: true, length: true, extrema: true},
}

// starrocksStringCapabilities decides one StarRocks type. StarRocks compares JSON
// values, groups arrays, has no regex on JSON or arrays, and has no MIN/MAX for
// either.
func starrocksStringCapabilities(name string) columnCapabilities {
	switch {
	case strings.HasPrefix(name, "array"):
		return columnCapabilities{grouping: true, countDistinct: true}
	case strings.HasPrefix(name, "map"), strings.HasPrefix(name, "struct"),
		name == "json", strings.HasPrefix(name, "bitmap"), strings.HasPrefix(name, "hll"),
		name == "percentile", name == "variant":
		return starrocksNestedCapabilities(name)
	}
	if capabilities, ok := starrocksTypeCapabilities[name]; ok {
		return capabilities
	}
	return columnCapabilities{}
}

// starrocksNestedCapabilities handles the semi-structured types, whose only
// special case is JSON: StarRocks compares JSON values and measures their length.
func starrocksNestedCapabilities(name string) columnCapabilities {
	if name == "json" {
		return columnCapabilities{comparison: true, length: true}
	}
	return columnCapabilities{}
}

var starrocksTypeCapabilities = map[string]columnCapabilities{
	"char": textCapabilities, "varchar": textCapabilities, "string": textCapabilities,
	"text": textCapabilities,
}

// dorisStringCapabilities decides one Doris type. Doris JSON supports GROUP BY,
// REGEXP and CHAR_LENGTH but rejects comparison predicates, COUNT(DISTINCT) and
// MIN/MAX.
func dorisStringCapabilities(name string) columnCapabilities {
	switch {
	case strings.HasPrefix(name, "array"), strings.HasPrefix(name, "map"),
		strings.HasPrefix(name, "struct"), name == "bitmap", name == "hll",
		name == "variant", name == "ipv4", name == "ipv6":
		return columnCapabilities{}
	}
	if capabilities, ok := dorisTypeCapabilities[name]; ok {
		return capabilities
	}
	return columnCapabilities{}
}

var dorisTypeCapabilities = map[string]columnCapabilities{
	"char": textCapabilities, "varchar": textCapabilities, "string": textCapabilities,
	"text": textCapabilities,
	"json": {grouping: true, regex: true, length: true},
}

// requireRegexColumn resolves a column and requires regular-expression support.
func requireRegexColumn(target ResolvedTarget, name string) (ColumnInfo, error) {
	column, err := requireColumn(target, name)
	if err != nil {
		return ColumnInfo{}, err
	}
	if column.Kind == ColumnKindUnknown {
		return ColumnInfo{}, invalidConfig("column %q has a type this build cannot classify", column.Name)
	}
	if !supportsRegex(column, target.Engine) {
		return ColumnInfo{}, invalidConfig("column %q is %s; this rule needs a text type the engine can match with a regular-expression operator", column.Name, column.Type)
	}
	return column, nil
}

// requireLengthColumn resolves a column and requires a character-length function.
func requireLengthColumn(target ResolvedTarget, name string) (ColumnInfo, error) {
	column, err := requireColumn(target, name)
	if err != nil {
		return ColumnInfo{}, err
	}
	if column.Kind == ColumnKindUnknown {
		return ColumnInfo{}, invalidConfig("column %q has a type this build cannot classify", column.Name)
	}
	if !supportsLength(column, target.Engine) {
		return ColumnInfo{}, invalidConfig("column %q is %s; this rule needs a text type with a character length", column.Name, column.Type)
	}
	return column, nil
}

// requireComparisonColumn resolves a column and requires the engine to compare its
// values by equality.
func requireComparisonColumn(target ResolvedTarget, name string) (ColumnInfo, error) {
	column, err := requireColumn(target, name)
	if err != nil {
		return ColumnInfo{}, err
	}
	if column.Kind == ColumnKindUnknown {
		return ColumnInfo{}, invalidConfig("column %q has a type this build cannot classify", column.Name)
	}
	if !supportsComparison(column, target.Engine) {
		return ColumnInfo{}, invalidConfig("column %q is %s; the engine cannot compare values of this type", column.Name, column.Type)
	}
	return column, nil
}

// requireGroupingColumn resolves a column and requires the engine to group its
// values, which uniqueness and group-check rules express with GROUP BY.
func requireGroupingColumn(target ResolvedTarget, name string) (ColumnInfo, error) {
	column, err := requireColumn(target, name)
	if err != nil {
		return ColumnInfo{}, err
	}
	if column.Kind == ColumnKindUnknown {
		return ColumnInfo{}, invalidConfig("column %q has a type this build cannot classify", column.Name)
	}
	if !supportsGrouping(column, target.Engine) {
		return ColumnInfo{}, invalidConfig("column %q is %s; the engine cannot group values of this type", column.Name, column.Type)
	}
	return column, nil
}

// comparableColumnTypes reports whether two columns of the same kind can be
// compared directly. Text pairs compare when both render as text; any other pair
// must name the same base type. Kind equality alone is too coarse for the "string"
// kind, which holds text, jsonb, uuid, bytea and user-defined types alike.
func comparableColumnTypes(left, right ColumnInfo, engine storepb.Engine) bool {
	if left.Kind != right.Kind {
		return false
	}
	if left.Kind != ColumnKindString {
		return true
	}
	leftCaps := capabilitiesFor(engine, left)
	rightCaps := capabilitiesFor(engine, right)
	if leftCaps.text && rightCaps.text {
		return true
	}
	return columnTypeBaseName(left.Type) == columnTypeBaseName(right.Type)
}
