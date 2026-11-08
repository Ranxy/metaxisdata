-- Verification for the data-quality end-to-end fixture.
--
-- Run against the Metaxisdata metadata database after `run_quality.sh`:
--
--   PGPASSWORD=dev psql -h localhost -U dev -d metaxisdata -f verify_quality.sql
--
-- This is the human-readable evidence, the same role verify_chain.sql has for
-- the lineage fixture. The machine gate is the driver's exit code; the sections
-- below let a reviewer see the verdict matrix, the evidence and the lifecycle
-- instead of trusting that exit code.
--
-- Sections 1-9 print the state. Section 10 is the gate: every row it prints is a
-- count that must be 0 (README lists what each one means).

\echo ''
\echo '=== 1. fixture rules per target (non-archived)'
SELECT r.target_guid,
       count(*)                                     AS rules,
       count(*) FILTER (WHERE r.state = 1)          AS enabled,
       count(*) FILTER (WHERE r.state = 2)          AS paused
FROM quality_rule r
WHERE NOT r.deleted
  AND (r.target_guid LIKE 'mysql-dev-1;e2e_dq;;%'
       OR r.target_guid LIKE 'test-pg-1;e2e_dq;e2e_dq;%'
       OR r.target_guid LIKE 'test-pg-1;e2e_dq_ref;e2e_dq;%')
GROUP BY r.target_guid
ORDER BY r.target_guid;

\echo ''
\echo '=== 2. latest verdict per rule (the expected matrix)'
WITH latest AS (
  SELECT DISTINCT ON (e.rule_id)
         e.rule_id, e.status, e.failed_rows, e.total_rows,
         e.observed_value, e.sampled, e.error_code
  FROM quality_execution e
  WHERE e.state = 3
  ORDER BY e.rule_id, e.id DESC
)
SELECT r.title,
       r.rule_type,
       l.status AS status,
       l.failed_rows,
       l.total_rows,
       l.observed_value,
       l.sampled,
       l.error_code
FROM latest l
JOIN quality_rule r ON r.id = l.rule_id
WHERE NOT r.deleted
  AND (r.target_guid LIKE 'mysql-dev-1;e2e_dq;;%'
       OR r.target_guid LIKE 'test-pg-1;e2e_dq;e2e_dq;%'
       OR r.target_guid LIKE 'test-pg-1;e2e_dq_ref;e2e_dq;%')
ORDER BY r.title;

\echo ''
\echo '=== 3. verdict matrix by rule type x status (latest judgments only)'
WITH latest AS (
  SELECT DISTINCT ON (e.rule_id) e.rule_id, e.status
  FROM quality_execution e
  WHERE e.state = 3
  ORDER BY e.rule_id, e.id DESC
)
SELECT r.rule_type,
       count(*) FILTER (WHERE l.status = 1) AS pass,
       count(*) FILTER (WHERE l.status = 2) AS warn,
       count(*) FILTER (WHERE l.status = 3) AS fail,
       count(*) FILTER (WHERE l.status = 4) AS error,
       count(*) FILTER (WHERE l.status = 5) AS skipped,
       count(*) FILTER (WHERE l.status = 6) AS not_evaluated
FROM latest l
JOIN quality_rule r ON r.id = l.rule_id
WHERE NOT r.deleted
  AND (r.target_guid LIKE 'mysql-dev-1;e2e_dq;;%'
       OR r.target_guid LIKE 'test-pg-1;e2e_dq;e2e_dq;%'
       OR r.target_guid LIKE 'test-pg-1;e2e_dq_ref;e2e_dq;%')
GROUP BY r.rule_type
ORDER BY r.rule_type;

\echo ''
\echo '=== 4. failure/passing samples: every sample carries its columns, NULL is marked'
WITH fixture AS (
  SELECT e.id, e.sample, e.status
  FROM quality_execution e
  JOIN quality_rule r ON r.id = e.rule_id
  WHERE e.state = 3
    AND e.sample <> '{}'::jsonb
    AND NOT r.deleted
    AND (r.target_guid LIKE 'mysql-dev-1;e2e_dq;;%'
         OR r.target_guid LIKE 'test-pg-1;e2e_dq;e2e_dq;%')
)
SELECT count(*)                                                          AS executions_with_evidence,
       count(*) FILTER (WHERE jsonb_array_length(sample->'samples') > 0)  AS with_failure_samples,
       count(*) FILTER (WHERE jsonb_array_length(sample->'passingSamples') > 0) AS with_passing_samples,
       count(*) FILTER (WHERE sample->'samples' @> '[{"columns": [{"isNull": true}]}]'::jsonb) AS with_null_values
FROM fixture;

\echo ''
\echo '=== 5. issues of the live fixture rules (resolution reason 5 = AUTO_RECOVERED)'
SELECT i.status,
       i.resolution_reason,
       count(*)            AS issues,
       sum(i.occurrence_count) AS occurrences
FROM quality_issue i
JOIN quality_rule r ON r.id = i.rule_id
WHERE NOT r.deleted
  AND (r.target_guid LIKE 'mysql-dev-1;e2e_dq;;%'
       OR r.target_guid LIKE 'test-pg-1;e2e_dq;e2e_dq;%')
GROUP BY i.status, i.resolution_reason
ORDER BY i.status, i.resolution_reason;

\echo ''
\echo '=== 6. notifications the live fixture rules raised (DATA_QUALITY_* kinds)'
SELECT n.payload->'dataQuality'->>'kind' AS notification_kind,
       count(*) AS notifications
FROM notification n
JOIN quality_rule r
  ON n.payload->'dataQuality'->>'rule' LIKE '%/qualityRules/' || r.resource_id
WHERE NOT r.deleted
  AND (r.target_guid LIKE 'mysql-dev-1;e2e_dq;;%'
       OR r.target_guid LIKE 'test-pg-1;e2e_dq;e2e_dq;%')
GROUP BY 1
ORDER BY 1;

\echo ''
\echo '=== 7. dispatch batches per target from the last two hours (trigger 1 = MANUAL, 2 = SCHEDULED)'
SELECT q.target_guid,
       q.trigger,
       q.status,
       q.outcome,
       count(*) AS batches
FROM quality_run q
WHERE q.started_at > NOW() - INTERVAL '2 hours'
  AND (q.target_guid LIKE 'mysql-dev-1;e2e_dq;;%'
       OR q.target_guid LIKE 'test-pg-1;e2e_dq;e2e_dq;%')
GROUP BY q.target_guid, q.trigger, q.status, q.outcome
ORDER BY q.target_guid, q.trigger, q.status;

\echo ''
\echo '=== 8. target schedules (interval is stored in seconds)'
SELECT s.target_guid,
       s.schedule_interval_seconds,
       s.schedule_cron,
       s.next_run_at
FROM quality_target_schedule s
WHERE s.target_guid LIKE 'mysql-dev-1;e2e_dq;;%'
   OR s.target_guid LIKE 'test-pg-1;e2e_dq;e2e_dq;%'
ORDER BY s.target_guid;

\echo ''
\echo '=== 9. audit rows the fixture produced (report body must be redacted)'
SELECT payload->>'method'                                        AS method,
       count(*)                                                  AS rows,
       count(*) FILTER (WHERE payload->'response'->>'htmlContent' = '[REDACTED]') AS redacted_bodies
FROM audit_log
WHERE payload->>'method' IN (
        '/metaxisdata.v1.DataQualityService/CreateQualityRule',
        '/metaxisdata.v1.DataQualityService/BatchCreateQualityRules',
        '/metaxisdata.v1.DataQualityService/RunQualityRules',
        '/metaxisdata.v1.DataQualityService/ExportQualityReport',
        '/metaxisdata.v1.DataQualityService/AcknowledgeQualityTargetSchema')
GROUP BY 1
ORDER BY 1;

\echo ''
\echo '=== 10. GATES: every count below must be 0'
WITH fixture_rules AS (
  SELECT r.id
  FROM quality_rule r
  WHERE NOT r.deleted
    AND (r.target_guid LIKE 'mysql-dev-1;e2e_dq;;%'
         OR r.target_guid LIKE 'test-pg-1;e2e_dq;e2e_dq;%'
         OR r.target_guid LIKE 'test-pg-1;e2e_dq_ref;e2e_dq;%')
),
latest AS (
  SELECT DISTINCT ON (e.rule_id) e.rule_id, e.status, e.error_code
  FROM quality_execution e
  WHERE e.state = 3
  ORDER BY e.rule_id, e.id DESC
),
gates AS (
  SELECT 'latest verdict is FAIL' AS gate,
         count(*)::bigint AS must_be_zero
  FROM latest l JOIN fixture_rules f ON f.id = l.rule_id
  WHERE l.status = 3
  UNION ALL
  SELECT 'latest verdict is ERROR', count(*)
  FROM latest l JOIN fixture_rules f ON f.id = l.rule_id
  WHERE l.status = 4
  UNION ALL
  SELECT 'open issue on a live fixture rule', count(*)
  FROM quality_issue i JOIN fixture_rules f ON f.id = i.rule_id
  WHERE i.status <> 3
  UNION ALL
  -- A storm summary carries no rule; it names the instance, the target and the
  -- counts, so scope it by the fixture's target GUIDs and the last two hours.
  SELECT 'storm summary instead of per-rule messages', count(*)
  FROM notification n
  WHERE n.payload->>'type' = 'NOTIFICATION_TYPE_DATA_QUALITY'
    AND n.payload->'dataQuality'->>'kind' = 'DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY'
    AND n.created_at > NOW() - INTERVAL '2 hours'
    AND (n.payload->'dataQuality'->>'targetGuid' LIKE 'mysql-dev-1;e2e_dq;;%'
         OR n.payload->'dataQuality'->>'targetGuid' LIKE 'test-pg-1;e2e_dq;e2e_dq;%')
  UNION ALL
  SELECT 'abandoned dispatch batch', count(*)
  FROM quality_run q
  WHERE q.status = 4
    AND (q.target_guid LIKE 'mysql-dev-1;e2e_dq;;%'
         OR q.target_guid LIKE 'test-pg-1;e2e_dq;e2e_dq;%')
  UNION ALL
  SELECT 'enabled checks but no next run', count(*)
  FROM quality_target_schedule s
  WHERE s.next_run_at IS NULL
    AND (s.target_guid LIKE 'mysql-dev-1;e2e_dq;;%'
         OR s.target_guid LIKE 'test-pg-1;e2e_dq;e2e_dq;%')
    AND EXISTS (SELECT 1 FROM quality_rule r
                WHERE r.target_guid = s.target_guid AND NOT r.deleted AND r.state = 1)
  UNION ALL
  SELECT 'rule with more than one unresolved issue', count(*)
  FROM (SELECT rule_id FROM quality_issue WHERE status <> 3
        GROUP BY rule_id HAVING count(*) > 1) d
  UNION ALL
  SELECT 'report audit row with an unredacted body', count(*)
  FROM audit_log
  WHERE payload->>'method' = '/metaxisdata.v1.DataQualityService/ExportQualityReport'
    AND COALESCE(payload->'response'->>'htmlContent', '') <> '[REDACTED]'
  UNION ALL
  SELECT 'fewer than 20 distinct rule types',
         CASE WHEN count(*) < 20 THEN 1 ELSE 0 END
  FROM (SELECT DISTINCT q.rule_type
        FROM quality_rule q
        JOIN fixture_rules f ON f.id = q.id) t
)
SELECT gate, must_be_zero FROM gates ORDER BY gate;
