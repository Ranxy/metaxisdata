-- StarRocks serving layer for the MSSQL hop: the landing table the
-- mssql_to_sr DataX hop loads, and one view over it.
--
-- Apply with: mysql -uroot -h127.0.0.1 -P9030 < 30_mssql_serving_ddl.sql

CREATE DATABASE IF NOT EXISTS e2e_ods;
CREATE DATABASE IF NOT EXISTS e2e_ads;

DROP VIEW IF EXISTS e2e_ads.v_sr_mssql_kpi;
DROP TABLE IF EXISTS e2e_ods.mssql_kpi_daily;

-- One column per column of e2e_ads.v_kpi_daily on the MSSQL side.
CREATE TABLE e2e_ods.mssql_kpi_daily (
  region              VARCHAR(32)   NOT NULL,
  selling_days        BIGINT        NOT NULL,
  order_count         BIGINT        NOT NULL,
  net_line_amount     DECIMAL(16,2) NOT NULL,
  customer_net        DECIMAL(14,2) NOT NULL,
  revenue_rank        BIGINT        NOT NULL,
  customer_count      BIGINT        NOT NULL,
  high_value_count    INT           NOT NULL,
  balance_due         DECIMAL(14,2) NOT NULL,
  orders_per_customer DECIMAL(18,4) NOT NULL,
  revenue_band        VARCHAR(16)   NOT NULL
)
DUPLICATE KEY(region)
DISTRIBUTED BY HASH(region) BUCKETS 3
PROPERTIES ("replication_num" = "1");

CREATE VIEW e2e_ads.v_sr_mssql_kpi AS
SELECT k.region,
       k.customer_count,
       k.high_value_count,
       k.order_count,
       k.net_line_amount,
       k.orders_per_customer,
       CASE WHEN k.revenue_rank <= 3 THEN 'top' ELSE 'tail' END AS band
FROM e2e_ods.mssql_kpi_daily k;
