-- Doris e2e_ods: landing tables for the PostgreSQL -> DataX hop. They must exist
-- before DataX runs, because doriswriter is a Stream Load into an existing table.
-- Apply with: mysql -uroot -h127.0.0.1 -P9031 < 10_ods_ddl.sql
--
-- One column per column of the PostgreSQL source, in the source's order: the
-- DataX writer maps positionally and the OpenLineage schema facet is built from
-- the job's explicit column list, so a missing or reordered column silently
-- becomes a wrong edge.
--
-- The local Doris deployment is one BE (and one FE), so every table is
-- REPLICATION_NUM=1 and BUCKETS 1: the default replication of 3 is refused with
-- "replication num should be less than the number of available backends".

CREATE DATABASE IF NOT EXISTS e2e_ods;

DROP TABLE IF EXISTS e2e_ods.dwd_customer_360;
DROP TABLE IF EXISTS e2e_ods.ads_daily_sales;

CREATE TABLE e2e_ods.dwd_customer_360 (
  customer_id      BIGINT        NOT NULL,
  customer_code    VARCHAR(20)   NOT NULL,
  customer_name    VARCHAR(100)  NOT NULL,
  country          VARCHAR(2)    NOT NULL,
  region           VARCHAR(32)   NOT NULL,
  sub_region       VARCHAR(32)   NOT NULL,
  city             VARCHAR(64)   NOT NULL,
  status           VARCHAR(16)   NOT NULL,
  loyalty_tier     VARCHAR(16)   NOT NULL,
  tenure_days      INT           NOT NULL,
  order_count      INT           NOT NULL,
  lifetime_gross   DECIMAL(14,2) NOT NULL,
  lifetime_net     DECIMAL(14,2) NOT NULL,
  avg_order_value  DECIMAL(14,2) NULL,
  lifetime_units   INT           NOT NULL,
  paid_amount      DECIMAL(14,2) NOT NULL,
  refund_amount    DECIMAL(14,2) NOT NULL,
  net_collected    DECIMAL(14,2) NOT NULL,
  balance_due      DECIMAL(14,2) NOT NULL,
  collection_rate  DECIMAL(9,4)  NULL,
  customer_segment VARCHAR(16)   NOT NULL,
  value_rank       INT           NOT NULL,
  loaded_at        DATETIME      NOT NULL
)
DUPLICATE KEY(customer_id)
DISTRIBUTED BY HASH(customer_id) BUCKETS 1
PROPERTIES ("replication_num" = "1");

CREATE TABLE e2e_ods.ads_daily_sales (
  sales_date      DATE          NOT NULL,
  channel         VARCHAR(20)   NOT NULL,
  region          VARCHAR(32)   NOT NULL,
  currency        VARCHAR(3)    NOT NULL,
  order_count     BIGINT        NOT NULL,
  customer_count  BIGINT        NOT NULL,
  units           BIGINT        NOT NULL,
  gross_amount    DECIMAL(16,2) NOT NULL,
  discount_amount DECIMAL(16,2) NOT NULL,
  net_line_amount DECIMAL(16,2) NOT NULL,
  avg_line_amount DECIMAL(14,2) NULL,
  order_share_pct DECIMAL(9,4)  NULL,
  revenue_band    VARCHAR(16)   NOT NULL,
  loaded_at       DATETIME      NOT NULL
)
DUPLICATE KEY(sales_date, channel, region, currency)
DISTRIBUTED BY HASH(sales_date) BUCKETS 1
PROPERTIES ("replication_num" = "1");
