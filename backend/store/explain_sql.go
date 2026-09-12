package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/pkg/errors"
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

// QueryColumnLineageSources returns unique source guids for a given meta_guid.
func (s *Store) QueryColumnLineageSources(ctx context.Context, metaGUID string) ([]string, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT DISTINCT source_guid FROM column_lineage WHERE meta_guid = $1
	`, metaGUID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query column lineage sources")
	}
	defer rows.Close()

	var guids []string
	for rows.Next() {
		var guid string
		if err := rows.Scan(&guid); err != nil {
			return nil, err
		}
		guids = append(guids, guid)
	}
	return guids, rows.Err()
}

// QueryColumnLineageTargets returns unique target guids for a given source_guid.
func (s *Store) QueryColumnLineageTargets(ctx context.Context, metaGUID string) ([]string, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT DISTINCT meta_guid FROM column_lineage WHERE source_guid = $1
	`, metaGUID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query column lineage targets")
	}
	defer rows.Close()

	var guids []string
	for rows.Next() {
		var guid string
		if err := rows.Scan(&guid); err != nil {
			return nil, err
		}
		guids = append(guids, guid)
	}
	return guids, rows.Err()
}

// InsertLLMDebugLog inserts a debug log entry.
func (s *Store) InsertLLMDebugLog(ctx context.Context, provider, model, reqBody, respBody string) error {
	_, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO llm_debug_log (provider, model, request_body, response_body, created_at)
		VALUES ($1, $2, $3, $4, NOW())
	`, provider, model, reqBody, respBody)
	return err
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
