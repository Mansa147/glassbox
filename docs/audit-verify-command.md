# `glassbox audit:verify` — Audit Log Verification Reference

The `audit:verify` command checks the integrity and authenticity of a signed audit log produced by [`audit:sign`](./audit-signing.md). It re-derives the payload hash, compares it to the embedded `trace_hash`, verifies the Ed25519 signature, and — when requested — validates the payload against a JSON schema and verifies the log's place in a tamper-evident **audit chain**.

---

## Synopsis

```
glassbox audit:verify --audit-log <file> [flags]
```

---

## Flags

| Flag | Description |
|---|---|
| `--audit-log` | Path to the signed audit log JSON file. **Required.** |
| `--public-key` | Hex-encoded Ed25519 public key (64 hex chars) that overrides the key embedded in the log, for independent verification. |
| `--schema` | Path to a JSON schema file to validate the payload structure against. |
| `--previous-signature-hash` | Expected hex SHA-256 of the previous log in the chain. When set, verifies that this log links to that predecessor. |
| `--json` | Emit the verification result as machine-readable JSON. |

---

## Validation (fail-fast)

All inputs are validated **before the audit log is read**, in `PreRunE`:

1. `--audit-log` is required and must point to a readable file.
2. `--public-key`, when provided, must be valid hex of exactly 32 bytes (64 hex characters).
3. `--schema`, when provided, must point to an existing file.
4. `--previous-signature-hash`, when provided, must be a 64-character hex string.

After the log is parsed, its **required fields** are checked before any cryptographic work. A log missing `signature`, `trace_hash`, `public_key`, or `payload` is rejected with an explicit message naming the missing field(s):

```
audit log is missing required field(s): signature
  A signed audit log must contain signature, trace_hash, public_key, and payload.
  If the public key is provided separately, pass it with --public-key.
```

(If `--public-key` is supplied, an absent embedded `public_key` is allowed.)

---

## Audit chain integrity

Signed logs may carry a `provenance.previous_signature_hash` that links each log to its predecessor, forming a tamper-evident chain (see [`audit:sign --previous-signature-hash`](./audit-signing.md)).

**Verifying a link:** pass the expected predecessor hash with `--previous-signature-hash`. The command compares it (case-insensitively) to the log's `previous_signature_hash` and reports a `Chain link` check:

```sh
glassbox audit:verify --audit-log log-2.json --previous-signature-hash <hash-of-log-1>
```

| Condition | Result |
|---|---|
| Hashes match | `[PASS] Chain link` |
| Hashes differ | `[FAIL] Chain link` + `chain link broken: log's previous_signature_hash … does not match the expected predecessor …` |
| Log has no `previous_signature_hash` | `[FAIL] Chain link` + a message noting it may be the genesis entry, or was signed without provenance |

**Not verifying a link:** if a log carries a `previous_signature_hash` but you do **not** pass `--previous-signature-hash`, the hash is only **format-checked**, not verified against a real predecessor. To avoid a passing report being mistaken for a verified chain, the command prints an explicit note:

```
Note: previous_signature_hash is present but chain linkage was not verified;
pass --previous-signature-hash <hex> to confirm this log links to the expected predecessor
```

---

## Output

Human-readable output lists each check as `[PASS]`/`[FAIL]` and ends with a `VALID` / `INVALID` result line. A failed verification exits non-zero.

```
Audit Log Verification
──────────────────────────────────────────────────
  Version:    1.0.0
  Provider:   software
  Key ID:     1a2b3c4d…7e8f9a0b
  Trace Hash: 5c0a…90ab

  [PASS] Payload hash
  [PASS] Signature
  [PASS] Provenance
  [PASS] Chain link

Result: VALID — audit log integrity confirmed.
```

With `--json`, the same result is emitted as an object including `hash_valid`, `signature_valid`, and (when applicable) `schema_valid`, `provenance_valid`, and `chain_link_valid`.

---

## Examples

```sh
# Verify with the embedded public key
glassbox audit:verify --audit-log signed-audit.json

# Independent verification with an out-of-band public key
glassbox audit:verify --audit-log signed-audit.json --public-key <hex>

# Validate payload structure against a schema
glassbox audit:verify --audit-log signed-audit.json --schema payload-schema.json

# Verify this log links to the expected previous log in the chain
glassbox audit:verify --audit-log log-2.json --previous-signature-hash <hash-of-log-1>

# Machine-readable JSON
glassbox audit:verify --audit-log signed-audit.json --json
```

---

## See Also

- [`glassbox audit:sign`](./audit-signing.md) — produce a signed audit log
- [Audit canonicalization](./audit-canonicalization.md) — how the payload hash is derived
- [KMS signing](./audit-kms-signing.md)

---

## Architecture Decision Records

The following ADRs govern the design decisions behind audit verification:

- [ADR-005: Canonicalization Ownership](adr/005-canonicalization-ownership.md) — specifies the four-step verification procedure (canonical bytes → SHA-256 → hash comparison → Ed25519 verify) and the stability guarantees that make it reproducible offline.
- [ADR-007: Offline Guarantees](adr/007-offline-guarantees.md) — confirms that `audit:verify` requires no network access and remains functional indefinitely from only the log file and the public key.
- [ADR-003: Trust Boundaries and Component Trust Levels](adr/003-trust-boundaries.md) — documents the trust model under which out-of-band public key verification (`--public-key`) is valid.
