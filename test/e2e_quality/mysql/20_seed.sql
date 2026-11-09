-- Deterministic defect seed for test/e2e_quality (MySQL).
--
-- The defect inventory, all of it on dq_orders unless noted:
--
--   id 2, 3          customer_email IS NULL            -> null_percent WARN (2/10)
--   id 4, 6          external_ref = 'R-4'             -> unique FAIL (1 duplicate beyond the first)
--   id 1, 10         (customer_id, product_code) dup  -> unique_combination FAIL (1 duplicate beyond the first)
--   id 5             status = 'cancelled'             -> accepted_values FAIL (1)
--   id 4             amount = -5.00                   -> range FAIL (1), CEL FAIL (1)
--   id 4             product_code = 'AB'              -> regex FAIL (1), string_length FAIL (1)
--   id 7             customer_id = 999 (no parent)    -> referential_integrity FAIL (1)
--   id 6             shipped_quantity 7 > quantity 6  -> cross_column FAIL (1)
--   id 10            credit_code check digit wrong    -> standard_code FAIL (1)
--   dq_groups        bucket 'a' has 3 rows, max is 2  -> group_check FAIL (1 group)
--   dq_heartbeat     every updated_at is 2020-01-01   -> freshness FAIL
--   dq_empty         no rows                          -> NOT_EVALUATED
--   dq_segment       east/west/north, second run loses east -> segment_check FAIL
--   dq_drift_target  snapshot 2 columns, later +1     -> schema_drift FAIL
--
-- Everything not named above is valid, so a repaired seed (50_repair.sql)
-- turns every rule PASS and every open issue into AUTO_RECOVERED.

USE e2e_dq;

INSERT INTO dq_reference_customers (id, name) VALUES
  (1, 'Acme'),
  (2, 'Globex'),
  (3, 'Initech');

INSERT INTO dq_orders
  (id, customer_email, credit_code, external_ref, amount, status, product_code, customer_id, quantity, shipped_quantity, updated_at)
VALUES
  (1,  'alice@example.com', '91350100M000100Y43', 'R-1',   10.00, 'paid',      'ABC',      1,   1,  1, '2026-04-07 10:00:00'),
  (2,  NULL,                '91330100799655058B', 'R-2',   20.00, 'pending',   'ABCD',     2,   2,  2, '2026-04-07 10:00:00'),
  (3,  NULL,                '91440300708461136T', 'R-3',   30.00, 'shipped',   'ABCDE',    1,   3,  3, '2026-04-07 10:00:00'),
  (4,  'bob@example.com',   '91350100M000100Y43', 'R-4',   -5.00, 'paid',      'AB',       2,   4,  4, '2026-04-07 10:00:00'),
  (5,  'carol@example.com', '91330100799655058B', 'R-5',   50.00, 'cancelled', 'ABCDEF',   3,   5,  5, '2026-04-07 10:00:00'),
  (6,  'dave@example.com',  '91440300708461136T', 'R-4',   60.00, 'paid',      'ABCDEFG',  1,   6,  7, '2026-04-07 10:00:00'),
  (7,  'erin@example.com',  '91350100M000100Y43', 'R-7',   70.00, 'paid',      'ABCDEFGH', 999, 7,  7, '2026-04-07 10:00:00'),
  (8,  'frank@example.com', '91330100799655058B', 'R-8',   80.00, 'pending',   'ABC',      2,   8,  8, '2026-04-07 10:00:00'),
  (9,  'grace@example.com', '91440300708461136T', 'R-9',   90.00, 'shipped',   'ABC',      3,   9,  9, '2026-04-07 10:00:00'),
  (10, 'heidi@example.com', '91350100M000100Y44', 'R-10', 100.00, 'paid',      'ABC',      1,  10, 10, '2026-04-07 10:00:00');

INSERT INTO dq_orders_rollup (id) VALUES
  (1), (2), (3), (4), (5), (6), (7), (8), (9), (10);

INSERT INTO dq_groups (id, bucket) VALUES
  (1, 'a'), (2, 'a'), (3, 'a'), (4, 'b'), (5, 'c');

INSERT INTO dq_heartbeat (id, updated_at) VALUES
  (1, '2020-01-01 00:00:00'),
  (2, '2020-01-02 00:00:00');

INSERT INTO dq_segment (id, region) VALUES
  (1, 'east'), (2, 'west'), (3, 'north');

INSERT INTO dq_drift_target (id, name) VALUES
  (1, 'baseline');

INSERT INTO dq_orphan_target (id) VALUES
  (1);
