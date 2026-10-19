package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// dataQualityLLMCacheColumns lists the columns every read of
// data_quality_llm_cache returns, in the order scanDataQualityLLMCache expects
// them.
const dataQualityLLMCacheColumns = `cache_key, kind, target_guid, provider, model, language,
	prompt_version, result_json, created_at`

// getDataQualityLLMCacheQuery reads one cached result by its key. The key already
// carries the prompt version, the model and the language, so an entry written by
// an older prompt or another model is simply never looked up again.
const getDataQualityLLMCacheQuery = `
	SELECT ` + dataQualityLLMCacheColumns + ` FROM data_quality_llm_cache WHERE cache_key = $1`

// upsertDataQualityLLMCacheQuery replaces the entry for one key. A validated
// result is cached again after a regeneration, so the row is refreshed rather
// than appended.
const upsertDataQualityLLMCacheQuery = `
	INSERT INTO data_quality_llm_cache (
		cache_key, kind, target_guid, provider, model, language, prompt_version, result_json, created_at
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	ON CONFLICT (cache_key) DO UPDATE SET
		kind = EXCLUDED.kind,
		target_guid = EXCLUDED.target_guid,
		provider = EXCLUDED.provider,
		model = EXCLUDED.model,
		language = EXCLUDED.language,
		prompt_version = EXCLUDED.prompt_version,
		result_json = EXCLUDED.result_json,
		created_at = EXCLUDED.created_at`

// deleteExpiredDataQualityLLMCacheQuery prunes cached results past their
// retention window. The prompt version is part of the key, so pruning is space
// reclamation, not invalidation.
const deleteExpiredDataQualityLLMCacheQuery = `DELETE FROM data_quality_llm_cache WHERE created_at < $1`

// DataQualityLLMCacheKind is what a cached data-quality LLM result is: a rule
// draft or a failure explanation. The column is a small integer and has no proto
// enum counterpart on purpose -- the value classifies the cache entry, not the
// domain (the domain kind is QualitySuggestionKind on a suggestion), so a
// store-local type keeps the proto surface free of a cache concern. The numbers
// are persisted and must not be renumbered.
type DataQualityLLMCacheKind int16

const (
	// DataQualityLLMCacheKindRuleDraft caches a RuleDraftResult.
	DataQualityLLMCacheKindRuleDraft DataQualityLLMCacheKind = 1
	// DataQualityLLMCacheKindExplanation caches an ExplanationResult.
	DataQualityLLMCacheKindExplanation DataQualityLLMCacheKind = 2
)

// known reports whether kind is one of the cache kinds this build writes.
func (k DataQualityLLMCacheKind) known() bool {
	return k == DataQualityLLMCacheKindRuleDraft || k == DataQualityLLMCacheKindExplanation
}

// DataQualityLLMCacheRow is one row of data_quality_llm_cache: the validated
// output of one analysis, with the identity of the question that produced it.
type DataQualityLLMCacheRow struct {
	// CacheKey is built by the caller and must carry the target, the model, the
	// language and the prompt version.
	CacheKey      string
	Kind          DataQualityLLMCacheKind
	TargetGUID    string
	Provider      string
	Model         string
	Language      string
	PromptVersion string
	// Result is the validated output; never nil on a row that was read.
	Result    *storepb.DataQualityLLMResult
	CreatedAt time.Time
}

// normalizeDataQualityLLMCacheRow keeps the denormalized columns and the result's
// copy of the same facts in agreement. A column is the truth -- it is what the
// cache is queried and pruned by -- so the result's provider, model and prompt
// version are stamped from it; a column the caller left empty is filled from the
// result first, so a caller that set only one of the two still stores both.
func normalizeDataQualityLLMCacheRow(row *DataQualityLLMCacheRow) {
	if row == nil || row.Result == nil {
		return
	}
	if row.Provider == "" {
		row.Provider = row.Result.GetProvider()
	}
	if row.Model == "" {
		row.Model = row.Result.GetModel()
	}
	if row.PromptVersion == "" {
		row.PromptVersion = row.Result.GetPromptVersion()
	}
	row.Result.Provider = row.Provider
	row.Result.Model = row.Model
	row.Result.PromptVersion = row.PromptVersion
}

// dataQualityLLMCacheKindForResult reports the kind a result's oneof arm implies.
func dataQualityLLMCacheKindForResult(result *storepb.DataQualityLLMResult) (DataQualityLLMCacheKind, bool) {
	switch result.GetResult().(type) {
	case *storepb.DataQualityLLMResult_RuleDraft:
		return DataQualityLLMCacheKindRuleDraft, true
	case *storepb.DataQualityLLMResult_Explanation:
		return DataQualityLLMCacheKindExplanation, true
	case nil:
		return 0, false
	default:
		return 0, false
	}
}

// validateDataQualityLLMCacheRow checks what a cache row cannot be written
// without, and that the kind and the result's oneof arm agree: a reader that
// trusts the kind must never unmarshal a result of the other shape.
func validateDataQualityLLMCacheRow(row *DataQualityLLMCacheRow) error {
	if row == nil {
		return common.Errorf(common.Invalid, "data quality LLM cache row is required")
	}
	if strings.TrimSpace(row.CacheKey) == "" {
		return common.Errorf(common.Invalid, "data quality LLM cache key is required")
	}
	if row.Result == nil {
		return common.Errorf(common.Invalid, "data quality LLM cache result is required")
	}
	implied, ok := dataQualityLLMCacheKindForResult(row.Result)
	if !ok {
		return common.Errorf(common.Invalid, "data quality LLM cache result carries no rule draft or explanation")
	}
	if row.Kind == 0 {
		// A caller that only set the result gets the kind its arm implies.
		row.Kind = implied
		return nil
	}
	if !row.Kind.known() {
		return common.Errorf(common.Invalid, "unknown data quality LLM cache kind %d", row.Kind)
	}
	if row.Kind != implied {
		return common.Errorf(common.Invalid, "data quality LLM cache kind %d does not match its result", row.Kind)
	}
	return nil
}

// scanDataQualityLLMCache maps one data_quality_llm_cache row.
func scanDataQualityLLMCache(row rowScanner) (*DataQualityLLMCacheRow, error) {
	var (
		message    DataQualityLLMCacheRow
		kind       int16
		resultJSON []byte
	)
	if err := row.Scan(
		&message.CacheKey, &kind, &message.TargetGUID, &message.Provider, &message.Model,
		&message.Language, &message.PromptVersion, &resultJSON, &message.CreatedAt,
	); err != nil {
		return nil, err
	}
	message.Kind = DataQualityLLMCacheKind(kind)
	result := &storepb.DataQualityLLMResult{}
	if len(resultJSON) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(resultJSON, result); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal a data quality LLM result")
		}
	}
	message.Result = result
	normalizeDataQualityLLMCacheRow(&message)
	return &message, nil
}

// GetDataQualityLLMCache returns the cached result for one key, or (nil, nil)
// when nothing was cached under it. Expiry is the maintenance runner's job: the
// key carries the prompt version, so a stale entry is unreachable rather than
// wrong.
func (s *Store) GetDataQualityLLMCache(ctx context.Context, cacheKey string) (*DataQualityLLMCacheRow, error) {
	if strings.TrimSpace(cacheKey) == "" {
		return nil, common.Errorf(common.Invalid, "data quality LLM cache key is required")
	}
	row, err := scanDataQualityLLMCache(s.GetDB().QueryRowContext(ctx, getDataQualityLLMCacheQuery, cacheKey))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to get a data quality LLM cache entry")
	}
	return row, nil
}

// UpsertDataQualityLLMCache writes one validated result, replacing any entry with
// the same key. CreatedAt is stamped when the caller did not set it, so an entry
// can never claim to be written at the zero time that retention would prune
// immediately.
func (s *Store) UpsertDataQualityLLMCache(ctx context.Context, row *DataQualityLLMCacheRow) error {
	if err := validateDataQualityLLMCacheRow(row); err != nil {
		return err
	}
	normalizeDataQualityLLMCacheRow(row)

	resultJSON, err := protojson.Marshal(row.Result)
	if err != nil {
		return errors.Wrap(err, "failed to marshal a data quality LLM result")
	}
	createdAt := row.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	} else {
		createdAt = createdAt.UTC()
	}
	if _, err := s.GetDB().ExecContext(ctx, upsertDataQualityLLMCacheQuery,
		row.CacheKey, int16(row.Kind), row.TargetGUID, row.Provider, row.Model, row.Language,
		row.PromptVersion, resultJSON, createdAt,
	); err != nil {
		return errors.Wrap(err, "failed to upsert a data quality LLM cache entry")
	}
	row.CreatedAt = createdAt
	return nil
}

// DeleteExpiredDataQualityLLMCache removes cached results written before the
// cutoff and reports how many rows were dropped.
func (s *Store) DeleteExpiredDataQualityLLMCache(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := s.GetDB().ExecContext(ctx, deleteExpiredDataQualityLLMCacheQuery, cutoff.UTC())
	if err != nil {
		return 0, errors.Wrap(err, "failed to delete expired data quality LLM cache entries")
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count deleted data quality LLM cache entries")
	}
	return deleted, nil
}
