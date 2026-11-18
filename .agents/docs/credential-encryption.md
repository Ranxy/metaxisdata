# Credential encryption — Reference

> Maintenance reference for `backend/common/crypto` and the credential paths of `backend/store` / `backend/server`. Related: the "Credentials and sessions" section of [docs/security-posture.md](../../docs/security-posture.md) (the deployment posture, not repeated here) and the `ENCRYPTION_KEY` setting (`SettingName_ENCRYPTION_KEY`).

## What it is

Instance and data-source credentials and the LLM profile API key are stored as `"v1:" + base64(nonce ‖ ciphertext ‖ tag)` — AES-256-GCM with a fresh 12-byte nonce per value (`crypto.Encrypt`, `crypto.Decrypt`); an empty plaintext stays empty. The encrypted fields are exactly the `credentialFields` pairs plus `api_key_ciphertext` on an LLM profile. The data key is 32 random bytes in the `ENCRYPTION_KEY` setting, minted on first startup by `initializeSetting`; `AUTH_SECRET` signs JWTs only. `METAXISDATA_ENCRYPTION_KEY` (`crypto.KeyEnvironment`) optionally wraps that key; `METAXISDATA_ENCRYPTION_KEY_PREVIOUS` (`crypto.PreviousKeyEnvironment`) opens a wrapped key during a rotation. Nothing is migrated from the retired scheme: such a row reads back empty with no error, and its next write drops the old value.

## Decisions

| Decision | Why |
| --- | --- |
| AES-256-GCM, `v1:` prefixed, fresh nonce per value | Authenticated: a wrong key or an edited value errors instead of decrypting to garbage the next write would store as the new plaintext. |
| Data key separate from `AUTH_SECRET` | A signing key that leaks with a token must not be a decryption key. |
| One generated data key; KEK optional | Unset is zero-config, at the accepted cost that database read access recovers every credential; set wraps the data key with a key kept outside the database. |
| Rotation without re-encryption | Retired KEKs only ever **open** a wrapped key, never wrap one, so retiring a key cannot promote it back; a key under a retired KEK is re-wrapped in place. |
| Typed, tested field table | `credentialFields` pairs plaintext with `*_ciphertext` and a name used in errors; a guard test walks the stored messages, so only a field named with none of the credential words escapes it. |
| LLM key never in the store message | `LLMProfileMessage` carries `Metadata` (the row) and `APIKey` (memory only), so re-marshalling metadata cannot store the key in the clear. |

## Invariants

1. **Fail closed at startup** — an empty, unusable or unopenable `ENCRYPTION_KEY`, a wrapped key with no KEK configured, or a KEK that opens nothing stops the server (`store.ResolveCipher` → `loadCredentialCipher` → `unwrapDataKey`, invoked by `resolveCredentialCipher`). A data key is minted **only when the setting is absent** (`initializeSetting`), so a database that kept credentials but lost the row fails rather than running on a key that opens none of them.
2. **A resolved key must open a stored credential before it is used** — `checkCipherAgainstCandidates` samples at most `credentialSampleSize = 10` rows and accepts the first that opens, so a hand-edited key or one restored from another deployment is refused. The check runs **before** a configured KEK would re-wrap the key (`loadCredentialCipher`), so a substituted key is never blessed by being wrapped under the real one. No stored credential means nothing to prove.
3. **A store without an installed cipher refuses every credential read and write** — `credentialCipher()` errors and callers propagate it; the server never serves such a store.
4. **No plaintext reaches a stored row** — `encryptInstanceMetadata` clears each plaintext field as it writes the ciphertext, and `encryptLLMProfileKey` stores only the ciphertext.
5. **A per-row failure stays per-row** — one damaged credential is the running server's error naming the field and instance, not a startup outage; a write to `ENCRYPTION_KEY` while the process runs only takes effect at the next start, where it is refused.

## Open items

These bound what this subsystem does and are tracked as open findings in [security-open-items.md](security-open-items.md) (the AAD residual and the `ssl_ca` redaction gap).

- **No AAD binding.** The ciphertext is not bound to its field or row (`Seal(..., nil)` and `Open(..., nil)`): database *write* access can move one instance's ciphertext into another's row. That access already permits repointing the row, so an AAD is the remedy only if it ever matters.
- **`idp.config.client_secret` is encrypted in place** by `backend/store/idp.go` (`decryptIdentityProviderConfig` / `encryptIdentityProviderConfig`), registered in `idpCredentialFields` with a guard test. It uses no `_ciphertext` sibling — the whole config is one JSONB column — so the ciphertext replaces the field, and a row written before the encryption existed is **refused** rather than read as plaintext. See [sso.md](sso.md).
- **`AUTH_SECRET` still lives in the database**, so database read access still forges a token; moving the signing key out is a separate decision.
- **No data-key rotation.** Replacing the data key would require re-encrypting every credential, which nothing needs before the deployment ships.
- **Nothing re-verifies the data key while the process runs** (see invariant 5).
- **Three pre-existing clear paths:** an LLM profile's `base_url` is read from the row and used with the decrypted key; `extra_connection_parameters` is free-form and can carry e.g. `sslpassword` beside the encrypted `ssl_key`; and the audit redactor's name list misses `ssl_ca` (`IsSensitiveAuditField` matches `sslkey`/`sslcert` but not `sslca`).

## Where things live

- Cipher and key parsing: `backend/common/crypto/crypto.go`; tests `crypto_test.go`.
- Key lifecycle: `backend/store/encryption.go` (`ResolveCipher`, `loadCredentialCipher`, `verifyCipher`, `checkCipherAgainstCandidates`, `unwrapDataKey`); tests `backend/store/encryption_test.go`, `instance_test.go` (field guard).
- Wiring: `backend/server/init.go` (`initializeSetting`) and `backend/server/encryption.go` (`resolveCredentialCipher`), called from the server startup path next to `resolveJWTSecret`.
- Encrypted fields: `backend/store/instance.go` (`credentialFields`, `encryptInstanceMetadata`, `decryptInstance`), `backend/store/llm.go` (`encryptLLMProfileKey`, `CreateLLMProfile`).
- Integration: `backend/test/integration/runner/instance_credential_encryption_service_test.go`.
- Gate: `gofmt`, `golangci-lint run --allow-parallel-runners`, `go test ./...`, build; integration via `make test-integration-smoke`.
