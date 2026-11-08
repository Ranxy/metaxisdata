-- Row-count-change fixture, step 2: remove the five rows 30_row_count_change_growth.sql
-- added, so the repair rerun sees the original 10 rows again.

USE e2e_dq;

DELETE FROM dq_orders WHERE id BETWEEN 11 AND 15;
