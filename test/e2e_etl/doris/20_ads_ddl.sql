-- Doris e2e_ads: the layer the Doris definition analyzer reads. Every object here
-- is engine-valid SQL that the analyzer has to resolve; nothing is a diagnostic
-- fixture on purpose, because a shape this engine rejects at CREATE VIEW never
-- reaches the registry at all.
--
-- Apply with: mysql -uroot -h127.0.0.1 -P9031 < 20_ads_ddl.sql
--
-- The one shape that cannot come from PostgreSQL is the VARIANT path: Doris stores
-- a VARIANT column's sub-field access as `props['user']['name']`, and DataX would
-- have to copy a PostgreSQL jsonb column through a CSV Stream Load to land one.
-- `event_props` is therefore a Doris-side source table, inserted deterministically
-- here, and the views over it exist to exercise the VARIANT path, the ARRAY
-- LATERAL VIEW and the derived-table rewrite that are this dialect's own.

CREATE DATABASE IF NOT EXISTS e2e_ads;

-- The DataX hops read Doris through rdbms readers (mysqlreader), and those reject an
-- empty password outright ("[password] is a required parameter"). Root has none in
-- this local deployment, so the fixture creates one read-only user for the copy out
-- of Doris instead of putting a password on root.
DROP USER IF EXISTS 'e2e_reader';
CREATE USER 'e2e_reader' IDENTIFIED BY 'e2e_reader';
GRANT SELECT_PRIV ON e2e_ods.* TO 'e2e_reader';
GRANT SELECT_PRIV ON e2e_ads.* TO 'e2e_reader';

DROP MATERIALIZED VIEW IF EXISTS e2e_ads.mv_region_revenue;
DROP VIEW IF EXISTS e2e_ads.v_doris_kpi_daily;
DROP VIEW IF EXISTS e2e_ads.v_event_props_exploded;
DROP VIEW IF EXISTS e2e_ads.v_customer_sales_cte;
DROP VIEW IF EXISTS e2e_ads.v_customer_sales_join;
DROP VIEW IF EXISTS e2e_ads.v_channel_revenue_daily;
DROP VIEW IF EXISTS e2e_ads.v_customer_segment_rollup;
DROP TABLE IF EXISTS e2e_ods.event_props;

-- The self-contained VARIANT and ARRAY source, six deterministic rows.
CREATE TABLE e2e_ods.event_props (
  event_id BIGINT         NOT NULL,
  uid      VARCHAR(32)    NOT NULL,
  props    VARIANT        NULL,
  tags     ARRAY<VARCHAR(32)> NULL
)
DUPLICATE KEY(event_id)
DISTRIBUTED BY HASH(event_id) BUCKETS 1
PROPERTIES ("replication_num" = "1");

INSERT INTO e2e_ods.event_props VALUES
  (1, 'u001', '{"user": {"name": "alice",  "tier": "gold"},   "amount": 12.5}',  ['gold', 'vip']),
  (2, 'u002', '{"user": {"name": "bob",    "tier": "silver"}, "amount": 7.0}',   ['silver']),
  (3, 'u003', '{"user": {"name": "carol",  "tier": "gold"},   "amount": 31.25}', ['gold', 'vip', 'referral']),
  (4, 'u004', '{"user": {"name": "dave",   "tier": "bronze"}, "amount": 4.75}',  ['bronze']),
  (5, 'u005', '{"user": {"name": "erin",   "tier": "silver"}, "amount": 19.0}',  ['silver', 'referral']),
  (6, 'u006', '{"user": {"name": "frank",  "tier": "gold"},   "amount": 55.5}',  ['gold']);

-- GROUP BY ROLLUP: the grouped keys have to come back as base-table columns, not
-- as the grouped expression.
CREATE VIEW e2e_ads.v_customer_segment_rollup AS
SELECT c.region,
       c.country,
       c.customer_segment,
       count(*)                 AS customer_count,
       sum(c.lifetime_net)      AS total_net,
       avg(c.avg_order_value)   AS avg_order_value,
       max(c.value_rank)        AS top_value_rank
FROM e2e_ods.dwd_customer_360 c
GROUP BY ROLLUP(c.region, c.country, c.customer_segment);

-- A window function over an aggregate, plus a CASE band.
CREATE VIEW e2e_ads.v_channel_revenue_daily AS
SELECT s.sales_date,
       s.channel,
       s.region,
       s.currency,
       sum(s.net_line_amount) AS net_line_amount,
       sum(s.order_count)     AS order_count,
       CASE WHEN sum(s.net_line_amount) >= 100000 THEN 'A'
            WHEN sum(s.net_line_amount) >= 10000  THEN 'B'
            ELSE 'C' END      AS revenue_tier,
       rank() OVER (PARTITION BY s.sales_date ORDER BY sum(s.net_line_amount) DESC) AS day_revenue_rank
FROM e2e_ods.ads_daily_sales s
GROUP BY s.sales_date, s.channel, s.region, s.currency;

-- A join to a derived aggregate, and a window over its result.
CREATE VIEW e2e_ads.v_customer_sales_join AS
SELECT c.customer_id,
       c.customer_name,
       c.region,
       c.country,
       c.customer_segment,
       c.lifetime_net,
       c.balance_due,
       r.region_net_line_amount,
       CASE WHEN c.balance_due > 0 THEN 'open' ELSE 'settled' END AS balance_state,
       row_number() OVER (PARTITION BY c.region ORDER BY c.lifetime_net DESC) AS region_net_rank
FROM e2e_ods.dwd_customer_360 c
JOIN (
  SELECT region, sum(net_line_amount) AS region_net_line_amount
  FROM e2e_ods.ads_daily_sales
  GROUP BY region
) r ON r.region = c.region;

-- A CTE with a SHORT declared column list: the first two body columns are renamed,
-- the rest keep the aliases the body gave them. That positional exposure is what
-- the analyzer has to reproduce; a name it invents would show up as a phantom
-- source, and a name it misses as a reported gap.
CREATE VIEW e2e_ads.v_customer_sales_cte AS
WITH scored (cid, cname) AS (
  SELECT customer_id,
         customer_name,
         region,
         customer_segment,
         balance_state,
         region_net_rank,
         region_net_line_amount
  FROM e2e_ads.v_customer_sales_join
)
SELECT scored.region                AS region,
       scored.customer_segment       AS customer_segment,
       count(*)                      AS customer_count,
       sum(scored.region_net_line_amount) AS region_pool,
       min(scored.region_net_rank)   AS top_rank,
       max(scored.cname)             AS top_customer
FROM scored
GROUP BY scored.region, scored.customer_segment;

-- VARIANT path access (`props['user']['name']` is the spelling this engine stores)
-- over a derived table, with an ARRAY LATERAL VIEW on top of it. The derived-table
-- LATERAL VIEW is the one shape omni's Doris parser refuses, so the analyzer has to
-- recover it, and the recovery has to keep every column the view declares.
CREATE VIEW e2e_ads.v_event_props_exploded AS
SELECT e.event_id,
       e.user_name,
       e.tier,
       e.amount,
       t.tag
FROM (
  SELECT event_id,
         props['user']['name'] AS user_name,
         props['user']['tier'] AS tier,
         cast(props['amount'] AS double) AS amount,
         tags
  FROM e2e_ods.event_props
) e
LATERAL VIEW explode(e.tags) t AS tag;

-- An async materialized view. Doris reports it as a BASE TABLE in
-- information_schema.tables and exposes its body through mv_infos(), which is the
-- one catalog the schema sync branches on for this engine.
CREATE MATERIALIZED VIEW e2e_ads.mv_region_revenue
REFRESH COMPLETE ON SCHEDULE EVERY 1 HOUR
DISTRIBUTED BY HASH(region) BUCKETS 1
PROPERTIES ("replication_num" = "1")
AS
SELECT region,
       currency,
       count(*)               AS order_days,
       sum(order_count)       AS order_count,
       sum(net_line_amount)   AS net_line_amount,
       max(gross_amount)      AS max_gross_amount
FROM e2e_ods.ads_daily_sales
GROUP BY region, currency;

-- The object the last hop copies into StarRocks: a view over the materialized view
-- and over another view, aggregated back to one row per region so the copied table
-- reconciles row-for-row. Two producers meet at this node - the analyzer owns
-- `doris-dev-1;e2e_ads;;v_doris_kpi_daily` and the DataX event names it.
CREATE VIEW e2e_ads.v_doris_kpi_daily AS
SELECT m.region,
       m.order_days,
       m.order_count,
       m.net_line_amount,
       j.customer_count,
       j.region_pool,
       j.top_rank,
       CASE WHEN m.net_line_amount >= 100000 THEN 'top' ELSE 'tail' END AS revenue_band
FROM (
  SELECT region,
         sum(order_days)     AS order_days,
         sum(order_count)    AS order_count,
         sum(net_line_amount) AS net_line_amount
  FROM e2e_ads.mv_region_revenue
  GROUP BY region
) m
JOIN (
  SELECT region,
         sum(customer_count) AS customer_count,
         sum(region_pool)    AS region_pool,
         min(top_rank)       AS top_rank
  FROM e2e_ads.v_customer_sales_cte
  GROUP BY region
) j ON j.region = m.region;
