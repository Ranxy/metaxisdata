package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// ExplainSQLCacheTTL bounds how long a cached explanation is served. Metadata
// explanations key on the metadata hash, so a schema change invalidates them by
// itself; the TTL mainly bounds "sql:<hash>" entries, whose SQL text can stay
// unchanged while the schema underneath it moves.
const ExplainSQLCacheTTL = 7 * 24 * time.Hour

// ExplainSQLCacheRow is a row from explain_sql_cache.
type ExplainSQLCacheRow struct {
	CacheKey        string
	CacheType       int32
	MetaGUID        string
	Scope           string
	SQLText         string
	Provider        string
	Model           string
	ExplanationJSON string
	CreatedAt       time.Time
}

// LLMDebugLogEntry is one captured LLM call: the provider facts, the feature that
// made it, the caller's correlation id, the classifier code when it failed, and
// the raw request/response bodies. It is only written when runtime debug is on and
// is pruned after seven days.
type LLMDebugLogEntry struct {
	Provider      string
	Model         string
	Kind          string
	CorrelationID string
	ErrorCode     string
	RequestBody   string
	ResponseBody  string
	CreatedAt     time.Time
}

// insertLLMDebugLogQuery is the one write of the debug log. The correlation id is
// what a failure diagnostic carries, so its index is the retrieval path.
const insertLLMDebugLogQuery = `
	INSERT INTO llm_debug_log (provider, model, kind, correlation_id, error_code, request_body, response_body, created_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

// getLLMDebugLogByCorrelationQuery reads the newest capture for one correlation id.
// A correlation id is per call, so more than one row would only happen after a
// retried write; the newest is the interesting one.
const getLLMDebugLogByCorrelationQuery = `
	SELECT provider, model, kind, correlation_id, error_code, request_body, response_body, created_at
	FROM llm_debug_log
	WHERE correlation_id = $1
	ORDER BY id DESC
	LIMIT 1`

// setLLMDebugLogErrorCodeQuery stamps the classifier on the newest capture for one
// correlation id. It runs after the failure was classified, when the row may
// already have been written by the asynchronous debug writer.
const setLLMDebugLogErrorCodeQuery = `
	UPDATE llm_debug_log SET error_code = $2 WHERE correlation_id = $1`

// InsertLLMDebugLog inserts one debug log entry.
func (s *Store) InsertLLMDebugLog(ctx context.Context, entry *LLMDebugLogEntry) error {
	if entry == nil {
		return common.Errorf(common.Invalid, "an LLM debug log entry is required")
	}
	createdAt := entry.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	} else {
		createdAt = createdAt.UTC()
	}
	_, err := s.GetDB().ExecContext(ctx, insertLLMDebugLogQuery,
		entry.Provider, entry.Model, entry.Kind, entry.CorrelationID, entry.ErrorCode,
		entry.RequestBody, entry.ResponseBody, createdAt)
	return errors.Wrap(err, "failed to insert an LLM debug log entry")
}

// SetLLMDebugLogErrorCode records the classifier code on the newest captured call
// for a correlation id. The capture is a debug aid, so this is best effort: an
// empty id/code or a row that was never written (or was already pruned) is not an
// error.
func (s *Store) SetLLMDebugLogErrorCode(ctx context.Context, correlationID, code string) error {
	correlationID = strings.TrimSpace(correlationID)
	code = strings.TrimSpace(code)
	if correlationID == "" || code == "" {
		return nil
	}
	if _, err := s.GetDB().ExecContext(ctx, setLLMDebugLogErrorCodeQuery, correlationID, code); err != nil {
		return errors.Wrap(err, "failed to set the LLM debug log error code")
	}
	return nil
}

// GetLLMDebugLogByCorrelation returns the newest captured call for one correlation
// id, or (nil, nil) when runtime debug did not capture it or retention pruned it.
func (s *Store) GetLLMDebugLogByCorrelation(ctx context.Context, correlationID string) (*LLMDebugLogEntry, error) {
	if strings.TrimSpace(correlationID) == "" {
		return nil, nil
	}
	entry := &LLMDebugLogEntry{}
	err := s.GetDB().QueryRowContext(ctx, getLLMDebugLogByCorrelationQuery, strings.TrimSpace(correlationID)).Scan(
		&entry.Provider, &entry.Model, &entry.Kind, &entry.CorrelationID, &entry.ErrorCode,
		&entry.RequestBody, &entry.ResponseBody, &entry.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the LLM debug log entry")
	}
	return entry, nil
}

// GetExplainSQLCache looks up a cached explanation by key. Entries older than
// ExplainSQLCacheTTL are reported as a miss.
func (s *Store) GetExplainSQLCache(ctx context.Context, cacheKey string) (*ExplainSQLCacheRow, error) {
	row := s.GetDB().QueryRowContext(ctx, `
		SELECT cache_key, cache_type, meta_guid, scope, sql_text, provider, model, explanation_json, created_at
		FROM explain_sql_cache
		WHERE cache_key = $1 AND created_at > $2
	`, cacheKey, time.Now().Add(-ExplainSQLCacheTTL))

	var r ExplainSQLCacheRow
	err := row.Scan(&r.CacheKey, &r.CacheType, &r.MetaGUID, &r.Scope, &r.SQLText, &r.Provider, &r.Model, &r.ExplanationJSON, &r.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to query explain SQL cache")
	}
	return &r, nil
}

// UpsertExplainSQLCache inserts or replaces a cached explanation.
func (s *Store) UpsertExplainSQLCache(ctx context.Context, r *ExplainSQLCacheRow) error {
	_, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO explain_sql_cache (cache_key, cache_type, meta_guid, scope, sql_text, provider, model, explanation_json, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (cache_key) DO UPDATE SET
			cache_type = EXCLUDED.cache_type,
			meta_guid = EXCLUDED.meta_guid,
			scope = EXCLUDED.scope,
			sql_text = EXCLUDED.sql_text,
			provider = EXCLUDED.provider,
			model = EXCLUDED.model,
			explanation_json = EXCLUDED.explanation_json,
			created_at = EXCLUDED.created_at
	`, r.CacheKey, r.CacheType, r.MetaGUID, r.Scope, r.SQLText, r.Provider, r.Model, r.ExplanationJSON, r.CreatedAt)
	return errors.Wrap(err, "failed to upsert explain SQL cache")
}
