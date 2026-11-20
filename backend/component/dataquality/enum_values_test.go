package dataquality

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

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

func TestEnumValueErrorBoundsTheNamedLabels(t *testing.T) {
	t.Parallel()

	labels := make([]string, 0, maxEnumLabelsNamed+2)
	for index := 0; index <= maxEnumLabelsNamed+1; index++ {
		labels = append(labels, "label_"+string(rune('a'+index%26)))
	}
	column := ColumnInfo{Name: "status", Type: "public.status", Kind: ColumnKindString, EnumValues: labels}
	_, err := convertFilterValue(column, "missing")
	require.ErrorContains(t, err, "(2 more)")
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
