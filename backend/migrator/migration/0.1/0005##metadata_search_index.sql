-- Serve the metadata search from an index instead of a sequential scan.
--
-- SearchMetaRegistryResource matches `metadata -> <inner> ->> name|title|comment|
-- userComment ILIKE '%x%'`. No jsonb index can serve a substring match, so the
-- LATERAL jsonb_each scan read every row. search_text holds exactly the
-- concatenation of those four fields, maintained by PostgreSQL as a stored
-- generated column, so the predicate can be rewritten to `search_text ILIKE
-- '%x%'`: it matches the same rows and can be answered by a trigram GIN index.
--
-- pg_trgm is a standard contrib extension and ships with every supported
-- PostgreSQL distribution.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE OR REPLACE FUNCTION meta_registry_search_text(metadata jsonb) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT COALESCE(string_agg(field, ' '), '')
    FROM jsonb_each(metadata) AS e(key, inner_meta)
    CROSS JOIN LATERAL (
        VALUES (inner_meta->>'name'), (inner_meta->>'title'),
               (inner_meta->>'comment'), (inner_meta->>'userComment')
    ) AS fields(field)
    WHERE field IS NOT NULL
$$;

ALTER TABLE meta_registry_resource
    ADD COLUMN IF NOT EXISTS search_text TEXT
    GENERATED ALWAYS AS (meta_registry_search_text(metadata)) STORED;

CREATE INDEX IF NOT EXISTS idx_meta_registry_resource_search_text
    ON meta_registry_resource USING GIN (search_text gin_trgm_ops);

-- The maintenance runner prunes both tables by created_at.
CREATE INDEX IF NOT EXISTS idx_explain_sql_cache_created_at
    ON explain_sql_cache(created_at);

CREATE INDEX IF NOT EXISTS idx_llm_debug_log_created_at
    ON llm_debug_log(created_at);
