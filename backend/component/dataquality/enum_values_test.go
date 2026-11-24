package dataquality

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestEnumValuesFromType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		want    []string
		wantNil bool
	}{
		{name: "plain labels", raw: "enum('small','medium','large')", want: []string{"small", "medium", "large"}},
		{name: "spaces after commas", raw: "enum('a', 'b')", want: []string{"a", "b"}},
		{name: "case-insensitive keyword", raw: "ENUM('a')", want: []string{"a"}},
		{name: "quotes, commas and backslashes", raw: `enum('it''s ok','a,b','c\\d')`, want: []string{"it's ok", "a,b", `c\d`}},
		{name: "control characters", raw: "enum('a\tb','c\\nd','q\"z','r\\0s')", want: []string{"a\tb", "c\nd", `q"z`, "r\x00s"}},
		{name: "carriage return and ctrl-z", raw: "enum('a\\rb','a\\Zb')", want: []string{"a\rb", "a\u001ab"}},
		{name: "padded label keeps its spaces", raw: "enum(' draft ','active')", want: []string{" draft ", "active"}},
		{name: "trailing separator", raw: "enum('a',)", want: []string{"a"}},
		{name: "dangling backslash", raw: "enum('a\\)", wantNil: true},
		{name: "unknown escape", raw: "enum('a\\qb')", wantNil: true},
		{name: "set is not an enum", raw: "set('a','b')", wantNil: true},
		{name: "varchar", raw: "varchar(255)", wantNil: true},
		{name: "text", raw: "text", wantNil: true},
		{name: "empty enum", raw: "enum()", wantNil: true},
		{name: "unterminated", raw: "enum('a'", wantNil: true},
		{name: "unquoted label", raw: "enum(a,b)", wantNil: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := enumValuesFromType(testCase.raw)
			if testCase.wantNil {
				require.Empty(t, got)
				return
			}
			require.Equal(t, testCase.want, got)
		})
	}
}

func TestPostgresUserDefinedType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw          string
		schema, name string
		ok           bool
	}{
		{raw: "public.product_status", schema: "public", name: "product_status", ok: true},
		{raw: "sales.status_enum", schema: "sales", name: "status_enum", ok: true},
		{raw: "public.citext", schema: "public", name: "citext", ok: true},
		{raw: "character varying(64)"},
		{raw: "numeric(10,2)"},
		{raw: "text[]"},
		{raw: "_int4"},
		{raw: "timestamp(3) with time zone"},
		{raw: "public."},
		{raw: ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.raw, func(t *testing.T) {
			t.Parallel()
			schema, name, ok := postgresUserDefinedType(testCase.raw)
			require.Equal(t, testCase.ok, ok)
			require.Equal(t, testCase.schema, schema)
			require.Equal(t, testCase.name, name)
		})
	}
}

// enumValidationColumn is an enum column as the resolver would enrich it.
func enumValidationColumn() ColumnInfo {
	return ColumnInfo{
		Name:       "status",
		Type:       "public.product_status",
		Kind:       ColumnKindString,
		Nullable:   true,
		EnumValues: []string{"active", "inactive", "archived", "draft"},
	}
}

func TestEnumValueConversionRejectsAnUndeclaredLabel(t *testing.T) {
	t.Parallel()

	column := enumValidationColumn()
	_, err := convertFilterValue(column, "discontinued")
	require.ErrorContains(t, err, `"discontinued"`)
	require.ErrorContains(t, err, "valid labels")
	require.ErrorContains(t, err, `"active"`)

	_, err = compileValueArg(column, "discontinued")
	require.ErrorContains(t, err, "valid labels")

	value, err := convertFilterValue(column, "draft")
	require.NoError(t, err)
	require.Equal(t, "draft", value)

	argument, err := compileValueArg(column, "draft")
	require.NoError(t, err)
	require.Equal(t, "draft", argument)
}

func TestEnumValueValidationStaysSilentWhenLabelsAreUnknown(t *testing.T) {
	t.Parallel()

	column := ColumnInfo{Name: "status", Type: "public.product_status", Kind: ColumnKindString}
	_, err := convertFilterValue(column, "anything")
	require.NoError(t, err)
}

// A large enum type must not make the stored/structured error unbounded, and the
// label list has to survive the caps.
func TestEnumValueErrorBoundsTheNamedLabels(t *testing.T) {
	t.Parallel()

	labels := make([]string, 0, maxEnumLabelsNamed+2)
	for index := 0; index <= maxEnumLabelsNamed+1; index++ {
		labels = append(labels, "label_"+string(rune('a'+index%26)))
	}
	column := ColumnInfo{Name: "status", Type: "public.status", Kind: ColumnKindString, EnumValues: labels}
	_, err := convertFilterValue(column, "missing")
	require.ErrorContains(t, err, "(2 more)")

	// Hundreds of labels still keep the error text short enough for a stored error
	// and a draft-rejection reason.
	many := make([]string, 0, 300)
	for index := 0; index < 300; index++ {
		many = append(many, fmt.Sprintf("label_%03d", index))
	}
	_, err = convertFilterValue(ColumnInfo{
		Name: "status", Kind: ColumnKindString,
		Type:       "enum('" + strings.Join(many, "','") + "')",
		EnumValues: many,
	}, "missing")
	require.ErrorContains(t, err, "valid labels")
	require.Less(t, len(err.Error()), 1024)
	require.NotContains(t, err.Error(), "label_299")
}

func TestEnrichColumnEnumValuesFromInlineTypes(t *testing.T) {
	t.Parallel()

	target := ResolvedTarget{
		Engine: storepb.Engine_MYSQL,
		Columns: []ColumnInfo{
			{Name: "status", Type: "enum('draft','published')", Kind: ColumnKindString},
			{Name: "tags", Type: "set('red','green')", Kind: ColumnKindString},
			{Name: "name", Type: "varchar(64)", Kind: ColumnKindString},
		},
	}
	enrichColumnEnumValues(t.Context(), nil, &target)
	require.Equal(t, []string{"draft", "published"}, target.Columns[0].EnumValues)
	require.Empty(t, target.Columns[1].EnumValues)
	require.Empty(t, target.Columns[2].EnumValues)
}

func TestValidateConfigRejectsAnUndeclaredAcceptedValue(t *testing.T) {
	t.Parallel()

	target := validationTarget(storepb.Engine_POSTGRES)
	target.Columns = append(target.Columns, enumValidationColumn())
	config := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_AcceptedValues{AcceptedValues: &storepb.AcceptedValuesConfig{
			Column:        "status",
			Values:        []string{"draft", "discontinued"},
			FailThreshold: proto.Float64(0),
		}},
	}
	err := ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, config)
	require.ErrorContains(t, err, "valid labels")
	require.ErrorContains(t, err, "discontinued")

	config.GetAcceptedValues().Values = []string{"draft", "active"}
	require.NoError(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, config))
}

func TestValidateConfigRejectsAnUndeclaredFilterValue(t *testing.T) {
	t.Parallel()

	target := validationTarget(storepb.Engine_POSTGRES)
	target.Columns = append(target.Columns, enumValidationColumn())
	config := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{
			MinRows: proto.Int64(1),
		}},
		Filter: &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{{
			Column: "status",
			Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ,
			Values: []string{"discontinued"},
		}}},
	}
	err := ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, config)
	require.ErrorContains(t, err, "valid labels")

	config.Filter.Clauses[0].Values = []string{"draft"}
	require.NoError(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, config))
}

// One drafting pass reports a bounded list of refusals with bounded reasons.
func TestAppendDraftRejectionBoundsTheList(t *testing.T) {
	t.Parallel()

	rejections := make([]DraftRejection, 0, maxDraftRejections)
	for index := 0; index < maxDraftRejections+3; index++ {
		rejections = appendDraftRejection(rejections, "", `value "x" is not a label of the enum type "public.status"; valid labels: "active", "draft"`)
	}
	require.Len(t, rejections, maxDraftRejections)
	require.Equal(t, "untitled draft", rejections[0].Title)

	long := appendDraftRejection(nil, "t", strings.Repeat("x", maxDraftRejectionReasonChars*2))
	require.Len(t, long[0].Reason, maxDraftRejectionReasonChars)
}

// The MySQL family compares an ENUM through the column collation: a non-binary
// collation is case-insensitive, and a non-strict ENUM also holds "".
func TestEnumValueComparisonFollowsTheEngine(t *testing.T) {
	t.Parallel()

	mysql := ColumnInfo{
		Name: "status", Type: "enum('draft','active')", Kind: ColumnKindString,
		EnumValues: []string{"draft", "active"}, EnumCaseInsensitive: true, EnumErrorMember: true,
	}
	for _, value := range []string{"Draft", "ACTIVE", ""} {
		_, err := convertFilterValue(mysql, value)
		require.NoError(t, err, "value %q", value)
	}
	_, err := convertFilterValue(mysql, "archived")
	require.ErrorContains(t, err, `"status"`)

	binary := mysql
	binary.EnumCaseInsensitive = false
	_, err = convertFilterValue(binary, "Draft")
	require.ErrorContains(t, err, "valid labels")

	postgres := enumValidationColumn()
	_, err = convertFilterValue(postgres, "Draft")
	require.ErrorContains(t, err, "valid labels")
}

// The PostgreSQL enrichment maps each column to the schema its type names, and
// copies the labels instead of aliasing the registry's cached slice.
func TestApplyEnumLabelsResolvesTheTypesSchema(t *testing.T) {
	t.Parallel()

	enums := map[string]map[string][]string{
		"public": {"product_status": {"active", "draft"}},
		"shared": {"mood": {"happy", "sad"}},
	}
	target := ResolvedTarget{
		Engine: storepb.Engine_POSTGRES,
		Columns: []ColumnInfo{
			{Name: "status", Type: "public.product_status", Kind: ColumnKindString},
			{Name: "mood", Type: "shared.mood", Kind: ColumnKindString},
			{Name: "other", Type: "public.citext", Kind: ColumnKindString},
			{Name: "names", Type: "text[]", Kind: ColumnKindString},
		},
	}
	applyEnumLabels(&target, func(schema string) map[string][]string { return enums[schema] })
	require.Equal(t, []string{"active", "draft"}, target.Columns[0].EnumValues)
	require.Equal(t, []string{"happy", "sad"}, target.Columns[1].EnumValues)
	require.Empty(t, target.Columns[2].EnumValues)
	require.Empty(t, target.Columns[3].EnumValues)

	// The resolver copies out of the registry's cached message.
	target.Columns[0].EnumValues[0] = "mutated"
	require.Equal(t, []string{"active", "draft"}, enums["public"]["product_status"])
}

// Contract filters and null_percent custom values bind through the same helpers
// as rule filters, so they reject an undeclared label too.
func TestEnumValidationCoversContractFilterAndNullValues(t *testing.T) {
	t.Parallel()

	target := validationTarget(storepb.Engine_POSTGRES)
	target.Columns = append(target.Columns, enumValidationColumn())

	dialect, err := DialectFor(storepb.Engine_POSTGRES)
	require.NoError(t, err)
	_, _, err = CompileFilter(dialect, target, Filter{Clauses: []FilterClause{{
		Column: "status", Op: FilterEQ, Values: []string{"discontinued"},
	}}})
	require.ErrorContains(t, err, "valid labels")

	nullConfig := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_NullPercent{NullPercent: &storepb.NullPercentConfig{
			Column:       "status",
			NullValues:   []string{"discontinued"},
			MaxNullCount: proto.Int64(0),
		}},
	}
	err = ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, nullConfig)
	require.ErrorContains(t, err, "valid labels")
}

// A draft's internal failure is logged, not reported as the model's refusal.
func TestReportableDraftRejection(t *testing.T) {
	t.Parallel()

	require.True(t, reportableDraftRejection(invalidConfig("bad value")))
	require.False(t, reportableDraftRejection(common.Errorf(common.Internal, "store down")))
}
