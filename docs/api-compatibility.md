# API Compatibility Checks

**Issue #597 implementation.**

Glassbox protects consumers from accidental breaking changes by generating
deterministic snapshots of its public API surface and failing CI when those
snapshots diverge.

## What is covered

| Artifact | Tool | Snapshot location |
|---|---|---|
| Go exported symbols (types, funcs, consts) | `internal/apicompat` | `internal/apicompat/testdata/api-snapshots/*.txt` |
| TypeScript exports from `src/index.ts` | `scripts/api-snapshot.sh` | `.api-snapshots/ts-main.txt` |
| TypeScript exports from `src/audit/browser/index.ts` | `scripts/api-snapshot.sh` | `.api-snapshots/ts-browser.txt` |
| CLI `--help` output for each sub-command | `scripts/api-snapshot.sh` | `.api-snapshots/cli-*.txt` |
| JSON schema files (`docs/schema/`) | `scripts/api-snapshot.sh` | `.api-snapshots/schemas.txt` |

Private implementation details (unexported Go symbols, `__tests__` helpers,
internal command-building helpers) are deliberately excluded.

## Running the checks locally

### Go symbol check

```bash
# Check (exits 1 on regression):
go test ./internal/apicompat/...

# Regenerate after intentional change:
go test ./internal/apicompat/... -update
```

### TypeScript / CLI / Schema check

```bash
# Generate snapshots (first time or after intentional change):
scripts/api-snapshot.sh generate

# Check:
scripts/api-snapshot.sh check
```

## CI pipeline

`scripts/api-snapshot.sh check` is integrated into the CI workflow. It runs
after the build step so it has access to the compiled `glassbox` binary for
CLI snapshots.

## Updating snapshots after an intentional change

1. Make your change (add/remove/rename an export).
2. Run:
   ```bash
   scripts/api-snapshot.sh generate          # TypeScript, CLI, schemas
   go test ./internal/apicompat/... -update  # Go symbols
   ```
3. Review the diff:
   ```bash
   git diff .api-snapshots/ internal/apicompat/testdata/
   ```
4. Write a **migration note** in your PR description explaining what changed
   and what consumers need to do (if anything).
5. Commit the updated snapshot files alongside your code change.

## What counts as a breaking change

Removals and renames of exported symbols are always flagged as failures by
`TestAPICompatibility`. Additions are logged informally (non-breaking).

For TypeScript and CLI, any diff in the snapshot is reported; the developer
decides whether it is intentional and accepts it by regenerating.

## Determinism guarantee

Snapshots are byte-for-byte identical across runs because:
- Go symbol extraction uses `sort.Strings` on identifier names.
- TypeScript export lines are extracted with `sort` in the shell script.
- CLI help text is normalised to strip version strings.
- Schema checksums use `sha256sum` with sorted file paths.
