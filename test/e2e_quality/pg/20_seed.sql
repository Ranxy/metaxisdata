-- PostgreSQL fixture seed, run against database e2e_dq as the dev user.
--
-- Defects (exact, so the expected verdicts in rules/postgres.json are stable):
--   id 2, 3   customer_email IS NULL   -> null_percent FAIL (2/5 = 40%)
--   dq_big    all 5000 amounts > 1000  -> range (max 1000, percent sample) FAIL
--
-- Everything else is valid: the percentile aggregate over dq_orders, and the
-- cross-database reconciliation against e2e_dq_ref (5 rows on each side) pass.

INSERT INTO e2e_dq.dq_orders (id, customer_email, amount, status, payload, updated_at) VALUES
  (1, 'alice@example.com', 10.00, 'paid', '{"tier": 1}', '2026-04-07 10:00:00'),
  (2, NULL,                20.00, 'paid', '{"tier": 2}', '2026-04-07 10:00:00'),
  (3, NULL,                30.00, 'paid', '{"tier": 3}', '2026-04-07 10:00:00'),
  (4, 'dave@example.com',  40.00, 'paid', '{"tier": 4}', '2026-04-07 10:00:00'),
  (5, 'erin@example.com',  50.00, 'paid', '{"tier": 5}', '2026-04-07 10:00:00');

INSERT INTO e2e_dq.dq_orders_rollup (id) VALUES (1), (2), (3), (4), (5);

-- 5000 rows, every one above the range rule's min bound.
INSERT INTO e2e_dq.dq_big (id, amount)
SELECT g, 2000.00 FROM generate_series(1, 5000) AS g;
