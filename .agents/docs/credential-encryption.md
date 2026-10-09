# Credential encryption — Reference

> Status: **implemented**. Maintenance reference for `backend/common/crypto` and the credential paths of `backend/store` / `backend/server`. Related: `docs/security-posture.md:7` — the deployment posture and the retired scheme's post-mortem, not repeated here; and `backend/generated-go/store/setting.pb.go:38` (`SettingName_ENCRYPTION_KEY = 16`).

## What it is

Instance/data-source credentials and the LLM profile API key are stored as `"v1:" + base64(nonce ‖ ciphertext ‖ tag)` — AES-256-GCM with a fresh 12-byte nonce per value (`backend/common/crypto/crypto.go:29`, `:127`); an empty plaintext stays empty. The encrypted fields are exactly the `credentialFields` pairs (`backend/store/instance.go:370`) plus `api_key_ciphertext`. The data key is 32 random bytes in the `ENCRYPTION_KEY` setting, minted on first startup (`backend/server/init.go:49`); `AUTH_SECRET` signs JWTs only. `METAXISDATA_ENCRYPTION_KEY` optionally wraps that key, and `METAXISDATA_ENCRYPTION_KEY_PREVIOUS` opens a wrapped one during a rotation (`crypto.go:38`, `:43`). Nothing is migrated from the retired scheme: such a row reads back **empty with no error** (the tolerant unmarshaler discards its old JSONB key, `backend/common/utils.go:18`) and loses the old value on its next write.

## Decisions

| Decision | Why |
| --- | --- |
| AES-256-GCM, `v1:` prefixed, fresh nonce per value | Authenticated: a wrong key or an edited value errors instead of decrypting to garbage the next write would store as the new plaintext. |
| Data key separate from `AUTH_SECRET` | A signing key that leaks with a token must not be a decryption key; the retired scheme shared one secret. |
| One generated data key; KEK optional | Unset is zero-config, at the accepted cost that database read access recovers every credential; set wraps the data key with a key kept outside the database. |
| Rotation without re-encryption | Retired KEKs only ever **open** a wrapped key, never wrap one, so retiring a key cannot promote it back; a key under a retired KEK is re-wrapped in place. |
| Typed, tested field table | `credentialFields` pairs plaintext with `*_ciphertext` and a name used in errors; a guard test walks the stored messages, so only a field named with none of the credential words escapes it. |
| LLM key never in the store message | `LLMProfileMessage` carries `Metadata` (the row) and `APIKey` (memory only), so re-marshalling metadata cannot store the key in the clear (`backend/store/llm.go:23`). |

## Invariants

1. **Fail closed at startup** — an empty, unusable or unopenable `ENCRYPTION_KEY`, a wrapped key with no KEK configured, or a KEK that opens nothing stops the server (`backend/store/encryption.go:90`, `:235`, `:248`; `backend/server/encryption.go:33`). A data key is minted **only when the setting is absent** (`backend/server/init.go:53`), so a database that kept credentials but lost the row fails rather than running on a key that opens none of them.
2. **A resolved key must open a stored credential before it is used** — `checkCipherAgainstCandidates` samples at most `credentialSampleSize = 10` rows and accepts the first that opens (`encryption.go:134`, `:209`), so a hand-edited key or one restored from another deployment is refused. The check runs **before** a configured KEK would re-wrap the key (`encryption.go:107`–`:128`), so a substituted key is never blessed by being wrapped under the real one. No stored credential means nothing to prove.
3. **A store without an installed cipher refuses every credential read and write** — `credentialCipher()` errors (`encryption.go:25`) and callers propagate it; the server never serves such a store.
4. **No plaintext reaches a stored row** — `encryptInstance` clears each plaintext field as it writes the ciphertext (`backend/store/instance.go:323`).
5. **A per-row failure stays per-row** — one damaged credential is the running server's error naming the field and instance, not a startup outage; a write to `ENCRYPTION_KEY` while the process runs only takes effect at the next start, where it is refused.

## Open items

Also on `.agents/docs/security-open-items.md` (the AAD residual, the IdP secret and L5); kept here because they bound what this subsystem does.

- **No AAD binding.** The ciphertext is not bound to its field or row (`Seal(..., nil)`, `crypto.go:135`): database *write* access can move one instance's ciphertext into another's row. That access already permits repointing the row, so an AAD is the remedy only if it ever matters.
- **`idp.config.client_secret` is still plaintext** (`proto/store/store/idp.proto:37`); its only write path is hand-edited SQL, so encrypting it would force an operator to compute a ciphertext by hand.
- **`AUTH_SECRET` still lives in the database**, so database read access still forges a token; moving the signing key out is a separate decision.
- **No data-key rotation.** Replacing the data key would require re-encrypting every credential, which nothing needs before the deployment ships.
- **Nothing re-verifies the data key while the process runs** (see invariant 5).
- **Three pre-existing clear paths, unchanged:** an LLM profile's `base_url` is read from the row and used with the decrypted key; `extra_connection_parameters` is free-form and can carry e.g. `sslpassword` beside the encrypted `ssl_key`; and the audit ledger's redaction list misses `ssl_ca` (`backend/component/audit/audit.go:224` redacts the credential words, but not `ssl_ca`).

## Where things live

- Cipher + key parsing: `backend/common/crypto/crypto.go`; tests `crypto_test.go`.
- Key lifecycle: `backend/store/encryption.go` (`ResolveCipher`, `loadCredentialCipher`, `verifyCipher`, `checkCipherAgainstCandidates`, `unwrapDataKey`); tests `backend/store/encryption_test.go`, `instance_test.go` (field guard at `:147`).
- Wiring: `backend/server/init.go` (`initializeSetting`) and `backend/server/encryption.go` (`resolveCredentialCipher`), called at `backend/server/server.go:132`, `:140`.
- Encrypted fields: `backend/store/instance.go` (`encryptInstance`/`decryptInstance`), `backend/store/llm.go` (`encryptLLMProfileKey`, `CreateLLMProfile`).
- Integration: `backend/test/integration/runner/instance_credential_encryption_service_test.go`.
- Gate: `gofmt`, `golangci-lint run --allow-parallel-runners`, `go test ./...`, build; integration via `make test-integration-smoke`.
