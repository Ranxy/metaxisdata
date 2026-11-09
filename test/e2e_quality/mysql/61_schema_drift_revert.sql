-- Schema-drift fixture, revert: remove the extra column so the next run passes
-- again and the open drift issue recovers.

USE e2e_dq;

ALTER TABLE dq_drift_target DROP COLUMN extra_col;
