-- Proposal-pending attentions used to be written without their target session.
-- The dedupe key still names the target (`proposal:<target_guid>`), so recover
-- the session without inventing a target. Prefer the active session and fall
-- back to the newest archived one when a target is no longer enrolled.
UPDATE quality_agent_attention a
SET session_id = s.id
FROM (
    SELECT DISTINCT ON (agent_id, target_guid)
        id,
        agent_id,
        target_guid
    FROM quality_agent_session
    WHERE kind = 1
    ORDER BY agent_id, target_guid, (archived_at IS NULL) DESC, created_at DESC
) s
WHERE a.session_id IS NULL
    AND a.kind = 2
    AND a.agent_id = s.agent_id
    AND a.dedupe_key = 'proposal:' || s.target_guid;
