-- The MSSQL-side warehouse: two tables the chain loads from the landing tables.
--
-- Their DDL is ordinary T-SQL on purpose - what this hop tests is not the DDL
-- but the lineage the analyzer derives from the views built on top of them, and
-- the 4-part GUIDs (instance;database;schema;table) every edge has to carry.
--
-- Apply with: sqlcmd -S 127.0.0.1 -U sa -C -b -d e2e -i 20_dwd_ddl.sql
USE e2e;
GO

DROP TABLE IF EXISTS e2e_dwd.dim_customer;
CREATE TABLE e2e_dwd.dim_customer (
    customer_id      BIGINT          NOT NULL,
    customer_code    NVARCHAR(20)    NOT NULL,
    customer_name    NVARCHAR(100)   NOT NULL,
    country          NVARCHAR(4)     NOT NULL,
    region           NVARCHAR(32)    NOT NULL,
    city             NVARCHAR(64)    NOT NULL,
    status           NVARCHAR(16)    NOT NULL,
    loyalty_tier     NVARCHAR(16)    NOT NULL,
    tenure_days      INT             NOT NULL,
    lifetime_net     DECIMAL(14, 2)  NOT NULL,
    paid_amount      DECIMAL(14, 2)  NOT NULL,
    refund_amount    DECIMAL(14, 2)  NOT NULL,
    balance_due      DECIMAL(14, 2)  NOT NULL,
    customer_segment NVARCHAR(16)    NOT NULL,
    value_rank       INT             NOT NULL,
    is_high_value    BIT             NOT NULL,
    refreshed_at     DATETIME2(3)    NOT NULL,
    CONSTRAINT pk_dwd_dim_customer PRIMARY KEY (customer_id)
);
GO

DROP TABLE IF EXISTS e2e_dwd.fact_daily_sales;
CREATE TABLE e2e_dwd.fact_daily_sales (
    sales_date      DATE            NOT NULL,
    channel         NVARCHAR(20)    NOT NULL,
    region          NVARCHAR(32)    NOT NULL,
    currency        NVARCHAR(8)     NOT NULL,
    order_count     BIGINT          NOT NULL,
    customer_count  BIGINT          NOT NULL,
    units           BIGINT          NOT NULL,
    gross_amount    DECIMAL(16, 2)  NOT NULL,
    net_line_amount DECIMAL(16, 2)  NOT NULL,
    revenue_tier    NVARCHAR(4)     NOT NULL,
    share_pct       DECIMAL(9, 4)   NULL,
    loaded_at       DATETIME2(3)    NOT NULL,
    CONSTRAINT pk_dwd_fact_daily_sales PRIMARY KEY (sales_date, channel, region, currency)
);
GO
