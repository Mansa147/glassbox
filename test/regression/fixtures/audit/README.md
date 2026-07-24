# Audit Fixtures

Test payloads, pre-signed audit log JSON files, and Ed25519 test key material
for audit-layer regression tests.

## File naming

```
<scenario>_<issue-or-pr-slug>.payload.json     # unsigned input payload
<scenario>_<issue-or-pr-slug>.signed.json      # expected SignedAuditLog output
<scenario>_<issue-or-pr-slug>.key.pem          # PKCS#8 Ed25519 test key (TEST USE ONLY)
```

Examples:
- `empty_payload_rejected.payload.json`
- `canonical_hash_deterministic.payload.json`
- `pkcs11_missing_module_error.payload.json`
- `testonly_ed25519.key.pem`

## Rules

- ALL keys in this directory are for TEST USE ONLY and must be clearly labeled
  as such in the file itself and the filename (`testonly_`).
- Never commit real private keys, HSM PINs, or production signing material.
- Signed fixture files must be regenerated when the canonicalization algorithm
  changes; include a `# regenerate: glassbox audit:sign ...` comment in the
  fixture header when possible.
- Payloads must use the synthetic timestamp `2026-01-01T00:00:00.000Z`.
