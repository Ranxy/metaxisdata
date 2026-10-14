-- Landing tables in e2e.e2e_ods: what the PostgreSQL dwd/ads layer copies into
-- MSSQL over the pg_to_mssql DataX hop.
--
-- One column per column of the source table, in the source's order: the DataX
-- writer maps positionally, and the OpenLineage schema facet is built from the
-- job's explicit column list, so a missing or reordered column silently becomes
-- a wrong edge.
--
-- Apply with: sqlcmd -S 127.0.0.1 -U sa -C -b -d e2e -i 10_ods_ddl.sql
USE e2e;
GO

DROP TABLE IF EXISTS e2e_ods.dwd_customer_360;
CREATE TABLE e2e_ods.dwd_customer_360 (
    customer_id      BIGINT          NOT NULL,
    customer_code    NVARCHAR(20)    NOT NULL,
    customer_name    NVARCHAR(100)   NOT NULL,
    country          NVARCHAR(4)     NOT NULL,
    region           NVARCHAR(32)    NOT NULL,
    sub_region       NVARCHAR(32)    NOT NULL,
    city             NVARCHAR(64)    NOT NULL,
    status           NVARCHAR(16)    NOT NULL,
    loyalty_tier     NVARCHAR(16)    NOT NULL,
    tenure_days      INT             NOT NULL,
    order_count      INT             NOT NULL,
    lifetime_gross   DECIMAL(14, 2)  NOT NULL,
    lifetime_net     DECIMAL(14, 2)  NOT NULL,
    avg_order_value  DECIMAL(14, 2)  NULL,
    lifetime_units   INT             NOT NULL,
    paid_amount      DECIMAL(14, 2)  NOT NULL,
    refund_amount    DECIMAL(14, 2)  NOT NULL,
    net_collected    DECIMAL(14, 2)  NOT NULL,
    balance_due      DECIMAL(14, 2)  NOT NULL,
    collection_rate  DECIMAL(9, 4)   NULL,
    customer_segment NVARCHAR(16)    NOT NULL,
    value_rank       INT             NOT NULL,
    loaded_at        DATETIME2(3)    NOT NULL,
    CONSTRAINT pk_ods_dwd_customer_360 PRIMARY KEY (customer_id)
);
GO

DROP TABLE IF EXISTS e2e_ods.ads_daily_sales;
CREATE TABLE e2e_ods.ads_daily_sales (
    sales_date      DATE            NOT NULL,
    channel         NVARCHAR(20)    NOT NULL,
    region          NVARCHAR(32)    NOT NULL,
    currency        NVARCHAR(8)     NOT NULL,
    order_count     BIGINT          NOT NULL,
    customer_count  BIGINT          NOT NULL,
    units           BIGINT          NOT NULL,
    gross_amount    DECIMAL(16, 2)  NOT NULL,
    discount_amount DECIMAL(16, 2)  NOT NULL,
    net_line_amount DECIMAL(16, 2)  NOT NULL,
    avg_line_amount DECIMAL(14, 2)  NULL,
    order_share_pct DECIMAL(9, 4)   NULL,
    revenue_band    NVARCHAR(16)    NOT NULL,
    loaded_at       DATETIME2(3)    NOT NULL,
    CONSTRAINT pk_ods_ads_daily_sales PRIMARY KEY (sales_date, channel, region, currency)
);
GO
