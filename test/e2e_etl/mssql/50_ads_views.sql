-- The MSSQL serving layer: views over the dwd views and tables.
--
-- v_kpi_daily is what the mssql_to_sr hop copies into StarRocks, so its columns
-- are the source of a cross-engine edge whose source GUID has to be the view's
-- own registry GUID (ms-sql-1;e2e;e2e_ads;v_kpi_daily), not a table's.
--
-- Apply with: sqlcmd -S 127.0.0.1 -U sa -C -b -d e2e -i 50_ads_views.sql
USE e2e;
GO

CREATE OR ALTER VIEW e2e_ads.v_kpi_daily AS
SELECT r.region,
       r.selling_days,
       r.order_count,
       r.net_line_amount,
       r.customer_net,
       r.revenue_rank,
       m.customer_count,
       m.high_value_count,
       m.balance_due,
       CAST(CASE WHEN m.customer_count > 0
                 THEN CAST(r.order_count AS DECIMAL(18, 4)) / m.customer_count
                 ELSE 0 END AS DECIMAL(18, 4)) AS orders_per_customer,
       CASE WHEN r.revenue_rank <= 3 THEN N'top'
            WHEN r.revenue_rank <= 6 THEN N'middle'
            ELSE N'long tail' END AS revenue_band
FROM e2e_dwd.v_region_revenue AS r
JOIN e2e_dwd.v_customer_mix AS m ON m.region = r.region;
GO

-- PIVOT: the channel values become columns, and every one of them has to trace
-- back to fact_daily_sales.net_line_amount. The pivot reads the base table
-- directly: the grouping column it carries through (region) is enumerated from
-- the source relation's registered columns, which a derived table does not have.
-- 60_gap_fixtures.sql holds the derived-table spelling, where it degrades.
CREATE OR ALTER VIEW e2e_ads.v_pivot_channel AS
SELECT region,
       [web],
       [store],
       [partner]
FROM e2e_dwd.fact_daily_sales
PIVOT (
    SUM(net_line_amount) FOR channel IN ([web], [store], [partner])
) AS p;
GO

-- APPLY: a correlated subquery in the FROM clause, projected as a column.
CREATE OR ALTER VIEW e2e_ads.v_channel_band AS
SELECT c.channel,
       c.region,
       c.net_line_amount,
       c.order_count,
       b.band
FROM e2e_dwd.v_channel_cube AS c
CROSS APPLY (
    SELECT CASE WHEN c.net_line_amount >= 100000 THEN N'high'
                WHEN c.net_line_amount >= 10000 THEN N'medium'
                ELSE N'low' END AS band
) AS b
WHERE c.region IS NOT NULL AND c.channel IS NOT NULL;
GO

-- A table hint behind the alias, in the spelling SQL Server accepts (`WITH
-- (NOLOCK)`, not a bare `(NOLOCK)`). The parser is documented to read `WITH (…)`
-- as part of the relation, and this is the case that keeps that claim checked.
CREATE OR ALTER VIEW e2e_ads.v_hint_after_alias AS
SELECT f.region,
       f.net_line_amount,
       f.order_count
FROM e2e_dwd.fact_daily_sales AS f WITH (NOLOCK)
WHERE f.region = N'EMEA';
GO
