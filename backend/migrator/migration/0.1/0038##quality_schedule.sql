-- P1-4: a target can be scheduled by a five-field cron expression in an IANA time
-- zone instead of (or again after) a fixed interval, and one check can declare a
-- coarser run frequency than its target.
--
-- The interval column stays: it is the default schedule and the value a target
-- falls back to when it returns to interval mode. The cron columns are empty for
-- an interval target, and a cron target keeps its interval column as the fallback.
ALTER TABLE quality_target_schedule
    ADD COLUMN IF NOT EXISTS schedule_cron TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS schedule_timezone TEXT NOT NULL DEFAULT '';

-- How often one check needs to run inside the target's schedule: 0 follows the
-- target (every cycle), 1 is at most hourly, 2 at most daily, 3 at most weekly.
-- It is scheduling metadata, not part of the definition, so it is not revisioned;
-- the target tick stays the floor and a manual run is unaffected.
ALTER TABLE quality_rule
    ADD COLUMN IF NOT EXISTS frequency_tier INT2 NOT NULL DEFAULT 0;
