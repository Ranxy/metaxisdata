-- Schema-drift fixture: add a column the saved snapshot does not name.
-- After a full sync the next schema_drift run FAILs with one drift item.

USE e2e_dq;

ALTER TABLE dq_drift_target ADD COLUMN extra_col VARCHAR(8) NULL;
