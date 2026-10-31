package v1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// TestQualityLabelColumnBatches pins the batch split: at most 20 columns per model
// call, at most 3 calls, and the per-request cap fits in the batch capacity.
func TestQualityLabelColumnBatches(t *testing.T) {
	t.Parallel()

	columns := make([]string, 0, 61)
	for index := 0; index < 61; index++ {
		columns = append(columns, "c"+string(rune('a'+index%26)))
	}

	require.Equal(t, [][]string{columns[0:20], columns[20:40], columns[40:60]},
		qualityLabelColumnBatches(columns, qualityLabelBatchColumns, qualityLabelMaxBatches))
	require.Len(t, qualityLabelColumnBatches(columns, qualityLabelBatchColumns, qualityLabelMaxBatches), qualityLabelMaxBatches)
	require.Equal(t, [][]string{columns[0:20], columns[20:40], columns[40:45]},
		qualityLabelColumnBatches(columns[:45], qualityLabelBatchColumns, qualityLabelMaxBatches))
	require.Nil(t, qualityLabelColumnBatches(nil, qualityLabelBatchColumns, qualityLabelMaxBatches))
	require.Nil(t, qualityLabelColumnBatches(columns, 0, qualityLabelMaxBatches))
	require.Nil(t, qualityLabelColumnBatches(columns, qualityLabelBatchColumns, 0))

	// The per-request column cap must fit in the batches one request may make;
	// otherwise a valid request would be silently truncated.
	require.GreaterOrEqual(t, qualityLabelBatchColumns*qualityLabelMaxBatches, dataquality.MaxDraftColumns)
}

// TestQualityLabelStaleProfileColumns pins the freshness rule: a missing, empty
// or older-than-seven-days profile cannot be used to label a column.
func TestQualityLabelStaleProfileColumns(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	profiles := map[string]*store.ColumnProfileMessage{
		"fresh":       {ColumnName: "fresh", Profile: &storepb.ColumnProfile{}, ProfiledAt: now.Add(-time.Hour)},
		"exactly-old": {ColumnName: "exactly-old", Profile: &storepb.ColumnProfile{}, ProfiledAt: now.Add(-qualityLabelProfileMaxAge)},
		"stale":       {ColumnName: "stale", Profile: &storepb.ColumnProfile{}, ProfiledAt: now.Add(-8 * 24 * time.Hour)},
		"empty":       {ColumnName: "empty", Profile: nil, ProfiledAt: now},
		"nil-message": nil,
	}
	columns := []string{"fresh", "exactly-old", "stale", "empty", "nil-message", "missing"}
	require.Equal(t, []string{"stale", "empty", "nil-message", "missing"},
		qualityLabelStaleProfileColumns(profiles, columns, now))

	require.Empty(t, qualityLabelStaleProfileColumns(profiles, []string{"fresh", "exactly-old"}, now))
	require.Empty(t, qualityLabelStaleProfileColumns(nil, nil, now))
}

// TestQualityLabelLabelMap pins the prompt's existing-label map: an unset label is
// not a label and a nil row is skipped.
func TestQualityLabelLabelMap(t *testing.T) {
	t.Parallel()

	labels := []*store.ColumnSemanticLabelMessage{
		{ColumnName: "email", Label: storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL},
		{ColumnName: "unset", Label: storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_UNSPECIFIED},
		nil,
	}
	require.Equal(t, map[string]storepb.QualitySemanticType{
		"email": storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL,
	}, qualityLabelLabelMap(labels))
	require.Nil(t, qualityLabelLabelMap(nil))
}

// TestQualityLabelDraftRuleType pins the first filter stage a label-generated rule
// draft passes: its config's oneof arm is the rule type the handler validates
// against the target. The resolver knows every arm this build carries, so a stored
// draft is classified by its config even if a future producer names a v1.1 type. A
// draft with no recognized arm is dropped before validation, which keeps an
// unsupported config out of the review queue.
func TestQualityLabelDraftRuleType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config *storepb.QualityRuleConfig
		want   storepb.QualityRuleType
	}{
		{
			name:   "row count",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{}}},
			want:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		},
		{
			name:   "null percent",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{NullPercent: &storepb.NullPercentConfig{}}},
			want:   storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		},
		{
			name:   "unique",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{}}},
			want:   storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		},
		{
			name:   "accepted values",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_AcceptedValues{AcceptedValues: &storepb.AcceptedValuesConfig{}}},
			want:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		},
		{
			name:   "range",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Range{Range: &storepb.RangeConfig{}}},
			want:   storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		},
		{
			name:   "regex",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Regex{Regex: &storepb.RegexConfig{}}},
			want:   storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		},
		{
			name:   "string length",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_StringLength{StringLength: &storepb.StringLengthConfig{}}},
			want:   storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		},
		{
			name:   "freshness",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Freshness{Freshness: &storepb.FreshnessConfig{}}},
			want:   storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
		},
		{
			name:   "custom SQL",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CustomSql{CustomSql: &storepb.CustomSQLConfig{}}},
			want:   storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
		},
		{
			name:   "row count change",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCountChange{RowCountChange: &storepb.RowCountChangeConfig{}}},
			want:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		},
		{
			name:   "unique combination",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_UniqueCombination{UniqueCombination: &storepb.UniqueCombinationConfig{}}},
			want:   storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
		},
		{
			name:   "referential integrity",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_ReferentialIntegrity{ReferentialIntegrity: &storepb.ReferentialIntegrityConfig{}}},
			want:   storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
		},
		{
			name:   "cross column",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossColumn{CrossColumn: &storepb.CrossColumnConfig{}}},
			want:   storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
		},
		{
			name:   "CEL expression",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CelExpression{CelExpression: &storepb.CelExpressionConfig{}}},
			want:   storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION,
		},
		{
			name:   "schema drift",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_SchemaDrift{SchemaDrift: &storepb.SchemaDriftConfig{}}},
			want:   storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT,
		},
		{
			name:   "group check",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_GroupCheck{GroupCheck: &storepb.GroupCheckConfig{}}},
			want:   storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK,
		},
		{
			name:   "aggregate",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Aggregate{Aggregate: &storepb.AggregateConfig{}}},
			want:   storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
		},
		{
			name:   "cross table",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossTable{CrossTable: &storepb.CrossTableConfig{}}},
			want:   storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ruleType, ok := qualityLabelDraftRuleType(&storepb.QualityRuleDraftConfig{Config: test.config})
			require.True(t, ok)
			require.Equal(t, test.want, ruleType)
		})
	}

	// A draft with no config, or a config with no oneof arm, cannot be validated
	// and is filtered before it reaches the queue.
	for _, draft := range []*storepb.QualityRuleDraftConfig{
		nil,
		{},
		{Config: &storepb.QualityRuleConfig{}},
	} {
		ruleType, ok := qualityLabelDraftRuleType(draft)
		require.False(t, ok)
		require.Equal(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED, ruleType)
	}
}

// labelSuggestionFixture builds one queued label suggestion for the accept path.
func labelSuggestionFixture() *store.QualitySuggestionMessage {
	return &store.QualitySuggestionMessage{
		ResourceID: "suggestion-1",
		Kind:       storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL,
		Source:     storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_PROFILE,
		TargetGUID: "instances/ins1;shop;public;orders",
		TargetType: storepb.MetaType_TABLE,
		Payload: &storepb.QualitySuggestionPayload{Payload: &storepb.QualitySuggestionPayload_Label{
			Label: &storepb.SemanticLabelSuggestion{
				Column:     "email",
				Label:      storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL,
				Confidence: 0.9,
				Evidence: &storepb.SemanticLabelEvidence{
					Columns:   []string{"email"},
					Metrics:   []string{"null_rate=0.2000", "distinct=40"},
					Rationale: "looks like an email address",
				},
				SuggestedRuleTypes: []storepb.QualityRuleType{
					storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
					storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
				},
			},
		}},
	}
}

// TestLabelSuggestionToLabelMessage pins the accept branch: accepting a label
// suggestion writes an ACCEPTED, LLM-provenance column_semantic_label row carrying
// the model's confidence and evidence.
func TestLabelSuggestionToLabelMessage(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	acceptedBy := int32(7)
	message, err := labelSuggestionToLabelMessage(labelSuggestionFixture(), &acceptedBy, at)
	require.NoError(t, err)
	require.Equal(t, "instances/ins1;shop;public;orders", message.TableGUID)
	require.Equal(t, storepb.MetaType_TABLE, message.TableType)
	require.Equal(t, "email", message.ColumnName)
	require.Equal(t, storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL, message.Label)
	require.Equal(t, storepb.QualityLabelSource_QUALITY_LABEL_SOURCE_LLM, message.Source)
	require.Equal(t, storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_ACCEPTED, message.Status)
	require.Equal(t, 0.9, message.Confidence)
	require.True(t, proto.Equal(labelSuggestionFixture().Payload.GetLabel().GetEvidence(), message.Evidence))
	require.Equal(t, &acceptedBy, message.AcceptedBy)
	require.Equal(t, at, *message.AcceptedAt)
}

// TestLabelSuggestionToLabelMessageRejectsBadPayloads pins the guard: only a
// complete label suggestion can assert a label.
func TestLabelSuggestionToLabelMessageRejectsBadPayloads(t *testing.T) {
	t.Parallel()

	acceptedBy := int32(7)
	at := time.Now().UTC()

	tests := []struct {
		name       string
		suggestion func() *store.QualitySuggestionMessage
	}{
		{
			name:       "nil suggestion",
			suggestion: func() *store.QualitySuggestionMessage { return nil },
		},
		{
			name: "rule draft kind",
			suggestion: func() *store.QualitySuggestionMessage {
				suggestion := labelSuggestionFixture()
				suggestion.Kind = storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT
				return suggestion
			},
		},
		{
			name: "nil payload",
			suggestion: func() *store.QualitySuggestionMessage {
				suggestion := labelSuggestionFixture()
				suggestion.Payload = nil
				return suggestion
			},
		},
		{
			name: "empty payload arm",
			suggestion: func() *store.QualitySuggestionMessage {
				suggestion := labelSuggestionFixture()
				suggestion.Payload = &storepb.QualitySuggestionPayload{}
				return suggestion
			},
		},
		{
			name: "empty column",
			suggestion: func() *store.QualitySuggestionMessage {
				suggestion := labelSuggestionFixture()
				suggestion.Payload.GetLabel().Column = " "
				return suggestion
			},
		},
		{
			name: "unset label",
			suggestion: func() *store.QualitySuggestionMessage {
				suggestion := labelSuggestionFixture()
				suggestion.Payload.GetLabel().Label = storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_UNSPECIFIED
				return suggestion
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := labelSuggestionToLabelMessage(test.suggestion(), &acceptedBy, at)
			require.Error(t, err)
			require.Equal(t, common.Invalid, common.ErrorCode(err))
		})
	}
}

// The label pass decides what to analyze and what to close before spending quota.
// These cases pin the reconciliation rules: fact wins, pending is not re-analyzed,
// rejection keeps its column out, and force keeps everything in but closes nothing.
func TestPlanQualityLabelPass(t *testing.T) {
	t.Parallel()

	columns := []string{"id", "email", "phone", "country", "created_at"}
	labelSignature := dataquality.LabelSuggestionSignature
	states := map[string]store.QualitySuggestionSignatureState{
		labelSignature("email"):      {Pending: true, LastDecision: storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_UNSPECIFIED},
		labelSignature("phone"):      {LastDecision: storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED},
		labelSignature("country"):    {Pending: true, LastDecision: storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_UNSPECIFIED},
		labelSignature("created_at"): {LastDecision: storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED},
	}
	labels := map[string]storepb.QualitySemanticType{
		"country": storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_COUNTRY_CODE,
	}

	t.Run("facts, pending and decided columns are skipped", func(t *testing.T) {
		t.Parallel()

		plan := planQualityLabelPass(columns, labels, states, false)
		require.Equal(t, []string{"id", "created_at"}, plan.Columns)
		require.Equal(t, []string{labelSignature("country")}, plan.SupersedeSignatures,
			"only a pending proposal whose fact now exists is closed")
		require.Equal(t, int32(1), plan.Summary.SkippedExistingFact)
		require.Equal(t, int32(1), plan.Summary.SkippedPending)
		require.Equal(t, int32(1), plan.Summary.SkippedDecided)
		require.Equal(t, []string{"email", "phone", "country"}, plan.Summary.SkippedColumns,
			"skipped columns stay in request order")
	})

	t.Run("force keeps every column and closes nothing", func(t *testing.T) {
		t.Parallel()

		plan := planQualityLabelPass(columns, labels, states, true)
		require.Equal(t, columns, plan.Columns)
		require.Empty(t, plan.SupersedeSignatures)
		require.Zero(t, plan.Summary.GetSkippedExistingFact())
		require.Zero(t, plan.Summary.GetSkippedPending())
		require.Zero(t, plan.Summary.GetSkippedDecided())
		require.Empty(t, plan.Summary.GetSkippedColumns())
	})

	t.Run("nothing to analyze yields an empty plan but the skip counts", func(t *testing.T) {
		t.Parallel()

		plan := planQualityLabelPass([]string{"country"}, labels, states, false)
		require.Empty(t, plan.Columns)
		require.Equal(t, []string{labelSignature("country")}, plan.SupersedeSignatures)
		require.Equal(t, int32(1), plan.Summary.GetSkippedExistingFact())
	})

	t.Run("a decided proposal without a fact does not suppress", func(t *testing.T) {
		t.Parallel()

		onlyAccepted := map[string]store.QualitySuggestionSignatureState{
			labelSignature("email"): {LastDecision: storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED},
		}
		plan := planQualityLabelPass([]string{"email"}, nil, onlyAccepted, false)
		require.Equal(t, []string{"email"}, plan.Columns,
			"an accepted label suggestion whose label was cleared may be proposed again")
	})
}
