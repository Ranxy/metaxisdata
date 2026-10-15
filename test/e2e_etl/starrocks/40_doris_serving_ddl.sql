-- StarRocks serving layer for the Doris hop: the landing table the doris_to_sr
-- DataX hop loads, and one view over it.
--
-- Apply with: mysql -uroot -h127.0.0.1 -P9030 < 40_doris_serving_ddl.sql
--
-- The source of the hop is a Doris view, and the reader is a plain mysqlreader on
-- the Doris FE's MySQL-protocol port: this is where an edge the Doris analyzer
-- produced and an edge an ingested event produced meet at the same node.

CREATE DATABASE IF NOT EXISTS e2e_ods;
CREATE DATABASE IF NOT EXISTS e2e_ads;

DROP VIEW IF EXISTS e2e_ads.v_sr_doris_kpi;
DROP TABLE IF EXISTS e2e_ods.doris_kpi_daily;

-- One column per column of e2e_ads.v_doris_kpi_daily on the Doris side.
CREATE TABLE e2e_ods.doris_kpi_daily (
  region          VARCHAR(32)   NOT NULL,
  order_days      BIGINT        NOT NULL,
  order_count     BIGINT        NOT NULL,
  net_line_amount DECIMAL(16,2) NOT NULL,
  customer_count  BIGINT        NOT NULL,
  region_pool     DECIMAL(18,2) NOT NULL,
  top_rank        BIGINT        NOT NULL,
  revenue_band    VARCHAR(16)   NOT NULL
)
DUPLICATE KEY(region)
DISTRIBUTED BY HASH(region) BUCKETS 3
PROPERTIES ("replication_num" = "1");

CREATE VIEW e2e_ads.v_sr_doris_kpi AS
SELECT k.region,
       k.order_count,
       k.net_line_amount,
       k.customer_count,
       k.region_pool,
       CASE WHEN k.revenue_band = 'top' THEN 1 ELSE 0 END AS is_top_band
FROM e2e_ods.doris_kpi_daily k;
