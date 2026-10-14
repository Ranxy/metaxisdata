-- The analysable MSSQL objects: views the t-SQL lineage analyzer reads from the
-- definitions the schema sync stores.
--
-- Each view is a shape the analyzer has to get exactly right, because a wrong
-- answer here is indistinguishable from a right one on the canvas: a CTE joined
-- to another CTE, an aggregate over a window, a wildcard, and the three grouped
-- clause forms (CUBE, ROLLUP, GROUPING SETS) whose grouping keys have to come
-- from the base table rather than from a pseudo-column of the grouped expression.
--
-- Apply with: sqlcmd -S 127.0.0.1 -U sa -C -b -d e2e -i 40_dwd_views.sql
USE e2e;
GO

CREATE OR ALTER VIEW e2e_dwd.v_region_revenue AS
WITH customer AS (
    SELECT region,
           COUNT(*) AS customer_count,
           SUM(lifetime_net) AS customer_net,
           SUM(CASE WHEN is_high_value = 1 THEN 1 ELSE 0 END) AS high_value_count
    FROM e2e_dwd.dim_customer
    GROUP BY region
),
sales AS (
    SELECT region,
           COUNT(DISTINCT sales_date) AS selling_days,
           SUM(order_count) AS order_count,
           SUM(net_line_amount) AS net_line_amount,
           SUM(units) AS units
    FROM e2e_dwd.fact_daily_sales
    GROUP BY region
)
SELECT s.region,
       c.customer_count,
       c.high_value_count,
       s.selling_days,
       s.order_count,
       s.net_line_amount,
       c.customer_net,
       CAST(CASE WHEN c.customer_net > 0
                 THEN s.net_line_amount / c.customer_net
                 ELSE 0 END AS DECIMAL(9, 4)) AS net_to_lifetime,
       RANK() OVER (ORDER BY s.net_line_amount DESC) AS revenue_rank
FROM sales AS s
JOIN customer AS c ON c.region = s.region;
GO

CREATE OR ALTER VIEW e2e_dwd.v_customer_mix AS
SELECT region,
       COUNT(*) AS customer_count,
       SUM(CASE WHEN is_high_value = 1 THEN 1 ELSE 0 END) AS high_value_count,
       SUM(CASE WHEN loyalty_tier = N'platinum' THEN 1 ELSE 0 END) AS platinum_count,
       SUM(lifetime_net) AS customer_net,
       SUM(balance_due) AS balance_due,
       MAX(refreshed_at) AS last_refreshed
FROM e2e_dwd.dim_customer
GROUP BY region;
GO

-- The wildcard: the view's own columns are the table's, plus two computed ones.
CREATE OR ALTER VIEW e2e_dwd.v_customer_value AS
SELECT d.*,
       NTILE(4) OVER (ORDER BY d.lifetime_net DESC) AS value_quartile,
       DENSE_RANK() OVER (PARTITION BY d.region ORDER BY d.lifetime_net DESC) AS region_rank
FROM e2e_dwd.dim_customer AS d
WHERE d.status = N'active';
GO

-- CUBE: the grouping keys are channel and region; GROUPING() is a function of
-- the grouping, not a column of any base table.
CREATE OR ALTER VIEW e2e_dwd.v_channel_cube AS
SELECT channel,
       region,
       GROUPING(channel) AS channel_is_subtotal,
       SUM(order_count) AS order_count,
       SUM(net_line_amount) AS net_line_amount,
       SUM(units) AS units
FROM e2e_dwd.fact_daily_sales
GROUP BY CUBE(channel, region);
GO

-- ROLLUP: a hierarchy rather than a full cube.
CREATE OR ALTER VIEW e2e_dwd.v_daily_rollup AS
SELECT sales_date,
       channel,
       SUM(order_count) AS order_count,
       SUM(net_line_amount) AS net_line_amount,
       SUM(gross_amount) AS gross_amount,
       AVG(share_pct) AS avg_share_pct
FROM e2e_dwd.fact_daily_sales
GROUP BY ROLLUP(sales_date, channel);
GO

-- GROUPING SETS: three explicit levels, including the empty one.
CREATE OR ALTER VIEW e2e_dwd.v_segment_grouping_sets AS
SELECT customer_segment,
       region,
       COUNT(*) AS customer_count,
       SUM(lifetime_net) AS lifetime_net,
       AVG(lifetime_net) AS avg_lifetime_net,
       MAX(refreshed_at) AS last_refreshed
FROM e2e_dwd.dim_customer
GROUP BY GROUPING SETS ((customer_segment, region), (customer_segment), ());
GO
