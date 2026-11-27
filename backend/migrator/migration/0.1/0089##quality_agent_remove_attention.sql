-- The owner's to-do is now one of the agent's asks, not the legacy per-target attention
-- row. No runtime writer creates an attention; drop the table so a stale card cannot be
-- read or answered through the removed RPCs.
DROP TABLE IF EXISTS quality_agent_attention;
