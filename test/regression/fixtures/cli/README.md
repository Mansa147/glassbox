# CLI Fixtures

Input files and expected-output snapshots for CLI-level regression tests that
drive the binary through `integration.runErst()`.

## File naming

```
<command>_<scenario>_<issue-or-pr-slug>.input.txt    # stdin / flag value input
<command>_<scenario>_<issue-or-pr-slug>.expected.txt # expected stdout/stderr fragment
<command>_<scenario>_<issue-or-pr-slug>.env.json     # environment variable overrides
```

Examples:
- `debug_invalid_hash_error_message.expected.txt`
- `session_list_empty_store.expected.txt`
- `audit_sign_missing_payload_rejected.expected.txt`

## Rules

- Expected output files contain plain-text fragments that must appear in the
  actual command output (substring match, not exact).
- Do not hard-code version numbers or timestamps in expected output; use
  `<VERSION>` and `<TIMESTAMP>` as placeholders and strip them before matching.
- env.json files are flat `{"KEY": "VALUE"}` objects.  Never include real tokens
  or credentials.
