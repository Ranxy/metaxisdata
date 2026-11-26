-- The redesign moved "needs a person" from the session to the case. A parked session silenced every
-- target it covered, nothing could wake it (no event, no case, no failing check), and the only way
-- out was a person finding it in a list. Sessions parked on a decision code are released here: they
-- wake, re-diagnose from evidence, and either act or hand one case to the owner.
--
-- Infrastructure blocks are untouched. A budget window or a transient internal error really does stop
-- everything in the session, and those blocks are retryable by design.
UPDATE quality_agent_session
SET state = 1, blocked_code = '', blocked_reason = '', next_wake_at = NOW(), updated_at = NOW()
WHERE state = 3
    AND blocked_code <> ''
    AND blocked_code NOT IN ('token-budget', 'wake-budget', 'internal-error', 'concurrency-limit');
