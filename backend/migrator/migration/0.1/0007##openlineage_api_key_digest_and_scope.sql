-- Add a deterministic digest for O(1) API key lookup and an optional
-- OpenLineage namespace scope.
--
-- Until now ValidateOpenLineageAPIKey scanned every non-revoked row and ran
-- bcrypt on each, so a forged bearer token cost one bcrypt per stored key and
-- the unauthenticated endpoint was a CPU-exhaustion vector. The digest is a
-- SHA-256 of the plaintext key: it is only a lookup key, the bcrypt comparison
-- (already enforced by the unique hash index) still decides acceptance.
--
-- Rows created before this migration keep a NULL digest and therefore no longer
-- validate. The project is not deployed yet, so affected keys are simply
-- re-issued.

ALTER TABLE openlineage_api_key ADD COLUMN IF NOT EXISTS key_digest TEXT;
ALTER TABLE openlineage_api_key ADD COLUMN IF NOT EXISTS scope_namespace TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_openlineage_api_key_digest
    ON openlineage_api_key(key_digest)
    WHERE key_digest IS NOT NULL;
