# Replay Fixtures

Snapshot registry files (`*.registry.json`) and plain ledger-state JSON files
(`*.ledger.json`) used by `glassbox debug --load-snapshots` and replay unit
tests.

## File naming

```
<scenario>_<issue-or-pr-slug>.registry.json   # full snapshot registry
<scenario>_<issue-or-pr-slug>.ledger.json     # flat ledger-entries map
```

Examples:
- `budget_exhaustion_cpu_issue234.registry.json`
- `empty_footprint_edge_case.ledger.json`

## Rules

- Ledger entries must use base64-encoded XDR values — use `AAAA...=` stubs
  when real XDR is not needed to trigger the failure class.
- Registry files must pass `replay.VerifyIntegrity()` (integrity hash must
  match content) or they will cause spurious test failures.
- Do not include real contract bytecode or production ledger state.
