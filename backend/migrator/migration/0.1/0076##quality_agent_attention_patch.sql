-- An escalated case is a decision its owner owes, and often the decision is concrete: set this
-- field to this value, for this reason. The patch rides on the escalation so the owner reviews a
-- change instead of a paragraph, and the case link says which work item the decision unblocks.
--
-- The patch is written by the server from the diagnosis, validated with the same rule validation
-- a save runs, and never applied by the agent: an agent that could apply it would be changing
-- what a check considers acceptable, which the delegation withholds by default.
ALTER TABLE quality_agent_attention
    ADD COLUMN IF NOT EXISTS case_resource_id TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE quality_agent_attention ADD COLUMN IF NOT EXISTS suggested_patch JSONB;
COMMENT ON COLUMN quality_agent_attention.case_resource_id IS 'The agent work item the escalation is about, by resource id; empty for an agent-level event. It is a name rather than a foreign key because a case is pruned after its retention window while the inbox item is a record of what the owner was told';
COMMENT ON COLUMN quality_agent_attention.suggested_patch IS 'protojson of store.QualityAgentFieldPatch; NULL when the escalation carries no single change';
