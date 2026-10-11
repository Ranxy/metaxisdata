# SSO and identity providers — Reference

> Status: **implemented**. Maintenance reference for `IdentityProviderService` (`backend/api/v1/identity_provider_service.go`), the `idp` / `principal_idp_binding` tables (`backend/store/idp.go`, `:idp_binding.go`), the login half of `AuthService` (`backend/api/v1/auth_service.go:370`, `backend/api/v1/sso_identity.go`), the OAuth2 plugin (`backend/plugin/idp/`) and the SPA flow (`frontend/src/lib/oauth.ts`, `frontend/src/pages/LoginCallbackPage.vue`). Related: `.agents/docs/iam-and-permissions.md` (the `identityProviders.*` permissions), `.agents/docs/credential-encryption.md` (the client secret), `docs/security-posture.md` (the accepted decisions).

## What it is

A workspace administrator configures OAuth 2.0 providers; the login page lists them anonymously and sends the browser through the authorization-code flow; the server exchanges the code, reads the user info, and resolves the account **by the provider subject** it is bound to.

Before `IdentityProviderService` existed the `idp` table had no write path at all: a provider was configured by hand-written SQL and the client secret could not be encrypted, because encrypting it would have forced the operator to compute a ciphertext by hand. That is why the service and the encryption landed together.

Only **OAuth2** is implemented. `proto/store/store/idp.proto` reserves the OIDC and LDAP enum values and config fields, `idp_type_check` accepts `'OAUTH2'` alone, and the login path rejects anything else. The `IdentityProviderService` proto repeats the reservation so the API cannot drift from the row shape.

## The flow, end to end

1. `ListIdentityProviders` (public) feeds the buttons; `GetWorkspaceProfileSetting` (public) supplies `external_url`. `LoginPage.vue` also accepts `?sso=idps/{idp}` (a bare id works) and starts that flow without a click — matched against the full resource name, never by suffix. That link is what a workspace hands out when password sign-in is turned off.
2. `CreateSSOState` issues a 32-byte nonce. It is **the** value used twice over: the server stores it in the process-local `SSOStateCache` (TTL 5 min, capacity 4096, budgeted 120/min per source and 300/min globally) and the client sends the same value to the provider as the `state` parameter. One nonce, two checks — the server consumes it at login, the client matches its stored record against it at the callback, so the two cannot disagree.
3. The client generates a PKCE verifier and sends the S256 challenge. `Login` accepts `code_verifier`.
4. The callback page reads `code` and `state`, consumes the session-storage record, and calls `Login` with `idp_name` (`idps/{idp}`), the code, the nonce and the verifier.
5. The server: resolves the provider → consumes the nonce → requires `external_url` → exchanges the code with the stored secret → reads the user info → validates the subject mapping → resolves the binding → adopts, creates, or refuses the account.
6. A successful login gets the same treatment as a password login: access cookie, rotating refresh token, group sync.

## Decisions

| Decision | Why |
| --- | --- |
| One nonce for the OAuth `state` parameter and the server's nonce field | The server ignores the provider's echo of `state`; it validates its own. Using one value for both keeps the client's check and the server's check from being satisfiable separately. |
| The account is resolved by `(idp, subject)`, never by the address claim | An address claim is mutable: whoever can set it at the provider would otherwise take over the account that already answers to it. Opt out per provider with `sso_email_identity_idps`. |
| `client_secret` is encrypted in place inside the `config` JSONB | The whole config is one column; a sibling `_ciphertext` field would be schema churn for a value no caller ever reads. See "Invariants" below. |
| Reads never return the secret; an empty secret on update keeps the stored one | The client cannot send back what it was never given. `TestIdentityProvider` re-reads the stored secret for the same reason. |
| Only a saved provider can be tested | The code exchange needs the stored secret, and the browser must not hold it. |
| The test reuses `{external_url}/oauth/callback` as its redirect URI | No second callback has to be registered with the provider; the flow's `mode` in the stored record decides whether the callback signs in or reports a result. |
| A provider id is immutable, and re-creating the same id revives its accounts | Bindings are keyed by `(idp_resource_id, subject)` and survive a delete, so the id is load-bearing. |
| Binding unbind is addressed by `(user, idp, subject)` | A subject is an opaque provider string that may contain `/`, `@` and `:`; it cannot be a resource-name segment. The principal id is part of the delete predicate so a disagreement cannot delete another account's row. |
| `FieldMapping.groups` is read from a claim, and an unreadable claim means "unchanged" | Group sync clears memberships when a provider reports none. Treating an unparsable claim as "no groups" would revoke a whole account's roles on a provider quirk, so `idp.GroupValues` reports understood-ness separately. |

## Invariants

1. **A secret that cannot be decrypted fails the read.** `decryptIdentityProviderConfig` (`backend/store/idp.go`) errors on a value that is not a `v1:` ciphertext this deployment can open — it never passes the stored bytes through as the credential. A plaintext row inserted by hand is therefore unreadable, not silently accepted.
2. **No read path returns the secret.** `convertIdentityProviderConfigFromStore` blanks `client_secret` for `Get` and `List`, and the audit payload is redacted by field name.
3. **Every credential-like field of the stored config is registered with the cipher.** `TestEveryIdentityProviderCredentialIsEncrypted` walks `OAuth2IdentityProviderConfig` and fails on a credential-looking field that is not in `idpCredentialFields`. Public-by-design fields that match a credential word (`auth_url`, `token_url`, `auth_style`) are argued in `unencryptedCredentialLikeFields`.
4. **A login is refused before the provider is contacted when the mapping cannot resolve a subject.** `validateIDPSubjectMapping` runs first, so a misconfiguration is reported as one; `TestIdentityProvider` runs the same check so the test predicts the login.
5. **The nonce is single-use and TTL-bounded, and the state cache is process-local.** A create and its login must reach the same replica: one replica, or sticky routing. The `createSSOState` budget is what bounds anonymous growth of that cache, and its capacity (`ssoStateCapacity`) is derived from the global budget.
6. **An SSO login never revives a deactivated account.** Deactivation is final (`MemberDeleted` → `Unauthenticated`), and the login never sets `deleted = false`.
7. **Adoption is deliberate and one-way.** Only a provider listed in `sso_email_identity_idps` can take over an existing account, and only an account with no binding at all: its password is replaced with a random one and `profile.last_change_password_time` retires the earlier sessions.
8. **The first account created by an SSO login can become the workspace administrator.** `CreateUser` grants the role to the first active `END_USER`, exactly as signup does.
9. **`idp_type_check` and the service agree.** The API only creates `'OAUTH2'`; a row of any other type is rejected by the login path.
10. **The `idpCache` is invalidated on every write.** It previously had capacity 4 and no invalidation path, so a fifth provider thrashed and an edit needed a restart.

## Testing

Hermetic: `backend/store/idp_test.go` (round trip, fail-closed read, guard walk), `backend/api/v1/sso_identity_test.go`, `backend/api/v1/sso_state_test.go`, `backend/plugin/idp/util_test.go` (`GetValueWithKey`, `GroupValues`), `backend/plugin/idp/oauth2/oauth2_test.go` (config validation, exchange, claim mapping including groups, TLS verify), `frontend/src/lib/oauth.test.ts`, `frontend/src/composables/useSSOLogin.test.ts`.

Real server (`-tags integration`, needs Docker or `INTEGRATION_POSTGRES_*`): `backend/test/integration/runner/sso_login_service_test.go` (subject binding, address squatting, delete-frees-address, repeat login, deactivated account, state budget, caller cancellation), `…/sso_email_identity_service_test.go` (adoption semantics), `…/identity_provider_service_test.go` (CRUD, secret redaction, permissions, bindings). The providers are registered through the admin API — a hand-written row would carry a plaintext secret the server now refuses.

## Where things live

| Path | Holds |
| --- | --- |
| `proto/v1/v1/identity_provider_service.proto` | The admin surface: `Get`/public `List`/`Create`/`Update`/`Delete`/`Test`, plus `ListIdentityProviderBindings` and `DeleteIdentityProviderBinding` |
| `proto/store/store/idp.proto` | The row shape: `IdentityProviderConfig`, `FieldMapping`, `IdentityProviderUserInfo` |
| `backend/store/idp.go` | CRUD, the cipher round trip, the cache |
| `backend/store/idp_binding.go` | Binding lookup, account adoption, binding list and unbind |
| `backend/api/v1/identity_provider_service.go` | Validation, conversions, secret redaction, the test exchange |
| `backend/api/v1/sso_identity.go` | `validateIDPSubjectMapping`, `idpLoginSubject` |
| `backend/plugin/idp/oauth2/oauth2.go` | `NewIdentityProvider`, `ExchangeToken` (PKCE), `UserInfo` |
| `backend/plugin/idp/util.go` | Dotted claim lookup, group-claim normalisation |
| `frontend/src/lib/oauth.ts` | Nonce record, PKCE, authorize URL, redirect sanitisation |
| `frontend/src/composables/useSSOLogin.ts` | Issue the nonce, build the URL, leave the page |
| `frontend/src/pages/LoginCallbackPage.vue` | Both callback modes |
| `frontend/src/pages/settings/IdentityProviderSettingsPage.vue` | Admin page, including the linked accounts |

## Open items

- **The state cache is process-local** and is recorded in `docs/security-posture.md`; a database-backed nonce would remove the single-replica requirement.
- **No OIDC.** The proto reserves it, but there is no discovery, JWKS or `id_token` validation, and the plugin treats an OIDC provider as plain OAuth2 userinfo. Adding it is a plugin plus an `id_token` verifier, not a config change.
- **`skip_tls_verify` is still honoured.** The form defaults it off and warns prominently while it is on (`security-open-items.md` M22/D7), but nothing prevents it being enabled.
- **Group sync matches the group's ID only.** `syncUserGroups` compares the claim against `group.email`; a group's title is deliberately not consulted, so a provider's group names must equal the workspace group IDs.
- **No unbind on provider delete.** Removing a provider leaves its binding rows behind by design (re-creating the id revives them), so a workspace that never re-creates one accumulates unreachable rows. `ListIdentityProviderBindings` is how they are seen.
