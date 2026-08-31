# `glassbox debug` — Debug Command Reference

The `debug` command fetches a Soroban transaction from the Stellar network, runs it through the local simulator, and displays a detailed execution trace including contract events, token flows, budget usage, and security findings.

---

## Synopsis

```
glassbox debug [flags] <transaction-hash>
glassbox debug --wasm <path> [--args ...]
glassbox debug --demo
glassbox debug --dry-run --network testnet <transaction-hash>
glassbox debug --xdr-file <path>
glassbox debug --json-file <path>
glassbox debug --load-snapshots <registry-file>
```

---

## Arguments

| Argument | Description |
|---|---|
| `<transaction-hash>` | 64-character lowercase hex transaction hash. Required unless `--wasm`, `--demo`, `--xdr-file`, `--json-file`, or `--load-snapshots` is provided. |

**Validation:** The command validates the transaction hash format before making any network calls. An invalid hash produces an explicit error that includes the offending value and states the expected format (64 lowercase hex characters).

---

## Core Flags

| Flag | Default | Description |
|---|---|---|
| `--network`, `-n` | `mainnet` | Stellar network: `testnet`, `mainnet`, or `futurenet`. Auto-detected from the transaction when omitted. |
| `--rpc-url` | _(config)_ | Custom RPC URL. Overrides config and environment. Accepts comma-separated URLs for fallback. |
| `--rpc-token` | _(env: `GLASSBOX_RPC_TOKEN`)_ | RPC authentication token. |
| `--compare-network` | _(none)_ | Run the same transaction on a second network and diff the results. Must differ from `--network`. |

**Network validation:** Both `--network` and `--compare-network` are validated early in `PreRunE`. Providing the same value for both flags produces:
```
--network and --compare-network must be different networks; both are "testnet"
```

**Version and metadata:** The binary version, commit SHA, build date, and User-Agent string are surfaced by `glassbox version`. Use `glassbox version --json` for machine-readable output suitable for CI pipelines. When the binary was not built with `-ldflags` version injection (e.g. `go run ./...`), the version field shows `0.0.0-dev` and a `(dev build)` warning is displayed.

---

## Validation & Dry-Run

`--dry-run` validates inputs and checks the environment **without executing a simulation**. Use it in CI or before a long replay to catch configuration errors early.

> **Note:** `--dry-run` cannot be combined with `--show-metrics`, `--demo`, `--wasm`, `--load-snapshots`, or local envelope input flags. These combinations are rejected with a clear message explaining why.

**Checks performed by `--dry-run`:**

1. Transaction hash format (64 hex chars)
2. Network name validity (`testnet`, `mainnet`, `futurenet`, or a custom network defined in config)
3. Compare-network name validity (when `--compare-network` is set)
4. Compare-network distinctness (must differ from primary network)
5. RPC URL format validation (when `--rpc-url` is provided)
6. RPC endpoint reachability (health check with a 10-second timeout; empty health status is treated as a failure)
7. Simulator binary presence and version compatibility
8. Protocol version compatibility (when `--protocol-version` is set)
9. Trace output configuration validation (when `--trace-output` is provided)
10. `--contract-source` directory existence and type (when set)
11. `--source-alias` JSON validity (when set; alias target warnings are printed but not counted as failures)

Each check prints `[OK]` or `[FAIL]` on its own line with detailed remediation guidance. On failure the output ends with a numbered list of all failures so you can address them in one pass.

**Example output:**

```sh
# All checks pass:
glassbox debug --dry-run --network testnet 5c0a1234...ef7890ab

[OK]   Transaction hash format is valid (64 hex chars)
[OK]   Network selection: testnet
[OK]   RPC endpoint reachable (status: healthy)
[OK]   Simulator binary found: /usr/local/bin/glassbox-sim
       Version: 1.2.3
       Version compatibility: OK

Additional environment checks:
[OK]   Trace output configuration is valid: ./traces/debug.html

Dry-run PASSED: all checks succeeded for transaction 5c0a1234... on testnet

You can now run the full debug command by removing the --dry-run flag.

# Multiple failures with detailed remediation:
glassbox debug --dry-run --network badnet --compare-network badnet tooshort

[FAIL] Invalid transaction hash format: expected 64 hexadecimal characters
       Fix: transaction hashes must be 64 lowercase hexadecimal characters
       Example: 5c0a1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab
[FAIL] Invalid network "badnet" — must be testnet, mainnet, futurenet, or a custom network defined in config
       Fix: use --network testnet, --network mainnet, or --network futurenet
       Or define a custom network in glassbox.toml under [networks]
[FAIL] --compare-network must be different from --network; both are "badnet"
       Fix: select a different network for --compare-network to enable cross-network comparison
       Example: --network testnet --compare-network mainnet

Dry-run FAILED: 3 validation error(s)
  1. transaction hash: expected 64 hexadecimal characters
  2. network: invalid network "badnet"
  3. compare-network: cannot be the same as primary network "badnet"

Recommendation: Fix all errors listed above before executing the debug command.
For comprehensive diagnostics, run: glassbox doctor
```

**Exit code:** `0` on pass, `1` on any validation failure.

---

## Build Artifact Discovery

The debug command validates all local build artifacts before starting any network call or simulation.

### `--wasm <path>`

Validated at startup:
- File must exist and be readable. Missing files return: `--wasm: file not found: "<path>" — Build your contract first (e.g. 'cargo build --release ...')`
- File must begin with the WASM magic bytes (`\0asm`). Non-WASM files return: `--wasm: "<path>": not a valid WASM binary (bad magic bytes)`
- During replay, the full binary structure is analysed; size warnings are printed to stderr for binaries above 256 KiB.

### `--contract-source <path>`

Validated at startup:
- Path must exist on disk. Missing path returns: `--contract-source: directory not found: "<path>"`
- Path must be a directory, not a file. File paths return: `--contract-source: "<path>" is a file, not a directory`
- When the path is valid, DWARF source mapping is enabled automatically.

### `--mock-ledger-manifest <path>`

Validated at startup:
- File must exist. Missing file returns: `--mock-ledger-manifest: file not found: "<path>"`
- File must be valid JSON with a `"ledger_entries"` key.
- Each entry value must be non-empty and valid base64-encoded XDR: `--mock-ledger-manifest: entry "<key>" has an invalid base64 value`

### `--mock-ledger-entry key:value`

Validated at startup:
- Format must be `key:value` — missing colon returns: `--mock-ledger-entry: invalid format "<entry>" — expected key:value`
- Value must be non-empty: `--mock-ledger-entry: entry "<entry>" has an empty value`
- Value must be valid base64-encoded XDR.

### `--source-alias <path>`

Validated at startup:
- File must exist. Missing file returns: `--source-alias: file not found: "<path>"`
- File must be a valid JSON object. Invalid JSON returns: `--source-alias: failed to parse "<path>" as JSON`
- Alias target directories that don't exist on disk produce a **warning** (not an error) so you can still debug if only some aliases are stale.

---



### WASM replay (no network required)

```sh
glassbox debug --wasm ./contract.wasm --args "arg1" "arg2"
```

Runs the contract locally with mock ledger state. Useful for rapid iteration during development.
The `--wasm` file path is validated before execution — a missing or unreadable file surfaces an error immediately.

### Hot reload

```sh
glassbox debug --wasm ./contract.wasm --hot-reload
```

Watches the WASM file for changes and prompts to re-run after each rebuild. Requires `--wasm` — omitting it returns:
```
--hot-reload requires --wasm; provide --wasm <path> to enable hot reload
```

### Local envelope file

```sh
# From a raw base64 XDR file:
glassbox debug --xdr-file ./tx-envelope.xdr

# From a structured JSON export:
glassbox debug --json-file ./tx.json
```

Both files are validated for existence before any processing begins. The JSON format must contain an `envelope_xdr` field. Optionally include `result_meta_xdr` and `network`.

### Offline snapshot replay

```sh
glassbox debug --load-snapshots ./tx-registry.json
```

Replays a previously saved snapshot registry without any network connectivity. See [snapshot-deduplication.md](./snapshot-deduplication.md).

---

## Output Flags

| Flag | Default | Description |
|---|---|---|
| `--json` | `false` | Emit simulation results as machine-readable JSON. |
| `--format` | `text` | Output format: `text` or `json`. Any other value is rejected with the valid options listed. |
| `--trace-verbosity` | `normal` | Trace detail level: `summary`, `normal`, or `verbose`. Invalid values are caught early with the accepted list. |
| `--export-svg` | _(none)_ | Export the call graph as an SVG file. |
| `--show-metrics` | `false` | Print RPC and simulation performance metrics after the run (see Performance Metrics below). Cannot be combined with `--dry-run`. |
| `--verbose`, `-v` | `false` | Enable verbose logging (equivalent to `--log-level=debug`). |

---

## Performance Metrics (`--show-metrics`)

When `--show-metrics` is set, a performance summary is printed after the simulation completes. The output adapts to the active format:

- **`--format text`** (default): human-readable ASCII table
- **`--format json` / `--json`**: machine-readable JSON object with the same fields

**Text summary includes:**
- Total RPC call count and error count
- Aggregate total / min / max / avg durations
- Per-method breakdown (when more than one RPC method was used)
- ⚠ Slow-call warnings for any call exceeding 3 seconds, with a remediation tip

**Example text output:**

```
── Performance Summary ──────────────────────────────
  RPC calls     : 3
  RPC total     : 430ms
  RPC min/max   : 80ms / 200ms
  RPC avg       : 143ms

  Per-method breakdown:
    getTransaction               calls=1    total=200ms    avg=200ms
    getLedgerEntries             calls=2    total=230ms    avg=115ms

  ⚠  Slow RPC calls (>3s):
     getTransaction              3200ms
  Tip: consider using --rpc-url to switch to a faster RPC endpoint,
       or check your network connection.

  Replay time   : 85ms
─────────────────────────────────────────────────────
```

**Example JSON output (`--format json --show-metrics`):**

```json
{
  "rpc_calls": 3,
  "rpc_total_ms": 430.0,
  "rpc_min_ms": 80.0,
  "rpc_max_ms": 200.0,
  "rpc_avg_ms": 143.0,
  "sim_ms": 85.0,
  "by_method": [
    { "method": "getTransaction", "calls": 1, "total_ms": 200.0, "avg_ms": 200.0 }
  ]
}
```

---

## Simulation Control Flags

| Flag | Default | Description |
|---|---|---|
| `--snapshot` | _(none)_ | Load pre-captured ledger state from a JSON snapshot instead of fetching from the network. |
| `--live` / `--latest-ledger` | `false` | Replay against the current validated ledger state (live data). |
| `--protocol-version` | _(auto)_ | Override the Soroban protocol version for simulation. |
| `--mock-time` | `0` | Override the ledger timestamp (Unix seconds). |
| `--mock-base-fee` | `0` | Override the base fee (stroops) for fee sufficiency checks. |
| `--mock-gas-price` | `0` | Override the gas price multiplier. |
| `--mock-ledger-entry` | _(none)_ | Override individual ledger entries before simulation (`key:value`; repeatable). |
| `--mock-ledger-manifest` | _(none)_ | Path to a JSON manifest containing `ledger_entries` for bulk override. |
| `--op` / `--operation` | `-1` (all) | Select a specific zero-based operation index. Use `0` for first, `1` for second, etc. Values below `-1` are rejected. |

---

## Source Mapping Flags

| Flag | Default | Description |
|---|---|---|
| `--contract-source` | _(auto-discovery)_ | Explicit path to the contract source directory when auto-discovery fails. Path is validated at startup: must exist, be a directory, and not be blank. |
| `--skip-source-mapping` | `false` | Skip DWARF source mapping for faster raw trace replay. |
| `--source-alias` | _(none)_ | Path to a JSON file mapping embedded source paths to local directory paths. File must contain valid JSON, and each alias entry must have a non-empty name and non-empty target path. |

**Source discovery in CI:** In non-interactive environments (CI pipelines), the
interactive stdin prompt is skipped. When all discovery stages fail, an explicit
error is returned listing every stage that was tried and suggesting
`--contract-source` or `--skip-source-mapping` as remedies. The `--dry-run`
flag includes source-discovery pre-flight checks so configuration problems are
caught before any simulation begins.

---

## Theme Flag

| Flag | Default | Description |
|---|---|---|
| `--theme` | _(auto-detect)_ | Color theme override. Must be one of: `dark`, `light`, `none`, `default`, `deuteranopia`, `protanopia`, `tritanopia`, `high-contrast`. Invalid values are caught early. |

---

## Session & Watch Flags

| Flag | Default | Description |
|---|---|---|
| `--watch` | `false` | Poll for a pending transaction to appear on-chain before debugging. Cannot be combined with local envelope input. |
| `--watch-timeout` | `30` | Timeout in seconds for `--watch` mode. |
| `--save-snapshots` | _(none)_ | Save simulation results to a snapshot registry file. |
| `--pin-endpoint` | _(none)_ | Pin a specific RPC endpoint with the session. Must match `--rpc-url` when both are provided — a mismatch produces an explicit error naming both flags. |
| `--no-cache` | `false` | Disable local ledger state caching for this run. |
| `--snapshots` | `false` | Enable snapshot capture inside the simulator. |

---

## Audit & Decentralised Storage

| Flag | Default | Description |
|---|---|---|
| `--audit-key` | _(none)_ | Ed25519 private key (PEM) used to sign the audit trail before publishing. |
| `--publish-ipfs` | `false` | Publish a signed audit trail to IPFS after simulation. Requires `--audit-key`. |
| `--publish-arweave` | `false` | Publish a signed audit trail to Arweave after simulation. Requires `--audit-key`. |
| `--ipfs-node` | _(public gateway)_ | IPFS node API URL. |
| `--arweave-gateway` | _(none)_ | Arweave gateway URL. |
| `--arweave-wallet` | _(none)_ | Path to an Arweave wallet JSON file. |

See [audit-signing.md](./audit-signing.md) for the full audit workflow.

---

## Error Handling & Diagnostics

The debug command returns explicit, actionable errors for all common failure modes. Each error includes the invalid value and a suggested fix:

| Failure | Error message |
|---|---|
| Invalid transaction hash | `invalid transaction hash "…" — expected 64 hexadecimal characters` |
| Invalid `--network` | `invalid --network "…"; must be one of: testnet, mainnet, futurenet` |
| Invalid `--compare-network` | `invalid --compare-network "…"; must be one of: testnet, mainnet, futurenet` |
| Same `--network` and `--compare-network` | `--network and --compare-network must be different networks; both are "…"` |
| Missing `--wasm` with `--hot-reload` | `--hot-reload requires --wasm; provide --wasm <path> to enable hot reload` |
| `--wasm` file not found | `--wasm: file not found: "<path>" — Build your contract first …` |
| `--wasm` not a valid WASM binary | `--wasm: "<path>": not a valid WASM binary (bad magic bytes …)` |
| Source discovery exhausted (non-interactive) | `contract source not found: all discovery stages exhausted for contract "…" — provide --contract-source <path> or --skip-source-mapping` |
| `--contract-source` not found | `--contract-source: directory not found: "<path>"` |
| `--contract-source` is a file | `--contract-source: "<path>" is a file, not a directory` |
| `--mock-ledger-manifest` not found | `--mock-ledger-manifest: file not found: "<path>"` |
| `--mock-ledger-manifest` invalid JSON | `--mock-ledger-manifest: failed to parse "<path>" as JSON: …` |
| `--mock-ledger-manifest` empty/bad value | `--mock-ledger-manifest: entry "<key>" has an empty value` |
| `--mock-ledger-entry` bad format | `--mock-ledger-entry: invalid format "<entry>" — expected key:value` |
| `--mock-ledger-entry` empty value | `--mock-ledger-entry: entry "<entry>" has an empty value` |
| `--source-alias` not found | `--source-alias: file not found: "<path>"` |
| `--source-alias` invalid JSON | `--source-alias: failed to parse "<path>" as JSON: …` |
| Both `--xdr-file` and `--json-file` | `only one of --xdr-file or --json-file may be specified; remove one of the two flags` |
| Hash + local file conflict | `cannot specify both a transaction hash and a local envelope file; use either a hash or --xdr-file/--json-file, not both` |
| `--watch` with local file | `--watch cannot be used with local envelope input; remove --watch or provide a transaction hash instead` |
| `--dry-run` with `--show-metrics` | `--show-metrics cannot be used with --dry-run; no simulation is executed in dry-run mode` |
| `--dry-run` with local modes | `--dry-run cannot be combined with --demo, --wasm, --load-snapshots, or local envelope input` |
| `--dry-run` without hash | `transaction hash is required for --dry-run` |
| `--pin-endpoint` mismatch | `--pin-endpoint must match --rpc-url when both are provided; set them to the same URL or remove one` |
| Invalid `--trace-verbosity` | `invalid --trace-verbosity "…"; must be one of: summary, normal, verbose` |
| Invalid `--theme` | `invalid --theme "…"; must be one of: dark, light, none, default, …` |
| Invalid `--format` | `invalid --format "…"; must be one of: text, json` |
| Invalid `--op` value | `--op must be a non-negative integer or omitted; use 0 for the first operation, …` |
| Missing hash (no local mode) | `transaction hash is required when not using --wasm, --demo, --xdr-file, or --json-file` |
| RPC connection failure | `RPC connection failed: <underlying error>` |
| Transaction not found | `transaction not found` — check the hash and the selected network |
| Simulator not found | `simulator binary not found` — run `glassbox doctor --fix` |
| Simulation failure | `simulation execution failed: <detail>` — check the diagnostic section of the output |
| No simulation results | `no simulation results generated` — indicates an internal logic error |
| Snapshot fingerprint mismatch | `snapshot fingerprint mismatch: stored=… computed=…` — re-run the debug command to regenerate the snapshot |
| Snapshot tx hash mismatch | `snapshot tx hash mismatch: snapshot contains tx=… but replay requested tx=…` |
| Snapshot network mismatch | `snapshot network mismatch: snapshot was captured on "…" but replay is targeting "…"` |
| Snapshot is stale | `snapshot is stale: CLI parameters have changed since the snapshot was saved` — regenerate with the current flags |
| Empty trace (no events) | A note is printed to stderr explaining possible causes and suggesting `glassbox doctor --fix` |

For environment setup problems, run `glassbox doctor` for a comprehensive health check.

---

## Demo Mode

```sh
glassbox debug --demo
```

Prints sample output without making any network calls. Useful for testing terminal color detection.

---

## Examples

```sh
# Debug a transaction on mainnet (default)
glassbox debug 5c0a1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab

# Debug on testnet
glassbox debug --network testnet abc123...def789

# Validate parameters without running a simulation (safe for CI)
glassbox debug --dry-run --network testnet abc123...def789

# Compare execution between testnet and mainnet
glassbox debug --network testnet --compare-network mainnet abc123...def789

# Debug locally without a network connection
glassbox debug --wasm ./build/contract.wasm --args "my-arg"

# Debug from a saved XDR file
glassbox debug --xdr-file ./envelope.xdr

# Output machine-readable JSON
glassbox debug --json 5c0a1234...ef7890ab

# Show performance metrics after the run
glassbox debug --show-metrics --network testnet abc123...def789

# Show performance metrics as JSON
glassbox debug --show-metrics --format json abc123...def789

# Save ledger snapshots for offline replay
glassbox debug --save-snapshots ./registry.json 5c0a1234...ef7890ab

# Replay from snapshots (no network)
glassbox debug --load-snapshots ./registry.json
```

---

## Error diagnostics and recovery

When a debug run fails, Glassbox prints an actionable `Hint:` line alongside the
error, so failures explain how to recover rather than only what went wrong:

```
Error: ERST_RPC_CONNECTION_FAILED: RPC connection failed: dial tcp ...
Hint: The RPC endpoint could not be reached. Check your internet connection and the endpoint, pass a known-good one with --rpc-url <url>, and make sure it serves the selected --network.
```

Hints are surfaced for the most common recoverable failures, including an
unreachable or timed-out RPC endpoint, a transaction that cannot be found on the
selected network, and unsupported Soroban protocol versions.

---

## Protocol Conflict Resolution

The `glassbox://` deep-link scheme must be registered with the OS before protocol URIs dispatched from browsers or other tools can reach the CLI. If another application registers itself as the `glassbox://` handler (a **protocol conflict**), deep links silently open the wrong program.

### Detecting conflicts

```sh
glassbox protocol:diagnose
```

The command checks every registration artefact on the current platform and reports the overall status. When a conflict is detected it prints a clear warning and names the conflicting binary:

```
[FAIL] Protocol conflict: the glassbox:// scheme is claimed by a different application.
  Conflicting handler: /usr/bin/otherapp
  Expected handler:    /usr/local/bin/glassbox
  Another program has registered itself as the glassbox:// handler.
  Run 'glassbox protocol:repair' to reclaim the registration.

⚠  Protocol conflict detected: the glassbox:// scheme is currently handled by
   a different binary (/usr/bin/otherapp).
   Run 'glassbox protocol:repair' to reclaim the registration.

Remediation steps:
  1. Run 'glassbox protocol:repair' to overwrite the conflicting registration.
  2. If the conflicting application is still needed, uninstall or reconfigure it first.
```

**Conflict vs stale path:** Glassbox distinguishes between two cases:
- **Conflict** — a foreign binary (no "glassbox" in its path) owns the scheme. Logged as `ConflictDetected=true`.
- **Stale path** — an older Glassbox binary is registered. Logged as a stale path (no conflict flag). Both are fixed by `protocol:repair`.

**Exit codes for `protocol:diagnose`:**
- `0` — registration is healthy
- `1` — registration is missing, stale, or conflicting

### Resolving conflicts

```sh
# Automatic repair (recommended)
glassbox protocol:repair

# Or re-register manually
glassbox protocol:register
```

`protocol:register` validates prerequisites **before** writing any OS registration state:

- The Glassbox binary path must exist and be executable (or a runnable Windows extension).
- Linux requires `xdg-mime` from the `xdg-utils` package.
- Unsupported platforms are rejected immediately with remediation guidance.

On failure the command prints explicit `[FAIL]` diagnostics and numbered fix steps. After a successful registration, run `glassbox protocol:status` to confirm the registered binary path is usable.

`protocol:repair` runs `protocol:diagnose` first, then overwrites the registration with the current binary. A post-repair verification confirms the fix succeeded.

**JSON output** for CI pipelines:

```sh
glassbox protocol:diagnose --json
```

The `conflict_detected` and `conflicting_handler` fields are included in the JSON output so automated checks can distinguish conflicts from simple missing registrations.

### Handling `protocol:handle` errors

The `protocol:handle` sub-command parses and dispatches `glassbox://debug/…` URIs. It validates each field before executing:

| URI problem | Error |
|---|---|
| Empty URI | `protocol URI must not be empty` |
| Wrong scheme | `invalid protocol URI: expected glassbox://` |
| Missing transaction hash | `invalid transaction hash "": must be a 64-character hex string` |
| Invalid network | `invalid network "…": must be one of testnet, mainnet, futurenet` |
| Negative op index | `invalid operation index "…": must be a non-negative integer` with `Fix:` hint |
| Op index too large (>65535) | `operation index … exceeds the maximum allowed value (65535)` with `Fix:` hint |
| Unknown view | `invalid view "…": must be one of trace, flamegraph, events, auth, budget, storage` |
| `source` contains null bytes | `source parameter contains null bytes and cannot be used` |
| `signature` contains null bytes | `signature parameter contains null bytes and cannot be used` |
| `source` too long (>256 chars) | `source parameter is too long (… characters, max 256)` |
| `signature` too long (>512 chars) | `signature parameter is too long (… characters, max 512)` |

When `protocol:handle` receives a bad URI, the error wraps the specific parse failure with a format reminder and a `--help` hint:

```
invalid network "devnet": must be one of testnet, mainnet, futurenet
  Expected format: glassbox://debug/<64-char-hex>?network=<testnet|mainnet|futurenet>[&op=<n>][&view=<mode>]
  Run 'glassbox protocol:handle --help' for full parameter documentation
```

### `protocol:repair` pre-condition validation

`protocol:repair` validates that the registrar's executable path is non-empty and that the binary still exists before attempting any write. Running repair via `go run` or from a stripped build without a resolved executable fails immediately with:

```
cannot repair: executable path is empty
  Fix: ensure glassbox is invoked from a valid binary path, not via 'go run'
```

### Enhanced registration validation

All protocol registration operations (`protocol:register`, `protocol:unregister`, `protocol:verify`, `protocol:diagnose`, `protocol:repair`) now perform pre-flight validation:

- **Executable path checks**: Rejects empty paths, non-existent binaries, and system root directories
- **Permission checks**: On Unix, validates that the binary has execute permissions
- **Home directory checks**: Ensures the home directory is accessible before writing registration artefacts
- **Tool availability**: Validates that required system tools (`xdg-mime`, `reg`, `lsregister`) are present before attempting registration
- **Post-write validation**: After writing files, reads them back to confirm they reference the correct executable and contain the expected scheme declarations

When validation fails, errors include actionable `Fix:` hints to guide users toward resolution.

### Path normalization and safety

All filesystem paths used during protocol registration are normalized and validated to prevent security issues and improve robustness:

- **Path normalization**: Removes redundant separators, resolves `.` and `..` components, and rejects suspicious patterns
- **Path traversal protection**: Rejects paths containing `..` sequences that could indicate directory traversal attempts
- **Consecutive dots**: Rejects paths with `...` which may indicate attempts to hide files or create ambiguous paths
- **Length limits**: Enforces a maximum path length of 255 characters (conservative limit for cross-platform compatibility)
- **Null byte detection**: Rejects paths containing null bytes before any filesystem operations
- **Post-symlink validation**: After resolving symlinks, the resulting path is re-validated to ensure it remains safe

**Examples of rejected paths:**
- `/usr/local/bin/../../etc/passwd` — path traversal pattern
- `/path/to/.../file` — consecutive dots
- Paths exceeding 255 characters
- Paths containing null bytes (`\x00`)

When a path fails validation, the error message explains what was wrong and suggests a fix, such as moving the binary to a shorter path or using a direct path without `..` components.

### `protocol:diagnose --format` validation

The `--format` flag accepts only `text` (default) or `json`. Any other value is rejected before the diagnostic runs:

```
invalid --format "xml": must be 'text' or 'json'
```

### Registration summaries

`protocol:diagnose`, `protocol:status`, and `protocol:verify` now print a one-line **Summary** before detailed checks. The summary reflects the overall registration state (`ok`, `degraded`, `not_registered`, or `error`) and the number of detected issues.

`protocol:register` validates the current binary path and platform support **before** writing any OS registration artefacts. Invalid inputs fail immediately with actionable remediation text instead of low-level OS errors.

---

## Session Recovery and Integrity

### Crash recovery

When Glassbox is interrupted unexpectedly (crash, SIGKILL, power loss), the active session may be left in a partially saved state. On the next invocation, run:

```sh
glassbox session recover
```

The command:
1. Reads the crash-recovery checkpoint at `~/.Glassbox/active_session.json`
2. Validates all checkpoint fields (session ID, tx hash, network, PID, timestamp) before trusting them
3. Probes whether the originating process is still alive
4. Loads the session from the store and runs a full **integrity check** before making it active
5. Clears the checkpoint after successful recovery

If the checkpoint is corrupt or the session fails integrity validation, a numbered list of issues is printed with per-issue hints and remediation commands:

```
Checkpoint validation failed (2 issue(s)):
  1. checkpoint is missing the transaction hash
  2. checkpoint has an invalid PID: 0

Clearing corrupt checkpoint.
Hint: re-run 'glassbox debug <tx-hash>' to start a fresh session.
```

```
Session integrity check FAILED for session-abc123:
  1. [TxHash] transaction hash is empty
     Hint: The session was saved without a transaction hash. Re-run 'glassbox debug <tx-hash>'.
  2. [Network] network value "devnet" is not a recognised Stellar network
     Hint: Accepted values are: testnet, mainnet, futurenet.

The session exists in the store but has data integrity problems.
To remove it:  glassbox session delete session-abc123
To re-debug:   glassbox debug <tx-hash> --network <network>
```

### Integrity validation on `session resume`

`glassbox session resume` also runs an integrity check before activating any session. Corrupt sessions are blocked from becoming active, and the output names every failing field with a hint:

```
Session integrity check FAILED for corrupt-session:
  1. [TxHash] transaction hash is empty
     Hint: Re-run 'glassbox debug <tx-hash>' to create a valid session.

This session cannot be resumed safely.
To remove it:  glassbox session delete corrupt-session
To re-debug:   glassbox debug  --network testnet
```

**Fields validated by the integrity check:**

| Field | Check |
|---|---|
| `ID` | Non-empty |
| `TxHash` | Non-empty |
| `Network` | Non-empty and one of: `testnet`, `mainnet`, `futurenet` |
| `Status` | One of: `active`, `saved`, `resumed`, `recovered`, `expired` |
| `CreatedAt` | Non-zero |
| `LastAccessAt` | Non-zero and not before `CreatedAt` |
| `SchemaVersion` | ≤ current `SchemaVersion` constant |
| `EnvelopeXdr` | Non-empty when `SimRequestJSON` is set |
| `AuditHash` | 64-character SHA-256 hex string when set; required when `AuditSignature` or `PreviousSessionHash` is set |
| `AuditSignature` | 128-character hex-encoded Ed25519 signature when set; required when `AuditHash` or `PreviousSessionHash` is set |
| `PreviousSessionHash` | 64-character SHA-256 hex string when set; must differ from `AuditHash` |

`glassbox session save` now runs the same integrity validation before writing to the session store. That means malformed audit-chain fields such as a missing `AuditSignature`, a bad `PreviousSessionHash`, or a self-referential chain link are rejected immediately with field-specific hints instead of being persisted and only discovered later during resume or recovery.

### `session save` — `--pin-endpoint`

```sh
glassbox session save --pin-endpoint https://rpc.example.com
```

Pins an RPC endpoint URL with the saved session so that when the session is resumed the same endpoint is displayed and can be used with `--rpc-url`.

---

- [`glassbox profile`](./trace-profiling.md) — gas usage analysis, pprof flamegraph generation, and profiling export
- [`glassbox doctor`](./sandboxed-replay.md) — environment setup checker
- [`glassbox session`](./session-bookmarking.md) — save and restore debug sessions
- [Trace export validation](./trace-export-validation.md) — validation checks for `--trace-output`
- [Snapshot deduplication](./snapshot-deduplication.md)
- [Source mapping](./source-mapping.md)
- [Audit signing](./audit-signing.md)
- [Command Security Review Guide](./command-security-review.md) — checklist and worked examples for secrets, paths, network, subprocesses, and output redaction required for every new command
