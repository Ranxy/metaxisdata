-- MySQL-side views: exercise the MySQL analyzer at the head of the chain.
USE e2e_ods;

DROP VIEW IF EXISTS v_order_daily_region;
DROP VIEW IF EXISTS v_customer_active;

CREATE VIEW v_customer_active AS
SELECT c.customer_id,
       c.customer_code,
       c.full_name,
       c.city,
       c.country,
       r.region,
       r.sub_region,
       c.loyalty_tier,
       c.credit_limit,
       CASE
         WHEN c.loyalty_tier = 'platinum' THEN 'key'
         WHEN c.loyalty_tier = 'gold' THEN 'growth'
         ELSE 'standard'
       END AS account_segment,
       TIMESTAMPDIFF(YEAR, c.signup_date, CURRENT_DATE) AS tenure_years
FROM customers c
JOIN regions r ON c.country = r.country
WHERE c.status = 'active';

CREATE VIEW v_order_daily_region AS
SELECT o.order_date,
       r.region,
       o.channel,
       COUNT(DISTINCT o.order_id) AS order_count,
       SUM(o.item_count) AS item_count,
       SUM(o.gross_amount) AS gross_amount,
       SUM(o.discount_amount) AS discount_amount,
       SUM(o.net_amount) AS net_amount,
       CASE WHEN SUM(o.gross_amount) > 0
            THEN ROUND(SUM(o.discount_amount) / SUM(o.gross_amount) * 100, 2)
            ELSE 0
       END AS discount_rate_pct
FROM orders o
JOIN customers c ON o.customer_id = c.customer_id
JOIN regions r ON c.country = r.country
WHERE o.status <> 'cancelled'
GROUP BY o.order_date, r.region, o.channel;
