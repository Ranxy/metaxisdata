-- The built-in quality agent's principal no longer holds a workspace-wide
-- qualityAgent binding: the scoped authorizer derives that role on the agent's
-- databases. Remove the agent principal from existing roles/qualityAgent
-- bindings and drop a binding left empty; a binding an administrator gave the
-- principal under another role is left untouched. The rewrite is idempotent,
-- keeps the binding/member order, and moves updated_at so the policy etag
-- reflects the change.
WITH agent_members AS (
    SELECT 'users/' || principal_id AS member FROM quality_agent
),
rewritten AS (
    SELECT
        p.id,
        jsonb_set(
            p.payload,
            '{bindings}',
            COALESCE((
                SELECT jsonb_agg(binding_out ORDER BY ord)
                FROM (
                    SELECT
                        CASE
                            WHEN b.binding->>'role' = 'roles/qualityAgent' THEN
                                jsonb_set(
                                    b.binding,
                                    '{members}',
                                    COALESCE((
                                        SELECT jsonb_agg(m.member ORDER BY m.ord)
                                        FROM jsonb_array_elements_text(
                                            COALESCE(b.binding->'members', '[]'::jsonb)
                                        ) WITH ORDINALITY AS m(member, ord)
                                        WHERE m.member NOT IN (SELECT member FROM agent_members)
                                    ), '[]'::jsonb)
                                )
                            ELSE b.binding
                        END AS binding_out,
                        b.ord
                    FROM jsonb_array_elements(p.payload->'bindings') WITH ORDINALITY AS b(binding, ord)
                ) rewritten_binding
                WHERE binding_out->>'role' <> 'roles/qualityAgent'
                   OR COALESCE(jsonb_array_length(binding_out->'members'), 0) > 0
            ), '[]'::jsonb)
        ) AS payload
    FROM policy p
    WHERE p.type = 'IAM' AND p.payload ? 'bindings'
)
UPDATE policy p
SET payload = r.payload, updated_at = NOW()
FROM rewritten r
WHERE p.id = r.id;
