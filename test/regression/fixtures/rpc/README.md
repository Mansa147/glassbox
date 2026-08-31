# RPC Fixtures

Minimal JSON files that stand in for Stellar RPC responses in unit and
regression tests.  No live network calls are made when these fixtures are used.

## File naming

```
<layer>_<issue-or-pr-slug>_<variant>.json
```

Examples:
- `gettransaction_success_minimal.json`
- `gettransaction_notfound_empty_result.json`
- `getledgerentries_missing_footprint.json`

## Format

Each file is a single JSON object matching the shape of the relevant RPC
response struct in `internal/rpc`.  Only the fields required to exercise the
specific failure class need to be populated; all other fields may be omitted
or zero-valued.

## Rules

- No real transaction hashes from mainnet or testnet.  Use the canonical stub:
  `5c0a1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab`
- No authentication tokens, private keys, or bearer headers.
- Envelope XDR should be a minimal valid base64 string, or the constant
  `AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=` for "empty envelope" stubs.
