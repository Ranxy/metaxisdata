package dataquality

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

// standardCodeRuleConfig builds one standard-code rule config.
func standardCodeRuleConfig(column string, standard storepb.QualityStandardCode, sampleRows int32) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_StandardCode{StandardCode: &storepb.StandardCodeConfig{
		Column:     column,
		Standard:   standard,
		SampleRows: sampleRows,
	}}}
}

func validationStandardCodeConfig(config *storepb.StandardCodeConfig) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_StandardCode{StandardCode: config}}
}

// The compiler emits one bounded sample per dialect; the validation itself runs
// in process, so the statement shape is the whole SQL contract.
func TestCompileStandardCodeGoldens(t *testing.T) {
	t.Parallel()

	config := standardCodeRuleConfig("name", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, 1000)
	tests := []struct {
		engine storepb.Engine
		want   string
	}{
		{
			engine: storepb.Engine_MYSQL,
			want:   "SELECT `id`, `name` FROM `shop`.`orders` LIMIT 1000",
		},
		{
			engine: storepb.Engine_POSTGRES,
			want:   `SELECT "id", "name" FROM "public"."orders" LIMIT 1000`,
		},
		{
			engine: storepb.Engine_STARROCKS,
			want:   "SELECT `id`, `name` FROM `shop`.`orders` LIMIT 1000",
		},
		{
			engine: storepb.Engine_MSSQL,
			want:   "SELECT [id], [name] FROM [public].[orders] ORDER BY (SELECT NULL) OFFSET 0 ROWS FETCH NEXT 1000 ROWS ONLY",
		},
	}
	for _, test := range tests {
		t.Run(test.engine.String(), func(t *testing.T) {
			t.Parallel()
			dialect, err := DialectFor(test.engine)
			require.NoError(t, err)
			query, err := Compile(CompileInput{
				Dialect: dialect,
				Target:  compileTestTarget(test.engine),
				Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE,
				Config:  config,
			})
			require.NoError(t, err)
			require.Equal(t, QuerySample, query.Kind)
			require.Equal(t, []string{"id", "name"}, query.Columns)
			require.Nil(t, query.Args)
			require.Equal(t, test.want, query.SQL)
		})
	}
}

// A rule filter still bounds the sample, and its arguments keep the position the
// dialect expects.
func TestCompileStandardCodeCarriesTheRuleFilter(t *testing.T) {
	t.Parallel()

	dialect, err := DialectFor(storepb.Engine_MYSQL)
	require.NoError(t, err)
	query, err := Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE,
		Config:  standardCodeRuleConfig("email", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, 10),
		Filter:  Filter{Clauses: []FilterClause{{Column: "status", Op: FilterEQ, Values: []string{"active"}}}},
	})
	require.NoError(t, err)
	require.Equal(t, []any{"active"}, query.Args)
	require.Equal(t, "SELECT `id`, `email` FROM `shop`.`orders` WHERE `status` = ? LIMIT 10", query.SQL)
}

func TestValidateStandardCodeConfig(t *testing.T) {
	t.Parallel()

	failThreshold := func(value float64) *float64 { return &value }
	cases := []validationCase{
		{name: "valid uscc", config: validationStandardCodeConfig(&storepb.StandardCodeConfig{
			Column: "name", Standard: storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE,
		})},
		{name: "valid with thresholds", config: validationStandardCodeConfig(&storepb.StandardCodeConfig{
			Column: "name", Standard: storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ORGANIZATION_CODE,
			FailThreshold: failThreshold(5), WarnThreshold: failThreshold(1),
		})},
		{name: "valid percent threshold", config: func() *storepb.QualityRuleConfig {
			config := validationStandardCodeConfig(&storepb.StandardCodeConfig{
				Column: "name", Standard: storepb.QualityStandardCode_QUALITY_STANDARD_CODE_RESIDENT_ID_CARD,
				FailThreshold: failThreshold(100),
			})
			config.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
			return config
		}()},
		{name: "allow null", config: validationStandardCodeConfig(&storepb.StandardCodeConfig{
			Column: "name", Standard: storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ADMINISTRATIVE_DIVISION, AllowNull: true,
		})},
		{name: "no column", config: validationStandardCodeConfig(&storepb.StandardCodeConfig{
			Standard: storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE,
		}), wantErr: true},
		{name: "unknown column", config: validationStandardCodeConfig(&storepb.StandardCodeConfig{
			Column: "missing", Standard: storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE,
		}), wantErr: true},
		{name: "numeric column", config: validationStandardCodeConfig(&storepb.StandardCodeConfig{
			Column: "id", Standard: storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE,
		}), wantErr: true},
		{name: "no standard", config: validationStandardCodeConfig(&storepb.StandardCodeConfig{
			Column: "name",
		}), wantErr: true},
		{name: "unknown non-zero standard", config: validationStandardCodeConfig(&storepb.StandardCodeConfig{
			Column: "name", Standard: storepb.QualityStandardCode(6),
		}), wantErr: true},
		{name: "negative sample rows", config: validationStandardCodeConfig(&storepb.StandardCodeConfig{
			Column: "name", Standard: storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, SampleRows: -1,
		}), wantErr: true},
		{name: "sample rows above the maximum", config: validationStandardCodeConfig(&storepb.StandardCodeConfig{
			Column: "name", Standard: storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE,
			SampleRows: maxStandardCodeSampleRows + 1,
		}), wantErr: true},
		{name: "warn above fail", config: validationStandardCodeConfig(&storepb.StandardCodeConfig{
			Column: "name", Standard: storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE,
			FailThreshold: failThreshold(1), WarnThreshold: failThreshold(2),
		}), wantErr: true},
		{name: "percent above 100", config: func() *storepb.QualityRuleConfig {
			config := validationStandardCodeConfig(&storepb.StandardCodeConfig{
				Column: "name", Standard: storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE,
				FailThreshold: failThreshold(101),
			})
			config.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
			return config
		}(), wantErr: true},
		{name: "missing config", config: validationStandardCodeConfig(nil), wantErr: true},
	}
	runValidationCases(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE, validationTarget(storepb.Engine_POSTGRES), cases)
}

// A standard-code rule has its own sample_rows, so the shared sampling method is
// refused with a message that points at the right knob.
func TestValidateStandardCodeRefusesSharedSampling(t *testing.T) {
	t.Parallel()

	config := standardCodeRuleConfig("name", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, 100)
	config.Sampling = &storepb.QualityRuleSampling{Method: &storepb.QualityRuleSampling_MaxRows{MaxRows: 100}}
	err := ValidateConfig(validationTarget(storepb.Engine_MYSQL), storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE, config)
	require.Error(t, err)
	require.Contains(t, err.Error(), "sample_rows")
}

// Every engine that has a dialect can run the type: no SQL-side arithmetic is
// involved. Views carry no column registry rows, so the type is table-only.
func TestStandardCodeSupportedMatrix(t *testing.T) {
	t.Parallel()

	for _, engine := range []storepb.Engine{
		storepb.Engine_MYSQL,
		storepb.Engine_TIDB,
		storepb.Engine_MARIADB,
		storepb.Engine_OCEANBASE,
		storepb.Engine_POSTGRES,
		storepb.Engine_MSSQL,
		storepb.Engine_STARROCKS,
		storepb.Engine_DORIS,
	} {
		ok, reason := Supported(engine, storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE)
		require.True(t, ok, "%s: %s", engine, reason)
		require.Empty(t, reason)
	}
	require.True(t, RuleTypeForTarget(storepb.MetaType_TABLE, storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE))
	require.True(t, RuleTypeForTarget(storepb.MetaType_EXTERNAL_TABLE, storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE))
	require.False(t, RuleTypeForTarget(storepb.MetaType_VIEW, storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE))
	require.False(t, RuleTypeForTarget(storepb.MetaType_MATERIALIZED_VIEW, storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE))
	require.False(t, IsMetadataRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE))
}

func TestStandardCodeSampleRows(t *testing.T) {
	t.Parallel()

	rows, err := standardCodeSampleRows(0)
	require.NoError(t, err)
	require.Equal(t, defaultStandardCodeSampleRows, rows)

	rows, err = standardCodeSampleRows(maxStandardCodeSampleRows)
	require.NoError(t, err)
	require.Equal(t, maxStandardCodeSampleRows, rows)

	_, err = standardCodeSampleRows(-1)
	require.Error(t, err)
	_, err = standardCodeSampleRows(maxStandardCodeSampleRows + 1)
	require.Error(t, err)
}

// TestStandardCodeValidators pins the algorithm of every standard against known
// valid codes and the near misses a real column contains.
func TestStandardCodeValidators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		standard storepb.QualityStandardCode
		value    string
		want     bool
	}{
		// GB 32100-2015: digits and A-Z without I, O, S, V, Z; MOD 31 check digit.
		{"uscc valid", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, "91350100M000100Y43", true},
		{"uscc valid second", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, "91330100799655058B", true},
		{"uscc valid tencent", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, "91440300708461136T", true},
		{"uscc lowercase", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, "91350100m000100y43", true},
		{"uscc trailing space", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, "91350100M000100Y43 ", true},
		{"uscc wrong check", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, "91350100M000100Y44", false},
		{"uscc excluded letter", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, "91I50100M000100Y43", false},
		{"uscc bad registrar", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, "01350100M000100Y43", false},
		{"uscc bad division digits", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, "91A50100M000100Y43", false},
		{"uscc short", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, "91350100M000100Y4", false},
		{"uscc empty", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, "", false},

		// GB/T 11714-1997: eight body characters plus a MOD 11 check character.
		{"org valid", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ORGANIZATION_CODE, "D2143569X", true},
		{"org valid hyphen", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ORGANIZATION_CODE, "D2143569-X", true},
		{"org valid fullwidth hyphen", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ORGANIZATION_CODE, "D2143569－X", true},
		{"org lowercase", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ORGANIZATION_CODE, "d2143569x", true},
		{"org wrong check", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ORGANIZATION_CODE, "D21435690", false},
		{"org missing check", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ORGANIZATION_CODE, "D2143569", false},
		{"org hyphen misplaced", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ORGANIZATION_CODE, "D214356-9X", false},
		{"org body character", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ORGANIZATION_CODE, "D214356!X", false},

		// GB 11643-1999: 18 digits, real birth date, province prefix, MOD 11.
		{"id valid", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_RESIDENT_ID_CARD, "11010519491231002X", true},
		{"id valid second", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_RESIDENT_ID_CARD, "440524188001010014", true},
		{"id lowercase check", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_RESIDENT_ID_CARD, "11010519491231002x", true},
		{"id wrong check", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_RESIDENT_ID_CARD, "110105194912310021", false},
		{"id impossible date", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_RESIDENT_ID_CARD, "110105202302300021", false},
		{"id future date", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_RESIDENT_ID_CARD, "11010529991231002X", false},
		{"id unknown province", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_RESIDENT_ID_CARD, "99010519491231002X", false},
		{"id short", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_RESIDENT_ID_CARD, "11010519491231002", false},
		{"id letter inside", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_RESIDENT_ID_CARD, "11010519491231A02X", false},

		// GB/T 2260: membership in the embedded division table.
		{"division province", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ADMINISTRATIVE_DIVISION, "110000", true},
		{"division city", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ADMINISTRATIVE_DIVISION, "440300", true},
		{"division county", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ADMINISTRATIVE_DIVISION, "440305", true},
		{"division unknown", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ADMINISTRATIVE_DIVISION, "999999", false},
		{"division development zone", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ADMINISTRATIVE_DIVISION, "320571", false},
		{"division short", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ADMINISTRATIVE_DIVISION, "11000", false},
		{"division letters", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ADMINISTRATIVE_DIVISION, "11000A", false},

		// GB/T 4754-2017: section letter plus 2-4 digits, from the embedded table.
		{"industry section", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_INDUSTRY_CLASSIFICATION, "C", true},
		{"industry major", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_INDUSTRY_CLASSIFICATION, "C39", true},
		{"industry middle", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_INDUSTRY_CLASSIFICATION, "C391", true},
		{"industry minor", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_INDUSTRY_CLASSIFICATION, "C3911", true},
		{"industry lowercase", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_INDUSTRY_CLASSIFICATION, "c3911", true},
		{"industry numeric only", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_INDUSTRY_CLASSIFICATION, "3911", true},
		{"industry wrong section", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_INDUSTRY_CLASSIFICATION, "A39", false},
		{"industry unknown section", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_INDUSTRY_CLASSIFICATION, "Z01", false},
		{"industry one digit", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_INDUSTRY_CLASSIFICATION, "A3", false},
		{"industry too long", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_INDUSTRY_CLASSIFICATION, "C39111", false},
		{"industry empty", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_INDUSTRY_CLASSIFICATION, "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, validStandardCode(test.standard, test.value), "value %q", test.value)
		})
	}
}

// TestStandardCodeValueShape pins the normalization the whole rule relies on:
// case and CHAR padding are not defects, but leading whitespace is.
func TestStandardCodeValueShape(t *testing.T) {
	t.Parallel()

	require.True(t, validUnifiedSocialCreditCode("91350100M000100Y43 "))
	require.False(t, validUnifiedSocialCreditCode(" 91350100M000100Y43"))
	require.True(t, validResidentIDCard("11010519491231002X  "))
	require.False(t, validResidentIDCard(" 11010519491231002X"))
}

func TestEvaluateStandardCodeRows(t *testing.T) {
	t.Parallel()

	rows := [][]any{
		{int64(1), "91350100M000100Y43"},
		{int64(2), "broken"},
		{int64(3), nil},
		{int64(4), "91330100799655058B"},
	}
	violations, evaluated, failing, err := evaluateStandardCodeRows(
		storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, false, 1, rows, 1)
	require.NoError(t, err)
	require.Equal(t, int64(2), violations)
	require.Equal(t, int64(4), evaluated)
	require.Len(t, failing, 1, "the failing-row detail is capped by the caller's limit")
	require.Equal(t, int64(2), failing[0][0])
	require.Equal(t, "broken", failing[0][1])

	// allow_null turns a NULL row into a pass but leaves every other violation.
	violations, evaluated, _, err = evaluateStandardCodeRows(
		storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, true, 1, rows, 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), violations)
	require.Equal(t, int64(4), evaluated)
}

func TestEvaluateStandardCodeRowsRejectsNonText(t *testing.T) {
	t.Parallel()

	_, _, _, err := evaluateStandardCodeRows(
		storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, false, 1,
		[][]any{{int64(1), int64(42)}}, 0)
	require.Error(t, err)
}

// sampleColumnValue returns one column's value from a failure sample.
func sampleColumnValue(sample *storepb.FailureSample, column string) string {
	for _, cell := range sample.GetColumns() {
		if cell.GetColumn() == column {
			return cell.GetValue()
		}
	}
	return ""
}

// TestMaskedResidentIDCardValue pins the redaction shape: the division prefix and
// the last four characters stay visible, everything between is replaced.
func TestMaskedResidentIDCardValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"full id", "11010519491231002X", "110105********002X"},
		{"lowercase check", "11010519491231002x", "110105********002x"},
		{"15 digit legacy", "110105491231002", "110105*****1002"},
		{"too short", "110105", "******"},
		{"empty", "", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, maskedResidentIDCardValue(test.value))
		})
	}
}

// The resident-ID check must not persist the raw number as failed-row evidence: the
// stored sample keeps a display form and says it was masked.
func TestExecuteStandardCodeRuleMasksResidentIDSamples(t *testing.T) {
	t.Parallel()

	config := standardCodeRuleConfig("name", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_RESIDENT_ID_CARD, 10)
	execution, _, err := runStandardCodeExecution(t, config, &db.QueryResult{
		Columns: []string{"id", "name"},
		Rows:    [][]any{{int64(7), "110105194912310021"}},
	}, ExecOptions{StoreSamples: true, SampleLimit: 10})
	require.NoError(t, err)
	require.NotNil(t, execution.Samples)
	require.Len(t, execution.Samples.GetSamples(), 1)

	cell := execution.Samples.GetSamples()[0].GetColumns()[1]
	require.Equal(t, "name", cell.GetColumn())
	require.Equal(t, "110105********0021", cell.GetValue())
	require.True(t, cell.GetMasked())
	for _, sample := range execution.Samples.GetSamples() {
		for _, value := range sample.GetColumns() {
			require.NotContains(t, value.GetValue(), "19491231", "the birth date must not be stored")
		}
	}
}

// runStandardCodeExecution runs one standard-code rule through the executor with
// the sample rows the fake queryer returns.
func runStandardCodeExecution(t *testing.T, config *storepb.QualityRuleConfig, result *db.QueryResult, opts ExecOptions) (*Execution, *execFakeQueryer, error) {
	t.Helper()
	queryer := &execFakeQueryer{results: []*db.QueryResult{result}}
	execution, err := NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_MYSQL),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE, config),
		opts)
	return execution, queryer, err
}

func TestExecuteStandardCodeRuleCountsInvalidValues(t *testing.T) {
	t.Parallel()

	config := standardCodeRuleConfig("name", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, 100)
	execution, queryer, err := runStandardCodeExecution(t, config, &db.QueryResult{
		Columns: []string{"id", "name"},
		Rows:    [][]any{{int64(1), "91350100M000100Y43"}, {int64(2), "broken"}, {int64(3), "91330100799655058B"}},
	}, ExecOptions{StoreSamples: true, SampleLimit: 20})
	require.NoError(t, err)

	observation := execution.Observation
	require.Equal(t, int64(1), observation.FailedRows)
	require.Equal(t, int64(3), observation.TotalRows)
	require.Equal(t, storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT, observation.ObservedKind)
	// Fewer rows than the cap means the whole filtered column was read.
	require.False(t, observation.Sampled)
	require.Equal(t, int64(3), observation.SampleRows)
	require.False(t, execution.Truncated)

	require.Len(t, queryer.queries, 1, "a standard-code rule needs no second sample query")
	require.Equal(t, 100, queryer.options[0].MaxRows, "the cap must equal sample_rows")
	require.Equal(t, queryer.queries[0], execution.ExecutedSQL)

	require.NotNil(t, execution.Samples)
	require.Len(t, execution.Samples.GetSamples(), 1)
	require.Equal(t, "broken", sampleColumnValue(execution.Samples.GetSamples()[0], "name"))
	require.Equal(t, "2", sampleColumnValue(execution.Samples.GetSamples()[0], "id"), "the key column identifies the row")
}

func TestExecuteStandardCodeRuleMarksACappedSample(t *testing.T) {
	t.Parallel()

	config := standardCodeRuleConfig("name", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ADMINISTRATIVE_DIVISION, 2)
	execution, _, err := runStandardCodeExecution(t, config, &db.QueryResult{
		Columns: []string{"id", "name"},
		Rows:    [][]any{{int64(1), "110101"}, {int64(2), "440305"}},
	}, ExecOptions{})
	require.NoError(t, err)
	require.True(t, execution.Observation.Sampled)
	require.True(t, execution.Sampled)
	require.Equal(t, int64(2), execution.Observation.SampleRows)
}

func TestExecuteStandardCodeRuleSupportsPercentThresholds(t *testing.T) {
	t.Parallel()

	config := standardCodeRuleConfig("name", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE, 100)
	config.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
	config.GetStandardCode().FailThreshold = protoFloat(10)
	execution, _, err := runStandardCodeExecution(t, config, &db.QueryResult{
		Columns: []string{"id", "name"},
		Rows: [][]any{
			{int64(1), "91350100M000100Y43"}, {int64(2), "broken"},
			{int64(3), "91330100799655058B"}, {int64(4), "91440300708461136T"},
		},
	}, ExecOptions{})
	require.NoError(t, err)

	state := Evaluate(execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE, config), execution.Observation, time.Now())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, state.Status, "1 of 4 sampled values is 25%%, above the 10%% bound")
	require.Equal(t, storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN, state.Thresholds.GetComparison())
}

// The failed-row sample query is the in-memory sample, so the executor must not
// issue the second statement other rule types use.
func TestExecuteStandardCodeRuleNeedsNoSampleStatement(t *testing.T) {
	t.Parallel()

	config := standardCodeRuleConfig("name", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_INDUSTRY_CLASSIFICATION, 10)
	execution, queryer, err := runStandardCodeExecution(t, config, &db.QueryResult{
		Columns: []string{"id", "name"},
		Rows:    [][]any{{int64(1), "C39"}, {int64(2), "Z01"}},
	}, ExecOptions{StoreSamples: true, SampleLimit: 1})
	require.NoError(t, err)
	require.Len(t, queryer.queries, 1)
	require.NotNil(t, execution.Samples)
	require.Equal(t, "Z01", sampleColumnValue(execution.Samples.GetSamples()[0], "name"))
}

// A stored rule with an arm but no standard is a defect, not a silent all-fail
// verdict.
func TestExecuteStandardCodeRuleRejectsAnUnspecifiedStandard(t *testing.T) {
	t.Parallel()

	config := standardCodeRuleConfig("name", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNSPECIFIED, 10)
	_, _, err := runStandardCodeExecution(t, config, &db.QueryResult{Columns: []string{"id", "name"}}, ExecOptions{})
	require.Error(t, err)
}

// An unknown non-zero enum is a stored-data defect too: the executor must refuse
// it instead of counting every value as a violation.
func TestExecuteStandardCodeRuleRejectsAnUnknownStandard(t *testing.T) {
	t.Parallel()

	config := standardCodeRuleConfig("name", storepb.QualityStandardCode(6), 10)
	_, _, err := runStandardCodeExecution(t, config, &db.QueryResult{Columns: []string{"id", "name"}}, ExecOptions{})
	require.Error(t, err)
}

// The 1 MiB byte cap can cut a sample short before sample_rows. That partial read
// is still a sample: calling it a complete scan would overstate the verdict, and
// raising sample_rows could never fill it.
func TestExecuteStandardCodeRuleMarksAByteTruncatedSample(t *testing.T) {
	t.Parallel()

	config := standardCodeRuleConfig("name", storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ADMINISTRATIVE_DIVISION, 100)
	execution, _, err := runStandardCodeExecution(t, config, &db.QueryResult{
		Columns:   []string{"id", "name"},
		Rows:      [][]any{{int64(1), "110101"}},
		Truncated: true,
	}, ExecOptions{})
	require.NoError(t, err)
	require.True(t, execution.Sampled)
	require.True(t, execution.Observation.Sampled)
}
