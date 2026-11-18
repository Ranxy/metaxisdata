-- The manual AI drafting job is retired: drafting is now the agent's triage work and
-- its suggestions go through the same review queue. Drop the job table; suggestions
-- already queued are kept.
DROP TABLE IF EXISTS quality_suggestion_job;
