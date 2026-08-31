# Shell Completion

Glassbox generates native completion scripts for Bash, Zsh, Fish, and
PowerShell from the live command tree.  Every public command, subcommand,
flag, and enum-valued option is covered.

---

## Installation

### Bash

```bash
# System-wide (requires root / sudo)
glassbox completion bash | sudo tee /etc/bash_completion.d/glassbox

# Per-user
mkdir -p ~/.local/share/bash-completion/completions
glassbox completion bash > ~/.local/share/bash-completion/completions/glassbox
source ~/.bashrc
```

### Zsh

```zsh
# Add to a directory already on $fpath
glassbox completion zsh > "${fpath[1]}/_glassbox"

# Or create a dedicated completions directory
mkdir -p ~/.zsh/completions
glassbox completion zsh > ~/.zsh/completions/_glassbox

# Add to ~/.zshrc (if not already present)
echo 'fpath=(~/.zsh/completions $fpath)' >> ~/.zshrc
echo 'autoload -Uz compinit && compinit' >> ~/.zshrc
source ~/.zshrc
```

### Fish

```fish
glassbox completion fish > ~/.config/fish/completions/glassbox.fish
# Completions are picked up automatically on the next shell start.
```

### PowerShell

```powershell
# Load for the current session only
glassbox completion powershell | Out-String | Invoke-Expression

# Load for every new session (append to your profile)
glassbox completion powershell >> $PROFILE
```

---

## What is completed

### Commands

Every public command in the tree is reachable through completion, including
all subcommands under `session`, `protocol:*`, `offline`, `snapshot`, and
`audit:*`.

### Enum-valued flags

The following flags suggest only their accepted values — file-system
browsing is suppressed for these:

| Flag | Accepted values | Commands |
|---|---|---|
| `--network` | `testnet`, `mainnet`, `futurenet` (+ any saved custom networks) | `debug`, `compare`, `search`, `generate-bindings`, `check-bindings`, `offline generate`, … |
| `--network` (init) | adds `standalone` | `init` |
| `--theme` | `default`, `dark`, `light`, `deuteranopia`, `protanopia`, `tritanopia`, `high-contrast` | `debug`, `compare`, `trace` |
| `--format` / `--export-format` | `html`, `markdown`, `json`, `text` | `trace` |
| `--format` | `text`, `json` | `debug`, `export`, `generate-bindings` |
| `--format` | `text`, `json`, `html`, `pdf`, `html,pdf` | `report` |
| `--trace-verbosity` | `summary`, `normal`, `verbose` | `debug`, `trace` |
| `--view` | `trace`, `flamegraph`, `events`, `auth`, `budget`, `storage` | `debug` |
| `--log-level` | `trace`, `debug`, `info`, `warn`, `error` | all (root persistent flag) |
| `--profile-format` | `html`, `svg` | all (root persistent flag) |
| `--runtime` | `node`, `browser`, `universal` | `generate-bindings`, `check-bindings` |
| `--spec-format` | `json`, `xdr` | `generate-bindings`, `check-bindings` |
| `--type` | `ledger-entry`, `diagnostic-event` | `xdr` |
| `--failover-strategy` | `weighted`, `sticky`, `round_robin` | (config-level) |

### Command aliases

The following Cobra aliases are registered on their primary commands and
therefore appear in completion alongside the canonical names:

| Alias | Primary command |
|---|---|
| `db` | `debug` |
| `ps` | `profile` |
| `pb:register` | `protocol:register` |
| `pb:unregister` | `protocol:unregister` |
| `pb:status` | `protocol:status` |
| `pb:verify` | `protocol:verify` |
| `pb:handle` | `protocol:handle` |
| `pb:diagnose` | `protocol:diagnose` |
| `pb:repair` | `protocol:repair` |

---

## Guarantees

- **No network calls during completion.**  All completion functions return
  static in-process values.  The only I/O is a local config file read in
  `--network` completion (for custom network names), which is non-blocking
  and falls back cleanly on error.
- **Enum flags never show filenames.**  Every enum-valued flag uses
  `cobra.ShellCompDirectiveNoFileComp` so the shell does not mix filenames
  into the suggestions.

---

## Regenerating snapshots

Completion scripts are golden-file tested.  CI fails when the generated
output changes without the snapshots being updated.

```bash
# Linux / macOS
./scripts/gen-completion-snapshots.sh

# Windows
.\scripts\gen-completion-snapshots.ps1

# Or directly via go test
go test ./internal/cmd/... -run TestCompletionSnapshot -update -count=1
```

Commit the updated files under `internal/cmd/testdata/completion/`.

---

## Adding a new enum-valued flag

1. Add the accepted values to the relevant `XxxValues` slice in
   `internal/cmd/completion_helpers.go`.
2. Add a `completeXxxFlag` function in the same file.
3. Call `_ = yourCmd.RegisterFlagCompletionFunc("flag-name", completeXxxFlag)`
   in the command's `init()` block.
4. Add the flag name to `knownEnumFlags` in
   `internal/cmd/completion_snapshot_test.go`.
5. Regenerate snapshots (see above).

The test `TestPublicFlagsHaveCompletion` will fail in CI if step 3 or 4 is
skipped, catching the gap before it ships.
