package dataquality

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// recommendTestNow is the fixed clock the recommendation tests run at.
var recommendTestNow = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// recommendTestRows is the row count the test profiles report.
const recommendTestRows = 1000

// recommendTestTarget builds a table target with the given columns.
func recommendTestTarget(columns ...ColumnInfo) ResolvedTarget {
	return ResolvedTarget{
		Target:  Target{GUID: "inst;db;;orders", MetaType: storepb.MetaType_TABLE},
		Engine:  storepb.Engine_MYSQL,
		Table:   "orders",
		Columns: columns,
	}
}

// recommendTestProfile builds one column profile for a table of recommendTestRows rows.
func recommendTestProfile(nullCount, distinct int64) *storepb.ColumnProfile {
	return &storepb.ColumnProfile{TotalRows: recommendTestRows, NullCount: nullCount, DistinctCount: distinct}
}

// recommendTestInput wraps a target and its profiles into the pass input.
func recommendTestInput(target ResolvedTarget, profiles map[string]*storepb.ColumnProfile) RecommendInput {
	columns := make([]string, 0, len(target.Columns))
	for _, column := range target.Columns {
		columns = append(columns, column.Name)
	}
	return RecommendInput{Target: target, Profiles: profiles, Columns: columns, Now: recommendTestNow}
}

// recommendChecksForTest runs the pass and returns its candidates. The skipped-column
// summary has its own test.
func recommendChecksForTest(t *testing.T, input RecommendInput) []Recommendation {
	t.Helper()
	return RecommendChecks(input).Recommendations
}

// recommendFind returns the candidate of one rule type, or nil.
func recommendFind(recommendations []Recommendation, ruleType storepb.QualityRuleType) *Recommendation {
	for index := range recommendations {
		if recommendations[index].RuleType == ruleType {
			return &recommendations[index]
		}
	}
	return nil
}

func TestRecommendChecksRowCountAndFreshness(t *testing.T) {
	t.Parallel()

	target := recommendTestTarget(
		ColumnInfo{Name: "id", Type: "bigint", Kind: ColumnKindNumeric},
		ColumnInfo{Name: "created_at", Type: "datetime", Kind: ColumnKindTemporal},
	)
	profiles := map[string]*storepb.ColumnProfile{
		"id":         recommendTestProfile(0, 1000),
		"created_at": {TotalRows: 1000, DistinctCount: 900, MinValue: "2026-10-01T00:00:00Z", MaxValue: "2026-11-01T10:00:00Z"},
	}
	recommendations := recommendChecksForTest(t, recommendTestInput(target, profiles))

	rowCount := recommendFind(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT)
	require.NotNil(t, rowCount)
	require.Equal(t, int64(500), rowCount.Config.GetRowCount().GetMinRows())
	require.Equal(t, "orders: row count", rowCount.Title)

	// The newest value is two hours old, so the bound is twice that (four hours),
	// rounded up to the next readable unit (six hours).
	freshness := recommendFind(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS)
	require.NotNil(t, freshness)
	require.Equal(t, "created_at", freshness.Config.GetFreshness().GetTimestampColumn())
	require.Equal(t, int32((6 * time.Hour).Seconds()), freshness.Config.GetFreshness().GetMaxLagSeconds())
}

func TestRecommendChecksFreshnessSkipsArchivedTable(t *testing.T) {
	t.Parallel()

	target := recommendTestTarget(
		ColumnInfo{Name: "event_time", Type: "timestamp", Kind: ColumnKindTemporal},
	)
	profiles := map[string]*storepb.ColumnProfile{
		"event_time": {TotalRows: 1000, MinValue: "2020-01-01T00:00:00Z", MaxValue: "2020-06-01T00:00:00Z"},
	}
	recommendations := recommendChecksForTest(t, recommendTestInput(target, profiles))
	require.Nil(t, recommendFind(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS))
}

func TestRecommendChecksNullRate(t *testing.T) {
	t.Parallel()

	target := recommendTestTarget(
		ColumnInfo{Name: "email", Type: "varchar(255)", Kind: ColumnKindString, Nullable: true},
		ColumnInfo{Name: "id", Type: "bigint", Kind: ColumnKindNumeric},
		ColumnInfo{Name: "sparse", Type: "varchar(255)", Kind: ColumnKindString, Nullable: true},
	)
	profiles := map[string]*storepb.ColumnProfile{
		"email":  recommendTestProfile(5, 900),
		"id":     recommendTestProfile(0, 1000),
		"sparse": recommendTestProfile(800, 50),
	}
	recommendations := recommendChecksForTest(t, recommendTestInput(target, profiles))

	nullRate := recommendFind(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT)
	require.NotNil(t, nullRate)
	require.Equal(t, "email", nullRate.Config.GetNullPercent().GetColumn())
	require.Equal(t, 0.75, nullRate.Config.GetNullPercent().GetMaxNullPct())
	require.Contains(t, nullRate.Rationale, "0.50%")
	// The NOT NULL key column cannot hold a NULL, and the mostly-NULL column is
	// skipped rather than monitored: exactly one null-rate candidate remains.
	require.Equal(t, 1, recommendCount(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT))
}

func TestRecommendChecksAcceptedValues(t *testing.T) {
	t.Parallel()

	target := recommendTestTarget(
		ColumnInfo{Name: "status", Type: "varchar(16)", Kind: ColumnKindString, Nullable: true},
		ColumnInfo{Name: "currency", Type: "varchar(3)", Kind: ColumnKindString, Nullable: true},
	)
	profiles := map[string]*storepb.ColumnProfile{
		"status": {
			TotalRows: 1000, NullCount: 10, DistinctCount: 3, MaxLength: proto.Int64(7),
			TopValues: []*storepb.ProfileValue{
				{Value: "cancelled", Count: 300}, {Value: "done", Count: 400}, {Value: "open", Count: 290},
			},
		},
		// The sample enumerated fewer values than the exact distinct count: the set is
		// incomplete and must not be proposed.
		"currency": {
			TotalRows: 1000, DistinctCount: 4,
			TopValues: []*storepb.ProfileValue{{Value: "CNY", Count: 900}, {Value: "USD", Count: 50}},
		},
	}
	recommendations := recommendChecksForTest(t, recommendTestInput(target, profiles))

	accepted := recommendFind(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES)
	require.NotNil(t, accepted)
	require.Equal(t, "status", accepted.Config.GetAcceptedValues().GetColumn())
	require.Equal(t, []string{"cancelled", "done", "open"}, accepted.Config.GetAcceptedValues().GetValues())
	require.True(t, accepted.Config.GetAcceptedValues().GetAllowNull())
	require.Equal(t, float64(0), accepted.Config.GetAcceptedValues().GetFailThreshold())
}

func TestRecommendChecksCandidateKey(t *testing.T) {
	t.Parallel()

	target := recommendTestTarget(
		ColumnInfo{Name: "id", Type: "bigint", Kind: ColumnKindNumeric},
		ColumnInfo{Name: "order_no", Type: "varchar(32)", Kind: ColumnKindString},
		ColumnInfo{Name: "labeled_id", Type: "varchar(32)", Kind: ColumnKindString},
	)
	target.KeyColumns = []string{"id"}
	profiles := map[string]*storepb.ColumnProfile{
		"id":         recommendTestProfile(0, 1000),
		"order_no":   recommendTestProfile(0, 1000),
		"labeled_id": recommendTestProfile(0, 900),
	}
	input := recommendTestInput(target, profiles)
	input.Labels = map[string]storepb.QualitySemanticType{
		"labeled_id": storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_ID,
	}
	recommendations := recommendChecksForTest(t, input)

	unique := recommendFind(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE)
	require.NotNil(t, unique)
	require.Equal(t, "order_no", unique.Config.GetUnique().GetColumn())
	require.Equal(t, float64(0), unique.Config.GetUnique().GetFailThreshold())
}

func TestRecommendChecksRange(t *testing.T) {
	t.Parallel()

	target := recommendTestTarget(
		ColumnInfo{Name: "amount", Type: "decimal(10,2)", Kind: ColumnKindNumeric},
		ColumnInfo{Name: "constant", Type: "int", Kind: ColumnKindNumeric},
	)
	profiles := map[string]*storepb.ColumnProfile{
		"amount": {
			TotalRows: 1000, DistinctCount: 400,
			MinValue: "10", MaxValue: "20",
		},
		"constant": {
			TotalRows: 1000, DistinctCount: 1,
			MinValue: "7", MaxValue: "7",
		},
	}
	recommendations := recommendChecksForTest(t, recommendTestInput(target, profiles))

	rangeCheck := recommendFind(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE)
	require.NotNil(t, rangeCheck)
	require.Equal(t, "9", rangeCheck.Config.GetRange().GetMin())
	require.Equal(t, "21", rangeCheck.Config.GetRange().GetMax())
	require.True(t, rangeCheck.Config.GetRange().GetInclusive())
}

func TestRecommendChecksStringLength(t *testing.T) {
	t.Parallel()

	target := recommendTestTarget(
		ColumnInfo{Name: "note", Type: "varchar(255)", Kind: ColumnKindString},
	)
	profiles := map[string]*storepb.ColumnProfile{
		"note": {TotalRows: 1000, DistinctCount: 500, MaxLength: proto.Int64(10)},
	}
	input := recommendTestInput(target, profiles)
	input.Labels = map[string]storepb.QualitySemanticType{
		"note": storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_FREE_TEXT,
	}
	recommendations := recommendChecksForTest(t, input)

	length := recommendFind(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH)
	require.NotNil(t, length)
	require.Equal(t, int64(12), length.Config.GetStringLength().GetMaxLen())
}

func TestRecommendChecksReferentialIntegrity(t *testing.T) {
	t.Parallel()

	target := recommendTestTarget(
		ColumnInfo{Name: "customer_id", Type: "bigint", Kind: ColumnKindNumeric},
	)
	metadata := &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_TableMetadata{TableMetadata: &storepb.TableMetadata{
			ForeignKeys: []*storepb.ForeignKeyMetadata{
				{Columns: []string{"customer_id"}, ReferencedTable: "customers", ReferencedColumns: []string{"id"}},
				// A composite key asserts nothing per column and is skipped.
				{Columns: []string{"a", "b"}, ReferencedTable: "other", ReferencedColumns: []string{"x", "y"}},
			},
		}},
	}
	input := recommendTestInput(target, map[string]*storepb.ColumnProfile{
		"customer_id": recommendTestProfile(0, 300),
	})
	input.Metadata = metadata
	recommendations := recommendChecksForTest(t, input)

	referential := recommendFind(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY)
	require.NotNil(t, referential)
	require.Equal(t, "customer_id", referential.Config.GetReferentialIntegrity().GetChildColumn())
	require.Equal(t, "customers", referential.Config.GetReferentialIntegrity().GetParentTable())
	require.Equal(t, "id", referential.Config.GetReferentialIntegrity().GetParentColumn())
	require.False(t, referential.Config.GetReferentialIntegrity().GetAllowNull())
}

func TestRecommendChecksSkipsCrossSchemaForeignKey(t *testing.T) {
	t.Parallel()

	target := ResolvedTarget{
		Target:  Target{GUID: "inst;db;public;orders", MetaType: storepb.MetaType_TABLE},
		Engine:  storepb.Engine_POSTGRES,
		Schema:  "public",
		Table:   "orders",
		Columns: []ColumnInfo{{Name: "customer_id", Type: "bigint", Kind: ColumnKindNumeric}},
	}
	metadata := &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_TableMetadata{TableMetadata: &storepb.TableMetadata{
			ForeignKeys: []*storepb.ForeignKeyMetadata{{
				Columns: []string{"customer_id"}, ReferencedSchema: "sales",
				ReferencedTable: "customers", ReferencedColumns: []string{"id"},
			}},
		}},
	}
	input := RecommendInput{
		Target: target, Metadata: metadata, Columns: []string{"customer_id"},
		Profiles: map[string]*storepb.ColumnProfile{"customer_id": recommendTestProfile(0, 300)},
		Now:      recommendTestNow,
	}
	recommendations := recommendChecksForTest(t, input)
	require.Nil(t, recommendFind(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY))
}

func TestRecommendChecksPerTypeBudgetAndTotalCap(t *testing.T) {
	t.Parallel()

	columns := []ColumnInfo{
		{Name: "amount", Type: "decimal(10,2)", Kind: ColumnKindNumeric},
		{Name: "ts", Type: "timestamp", Kind: ColumnKindTemporal},
	}
	profiles := map[string]*storepb.ColumnProfile{
		"amount": {TotalRows: 1000, DistinctCount: 400, MinValue: "1", MaxValue: "2"},
		"ts":     {TotalRows: 1000, MinValue: "2026-10-31T00:00:00Z", MaxValue: "2026-11-01T11:00:00Z"},
	}
	labels := make(map[string]storepb.QualitySemanticType)
	// Enough candidates of every kind to exceed the total cap.
	for index := 0; index < 8; index++ {
		for _, group := range []struct {
			prefix string
			kind   ColumnKind
			build  func(name string) *storepb.ColumnProfile
		}{
			{"optional", ColumnKindString, func(string) *storepb.ColumnProfile {
				return recommendTestProfile(3, 40)
			}},
			{"enum", ColumnKindString, func(string) *storepb.ColumnProfile {
				return &storepb.ColumnProfile{TotalRows: 1000, DistinctCount: 2, MaxLength: proto.Int64(4),
					TopValues: []*storepb.ProfileValue{{Value: "a", Count: 500}, {Value: "b", Count: 500}}}
			}},
			{"numeric", ColumnKindNumeric, func(string) *storepb.ColumnProfile {
				return &storepb.ColumnProfile{TotalRows: 1000, DistinctCount: 400, MinValue: "1", MaxValue: "100"}
			}},
			{"text", ColumnKindString, func(string) *storepb.ColumnProfile {
				return &storepb.ColumnProfile{TotalRows: 1000, DistinctCount: 900, MaxLength: proto.Int64(10)}
			}},
		} {
			name := group.prefix + "_" + string(rune('a'+index))
			columns = append(columns, ColumnInfo{Name: name, Type: "varchar(16)", Kind: group.kind, Nullable: true})
			profiles[name] = group.build(name)
			if group.prefix == "text" {
				labels[name] = storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_FREE_TEXT
			}
		}
	}
	target := recommendTestTarget(columns...)
	input := recommendTestInput(target, profiles)
	input.Labels = labels
	recommendations := recommendChecksForTest(t, input)

	require.Len(t, recommendations, recommendMaxCandidates)
	require.NotNil(t, recommendFind(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT))
	require.NotNil(t, recommendFind(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS))
	// The per-type budget is what keeps a wide table from turning into a wall of one
	// kind of check.
	require.Equal(t, 3, recommendCount(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT))
	require.Equal(t, 3, recommendCount(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES))
	require.Equal(t, 2, recommendCount(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE))
	require.Equal(t, 2, recommendCount(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH))
}

// recommendCount counts the candidates of one rule type.
func recommendCount(recommendations []Recommendation, ruleType storepb.QualityRuleType) int {
	count := 0
	for _, recommendation := range recommendations {
		if recommendation.RuleType == ruleType {
			count++
		}
	}
	return count
}

func TestRecommendChecksIsDeterministic(t *testing.T) {
	t.Parallel()

	target := recommendTestTarget(
		ColumnInfo{Name: "id", Type: "bigint", Kind: ColumnKindNumeric},
		ColumnInfo{Name: "status", Type: "varchar(16)", Kind: ColumnKindString},
		ColumnInfo{Name: "updated_at", Type: "timestamp", Kind: ColumnKindTemporal},
	)
	profiles := map[string]*storepb.ColumnProfile{
		"id":         recommendTestProfile(0, 1000),
		"status":     {TotalRows: 1000, DistinctCount: 2, TopValues: []*storepb.ProfileValue{{Value: "a", Count: 500}, {Value: "b", Count: 500}}},
		"updated_at": {TotalRows: 1000, MinValue: "2026-11-01T00:00:00Z", MaxValue: "2026-11-01T11:00:00Z"},
	}
	first := recommendChecksForTest(t, recommendTestInput(target, profiles))
	second := recommendChecksForTest(t, recommendTestInput(target, profiles))
	require.Len(t, first, len(second))
	for index := range first {
		require.Equal(t, first[index].Signature, second[index].Signature)
		require.Equal(t, first[index].Title, second[index].Title)
	}
}

func TestRecommendChecksLocalizesTitles(t *testing.T) {
	t.Parallel()

	target := recommendTestTarget(
		ColumnInfo{Name: "id", Type: "bigint", Kind: ColumnKindNumeric},
	)
	profiles := map[string]*storepb.ColumnProfile{"id": recommendTestProfile(0, 1000)}
	input := recommendTestInput(target, profiles)
	input.Language = "zh-CN"
	recommendations := recommendChecksForTest(t, input)
	rowCount := recommendFind(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT)
	require.NotNil(t, rowCount)
	require.Equal(t, "orders: 行数", rowCount.Title)
	require.Contains(t, rowCount.Rationale, "观测到")

	input.Language = ""
	recommendations = recommendChecksForTest(t, input)
	rowCount = recommendFind(recommendations, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT)
	require.NotNil(t, rowCount)
	require.Equal(t, "orders: row count", rowCount.Title)
}

// TestRecommendChecksSurviveTheRulePipeline pins the pass against the same gate a
// saved rule goes through: every candidate it proposes must compile and validate on
// the target it was derived from, on every engine that supports it.
func TestRecommendChecksSurviveTheRulePipeline(t *testing.T) {
	t.Parallel()

	target := recommendTestTarget(
		ColumnInfo{Name: "id", Type: "bigint", Kind: ColumnKindNumeric},
		ColumnInfo{Name: "amount", Type: "decimal(10,2)", Kind: ColumnKindNumeric},
		ColumnInfo{Name: "status", Type: "varchar(16)", Kind: ColumnKindString, Nullable: true},
		ColumnInfo{Name: "note", Type: "varchar(255)", Kind: ColumnKindString},
		ColumnInfo{Name: "updated_at", Type: "timestamp", Kind: ColumnKindTemporal},
	)
	target.KeyColumns = []string{"id"}
	profiles := map[string]*storepb.ColumnProfile{
		"id":     recommendTestProfile(0, 1000),
		"amount": {TotalRows: 1000, DistinctCount: 400, MinValue: "1", MaxValue: "100"},
		"status": {
			TotalRows: 1000, NullCount: 10, DistinctCount: 2, MaxLength: proto.Int64(6),
			TopValues: []*storepb.ProfileValue{{Value: "done", Count: 900}, {Value: "open", Count: 90}},
		},
		"note":       {TotalRows: 1000, DistinctCount: 900, MaxLength: proto.Int64(40)},
		"updated_at": {TotalRows: 1000, MinValue: "2026-11-01T00:00:00Z", MaxValue: "2026-11-01T11:00:00Z"},
	}
	recommendations := recommendChecksForTest(t, recommendTestInput(target, profiles))
	require.NotEmpty(t, recommendations)
	for _, recommendation := range recommendations {
		require.NoErrorf(t, ValidateConfig(target, recommendation.RuleType, recommendation.Config),
			"rule type %s", recommendation.RuleType)
		require.Truef(t, RuleTypeForTarget(target.MetaType, recommendation.RuleType),
			"rule type %s", recommendation.RuleType)
	}
}

// TestRecommendSelectionHasText ensures the selection table and the localized names
// stay in step: a rule type the pass can propose must have a title in both languages.
func TestRecommendSelectionHasText(t *testing.T) {
	t.Parallel()

	for _, entry := range recommendSelection {
		require.Containsf(t, recommendRuleTypeNamesEN, entry.ruleType, "missing English name for %s", entry.ruleType)
		require.Containsf(t, recommendRuleTypeNamesZH, entry.ruleType, "missing Chinese name for %s", entry.ruleType)
		require.Containsf(t, recommendRationaleEN, entry.ruleType, "missing English rationale for %s", entry.ruleType)
		require.Containsf(t, recommendRationaleZH, entry.ruleType, "missing Chinese rationale for %s", entry.ruleType)
	}
}

// TestRecommendChecksSkipsUnprofiledAndUnknownColumns keeps the pass from proposing
// rules the engine gates would refuse: a column with no profile contributes no
// statistical candidates, and an unclassifiable type contributes nothing.
func TestRecommendChecksSkipsUnprofiledAndUnknownColumns(t *testing.T) {
	t.Parallel()

	target := recommendTestTarget(
		ColumnInfo{Name: "mystery", Type: "mystery_type", Kind: ColumnKindUnknown},
		ColumnInfo{Name: "status", Type: "varchar(16)", Kind: ColumnKindString},
	)
	profiles := map[string]*storepb.ColumnProfile{
		"mystery": recommendTestProfile(0, 1000),
	}
	recommendations := recommendChecksForTest(t, recommendTestInput(target, profiles))
	require.Empty(t, recommendations)
}

// TestRecommendChecksSkippedColumns pins what the summary calls skipped: a column no
// candidate was derived for, not one whose candidate lost the per-type budget.
func TestRecommendChecksSkippedColumns(t *testing.T) {
	t.Parallel()

	columns := []ColumnInfo{
		{Name: "id", Type: "bigint", Kind: ColumnKindNumeric},
		{Name: "note", Type: "varchar(255)", Kind: ColumnKindString},
		{Name: "email", Type: "varchar(128)", Kind: ColumnKindString, Nullable: true},
	}
	target := recommendTestTarget(columns...)
	target.KeyColumns = []string{"id"}
	profiles := map[string]*storepb.ColumnProfile{
		"id":    recommendTestProfile(0, 1000),
		"note":  {TotalRows: 1000, DistinctCount: 900, MaxLength: proto.Int64(40)},
		"email": {TotalRows: 1000, NullCount: 6, DistinctCount: 900},
	}
	result := RecommendChecks(recommendTestInput(target, profiles))
	require.Equal(t, []string{"id", "note"}, result.SkippedColumns)
	require.NotEmpty(t, result.Recommendations, "the email column still yields a check")
}
