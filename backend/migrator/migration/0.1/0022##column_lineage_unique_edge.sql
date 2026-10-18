-- column_lineage stores one row per distinct edge. Two writers can replace the same
-- object's edges at once (the analyzer and the OpenLineage revalidation path share a
-- process), and both delete-then-insert the same set, so without a uniqueness rule the
-- second insert duplicates every edge. The edge's identity is its endpoints and relation
-- type; transformation is a fact about the edge, not part of what identifies it.
--
-- Existing rows may already hold duplicates from that race, so collapse them (keeping the
-- earliest row per key) before the index is created. No history compatibility is owed:
-- duplicate edges describe the same fact.
DELETE FROM column_lineage
WHERE id IN (
    SELECT id
    FROM (
        SELECT
            id,
            row_number() OVER (
                PARTITION BY
                    meta_guid, meta_type,
                    source_guid, source_column,
                    target_guid, target_column,
                    relation_type
                ORDER BY id
            ) AS duplicate_rank
        FROM column_lineage
    ) ranked
    WHERE duplicate_rank > 1
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_column_lineage_edge ON column_lineage(
    meta_guid,
    meta_type,
    source_guid,
    source_column,
    target_guid,
    target_column,
    relation_type
);
