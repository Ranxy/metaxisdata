-- The MSSQL-side loads: a MERGE upsert for the dimension and an INSERT ...
-- SELECT for the fact.
--
-- These statements produce no lineage by themselves: the chain runs them with
-- sqlcmd, which reports nothing to the platform, and the definition analyzer
-- only reads objects the registry knows (views). They are here because the
-- chain has to be a real chain - the views below have to have something to
-- read - and because they are the statements an application would report on
-- the /api/v1/applicationSql/report path.
--
-- Apply with: sqlcmd -S 127.0.0.1 -U sa -C -b -d e2e -i 30_dwd_load.sql
USE e2e;
GO

MERGE e2e_dwd.dim_customer AS t
USING (
    SELECT customer_id, customer_code, customer_name, country, region, city, status,
           loyalty_tier, tenure_days, lifetime_net, paid_amount, refund_amount,
           balance_due, customer_segment, value_rank,
           CAST(CASE WHEN lifetime_net >= 20000 THEN 1 ELSE 0 END AS BIT) AS is_high_value,
           SYSDATETIME() AS refreshed_at
    FROM e2e_ods.dwd_customer_360
) AS s
    ON t.customer_id = s.customer_id
WHEN MATCHED THEN
    UPDATE SET t.customer_name = s.customer_name,
               t.region = s.region,
               t.city = s.city,
               t.status = s.status,
               t.loyalty_tier = s.loyalty_tier,
               t.tenure_days = s.tenure_days,
               t.lifetime_net = s.lifetime_net,
               t.paid_amount = s.paid_amount,
               t.refund_amount = s.refund_amount,
               t.balance_due = s.balance_due,
               t.customer_segment = s.customer_segment,
               t.value_rank = s.value_rank,
               t.is_high_value = s.is_high_value,
               t.refreshed_at = s.refreshed_at
WHEN NOT MATCHED THEN
    INSERT (customer_id, customer_code, customer_name, country, region, city, status,
            loyalty_tier, tenure_days, lifetime_net, paid_amount, refund_amount,
            balance_due, customer_segment, value_rank, is_high_value, refreshed_at)
    VALUES (s.customer_id, s.customer_code, s.customer_name, s.country, s.region, s.city,
            s.status, s.loyalty_tier, s.tenure_days, s.lifetime_net, s.paid_amount,
            s.refund_amount, s.balance_due, s.customer_segment, s.value_rank,
            s.is_high_value, s.refreshed_at);
GO

TRUNCATE TABLE e2e_dwd.fact_daily_sales;
GO

INSERT INTO e2e_dwd.fact_daily_sales
    (sales_date, channel, region, currency, order_count, customer_count, units,
     gross_amount, net_line_amount, revenue_tier, share_pct, loaded_at)
SELECT sales_date,
       channel,
       region,
       currency,
       order_count,
       customer_count,
       units,
       gross_amount,
       net_line_amount,
       CASE
           WHEN net_line_amount >= 200000 THEN 'A'
           WHEN net_line_amount >= 50000 THEN 'B'
           WHEN net_line_amount >= 10000 THEN 'C'
           ELSE 'D'
       END,
       CAST(ROUND(order_share_pct * 100, 4) AS DECIMAL(9, 4)),
       SYSDATETIME()
FROM (
    SELECT s.*,
           ROW_NUMBER() OVER (PARTITION BY sales_date ORDER BY net_line_amount DESC) AS day_rank
    FROM e2e_ods.ads_daily_sales AS s
) AS ranked
WHERE day_rank <= 200;
GO
