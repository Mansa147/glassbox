# Session Fixtures

SQLite database dumps and JSON session blobs used by session-layer regression
tests.

## File naming

```
<scenario>_<issue-or-pr-slug>.session.json   # single session record (JSON)
<scenario>_<issue-or-pr-slug>.db             # SQLite dump for store-level tests
```

Examples:
- `schema_v1_upgrade_path.session.json`
- `corrupt_sim_response_json.session.json`
- `integrity_missing_txhash.session.json`

## Rules

- Session IDs must use the synthetic prefix `sess_test_` to prevent confusion
  with real session IDs in developer environments.
- Do not embed real private keys, tokens, or horizon URLs from production.
- The `env_fingerprint` field may be left empty or set to `test-env`.
