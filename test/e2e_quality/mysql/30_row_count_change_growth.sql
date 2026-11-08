-- Row-count-change fixture, step 1: grow dq_orders from 10 to 15 rows (+50%).
-- The rule's only bound is max_increase_pct = 20, so the next run FAILs.
--
-- The five rows are valid on purpose: only the row count changes, no other
-- rule gets a new defect.

USE e2e_dq;

INSERT INTO dq_orders
  (id, customer_email, credit_code, external_ref, amount, status, product_code, customer_id, quantity, shipped_quantity, updated_at)
VALUES
  (11, 'ivy@example.com',   '91350100M000100Y43', 'R-11', 110.00, 'paid', 'AAA', 1, 1, 1, '2026-04-08 10:00:00'),
  (12, 'judy@example.com',  '91330100799655058B', 'R-12', 120.00, 'paid', 'BBB', 2, 1, 1, '2026-04-08 10:00:00'),
  (13, 'karl@example.com',  '91440300708461136T', 'R-13', 130.00, 'paid', 'CCC', 3, 1, 1, '2026-04-08 10:00:00'),
  (14, 'laura@example.com', '91350100M000100Y43', 'R-14', 140.00, 'paid', 'DDD', 1, 1, 1, '2026-04-08 10:00:00'),
  (15, 'mike@example.com',  '91330100799655058B', 'R-15', 150.00, 'paid', 'EEE', 2, 1, 1, '2026-04-08 10:00:00');
