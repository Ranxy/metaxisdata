# SSO and identity providers — Reference

> Maintenance reference for `IdentityProviderService` (`backend/api/v1/identity_provider_service.go`), the `idp` / `principal_idp_binding` tables (`backend/store/idp.go`, `backend/store/idp_binding.go`), the SSO paths of `AuthService` (`backend/api/v1/auth_service.go`, `backend/api/v1/sso_identity.go`), the OAuth2 plugin (`backend/plugin/idp/`), and the SPA flow (`frontend/src/lib/oauth.ts`, `frontend/src/pages/LoginCallbackPage.vue`). Related: [iam-and-permissions.md](iam-and-permissions.md) (`identityProviders.*`), [credential-encryption.md](credential-encryption.md) (the client secret), [docs/security-posture.md](../../docs/security-posture.md) (accepted decisions).

## What it is

A workspace administrator configures OAuth 2.0 providers; the login page lists them anonymously and sends the browser through the authorization-code flow; the server exchanges the code, reads the user info, and resolves the account **by the provider subject** it is bound to. `IdentityProviderService` is the only write path — validation, encrypted secret, secret redaction, a test exchange — so no provider is configured by hand-written SQL.

Only **OAuth2** is implemented. `proto/store/store/idp.proto` reserves the OIDC and LDAP enum values and config fields, `idp_type_check` accepts `'OAUTH2'` alone, the login path rejects anything else, and `IdentityProviderService` repeats the reservation so the API cannot drift from the row shape.

## The flow, end to end

1. `ListIdentityProviders` (public) feeds the buttons, and the workspace setting supplies `external_url`. `LoginPage.vue` also accepts `?sso=idps/{idp}` (a bare id works) and starts that flow without a click, matched against the full resource name, never by suffix — that link is what a workspace hands out when password sign-in is off.
2. `CreateSSOState` issues a 32-byte nonce used twice over: the server stores it in the process-local `SSOStateCache` (5-minute TTL, capacity 4096, budgeted 120/min per source and 300/min globally), and the client sends the same value to the provider as `state`. The server consumes its copy at login and the client matches its stored record at the callback, so the two checks cannot be satisfied separately.
3. The client generates a PKCE verifier and sends the S256 challenge; `Login` accepts `code_verifier`. The callback page reads `code` and `state`, consumes its session-storage record, and calls `Login` with `idp_name`, the code, the nonce and the verifier.
4. `getOrCreateUserWithIDP` resolves the provider, consumes the nonce, requires `external_url`, exchanges the code with the stored secret, reads the user info, validates the subject mapping, resolves the binding, then adopts, creates or refuses the account. A successful login gets the same treatment as a password login: access cookie, rotating refresh token, group sync.

## Decisions

| Decision | Why |
| --- | --- |
| One nonce for the OAuth `state` parameter and the server's own nonce field | The server validates its own value rather than the provider's echo; using one value keeps the client's check and the server's from being satisfiable separately. |
| The account is resolved by `(idp, subject)`, never by the address claim | An address claim is mutable: whoever can set it at the provider would otherwise take over the account answering to it. Opt in per provider with `sso_email_identity_idps`. |
| `client_secret` is encrypted in place inside the `config` JSONB; no read returns it, and an empty secret on update keeps the stored one | The whole config is one column, so a sibling `_ciphertext` field would be schema churn; the client cannot send back what it was never given. `TestIdentityProvider` re-reads the stored secret, and only a saved provider can be tested, because the exchange needs it. The test reuses `{external_url}/oauth/callback`, so no second callback has to be registered; the stored record's `mode` decides whether the callback signs in or reports a result. |
| A provider id is immutable, and unbind is addressed by `(user, idp, subject)` | Bindings are keyed by `(idp_resource_id, subject)` and survive a delete, so the id is load-bearing. A subject is an opaque provider string that may contain `/`, `@` and `:` and cannot be a resource-name segment; the principal id is part of the unbind predicate so a disagreement cannot delete another account's row. |
| `FieldMapping.groups` is read from a claim, and an unreadable claim means "unchanged" | Group sync clears memberships when a provider reports none; treating an unparsable claim as "no groups" would revoke a whole account's roles on a provider quirk. An array that yields no name at all (every entry an object) also counts as unreadable. |
| `idpCredentialFields` is a table of accessors, and `idpMu` orders the provider cache against its own writes | The registry is what `encryptIdentityProviderConfig` and `decryptIdentityProviderConfig` iterate, so adding an entry is what encrypts a field. A stale cache entry for a just-deleted provider would keep authenticating accounts, because the login path reads providers from the cache and a deleted provider's bindings survive on purpose. |

## Invariants

1. **A credential that cannot be read fails closed, and no read returns the secret.** `decryptIdentityProviderConfig` errors on a value that is not a `v1:` ciphertext this deployment can open and never passes stored bytes through as the credential; `GetIdentityProvider` and the login path report that row, while `ListIdentityProviders` — the anonymous endpoint the login page renders from — skips it with a warning so one hand-edited row cannot remove every sign-in button. `convertIdentityProviderConfigFromStore` blanks `client_secret` for `Get`/`List`, the update response carries only what the caller sent, and the audit payload redacts the field by name.
2. **The credential registry drives encryption, and a guard walks the stored config.** `TestEveryIdentityProviderCredentialIsEncrypted` walks every message reachable from `IdentityProviderConfig`, including nested ones, and fails on a credential-looking field missing from the table and on a table entry with no stored field. Public material matching a credential word (`auth_url`, `token_url`, `auth_style`) is argued in `unencryptedCredentialLikeFields`.
3. **A login is refused before the provider is contacted when the mapping cannot resolve a subject.** `validateIDPSubjectMapping` runs first, so a misconfiguration is reported as one; `TestIdentityProvider` runs the same check.
4. **The nonce is single-use and TTL-bounded, and the state cache is process-local.** A create and its login must reach the same replica: one replica, or sticky routing. The `createSSOState` budget bounds anonymous growth of the cache, and `ssoStateCapacity` is sized above what that budget can admit within a nonce's lifetime.
5. **Neither deactivation nor adoption is reversible by a login.** A deactivated account stays deactivated (`Unauthenticated`; the login never sets `deleted = false`). Adoption is deliberate and one-way: only a provider listed in `sso_email_identity_idps` can take over an existing account, and only one with no binding at all — its password is replaced with a random one, and `profile.last_change_password_time` retires the earlier sessions — while an account already signing in through a provider merely gains another way in.
6. **A changed provider subject does not silently move the account.** Bindings are `(idp, subject)`, so a new subject value leaves the old binding unresolvable. Without `sso_email_identity_idps` the login is refused while the address belongs to an active account, and an administrator must move or delete the address first; with it, the new subject is bound to the account at that address and adoption happens only when the account had no binding. Deleting a provider leaves its bindings, so re-creating the id revives those accounts.
7. **The first SSO-created account can become the workspace administrator.** `CreateUser` grants the role to the first active `END_USER`, exactly as signup does.
8. **`idp_type_check` and the service agree, and a permission annotation matches the button that offers it.** The API only creates `'OAUTH2'`; `DeleteIdentityProvider` requires `identityProviders.delete` and `DeleteIdentityProviderBinding` requires `.update`, which is what the settings page gates those buttons on.
9. **The `idpCache` is invalidated on every write, within the process, and a populate cannot outrun one.** `idpMu` is held across the read-and-populate of `Get`/`List` and across the write-and-invalidate of `Create`/`Update`/`Delete`, so a delete cannot land between the `SELECT` and the cache `Add`. The cache holds 256 entries, is process-local and has no TTL, so a write on one replica never invalidates another's entry.

## Failure modes

| Scenario | Behavior |
| --- | --- |
| The provider type is not `OAUTH2`, `external_url` is unset, or `fieldMapping.subject` is empty or equals the identifier (and the provider is not in `sso_email_identity_idps`) | `FailedPrecondition` before the provider is contacted, on the login and on `TestIdentityProvider`; the message names both ways out |
| The state nonce is unknown, already consumed or older than `SSOStateTTL` | `PermissionDenied`; the browser has to restart the flow |
| The provider row cannot be decrypted, or does not exist | `Get` and the login fail (`Internal` / `NotFound`); `List` skips the row |
| `(idp, subject)` is already bound to another account | `FailedPrecondition` ("sign in again") from the binding write; the existing binding is never moved |
| An active account holds the address but is not bound to this provider, and the provider is not in `sso_email_identity_idps` | `FailedPrecondition`: an administrator must move or delete the address, or list the provider |
| The account is deactivated | `Unauthenticated` after the provider exchange; the login neither undeletes it nor syncs its groups |
| The subject claim changes at the provider | Invariant 6: the old binding stops resolving — refused while the address belongs to an active account, or bound afresh when the provider is listed in `sso_email_identity_idps` |

## Testing

Hermetic: `backend/store/idp_test.go` (round trip, fail-closed read, guard walk), `backend/api/v1/sso_identity_test.go`, `backend/api/v1/sso_state_test.go`, `backend/plugin/idp/util_test.go`, `backend/plugin/idp/oauth2/oauth2_test.go` (config validation, exchange, claim mapping including groups, TLS verify), `frontend/src/lib/oauth.test.ts`, `frontend/src/composables/useSSOLogin.test.ts`. Real server (`-tags integration`): `backend/test/integration/runner/sso_login_service_test.go` (subject binding, address squatting, delete-frees-address, repeat login, deactivated account, state budget, caller cancellation), `sso_email_identity_service_test.go` (adoption semantics), `identity_provider_service_test.go` (CRUD, ciphertext at rest, secret redaction, keep-on-empty and replace branches, permissions, bindings). Providers are registered through the admin API; a hand-written row carries a plaintext secret the server refuses.

## Where things live

| Path | Holds |
| --- | --- |
| `proto/v1/v1/identity_provider_service.proto` and `proto/store/store/idp.proto` | Admin surface (`Get`/public `List`/`Create`/`Update`/`Delete`/`Test`, bindings) and the row shape (`IdentityProviderConfig`, `FieldMapping`, `IdentityProviderUserInfo`) |
| `backend/store/idp.go` and `backend/store/idp_binding.go` | CRUD, the cipher round trip and cache; binding lookup, account adoption (`BindAccountForSSO`), binding list and unbind |
| `backend/api/v1/{identity_provider_service,sso_identity,auth_service}.go` | Validation, conversions, secret redaction, the test exchange (`validateIDPSubjectMapping`, `idpLoginSubject`), and `Login` / `CreateSSOState` / `getOrCreateUserWithIDP` / `syncUserGroups` |
| `backend/plugin/idp/oauth2/oauth2.go`, `backend/plugin/idp/util.go` | `NewIdentityProvider`, `ExchangeToken` (PKCE), `UserInfo`; dotted claim lookup and group-claim normalization |
| `frontend/src/lib/oauth.ts`, `frontend/src/composables/useSSOLogin.ts`, `frontend/src/pages/LoginCallbackPage.vue`, `frontend/src/pages/settings/IdentityProviderSettingsPage.vue` | Nonce record, PKCE, authorize URL, redirect sanitization, the callback modes, the admin page and its linked accounts |

## Open items

- **No OIDC.** The proto reserves it, but there is no discovery, JWKS or `id_token` validation, and the plugin treats an OIDC provider as plain OAuth2 userinfo. Adding it is a plugin plus an `id_token` verifier, not a config change.
- **`skip_tls_verify` is still honored.** The form defaults it off and warns prominently while it is on, but nothing prevents enabling it (see [security-open-items.md](security-open-items.md)).
- **Group sync matches the group's ID only.** `syncUserGroups` compares the claim against `group.email`; a group's title is deliberately not consulted, so a provider's group names must equal the workspace group IDs.
- **A stored client secret cannot be cleared,** and the audit redactor hides `tokenUrl` (substring match on "token" in `IsSensitiveAuditField`), while `authUrl` is not caught — so the ledger is inconsistent about the two endpoints. Exempting endpoint fields is the fix, but the redactor is shared and a wrong exemption would hide a real secret. Removing a provider also leaves its binding rows behind by design, so a workspace that never re-creates one accumulates unreachable rows that only `ListIdentityProviderBindings` surfaces.
