-- The due slot a scheduled execution belongs to, and the cadence gate that reads
-- it. A run starts a few seconds after its slot (the scheduler plans it on the
-- next tick), so a cadence equal to the target's own period would skip every other
-- cycle if the gate compared the actual start time with "current slot minus the
-- period". Comparing slots instead is exact: a tiered check runs in a cycle only
-- when its previous completed scheduled run's slot is at least the tier period
-- earlier. Manual executions have no slot.
ALTER TABLE quality_execution
    ADD COLUMN IF NOT EXISTS scheduled_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_quality_execution_rule_slot
    ON quality_execution(rule_id, scheduled_at);
