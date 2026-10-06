-- The identity provider subjects an account may sign in with.
--
-- SSO used to resolve a login by the email claim alone: anyone who had already
-- registered that address -- by self-service signup, or by moving their own email
-- onto it -- owned the account, and the real employee's SSO login landed in it.
-- The binding is the identity a login matches from now on; the email column only
-- names the account. A row that carries no binding is not adopted by an email
-- match either: whoever created it still holds its password, and the server cannot
-- tell a pre-provisioned account from one an attacker created with someone else's
-- address.
--
-- The table keys one principal per (provider, subject), so an account is reachable
-- through every provider it is bound to: one workspace can configure several, and
-- a person -- or a whole set of users moving from one provider to another -- may
-- be enrolled in more than one of them. A single pair of columns on the principal
-- row could not express that: the second provider's login was refused with
-- "already linked to another subject", and switching a user's provider meant
-- deleting the account and losing its roles and history.
--
-- This file originally added those two columns, and a later one moved the bindings
-- into the table below. The release carrying either was never deployed, so the two
-- were merged into this single file before anyone ran them: no database holds a
-- binding in the columns, and none keeps them.

CREATE TABLE IF NOT EXISTS principal_idp_binding (
    principal_id integer NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
    idp_resource_id text NOT NULL,
    subject text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (idp_resource_id, subject)
);

CREATE INDEX IF NOT EXISTS idx_principal_idp_binding_principal ON principal_idp_binding (principal_id);
