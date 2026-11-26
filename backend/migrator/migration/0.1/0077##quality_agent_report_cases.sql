-- A patrol report answers the two questions a person asks about an agent: what did it fix, and what
-- is waiting for me. The work-item counts sit beside the execution counts so a later reader can
-- follow the agent's autonomy without re-reading the rendered body.
--
-- repaired_count counts the work items the agent closed by itself in the period (RESOLVED);
-- escalated_count the ones it handed over (ESCALATED or FROZEN); open_case_count is a snapshot of
-- the work still in progress at generation time, which is why it is not period-bounded.
ALTER TABLE quality_agent_report
    ADD COLUMN IF NOT EXISTS repaired_count INT4 NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS escalated_count INT4 NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS open_case_count INT4 NOT NULL DEFAULT 0;
COMMENT ON COLUMN quality_agent_report.repaired_count IS 'Work items the agent closed by itself inside the period';
COMMENT ON COLUMN quality_agent_report.escalated_count IS 'Work items the agent escalated or froze inside the period';
COMMENT ON COLUMN quality_agent_report.open_case_count IS 'Work items still in progress when the report was generated';
