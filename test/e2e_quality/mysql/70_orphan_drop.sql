-- Orphan fixture: drop the table behind an enabled rule. After a full sync the
-- next run is SKIPPED(ORPHANED), not ERROR.

USE e2e_dq;

DROP TABLE dq_orphan_target;
