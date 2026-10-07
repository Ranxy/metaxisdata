# Credential encryption

Implementation plan for §5.1 of [docs/security-review-2026-10.md](../docs/security-review-2026-10.md):
stored instance/data-source credentials and LLM provider API keys are encrypted
at rest instead of obfuscated, and the key that encrypts them is no longer the
key that signs JWTs.

The project has not shipped, so no compatibility with the retired scheme is
kept: nothing is migrated, and a value it wrote fails the read that finds it.

## The problem the change removes

`common.Obfuscate`/`Unobfuscate` were a repeating-key XOR (period = key length)
of the 32-byte `AUTH_SECRET`, stored as base64. Four properties came with that:

- **Determinism.** Equal plaintexts produced equal ciphertexts, so the same
  password in two instances was visible in the database.
- **Known-plaintext key recovery.** The keystream had the key's own period, so 32
  bytes of known plaintext — a PEM header in `ssl_ca`, `ssl_cert` or `ssl_key` —
  recovered the keystream, and the keystream *was* the key.
- **One secret in two roles.** That recovered key also signed every JWT, so a
  readable `instance.metadata` row (the `setting` table was not needed) was
  enough to forge an administrator token.
- **Unauthenticated decryption.** A wrong seed decoded to garbage with no error,
  which the next write stored as the new plaintext.

## Design

- **AES-256-GCM, `v1:` prefixed.** `backend/common/crypto` encrypts to
  `"v1:" + base64(nonce || ciphertext || tag)` with a fresh 12-byte nonce per
  value. `Decrypt` refuses a value without the prefix, a body too short to be
  authentic, or one whose tag does not verify: an unreadable credential is an
  error naming the field and the instance, never garbage. An empty plaintext
  stays empty — "not set" is not a value with a ciphertext.
- **One data key, generated on first startup.** `initializeSetting` writes 32
  random bytes to the `ENCRYPTION_KEY` setting. `AUTH_SECRET` now signs JWTs
  only; the two are independent.
- **An optional key-encryption key (KEK).** With `METAXISDATA_ENCRYPTION_KEY`
  set, `Store.ResolveCipher` stores the data key wrapped (the same GCM format)
  under it, so a leaked table, replica or backup is not enough to decrypt
  anything. Unset is the zero-config default: the data key sits in the same
  database, so read access to the whole database still recovers the credentials
  — that is the accepted trade-off, and startup logs one line saying so.
- **Rotation without re-encryption.** Setting the KEK later wraps the existing
  data key on the next startup. `METAXISDATA_ENCRYPTION_KEY_PREVIOUS` (a
  comma-separated list) only ever opens a wrapped key; a data key found under a
  retired KEK is re-wrapped under the current one in place, so rotating the KEK
  touches no credential.
- **Fail closed.** A missing, empty or unusable `ENCRYPTION_KEY`, a wrapped key
  with no KEK configured, or a KEK that opens nothing stops startup. The server
  never serves a store whose cipher was not installed: `credentialCipher()`
  errors, and every credential read and write propagates that.
- **The field table is typed and tested.** `credentialFields` pairs each
  plaintext field with its `*_ciphertext` counterpart and a name used in errors,
  and a guard test walks the `DataSource` descriptor: every stored
  `*_ciphertext` field must be registered, and a registered field's plaintext and
  ciphertext fields must both exist. A new credential field that nobody
  registered turns the test red instead of being stored in the clear.
- **The LLM profile keeps plaintext out of the store message.** `LLMProfileMessage`
  carries `Metadata` (whose `api_key_ciphertext` is exactly what the row holds)
  and `APIKey` (the decrypted key, in memory only), so a caller that marshals the
  metadata back cannot store the key in the clear.

## Surface

| Area | Change |
| --- | --- |
| `backend/common/crypto` | New package: `Cipher`, `Encrypt`/`Decrypt`, key generation/parsing, `v1:` format |
| `backend/store` | `SetCipher`/`ResolveCipher`/`credentialCipher`, encrypted instance and LLM profile fields, `credentialFields` table |
| `backend/server` | `initializeSetting` generates the data key; `resolveCredentialCipher` reads the KEK environment and installs the cipher |
| `proto/store` | `obfuscated_*` → `*_ciphertext`, `api_key_encrypted` → `api_key_ciphertext`, `SettingName.ENCRYPTION_KEY` |
| `backend/common` | `Obfuscate`/`Unobfuscate` and their tests are deleted |

## Tests

- Unit (`backend/common/crypto`): round trip, fresh nonce per value, wrong key,
  edited nonce and tag, foreign formats, short values, key parsing.
- Unit (`backend/store`): the per-row decrypt covers every field and names the
  field and data source it failed on; a write carries no plaintext; a store with
  no cipher refuses to write; the `*_ciphertext` guard test; the wrapping and
  re-wrapping decisions in `unwrapDataKey`.
- Integration (`TestStoredInstanceCredentialsAreEncryptedRealServerIntegration`):
  the real server stores the admin password as a `v1:` ciphertext, the row
  contains no plaintext field, the server still connects with it (a database
  sync), the observer store reads it back, and the same password in a second
  instance produces a different ciphertext.

## Not done here

- **`idp.config.client_secret` is still plaintext** in the `idp` table. Its only
  write path is hand-edited SQL (there is no IdP API or UI), so encrypting it
  would mean an operator has to compute a ciphertext by hand. Recorded in
  `docs/security-review-2026-10.md` §10 as a new finding.
- **The JWT signing key still lives in the database** (`AUTH_SECRET`), so
  database read access still permits forging a token. Encrypting credentials and
  signing sessions with one key was the coupling this change removes; moving the
  signing key out is a separate decision.
- **No data-key rotation.** Replacing the data key would require re-encrypting
  every credential, which nothing needs before the deployment ships.
