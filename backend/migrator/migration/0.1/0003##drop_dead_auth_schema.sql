-- Dead schema left over from the Bytebase-era organization model and from the
-- removed two-factor authentication.
--
-- 1. role: the CRUD, its LRU cache and every proto message were deleted; no Go
--    code reads or writes the table and nothing references it. The bigserial
--    sequence and the unique index are owned by the table and go with it.
DROP TABLE IF EXISTS role;

-- 2. principal.mfa_config: no reader, no writer and no proto message describes
--    it. The whole 2FA surface (User.recovery_codes, require_2fa) is gone.
ALTER TABLE principal DROP COLUMN IF EXISTS mfa_config;

-- 3. idp.type: only OAuth2 is implemented and the OIDC/LDAP enum values and
--    config messages were deleted, so the check is narrowed to match. The
--    drop/re-add pair is idempotent; a legacy row still carrying OIDC or LDAP
--    makes the re-add fail loudly rather than keeping an unsupported type that
--    the login path can no longer handle.
ALTER TABLE idp DROP CONSTRAINT IF EXISTS idp_type_check;
ALTER TABLE idp ADD CONSTRAINT idp_type_check CHECK (type IN ('OAUTH2'));
