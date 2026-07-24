# Trace Fixtures

Serialised `ExecutionTrace` JSON files used by trace-layer regression tests.

## File naming

```
<scenario>_<issue-or-pr-slug>.trace.json
```

Examples:
- `empty_steps_no_events.trace.json`
- `budget_exceeded_cpu_v22.trace.json`
- `deprecated_host_fn_bytes_copy.trace.json`

## Format

Each file is a JSON-serialised `trace.ExecutionTrace` struct.  Only the fields
relevant to the regression need to be populated.  The `tx_hash` field should
always be set to the canonical stub hash:

  `5c0a1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab`

## Rules

- No contract IDs from live networks.
- Synthetic event data only — do not copy real diagnostic_events payloads.
- Keep files small; 5–10 steps is sufficient for most regression scenarios.
