-- A scheduled target cycle can retry a check whose previous attempt was
-- abandoned (batch timeout, incomplete batch, lost process). The attempt number
-- makes the retry visible and bounds it: the due scan stops retrying a rule after
-- the configured number of attempts, so a repeatedly abandoned check cannot loop
-- forever. Manual executions start at 1.
ALTER TABLE quality_execution
    ADD COLUMN IF NOT EXISTS attempt INT2 NOT NULL DEFAULT 1;
