-- Two kinds of inbox items outlived the thing they asked about.
--
-- A block notice was written when a session blocked and never closed when the session ran again:
-- a person resumed it, or the budget window reopened, and the card stayed. A case card was keyed by
-- the case, so a case the server settled on its own — an empty target is a fact, not work — left its
-- card open too.
--
-- The write paths now close both (a session write clears its block notices, a settlement clears the
-- cards of the cases it closed). This closes what the old paths left behind. It is idempotent: the
-- predicates are the same ones the new paths use.
UPDATE quality_agent_attention a
SET status = 2, resolved_at = NOW()
WHERE a.status = 1
    AND a.dedupe_key LIKE 'blocked:%'
    AND NOT EXISTS (
        SELECT 1 FROM quality_agent_session s
        WHERE s.id = a.session_id
            AND s.state = 3
            AND a.dedupe_key = 'blocked:' || s.resource_id || ':' || s.blocked_code
    );

UPDATE quality_agent_attention a
SET status = 2, resolved_at = NOW()
WHERE a.status = 1
    AND a.dedupe_key LIKE 'case:%'
    AND NOT EXISTS (
        SELECT 1 FROM quality_agent_case c
        WHERE c.resource_id = a.case_resource_id AND c.state IN (1, 2, 3, 4, 6, 7)
    );
