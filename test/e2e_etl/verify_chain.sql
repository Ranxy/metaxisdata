-- Verification queries for the e2e lineage chain
--   MySQL e2e_ods -> PG e2e(e2e_ods) -> PG e2e(e2e_dwd/e2e_ads)
--     -> StarRocks e2e_ods/e2e_ads  and  -> MSSQL e2e(e2e_ods/e2e_dwd/e2e_ads)
--     -> StarRocks e2e_ods/e2e_ads (from the MSSQL view)
--     -> Doris e2e_ods/e2e_ads  and  -> StarRocks e2e_ods/e2e_ads (from the Doris view)
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
         WHEN source_guid LIKE 'test-pg-1%' AND target_guid LIKE 'ms-sql-1%' THEN '5 pg -> mssql (DataX/OL)'
         WHEN source_guid LIKE 'ms-sql-1%' AND target_guid LIKE 'ms-sql-1%' THEN '6 mssql internal (analyzer)'
         WHEN source_guid LIKE 'ms-sql-1%' AND target_guid LIKE 'starrocks%' THEN '7 mssql -> starrocks (DataX/OL)'
         WHEN source_guid LIKE 'test-pg-1%' AND target_guid LIKE 'doris-dev-1%' THEN '8 pg -> doris (DataX/OL)'
         WHEN source_guid LIKE 'doris-dev-1%' AND target_guid LIKE 'doris-dev-1%' THEN '9 doris internal (analyzer)'
         WHEN source_guid LIKE 'doris-dev-1%' AND target_guid LIKE 'starrocks%' THEN '10 doris -> starrocks (DataX/OL)'
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
-- After F7 the ingested SQL is analyzed per statement, so CTEs resolve properly,
-- and F8 ported the wildcard rule that kept a CTE reference resolvable. This
-- section must be empty: a non-empty result is the defect coming back.
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

\echo ''
\echo '=== 8. the MySQL view over a nested CTE with COUNT(*) (F8) names no CTE'
-- Its definition is the shape that used to make an analyzer emit an edge whose
-- source was the CTE (base/agg). Every source here must be a stored relation.
SELECT DISTINCT cl.source_guid, cl.source_column, cl.target_column
FROM column_lineage cl
WHERE cl.meta_guid = 'mysql-dev-1;e2e_ods;;v_region_order_stats'
ORDER BY 1, 2, 3;

-- Sections 9-13 measure what "Post-fix verification" in FINDINGS.md reports.
-- Ingestion files each run's edges under that run's own meta row, so the same
-- logical edge recurs once per run that produced it and every count below is
-- DISTINCT; a raw row count would depend on how many chains have run against
-- this database.

\echo ''
\echo '=== 9. distinct column edges for the e2e objects'
SELECT count(*) AS distinct_edges FROM (
  SELECT DISTINCT source_guid, source_column, target_guid, target_column
  FROM column_lineage
  WHERE source_guid LIKE '%e2e%' OR target_guid LIKE '%e2e%') x;

\echo ''
\echo '=== 10. analysed objects, by instance and type (17 = VIEW, 6 = MATERIALIZED VIEW)'
SELECT split_part(meta_guid, ';', 1) AS instance, meta_type, count(*) AS objects
FROM column_lineage_version
WHERE meta_guid LIKE '%e2e%' AND meta_type <> 100
GROUP BY 1, 2 ORDER BY 1, 2;

\echo ''
\echo '=== 11. column claims on a TABLE that name a column it does not have'
-- Must be empty. A view keeps its columns in its own metadata rather than as
-- registry rows, so only TABLE targets are judged here.
SELECT cl.target_guid, cl.target_column, count(*) AS edges
FROM column_lineage cl
WHERE cl.target_column <> ''
  AND EXISTS (SELECT 1 FROM meta_registry_resource t
              WHERE t.guid = cl.target_guid AND t.object_type = 4)
  AND NOT EXISTS (SELECT 1 FROM meta_registry_resource c
                  WHERE c.guid = cl.target_guid || ';' || cl.target_column AND c.object_type = 7)
GROUP BY 1, 2 ORDER BY 1, 2;

\echo ''
\echo '=== 12. positional placeholder columns and external endpoints'
SELECT (SELECT count(*) FROM column_lineage
        WHERE source_column ~ '^_[0-9]+$' OR target_column ~ '^_[0-9]+$') AS positional_columns,
       (SELECT count(DISTINCT g) FROM (
          SELECT source_guid AS g FROM column_lineage WHERE source_guid LIKE 'external:%'
          UNION SELECT target_guid FROM column_lineage WHERE target_guid LIKE 'external:%') e) AS external_endpoints;

\echo ''
\echo '=== 13. OpenLineage runs per day'
-- A single chain adds 30 runs (26 task runs + one DAG-level run per DAG); the
-- chain itself spans one to two minutes, so count from its start rather than
-- grouping by the minute.
SELECT date_trunc('day', created_at) AS day, count(*) AS runs
FROM openlineage_run GROUP BY 1 ORDER BY 1 DESC LIMIT 5;

-- Sections 14-22 are the MSSQL hop's. They rest on one fact that sections 1-13 do
-- not: an MSSQL object is addressed by four parts (instance;database;schema;name),
-- so every producer - the DataX job, the analyzer, and the schema sync - has to
-- write the same four parts or the chain gets two disconnected nodes with the same
-- name. Section 15 is where a mismatch would show as a shifted column mapping.

\echo ''
\echo '=== 14. the MSSQL hop: distinct edges per pair'
SELECT CASE
         WHEN source_guid LIKE 'test-pg-1%' AND target_guid LIKE 'ms-sql-1%' THEN '5 pg -> mssql (DataX/OL)'
         WHEN source_guid LIKE 'ms-sql-1%' AND target_guid LIKE 'ms-sql-1%' THEN '6 mssql internal (analyzer)'
         WHEN source_guid LIKE 'ms-sql-1%' AND target_guid LIKE 'starrocks%' THEN '7 mssql -> starrocks (DataX/OL)'
         ELSE 'other'
       END AS hop,
       count(*) AS edges
FROM (SELECT DISTINCT source_guid, source_column, target_guid, target_column
      FROM column_lineage
      WHERE source_guid LIKE 'ms-sql-1%' OR target_guid LIKE 'ms-sql-1%') e
GROUP BY 1 ORDER BY 1;

\echo ''
\echo '=== 15. the PG -> MSSQL copy is a same-name, same-column mapping'
-- The copy lists every column explicitly and maps them positionally. A shifted
-- mapping still produces edges, and every one of them would name a column that
-- exists, so the only thing that catches it is comparing the two names.
SELECT DISTINCT source_column, target_column
FROM column_lineage
WHERE target_guid IN ('ms-sql-1;e2e;e2e_ods;dwd_customer_360',
                      'ms-sql-1;e2e;e2e_ods;ads_daily_sales')
  AND source_column <> target_column;

\echo ''
\echo '=== 16. upstream of the MSSQL view the last hop copies into StarRocks'
WITH RECURSIVE up AS (
  SELECT source_guid, source_column, target_guid, target_column, relation_type, 0 AS depth
  FROM column_lineage
  WHERE target_guid = 'ms-sql-1;e2e;e2e_ads;v_kpi_daily'
    AND target_column = 'net_line_amount'
  UNION
  SELECT cl.source_guid, cl.source_column, cl.target_guid, cl.target_column, cl.relation_type, up.depth + 1
  FROM up
  JOIN column_lineage cl
    ON cl.target_guid = up.source_guid AND cl.target_column = up.source_column
  WHERE up.depth < 6
)
SELECT DISTINCT depth, source_guid, source_column, relation_type
FROM up ORDER BY depth, source_guid, source_column;

\echo ''
\echo '=== 17. the edge into StarRocks starts at the MSSQL VIEW, not at a table of that name'
-- The analyzer owns ms-sql-1;e2e;e2e_ads;v_kpi_daily; the DataX event has to write
-- the same GUID. A second row here, or a source that is a table, is the defect.
SELECT DISTINCT source_guid, target_guid
FROM column_lineage
WHERE target_guid = 'starrocks-dev-1;e2e_ods;;mssql_kpi_daily'
ORDER BY 1, 2;

\echo ''
\echo '=== 18. grouping keys of the grouped-clause views name base columns'
-- CUBE / ROLLUP / GROUPING SETS are what the key list is derived from; a key that
-- names the grouped expression ("CUBE(channel, region)") or a pseudo-column is the
-- shape that used to be recorded, and nothing else in this file would show it.
SELECT DISTINCT cl.target_guid, cl.target_column,
       t.value ->> 'expression' AS expression,
       t.value -> 'group_keys' AS group_keys
FROM column_lineage cl
CROSS JOIN LATERAL jsonb_array_elements(cl.transformation) AS t(value)
WHERE cl.target_guid IN ('ms-sql-1;e2e;e2e_dwd;v_channel_cube',
                         'ms-sql-1;e2e;e2e_dwd;v_daily_rollup',
                         'ms-sql-1;e2e;e2e_dwd;v_segment_grouping_sets')
  AND t.value ->> 'operation' = 'AGGREGATE'
ORDER BY 1, 2;

\echo ''
\echo '=== 19. what the MSSQL analysis reported instead of guessing'
-- Three objects, three answers: a view whose body the parser cannot read, a PIVOT
-- over a derived table whose grouping column cannot be enumerated, and a view that
-- reads a table-valued function. Each must say so on its version row.
SELECT meta_guid, meta_type, error_message
FROM column_lineage_version
WHERE meta_guid LIKE 'ms-sql-1;e2e;%' AND error_message IS NOT NULL AND error_message <> ''
ORDER BY 1;

\echo ''
\echo '=== 20. a view with a reported gap keeps the edges it did find'
SELECT cl.meta_guid, cl.source_guid, cl.source_column, cl.target_column
FROM column_lineage cl
WHERE cl.meta_guid IN ('ms-sql-1;e2e;e2e_ads;v_pivot_over_derived',
                       'ms-sql-1;e2e;e2e_ads;v_tvf_source')
ORDER BY 1, 2, 3, 4;

\echo ''
\echo '=== 21. no edge is attributed to a relation the analyzer cannot address'
-- Nothing may name the pivot alias, the function call or the table variable as a
-- source: those are the shapes a guess would invent.
SELECT DISTINCT source_guid, target_guid
FROM column_lineage
WHERE (source_guid LIKE 'ms-sql-1%' OR target_guid LIKE 'ms-sql-1%')
  AND (source_guid ~ ';@' OR target_guid ~ ';@'
       OR source_guid LIKE '%;e2e;e2e_ads;p' OR source_guid LIKE '%;e2e;e2e_ads;f'
       OR target_guid LIKE '%;e2e;e2e_ads;p' OR target_guid LIKE '%;e2e;e2e_ads;f')
ORDER BY 1, 2;

\echo ''
\echo '=== 22. an unparseable definition keeps no half a graph, and a routine is analysed at all'
-- The first number must be 0: the view whose body the parser rejects is recorded as
-- a failure with no edges, not as a view with partial lineage. The second must be 0
-- too: a procedure and a function are registry objects, but not ones the analyzer
-- reads, so no analysis row exists for them either way.
SELECT (SELECT count(*) FROM column_lineage
        WHERE meta_guid = 'ms-sql-1;e2e;e2e_ads;v_parenthesised') AS edges_from_the_unparseable_view,
       (SELECT count(*) FROM column_lineage_version
        WHERE meta_guid IN ('ms-sql-1;e2e;e2e_ads;usp_refresh_kpi',
                            'ms-sql-1;e2e;e2e_dwd;fn_region_net')) AS analysis_rows_for_routines;

-- Sections 23-32 are the Doris hop's. They rest on two facts the MSSQL sections do
-- not: a Doris object is addressed by three parts (instance;database;;name) because
-- Doris has no schema, and the engine stores a view definition with every column
-- qualified by its catalog and database (`internal`.`e2e_ods`.`c`.`region`), which
-- the analyzer has to read back as a relation in the same database.

\echo ''
\echo '=== 23. the Doris hop: distinct edges per pair'
SELECT CASE
         WHEN source_guid LIKE 'test-pg-1%' AND target_guid LIKE 'doris-dev-1%' THEN '8 pg -> doris (DataX/OL)'
         WHEN source_guid LIKE 'doris-dev-1%' AND target_guid LIKE 'doris-dev-1%' THEN '9 doris internal (analyzer)'
         WHEN source_guid LIKE 'doris-dev-1%' AND target_guid LIKE 'starrocks%' THEN '10 doris -> starrocks (DataX/OL)'
         ELSE 'other'
       END AS hop,
       count(*) AS edges
FROM (SELECT DISTINCT source_guid, source_column, target_guid, target_column
      FROM column_lineage
      WHERE source_guid LIKE 'doris-dev-1%' OR target_guid LIKE 'doris-dev-1%') e
GROUP BY 1 ORDER BY 1;

\echo ''
\echo '=== 24. the PG -> Doris copy is a same-name, same-column mapping'
-- Like section 15: the copy lists every column explicitly and maps them
-- positionally, so only a source/target name comparison catches a shifted mapping.
SELECT DISTINCT source_column, target_column
FROM column_lineage
WHERE target_guid IN ('doris-dev-1;e2e_ods;;dwd_customer_360',
                      'doris-dev-1;e2e_ods;;ads_daily_sales')
  AND source_column <> target_column;

\echo ''
\echo '=== 25. the edge into StarRocks starts at the Doris VIEW the analyzer owns'
-- One row with three numbers: one distinct source, that source being the view's GUID,
-- and eight column edges (distinct, so running the chain again does not inflate it).
-- A producer that wrote the three-part name F10 describes would show up here as a
-- second source or as source_is_the_analyzer_view = false, and section 31 forbids the
-- shifted GUID outright.
SELECT count(DISTINCT source_guid) AS sources,
       bool_and(source_guid = 'doris-dev-1;e2e_ads;;v_doris_kpi_daily') AS source_is_the_analyzer_view,
       count(DISTINCT source_column || '|' || target_column) AS column_edges
FROM column_lineage
WHERE target_guid = 'starrocks-dev-1;e2e_ods;;doris_kpi_daily';

\echo ''
\echo '=== 26. the Doris analyzer ran on every e2e analysable object, with no errors'
-- 5 = VIEW (6 of them), 6 = MATERIALIZED VIEW (the async one). The two fixture
-- databases are named explicitly, and so is the instance: another engine also has
-- `e2e_ods`/`e2e_ads` databases, and `doris-dev-1;e2e%` would also match any other
-- database someone left on the instance.
SELECT meta_type, count(*) AS objects, count(error_message) AS with_error
FROM column_lineage_version
WHERE split_part(meta_guid, ';', 1) = 'doris-dev-1'
  AND split_part(meta_guid, ';', 2) IN ('e2e_ods', 'e2e_ads')
GROUP BY 1 ORDER BY 1;

\echo ''
\echo '=== 27. a grouping-set key names the columns, not the grouped expression'
-- `GROUP BY ROLLUP(a, b)` and `GROUP BY a, b WITH ROLLUP` group by the same columns;
-- the first used to record the grouped expression itself as one key (F11).
SELECT DISTINCT cl.target_column, t.value ->> 'expression' AS expression,
       t.value -> 'group_keys' AS group_keys
FROM column_lineage cl
CROSS JOIN LATERAL jsonb_array_elements(cl.transformation) AS t(value)
WHERE cl.target_guid = 'doris-dev-1;e2e_ads;;v_customer_segment_rollup'
  AND t.value ->> 'operation' = 'AGGREGATE'
ORDER BY 1;

\echo ''
\echo '=== 28. no Doris e2e grouping key names the grouped expression'
-- Must be empty: a key may be a column (however qualified) and never ROLLUP/CUBE/
-- GROUPING SETS text. Section 27 shows the shape; this covers every object.
SELECT DISTINCT cl.target_guid, k.group_key
FROM column_lineage cl
CROSS JOIN LATERAL jsonb_array_elements(cl.transformation) AS t(value)
CROSS JOIN LATERAL jsonb_array_elements_text(t.value -> 'group_keys') AS k(group_key)
WHERE cl.target_guid LIKE 'doris-dev-1;%'
  AND split_part(cl.target_guid, ';', 2) IN ('e2e_ods', 'e2e_ads')
  AND k.group_key ~* '(rollup|cube|grouping[_ ]?sets)'
ORDER BY 1, 2;

\echo ''
\echo '=== 29. the VARIANT sub-path and the ARRAY LATERAL VIEW keep their own column'
-- Doris stores the body as `props['user']['name']` and an `explode(tags)` LATERAL VIEW
-- over a derived table. A sub-path selects inside one column, so that column is the
-- whole source, and the LATERAL VIEW's declared column comes from the array it
-- exploded: five rows, all from event_props.
SELECT DISTINCT cl.source_guid, cl.source_column, cl.target_column
FROM column_lineage cl
WHERE cl.meta_guid = 'doris-dev-1;e2e_ads;;v_event_props_exploded'
ORDER BY 1, 2, 3;

\echo ''
\echo '=== 30. a CTE with a short declared column list is exposed positionally'
-- `WITH scored (cid, cname) AS (SELECT customer_id, customer_name, ...)` renames the
-- first two body columns and leaves the rest as the body aliased them. The edge that
-- proves the rename is the one below; a name the analyzer invented would appear as a
-- source that is not a registered object (section 32), and a name it missed as no row.
SELECT DISTINCT cl.source_guid, cl.source_column, cl.target_column
FROM column_lineage cl
WHERE cl.meta_guid = 'doris-dev-1;e2e_ads;;v_customer_sales_cte'
  AND cl.target_column = 'top_customer'
ORDER BY 1, 2, 3;

\echo ''
\echo '=== 31. no Doris object is addressed with a schema segment'
-- Must be empty, and this is the gate for F10: Doris has no schema, so an edge naming
-- `doris-dev-1;e2e_ods;e2e_ads;…` is a shifted three-part dataset name
-- (`<URL database>.<qualified table>`) that no synced object has. Section 25 shows the
-- source it must not appear on.
SELECT DISTINCT guid FROM (
  SELECT source_guid AS guid FROM column_lineage
  WHERE source_guid LIKE 'doris-dev-1%' OR target_guid LIKE 'doris-dev-1%'
  UNION
  SELECT target_guid FROM column_lineage
  WHERE source_guid LIKE 'doris-dev-1%' OR target_guid LIKE 'doris-dev-1%'
) g
WHERE split_part(guid, ';', 1) = 'doris-dev-1' AND split_part(guid, ';', 3) <> ''
ORDER BY 1;

\echo ''
\echo '=== 32. every source of a Doris analyzer edge is a registered object'
-- Must be empty. The analyzer only ever records a relation it resolved, so a source
-- here that the registry does not know is a name it invented. External datasets are
-- excluded because an analyzer edge never passes through the resolver, so one
-- reaching here would be a different defect than this section judges.
SELECT DISTINCT cl.source_guid
FROM column_lineage cl
WHERE cl.meta_guid LIKE 'doris-dev-1;%'
  AND split_part(cl.meta_guid, ';', 2) IN ('e2e_ods', 'e2e_ads')
  AND cl.source_guid NOT LIKE 'external:%'
  AND NOT EXISTS (SELECT 1 FROM meta_registry_resource m WHERE m.guid = cl.source_guid)
ORDER BY 1;
