-- Verification queries for the e2e lineage chain
--   MySQL e2e_ods -> PG e2e(e2e_ods) -> PG e2e(e2e_dwd/e2e_ads) -> StarRocks e2e_ods/e2e_ads
--
-- Run against the metaxisdata metadata database:
--   PGPASSWORD=dev psql -h localhost -U dev -d metaxisdata -f verify_chain.sql

\echo '=== 1. edge count per hop (table level, e2e objects only)'
WITH e2e_edges AS (
  SELECT DISTINCT source_guid, target_guid, meta_type
  FROM column_lineage
  WHERE (source_guid LIKE '%e2e%' OR target_guid LIKE '%e2e%')
    AND source_guid NOT LIKE 'openlineage:%'
)
SELECT CASE
         WHEN source_guid LIKE 'mysql-dev-1%' AND target_guid LIKE 'test-pg-1%' THEN '1 mysql -> pg (DataX/OL)'
         WHEN source_guid LIKE 'mysql-dev-1%' AND target_guid LIKE 'mysql-dev-1%' THEN '0 mysql internal (analyzer)'
         WHEN source_guid LIKE 'test-pg-1%' AND target_guid LIKE 'test-pg-1%' THEN '2 pg internal (analyzer/OL)'
         WHEN source_guid LIKE 'test-pg-1%' AND target_guid LIKE 'starrocks%' THEN '3 pg -> starrocks (DataX/OL)'
         WHEN source_guid LIKE 'starrocks%' AND target_guid LIKE 'starrocks%' THEN '4 starrocks internal (analyzer)'
         ELSE 'other'
       END AS hop,
       count(*) AS edges
FROM e2e_edges
GROUP BY 1
ORDER BY 1;

\echo ''
\echo '=== 2. downstream chain reachable from mysql e2e_ods.orders'
WITH RECURSIVE chain AS (
  SELECT source_guid, target_guid, 0 AS depth
  FROM column_lineage
  WHERE source_guid = 'mysql-dev-1;e2e_ods;;orders'
  UNION
  SELECT cl.source_guid, cl.target_guid, c.depth + 1
  FROM chain c
  JOIN column_lineage cl ON cl.source_guid = c.target_guid
  WHERE c.depth < 8
)
SELECT DISTINCT depth, target_guid
FROM chain
ORDER BY depth, target_guid;

\echo ''
\echo '=== 3a. upstream column provenance of the REAL starrocks guid (what the metadata registry knows)'
WITH RECURSIVE up AS (
  SELECT source_guid, source_column, target_guid, target_column, relation_type, 0 AS depth
  FROM column_lineage
  WHERE target_guid = 'starrocks-dev-1;e2e_ods;;mv_daily_sales'
    AND target_column = 'net_line_amount'
  UNION
  SELECT cl.source_guid, cl.source_column, cl.target_guid, cl.target_column, cl.relation_type, up.depth + 1
  FROM up
  JOIN column_lineage cl
    ON cl.target_guid = up.source_guid AND cl.target_column = up.source_column
  WHERE up.depth < 8
)
SELECT DISTINCT depth, source_guid, source_column, target_guid, target_column, relation_type
FROM up
ORDER BY depth, source_guid, source_column;

\echo ''
\echo '=== 3b. same walk reaching the PHANTOM guid the DataX event actually wrote'
WITH RECURSIVE up AS (
  SELECT source_guid, source_column, target_guid, target_column, relation_type, 0 AS depth
  FROM column_lineage
  WHERE target_guid = 'starrocks-dev-1;;e2e_ods;mv_daily_sales'
    AND target_column = 'net_line_amount'
  UNION
  SELECT cl.source_guid, cl.source_column, cl.target_guid, cl.target_column, cl.relation_type, up.depth + 1
  FROM up
  JOIN column_lineage cl
    ON cl.target_guid = up.source_guid AND cl.target_column = up.source_column
  WHERE up.depth < 8
)
SELECT DISTINCT depth, source_guid, source_column, target_guid, target_column, relation_type
FROM up
ORDER BY depth, source_guid, source_column;

\echo ''
\echo '=== 4. lineage endpoints that are NOT registered metadata objects'
WITH refs AS (
  SELECT DISTINCT source_guid AS guid FROM column_lineage WHERE source_guid LIKE '%e2e%'
  UNION
  SELECT DISTINCT target_guid FROM column_lineage WHERE target_guid LIKE '%e2e%'
)
SELECT r.guid
FROM refs r
WHERE r.guid NOT LIKE 'external:%'
  AND NOT EXISTS (SELECT 1 FROM meta_registry_resource m WHERE m.guid = r.guid)
ORDER BY 1;

\echo ''
\echo '=== 5. target columns of PG e2e_dwd.dwd_order_fact that do not exist on the table'
-- An empty target_column is a table-level edge (the ingested column mapping was
-- unverifiable and was blanked), not a missing column, so it is excluded here.
SELECT DISTINCT cl.target_column,
       EXISTS (
         SELECT 1 FROM meta_registry_resource m
         WHERE m.guid = 'test-pg-1;e2e;e2e_dwd;dwd_order_fact;' || cl.target_column
       ) AS is_real_column
FROM column_lineage cl
WHERE cl.target_guid = 'test-pg-1;e2e;e2e_dwd;dwd_order_fact'
  AND cl.target_column <> ''
ORDER BY is_real_column, cl.target_column;

\echo ''
\echo '=== 6. edges whose source is a CTE that resolved as a relation (F8)'
-- After F7 the ingested SQL is analyzed per statement, so CTEs resolve properly.
-- What is left is an analyzer scope bug: a CTE referenced from a nested CTE
-- resolves as a table in the default schema.
SELECT DISTINCT source_guid, target_guid
FROM column_lineage
WHERE source_guid LIKE 'test-pg-1;e2e;public;%'
ORDER BY 1, 2;

\echo ''
\echo '=== 7. analyzer failures on e2e objects'
SELECT meta_guid, meta_type, error_message
FROM column_lineage_version
WHERE meta_guid LIKE '%e2e%' AND error_message IS NOT NULL
ORDER BY 1;
