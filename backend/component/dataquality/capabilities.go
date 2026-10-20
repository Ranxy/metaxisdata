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
// A type absent from an engine's table is treated as unsupported (the zero value):
// the tables are allow-lists, because a wrong guess fails the whole statement. This
// is stricter than the pre-capability behaviour, which allowed every "string" type;
// the spellings the syncer actually produces are enumerated instead of guessed.
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
	// orderable: the type has a btree ordering operator class, which is what array
	// comparison, GROUP BY on an array, and MIN/MAX over an array need. The built-in
	// geometric types compare with = but have no btree opclass (MIN(box) does not
	// exist, and an array of boxes cannot be compared), so comparison alone is not
	// enough for arrays.
	orderable bool
	// valueComparison: a scalar value can be bound to the column in a predicate
	// (filter, accepted values, custom null values). An array can be joined to
	// another array column but a scalar literal cannot be encoded for it.
	valueComparison bool
	// text: values render as human text, so the profile's value shapes are
	// meaningful. This is deliberately not the same as "text operators exist":
	// binary values have a length but are not words.
	text bool
	// noColumnCount: COUNT(col) itself is invalid (SQL Server text/ntext/image);
	// the profile must count non-NULL values with a CASE expression instead.
	noColumnCount bool
	// noNullTest: even "col IS NULL" is invalid (StarRocks bitmap/hll/percentile
	// reject every predicate on the type), so null checks and filters cannot run.
	noNullTest bool
	// binary: values are bytes, not words, so the profile renders them as hex
	// regardless of whether the bytes happen to be valid UTF-8.
	binary bool
}

// textCapabilities is a column rendered as text: every text rule applies.
var textCapabilities = columnCapabilities{
	comparison: true, grouping: true, countDistinct: true,
	regex: true, length: true, extrema: true, text: true, orderable: true, valueComparison: true,
}

// scalarCapabilities is a number or a date/time: comparable, groupable and
// ordered, with no text operators.
var scalarCapabilities = columnCapabilities{
	comparison: true, grouping: true, countDistinct: true, extrema: true, orderable: true, valueComparison: true,
}

// capabilitiesFor reports the SQL surface of one column's type.
func capabilitiesFor(engine storepb.Engine, column ColumnInfo) columnCapabilities {
	switch column.Kind {
	case ColumnKindNumeric, ColumnKindTemporal:
		return scalarCapabilities
	case ColumnKindBoolean:
		return columnCapabilities{comparison: true, grouping: true, countDistinct: true, orderable: true, valueComparison: true}
	case ColumnKindString:
		// Dispatch on the raw spelling: "character varying[]" would lose its array
		// suffix to the base-name normalizer, and StarRocks nested types contain
		// spaces and parentheses.
		return stringCapabilities(engine, strings.ToLower(strings.TrimSpace(column.Type)))
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

// supportsNullTest reports whether "col IS NULL" / "col IS NOT NULL" is valid for
// the column's type. StarRocks bitmap, hll and percentile values reject every
// predicate, including the NULL test.
func supportsNullTest(column ColumnInfo, engine storepb.Engine) bool {
	return !capabilitiesFor(engine, column).noNullTest
}

// postgresStringCapabilities decides one PostgreSQL base type name. A user-defined
// type is stored schema-qualified: citext is the one text type with every text
// operator, PostGIS geometry has none, and the remaining user-defined types (enums,
// hstore, composites) are comparable and groupable but not text.
func postgresStringCapabilities(raw string) columnCapabilities {
	// A materialized view's array is stored as "integer[]"/"character varying[]"
	// (format_type under an empty search_path), while a table's array is stored as
	// its element's udt name ("_int4"); the suffix check must run on the raw
	// spelling, before base-name normalization truncates at the space.
	if strings.HasSuffix(raw, "[]") {
		element := strings.TrimSpace(strings.TrimSuffix(raw, "[]"))
		return postgresArrayFromElement(postgresElementCapabilities(element))
	}
	name := columnTypeBaseName(raw)
	if strings.HasPrefix(name, "_") {
		return postgresArrayFromElement(postgresElementCapabilities(name[1:]))
	}
	if index := strings.LastIndexByte(name, '.'); index >= 0 {
		switch name[index+1:] {
		case "citext":
			return textCapabilities
		case "geometry", "geography":
			return columnCapabilities{}
		}
		// A user-defined type may be an enum (comparable and groupable) or a
		// domain/composite (whose support depends on its field types). The type string
		// cannot tell them apart, so the historical rule allowance stays; the profile
		// claims neither an exact count nor MIN/MAX because composites over json fail
		// them. A composite predicate cannot bind a parameter — a pre-existing
		// limitation recorded in the docs.
		return columnCapabilities{comparison: true, grouping: true, valueComparison: true}
	}
	// Range types compare and group; they have no MIN/MAX or text operators.
	for _, suffix := range []string{"multirange", "range"} {
		if strings.HasSuffix(name, suffix) {
			return columnCapabilities{comparison: true, valueComparison: true, grouping: true, countDistinct: true, orderable: true}
		}
	}
	if capabilities, ok := postgresTypeCapabilities[name]; ok {
		return capabilities
	}
	return columnCapabilities{}
}

// postgresElementCapabilities resolves one array element. Table arrays carry the
// element's internal udt name; materialized views carry the format_type spelling,
// so both are normalized through the same tables.
func postgresElementCapabilities(element string) columnCapabilities {
	element = strings.ToLower(strings.TrimSpace(element))
	if _, ok := postgresArrayElementCapabilities[element]; ok {
		return columnCapabilities{orderable: true}
	}
	return capabilitiesFor(storepb.Engine_POSTGRES, ColumnInfo{Type: element, Kind: ClassifyColumnType(element)})
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
	"citext":    textCapabilities,
	"varbit":    {comparison: true, valueComparison: true, grouping: true, countDistinct: true, orderable: true},
	// Equality but no ordering aggregates or text operators.
	"jsonb": {comparison: true, valueComparison: true, grouping: true, countDistinct: true, orderable: true},
	"uuid":  {comparison: true, valueComparison: true, grouping: true, countDistinct: true, orderable: true},
	"bytea": {comparison: true, valueComparison: true, grouping: true, countDistinct: true, binary: true, orderable: true},
	// Equality, ordering and MIN/MAX, but no text operators.
	"interval": {comparison: true, valueComparison: true, grouping: true, countDistinct: true, extrema: true, orderable: true},
	"inet":     {comparison: true, valueComparison: true, grouping: true, countDistinct: true, extrema: true, orderable: true},
	"cidr":     {comparison: true, valueComparison: true, grouping: true, countDistinct: true, extrema: true, orderable: true},
	"macaddr":  {comparison: true, valueComparison: true, grouping: true, countDistinct: true, orderable: true},
	"macaddr8": {comparison: true, valueComparison: true, grouping: true, countDistinct: true, orderable: true},
	"bit":      {comparison: true, valueComparison: true, grouping: true, countDistinct: true, orderable: true},
	// No equality operator at all.
	"json": {}, "xml": {}, "point": {}, "polygon": {},
	"geometry": {}, "geography": {}, "refcursor": {}, "txid_snapshot": {},
	// Comparable but not groupable, and with no MIN/MAX: PostgreSQL defines no
	// ordering aggregate for the built-in geometric types.
	"box":      {comparison: true, valueComparison: true},
	"line":     {comparison: true, valueComparison: true},
	"lseg":     {comparison: true, valueComparison: true},
	"path":     {comparison: true, valueComparison: true},
	"circle":   {comparison: true, valueComparison: true},
	"tsvector": {comparison: true, valueComparison: true, grouping: true, countDistinct: true, orderable: true},
	"tsquery":  {comparison: true, valueComparison: true, grouping: true, countDistinct: true, orderable: true},
	"oid":      {comparison: true, valueComparison: true, grouping: true, countDistinct: true, orderable: true},
	"pg_lsn":   {comparison: true, valueComparison: true, grouping: true, countDistinct: true, orderable: true},
}

// postgresArrayFromElement derives an array's capabilities from its element's. An
// array operation needs the element's btree ordering operator class: comparison and
// GROUP BY on the array, and MIN/MAX over it, all fail for a type that merely has an
// = operator (box) — PostgreSQL reports "could not identify a comparison function".
// Arrays have no text operators.
func postgresArrayFromElement(elementCaps columnCapabilities) columnCapabilities {
	if !elementCaps.orderable {
		return columnCapabilities{}
	}
	return columnCapabilities{
		comparison:    true,
		grouping:      true,
		countDistinct: true,
		extrema:       true,
		orderable:     true,
	}
}

// postgresArrayElementCapabilities lists the array element udt names whose type has
// a btree ordering operator class; that is the only element fact array derivation
// reads. Spellings not listed here fall back to the type table through
// postgresElementCapabilities.
var postgresArrayElementCapabilities = map[string]struct{}{
	"int2": {}, "int4": {}, "int8": {},
	"float4": {}, "float8": {}, "numeric": {}, "money": {},
	"text": {}, "varchar": {}, "bpchar": {}, "name": {}, "citext": {},
	"bool": {}, "uuid": {}, "bytea": {}, "jsonb": {}, "varbit": {},
	"timestamp": {}, "timestamptz": {}, "date": {}, "time": {}, "timetz": {},
	"interval": {}, "inet": {}, "cidr": {}, "macaddr": {}, "macaddr8": {},
	"tsvector": {}, "tsquery": {}, "oid": {}, "pg_lsn": {},
	"int4range": {}, "int8range": {}, "numrange": {}, "tsrange": {},
	"tstzrange": {}, "daterange": {},
}

// mssqlStringCapabilities decides one SQL Server base type name. The legacy text,
// ntext and image types cannot be compared, grouped or measured, and COUNT(col) is
// invalid for them; text and ntext still shape the profile's sample values.
func mssqlStringCapabilities(raw string) columnCapabilities {
	if capabilities, ok := mssqlTypeCapabilities[columnTypeBaseName(raw)]; ok {
		return capabilities
	}
	return columnCapabilities{}
}

// mssqlTextCapabilities is the text surface without the regex operator SQL Server
// does not have.
var mssqlTextCapabilities = columnCapabilities{
	comparison: true, valueComparison: true, grouping: true, countDistinct: true,
	length: true, extrema: true, text: true, orderable: true,
}

var mssqlTypeCapabilities = map[string]columnCapabilities{
	// SQL Server has no regex operator, so text types carry every other text fact.
	"char":             mssqlTextCapabilities,
	"varchar":          mssqlTextCapabilities,
	"nchar":            mssqlTextCapabilities,
	"nvarchar":         mssqlTextCapabilities,
	"text":             {text: true, noColumnCount: true},
	"ntext":            {text: true, noColumnCount: true},
	"image":            {noColumnCount: true, binary: true},
	"xml":              {},
	"binary":           {comparison: true, grouping: true, countDistinct: true, length: true, extrema: true, binary: true, orderable: true, valueComparison: true},
	"varbinary":        {comparison: true, grouping: true, countDistinct: true, length: true, extrema: true, binary: true, orderable: true, valueComparison: true},
	"uniqueidentifier": {comparison: true, grouping: true, countDistinct: true, length: true, extrema: true, orderable: true, binary: true, valueComparison: true},
	// sql_variant compares by precedence and orders, but has no LEN.
	"sql_variant": {comparison: true, grouping: true, countDistinct: true, extrema: true, orderable: true, valueComparison: true},
	// Spatial types have no comparison, grouping or aggregation. hierarchyid
	// compares, groups, orders and has MIN/MAX, but no LEN; it is binary-valued, so
	// its display form is hex.
	"geometry": {}, "geography": {},
	"hierarchyid": {comparison: true, grouping: true, countDistinct: true, extrema: true, orderable: true, binary: true, valueComparison: true},
}

// mysqlStringCapabilities decides one MySQL-wire base type name. MySQL-family
// engines coerce JSON, spatial and binary values to text for REGEXP and
// CHAR_LENGTH, and all of them can be grouped. JSON comparison is deliberately
// denied: a bound string never compares equal to a JSON document, so an
// accepted-values or filter rule would mark every JSON row as a violation.
func mysqlStringCapabilities(raw string) columnCapabilities {
	if capabilities, ok := mysqlTypeCapabilities[columnTypeBaseName(raw)]; ok {
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
	"json": {grouping: true, countDistinct: true, regex: true, length: true, extrema: true, orderable: true, valueComparison: false},
	// Spatial types convert to text, but MIN is invalid and value comparison is not
	// a data quality check the rule author means.
	"geometry":           {grouping: true, countDistinct: true, binary: true},
	"point":              {grouping: true, countDistinct: true, binary: true},
	"linestring":         {grouping: true, countDistinct: true, binary: true},
	"polygon":            {grouping: true, countDistinct: true, binary: true},
	"multipoint":         {grouping: true, countDistinct: true, binary: true},
	"multilinestring":    {grouping: true, countDistinct: true, binary: true},
	"multipolygon":       {grouping: true, countDistinct: true, binary: true},
	"geometrycollection": {grouping: true, countDistinct: true, binary: true},
	"geomcollection":     {grouping: true, countDistinct: true, binary: true},
	// Binary and blob values compare, group, order and measure by byte length.
	"binary":     {comparison: true, grouping: true, countDistinct: true, regex: true, length: true, extrema: true, binary: true, orderable: true, valueComparison: true},
	"varbinary":  {comparison: true, grouping: true, countDistinct: true, regex: true, length: true, extrema: true, binary: true, orderable: true, valueComparison: true},
	"tinyblob":   {comparison: true, grouping: true, countDistinct: true, regex: true, length: true, extrema: true, binary: true, orderable: true, valueComparison: true},
	"blob":       {comparison: true, grouping: true, countDistinct: true, regex: true, length: true, extrema: true, binary: true},
	"mediumblob": {comparison: true, grouping: true, countDistinct: true, regex: true, length: true, extrema: true, binary: true},
	"longblob":   {comparison: true, grouping: true, countDistinct: true, regex: true, length: true, extrema: true, binary: true, orderable: true, valueComparison: true},
}

// starrocksStringCapabilities decides one StarRocks type. StarRocks compares JSON
// values, groups arrays, has no regex on JSON or arrays, and has no MIN/MAX for
// either.
func starrocksStringCapabilities(raw string) columnCapabilities {
	name := columnTypeBaseName(raw)
	switch {
	case strings.HasPrefix(raw, "array"):
		// An array groups and counts distinctly unless a nested element type is one
		// the engine refuses to group; the check runs on the raw spelling because
		// nested types contain spaces and parentheses ("array<struct<a json>>").
		// Struct field names are backticked and may themselves be called "json", so
		// they are stripped before the token check.
		return starrocksArrayCapabilities(raw)
	case name == "json":
		return columnCapabilities{comparison: true, valueComparison: true, length: true}
	case strings.HasPrefix(name, "bitmap"), strings.HasPrefix(name, "hll"):
		return columnCapabilities{countDistinct: true, noNullTest: true, binary: true}
	case name == "percentile":
		return columnCapabilities{noNullTest: true}
	case strings.HasPrefix(name, "map"), strings.HasPrefix(name, "struct"), name == "variant":
		return columnCapabilities{}
	}
	if capabilities, ok := starrocksTypeCapabilities[name]; ok {
		return capabilities
	}
	return columnCapabilities{}
}

// starrocksArrayCapabilities decides whether a StarRocks array can be grouped. The
// nested element types the engine refuses to group are detected on the raw spelling
// with backticked struct field names removed.
func starrocksArrayCapabilities(raw string) columnCapabilities {
	cleaned := stripBacktickedFragments(raw)
	for _, nested := range []string{"json", "bitmap", "hll", "percentile", "variant", "quantile"} {
		if strings.Contains(cleaned, nested) {
			return columnCapabilities{}
		}
	}
	return columnCapabilities{grouping: true, countDistinct: true}
}

// stripBacktickedFragments removes `field names` from a type spelling, so a struct
// field called "json" does not make its array look like an array of JSON.
func stripBacktickedFragments(raw string) string {
	var builder strings.Builder
	inFragment := false
	for _, r := range raw {
		switch {
		case r == '`':
			inFragment = !inFragment
		case !inFragment:
			// strings.Builder writes cannot fail.
			_, _ = builder.WriteRune(r)
		default:
			// Inside a backticked fragment: skip the field name.
		}
	}
	return builder.String()
}

var starrocksTypeCapabilities = map[string]columnCapabilities{
	"char": textCapabilities, "varchar": textCapabilities, "string": textCapabilities,
	"text": textCapabilities,
	// varbinary has a byte length and COUNT(DISTINCT), but MIN/MAX and REGEXP are
	// invalid on it.
	"varbinary": {grouping: true, countDistinct: true, length: true, binary: true},
}

// dorisStringCapabilities decides one Doris type. Doris JSON supports GROUP BY,
// REGEXP and CHAR_LENGTH but rejects comparison predicates, COUNT(DISTINCT) and
// MIN/MAX.
func dorisStringCapabilities(raw string) columnCapabilities {
	name := columnTypeBaseName(raw)
	switch {
	case strings.HasPrefix(name, "array"), strings.HasPrefix(name, "map"),
		strings.HasPrefix(name, "struct"), name == "variant", name == "ipv4", name == "ipv6":
		return columnCapabilities{}
	case name == "bitmap", name == "hll":
		// No comparison or grouping, but COUNT(col) and IS [NOT] NULL work.
		return columnCapabilities{binary: true}
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

// requireValueComparisonColumn resolves a column and requires a scalar value to be
// bindable in a predicate. Arrays compare column-to-column but a scalar literal
// cannot be encoded for an array type.
func requireValueComparisonColumn(target ResolvedTarget, name string) (ColumnInfo, error) {
	column, err := requireColumn(target, name)
	if err != nil {
		return ColumnInfo{}, err
	}
	if column.Kind == ColumnKindUnknown {
		return ColumnInfo{}, invalidConfig("column %q has a type this build cannot classify", column.Name)
	}
	if !capabilitiesFor(target.Engine, column).valueComparison {
		return ColumnInfo{}, invalidConfig("column %q is %s; the engine cannot compare it with a scalar value", column.Name, column.Type)
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

// requireNullTestColumn resolves a column and requires the engine to be able to
// test it for NULL, which plain null_percent and IS [NOT] NULL filters need.
func requireNullTestColumn(target ResolvedTarget, name string) (ColumnInfo, error) {
	column, err := requireColumn(target, name)
	if err != nil {
		return ColumnInfo{}, err
	}
	if column.Kind == ColumnKindUnknown {
		return ColumnInfo{}, invalidConfig("column %q has a type this build cannot classify", column.Name)
	}
	if !supportsNullTest(column, target.Engine) {
		return ColumnInfo{}, invalidConfig("column %q is %s; the engine cannot test values of this type for NULL", column.Name, column.Type)
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
