# Source Map Fixtures

Minimal WASM binaries and DWARF-stripped stubs used by source-mapping regression
tests.  These files must never contain production contract bytecode.

## File naming

```
<scenario>_<issue-or-pr-slug>.wasm          # minimal WASM binary (8-byte magic stub or real minimal module)
<scenario>_<issue-or-pr-slug>.alias.json    # source-alias mapping file for --source-alias tests
```

Examples:
- `missing_dwarf_section.wasm`
- `null_source_location_issue190.wasm`
- `crate_alias_remap.alias.json`

## Rules

- Use the minimal WASM header `\0asm\x01\x00\x00\x00` for stubs that only need
  to pass magic-byte validation.
- Do not embed real contract logic or proprietary source paths.
- Alias JSON files must be valid UTF-8 flat objects (`{"crate": "/path"}`).
