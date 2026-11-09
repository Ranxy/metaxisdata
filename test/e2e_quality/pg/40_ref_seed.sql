-- PostgreSQL reference database seed, run against database e2e_dq_ref.
--
-- Five rows, matching the checked side, so the cross-database reconciliation
-- passes with observed 0 and a recorded reference statement.

INSERT INTO e2e_dq.dq_orders_rollup (id) VALUES (1), (2), (3), (4), (5);
