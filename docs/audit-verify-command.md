# `glassbox audit:verify` — Audit Log Verification Reference

The `audit:verify` command checks the integrity and authenticity of a signed audit log produced by [`audit:sign`](./audit-signing.md). It re-derives the payload hash, compares it to the embedded `trace_hash`, verifies the Ed25519 signature, and — when requested — validates the payload against a JSON schema and verifies the log's place in a tamper-evident **audit chain**.

Certificate and attestation verification can additionally be controlled through a configurable **trust policy** that specifies trust roots, issuer allowlists, validity windows, and revocation policy, rather than relying on ambient machine state.

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

### Trust Policy Flags

| Flag | Description |
|---|---|
| `--trust-roots <pem-file>` | Path to a PEM file of trusted root CA certificates. Only certificate chains anchored in these roots are accepted. |
| `--allowed-issuers <csv>` | Comma-separated list of allowed certificate issuer DN substrings. Untrusted issuers are explicitly identified in the output. |
| `--check-validity` | Enforce certificate `notBefore`/`notAfter` validity windows. The reference time is the log's own timestamp. |
| `--revoked-certs <csv\|file>` | Comma-separated list, or path to a text file, of revoked certificate serial numbers (hex). |
| `--revocation-mode <mode>` | `strict` (default when `--revoked-certs` is set): revoked certs fail the policy. `none`: informational only. |
| `--policy-config <json-file>` | Load a full `TrustPolicyConfig` from a JSON file. CLI flags always take precedence over file values. |

---

## Default Behaviour

When **no trust policy flags are set**, `audit:verify` runs in **cryptographic verification only** mode:

- Derives and compares the payload hash.
- Verifies the Ed25519 signature.
- Does **not** evaluate certificate issuers, validity windows, or revocation.

This is the documented safe default for offline audit review. The `policy` field is absent from JSON output in this mode.

---

## Trust Policy Evaluation

Changing any trust policy flag affects **only** the policy outcome, never the underlying cryptographic result. The two are displayed as separate sections:

```
Cryptographic Verification
  [PASS] Payload hash
  [PASS] Signature

Trust Policy
  [FAIL] Issuer
  Untrusted issuers: CN=Unknown Corp
  Issue: untrusted issuer "CN=Unknown Corp" is not in allowed issuers list

Result: INVALID — audit log failed verification.
```

Untrusted issuers are always explicitly named so they can be reviewed rather than producing an opaque failure.

---

## Validation (fail-fast)

All inputs are validated **before the audit log is read**, in `PreRunE`:

1. `--audit-log` is required and must point to a readable file.
2. `--public-key`, when provided, must be valid hex of exactly 32 bytes (64 hex characters).
3. `--schema`, when provided, must point to an existing file.
4. `--previous-signature-hash`, when provided, must be a 64-character hex string.
5. `--trust-roots`, when a file path, must point to an existing file (inline PEM strings are also accepted).
6. `--policy-config`, when provided, must point to an existing JSON file.
7. `--revocation-mode`, when provided, must be `"strict"` or `"none"`.

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

Human-readable output lists each check as `[PASS]`/`[FAIL]` under two separate sections — **Cryptographic Verification** and **Trust Policy** — and ends with a `VALID` / `INVALID` result line. A failed verification exits non-zero.

```
Audit Log Verification
──────────────────────────────────────────────────
  Version:    1.0.0
  Provider:   software
  Key ID:     1a2b3c4d…7e8f9a0b
  Trace Hash: 5c0a…90ab

Cryptographic Verification
  [PASS] Payload hash
  [PASS] Signature
  [PASS] Provenance
  [PASS] Chain link

Trust Policy
  [PASS] Trust root
  [PASS] Issuer
  [PASS] Validity
  [PASS] Revocation

Result: VALID — audit log integrity confirmed.
```

With `--json`, the same result is emitted as an object including `hash_valid`, `signature_valid`, and (when applicable) `schema_valid`, `provenance_valid`, `chain_link_valid`, and a `policy` object containing `trust_root_valid`, `issuer_allowed`, `validity_valid`, `revocation_valid`, `issues`, and `untrusted_issuers`.

---

## Examples

```sh
# Verify with the embedded public key
glassbox audit:verify --audit-log signed-audit.json

# Independent verification with an out-of-band public key
glassbox audit:verify --audit-log signed-audit.json --public-key <hex>

# Validate payload structure against a schema
glassbox audit:verify --audit-log signed-audit.json --schema payload-schema.json

# Verify with explicit trust roots and issuer allowlist
glassbox audit:verify --audit-log signed-audit.json \
  --trust-roots /etc/glassbox/trusted-ca.pem \
  --allowed-issuers "CN=My HSM CA,OU=Security"

# Enforce validity windows and check for revoked certificates
glassbox audit:verify --audit-log signed-audit.json \
  --check-validity --revoked-certs deadbeef01,cafebabe02

# Load trust policy from a JSON file
glassbox audit:verify --audit-log signed-audit.json --policy-config trust-policy.json

# Verify this log links to the expected previous log in the chain
glassbox audit:verify --audit-log log-2.json --previous-signature-hash <hash-of-log-1>

# Machine-readable JSON
glassbox audit:verify --audit-log signed-audit.json --json
```

---

## trust-policy.json format

```json
{
  "trust_roots": ["/etc/glassbox/ca-bundle.pem"],
  "allowed_issuers": ["CN=My HSM Attestation CA", "OU=Security Signing"],
  "check_validity": true,
  "revoked_certificates": ["deadbeef01", "cafebabe02"],
  "revocation_policy": "strict"
}
```

---

## See Also

- [`glassbox audit:sign`](./audit-signing.md) — produce a signed audit log
- [Audit canonicalization](./audit-canonicalization.md) — how the payload hash is derived
- [KMS signing](./audit-kms-signing.md)
- [Glassbox configuration reference](../glassbox.example.toml) — `[audit.trust_policy]` section
