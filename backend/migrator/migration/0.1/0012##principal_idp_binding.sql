-- Bind an account to the identity provider subject it signs in with.
--
-- SSO used to resolve a login by the email claim alone: anyone who had already
-- registered that address -- by self-service signup, or by moving their own
-- email onto it -- owned the account, and the real employee's SSO login landed
-- in it. The binding added here is the identity a login matches from now on;
-- the email column only names the account.
--
-- The columns start empty, so an existing account's next SSO login is refused
-- rather than adopted: the server cannot tell a pre-provisioned account from
-- one an attacker created with someone else's address.

ALTER TABLE principal ADD COLUMN IF NOT EXISTS idp_resource_id text NOT NULL DEFAULT '';
ALTER TABLE principal ADD COLUMN IF NOT EXISTS idp_subject text NOT NULL DEFAULT '';

-- One account per identity provider subject. Partial on the empty binding so
-- the password accounts, which have no subject, do not collide with each other.
CREATE UNIQUE INDEX IF NOT EXISTS idx_principal_unique_idp_subject ON principal (idp_resource_id, idp_subject) WHERE idp_resource_id <> '';
