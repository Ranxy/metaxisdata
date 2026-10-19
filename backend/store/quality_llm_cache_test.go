package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// dataQualityLLMCacheScannerStub stands in for one data_quality_llm_cache row.
type dataQualityLLMCacheScannerStub struct {
	cacheKey      string
	kind          int16
	targetGUID    string
	provider      string
	model         string
	language      string
	promptVersion string
	result        []byte
	createdAt     time.Time
}

func (s dataQualityLLMCacheScannerStub) Scan(dest ...any) error {
	*(dest[0].(*string)) = s.cacheKey
	*(dest[1].(*int16)) = s.kind
	*(dest[2].(*string)) = s.targetGUID
	*(dest[3].(*string)) = s.provider
	*(dest[4].(*string)) = s.model
	*(dest[5].(*string)) = s.language
	*(dest[6].(*string)) = s.promptVersion
	*(dest[7].(*[]byte)) = s.result
	*(dest[8].(*time.Time)) = s.createdAt
	return nil
}

func explanationResult() *storepb.DataQualityLLMResult {
	return &storepb.DataQualityLLMResult{
		Result: &storepb.DataQualityLLMResult_Explanation{
			Explanation: &storepb.ExplanationResult{
				Summary:    "the freshness check reads a stale replica",
				Confidence: 0.74,
				Hypotheses: []*storepb.ExplanationHypothesis{{
					Cause:    "replication lag",
					Evidence: []string{"last sync 40m ago"},
					NextStep: "check the replica lag",
				}},
			},
		},
		PromptVersion:    "dq.explain.v1",
		Provider:         "profiles/openai",
		Model:            "gpt-4",
		PromptTokens:     1200,
		CompletionTokens: 340,
		TotalTokens:      1540,
	}
}

// The kind numbers are persisted, so renumbering them would silently reinterpret
// every cached row; the constants are the contract.
func TestDataQualityLLMCacheKindValuesAreStable(t *testing.T) {
	t.Parallel()

	require.Equal(t, DataQualityLLMCacheKind(1), DataQualityLLMCacheKindRuleDraft)
	require.Equal(t, DataQualityLLMCacheKind(2), DataQualityLLMCacheKindExplanation)
	require.True(t, DataQualityLLMCacheKindRuleDraft.known())
	require.True(t, DataQualityLLMCacheKindExplanation.known())
	require.False(t, DataQualityLLMCacheKind(0).known())
	require.False(t, DataQualityLLMCacheKind(3).known())
}

func TestDataQualityLLMCacheKindForResult(t *testing.T) {
	t.Parallel()

	kind, ok := dataQualityLLMCacheKindForResult(&storepb.DataQualityLLMResult{
		Result: &storepb.DataQualityLLMResult_RuleDraft{RuleDraft: &storepb.RuleDraftResult{}},
	})
	require.True(t, ok)
	require.Equal(t, DataQualityLLMCacheKindRuleDraft, kind)

	kind, ok = dataQualityLLMCacheKindForResult(explanationResult())
	require.True(t, ok)
	require.Equal(t, DataQualityLLMCacheKindExplanation, kind)

	_, ok = dataQualityLLMCacheKindForResult(&storepb.DataQualityLLMResult{})
	require.False(t, ok)
}

// A cache entry must carry a key and a result whose oneof arm matches its kind:
// a reader that trusts the kind would otherwise unmarshal the other shape and
// render an empty analysis.
func TestValidateDataQualityLLMCacheRow(t *testing.T) {
	t.Parallel()

	valid := &DataQualityLLMCacheRow{
		CacheKey: "dq|target:g|model:gpt-4|lang:en-US|prompt:dq.explain.v1",
		Kind:     DataQualityLLMCacheKindExplanation,
		Result:   explanationResult(),
	}
	require.NoError(t, validateDataQualityLLMCacheRow(valid))
	require.Equal(t, common.Invalid, common.ErrorCode(validateDataQualityLLMCacheRow(nil)))

	broken := *valid
	broken.CacheKey = "  "
	require.Equal(t, common.Invalid, common.ErrorCode(validateDataQualityLLMCacheRow(&broken)))

	broken = *valid
	broken.Result = nil
	require.Equal(t, common.Invalid, common.ErrorCode(validateDataQualityLLMCacheRow(&broken)))

	// No oneof arm at all: there is nothing to cache.
	broken = *valid
	broken.Result = &storepb.DataQualityLLMResult{PromptVersion: "v1"}
	require.Equal(t, common.Invalid, common.ErrorCode(validateDataQualityLLMCacheRow(&broken)))

	// Kind and arm disagree.
	broken = *valid
	broken.Kind = DataQualityLLMCacheKindRuleDraft
	require.Equal(t, common.Invalid, common.ErrorCode(validateDataQualityLLMCacheRow(&broken)))

	// A kind this build does not know.
	broken = *valid
	broken.Kind = 9
	require.Equal(t, common.Invalid, common.ErrorCode(validateDataQualityLLMCacheRow(&broken)))
}

// A caller that only set the result gets the kind its arm implies, so the common
// write does not have to repeat what the payload already says.
func TestValidateDataQualityLLMCacheRowDerivesTheKind(t *testing.T) {
	t.Parallel()

	row := &DataQualityLLMCacheRow{CacheKey: "key", Result: explanationResult()}
	require.NoError(t, validateDataQualityLLMCacheRow(row))
	require.Equal(t, DataQualityLLMCacheKindExplanation, row.Kind)

	row = &DataQualityLLMCacheRow{
		CacheKey: "key",
		Result: &storepb.DataQualityLLMResult{
			Result: &storepb.DataQualityLLMResult_RuleDraft{RuleDraft: &storepb.RuleDraftResult{}},
		},
	}
	require.NoError(t, validateDataQualityLLMCacheRow(row))
	require.Equal(t, DataQualityLLMCacheKindRuleDraft, row.Kind)
}

// The provider, model and prompt version live in both the columns and the cached
// message. The column is the truth; a caller that set only one side still stores
// both, so the cache cannot disagree with the key it is stored under.
func TestNormalizeDataQualityLLMCacheRowKeepsColumnsAndResultInAgreement(t *testing.T) {
	t.Parallel()

	row := &DataQualityLLMCacheRow{
		CacheKey: "key",
		Kind:     DataQualityLLMCacheKindExplanation,
		Result:   explanationResult(),
	}
	normalizeDataQualityLLMCacheRow(row)
	require.Equal(t, "profiles/openai", row.Provider)
	require.Equal(t, "gpt-4", row.Model)
	require.Equal(t, "dq.explain.v1", row.PromptVersion)

	// The columns win when both are set: they are what the row is queried and
	// pruned by.
	row = &DataQualityLLMCacheRow{
		CacheKey:      "key",
		Provider:      "profiles/other",
		Model:         "gpt-4o",
		PromptVersion: "dq.explain.v2",
		Result:        explanationResult(),
	}
	normalizeDataQualityLLMCacheRow(row)
	require.Equal(t, "profiles/other", row.Result.GetProvider())
	require.Equal(t, "gpt-4o", row.Result.GetModel())
	require.Equal(t, "dq.explain.v2", row.Result.GetPromptVersion())

	// Nothing to normalize is not a panic.
	normalizeDataQualityLLMCacheRow(nil)
	normalizeDataQualityLLMCacheRow(&DataQualityLLMCacheRow{})
}

func TestScanDataQualityLLMCacheRoundTripsTheResult(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 5, 11, 0, 0, 0, time.UTC)
	stored := explanationResult()
	resultJSON, err := protojson.Marshal(stored)
	require.NoError(t, err)

	message, err := scanDataQualityLLMCache(dataQualityLLMCacheScannerStub{
		cacheKey:      "dq|target:g|model:gpt-4|lang:en-US|prompt:dq.explain.v1",
		kind:          int16(DataQualityLLMCacheKindExplanation),
		targetGUID:    "instance_1;db;public;orders",
		provider:      "profiles/openai",
		model:         "gpt-4",
		language:      "en-US",
		promptVersion: "dq.explain.v1",
		result:        resultJSON,
		createdAt:     createdAt,
	})
	require.NoError(t, err)
	require.Equal(t, "dq|target:g|model:gpt-4|lang:en-US|prompt:dq.explain.v1", message.CacheKey)
	require.Equal(t, DataQualityLLMCacheKindExplanation, message.Kind)
	require.Equal(t, "instance_1;db;public;orders", message.TargetGUID)
	require.Equal(t, "en-US", message.Language)
	require.Equal(t, createdAt, message.CreatedAt)
	require.NotNil(t, message.Result)
	require.Equal(t, stored.GetExplanation().GetSummary(), message.Result.GetExplanation().GetSummary())
	require.InDelta(t, 0.74, message.Result.GetExplanation().GetConfidence(), 1e-9)
	require.Len(t, message.Result.GetExplanation().GetHypotheses(), 1)
	require.Equal(t, "replication lag", message.Result.GetExplanation().GetHypotheses()[0].GetCause())
	require.Equal(t, int64(1200), message.Result.GetPromptTokens())
	require.Equal(t, int64(340), message.Result.GetCompletionTokens())
	require.Equal(t, int64(1540), message.Result.GetTotalTokens())
	// The columns are stamped back into the result, so a consumer that only looks
	// at the message sees the same facts the row was keyed by.
	require.Equal(t, "profiles/openai", message.Result.GetProvider())
	require.Equal(t, "gpt-4", message.Result.GetModel())
	require.Equal(t, "dq.explain.v1", message.Result.GetPromptVersion())
}

// A cached result is looked up by key only: the key already carries the target,
// the model, the language and the prompt version, so a stale entry is
// unreachable rather than wrong.
func TestDataQualityLLMCacheQueriesAreKeyedAndReplaceTheEntry(t *testing.T) {
	t.Parallel()

	require.Contains(t, getDataQualityLLMCacheQuery, "FROM data_quality_llm_cache")
	require.Contains(t, getDataQualityLLMCacheQuery, "WHERE cache_key = $1")
	requirePlaceholdersMatchArgs(t, getDataQualityLLMCacheQuery, []any{"key"})

	require.Contains(t, upsertDataQualityLLMCacheQuery, "INSERT INTO data_quality_llm_cache")
	require.Contains(t, upsertDataQualityLLMCacheQuery, "ON CONFLICT (cache_key) DO UPDATE")
	for _, assignment := range []string{
		"kind = EXCLUDED.kind",
		"target_guid = EXCLUDED.target_guid",
		"provider = EXCLUDED.provider",
		"model = EXCLUDED.model",
		"language = EXCLUDED.language",
		"prompt_version = EXCLUDED.prompt_version",
		"result_json = EXCLUDED.result_json",
		"created_at = EXCLUDED.created_at",
	} {
		require.Contains(t, upsertDataQualityLLMCacheQuery, assignment)
	}
	requirePlaceholdersMatchArgs(t, upsertDataQualityLLMCacheQuery,
		[]any{"key", int16(2), "g", "p", "m", "en-US", "v1", []byte("{}"), time.Now()})

	require.Contains(t, deleteExpiredDataQualityLLMCacheQuery, "DELETE FROM data_quality_llm_cache")
	require.Contains(t, deleteExpiredDataQualityLLMCacheQuery, "WHERE created_at < $1")
}
