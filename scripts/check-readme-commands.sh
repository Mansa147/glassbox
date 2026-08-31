#!/usr/bin/env bash
# Copyright 2026 Glassbox Users
# SPDX-License-Identifier: Apache-2.0
#
# check-readme-commands.sh
#
# Audit every `glassbox <subcommand>` invocation that appears in README.md
# against the actual CLI surface reported by `glassbox --help`.
#
# Exit codes:
#   0  all command references are valid (or explicitly allowed)
#   1  one or more unknown command references detected
#
# Usage:
#   ./scripts/check-readme-commands.sh [--readme <path>] [--binary <path>]
#
# Environment:
#   GLASSBOX_BINARY   Path to a pre-built binary (skips local build step)
#   README_PATH       Override the README path (default: README.md)
#
# The script accepts a small allow-list of well-known conceptual patterns
# (e.g. generic <transaction-hash> placeholders) that must not be treated
# as subcommand names.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
README_PATH="${README_PATH:-${REPO_ROOT}/README.md}"
BINARY_PATH="${GLASSBOX_BINARY:-}"

# ── Parse flags ──────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --readme)   README_PATH="$2"; shift 2 ;;
    --binary)   BINARY_PATH="$2"; shift 2 ;;
    *)          echo "Unknown flag: $1" >&2; exit 1 ;;
  esac
done

# ── Locate or build the binary ────────────────────────────────────────────────
if [[ -z "$BINARY_PATH" ]]; then
  candidates=(
    "${REPO_ROOT}/glassbox"
    "${REPO_ROOT}/bin/glassbox"
    "${REPO_ROOT}/dist/glassbox"
  )
  for c in "${candidates[@]}"; do
    if [[ -x "$c" ]]; then
      BINARY_PATH="$c"
      break
    fi
  done
fi

if [[ -z "$BINARY_PATH" || ! -x "$BINARY_PATH" ]]; then
  echo "[INFO] No pre-built binary found; building glassbox from source..."
  BUILD_OUT="${REPO_ROOT}/glassbox"
  (cd "${REPO_ROOT}" && GOWORK=off go build -o "${BUILD_OUT}" ./cmd/glassbox)
  BINARY_PATH="${BUILD_OUT}"
fi

echo "[INFO] Using binary: ${BINARY_PATH}"
echo "[INFO] Auditing README: ${README_PATH}"

# ── Collect the top-level subcommands from --help ─────────────────────────────
#
# `glassbox --help` lists groups and their commands.  We scrape every word that
# appears after the leading whitespace on a help line and looks like a command
# name (lowercase letters, digits, hyphens, and colons — colons are used by
# protocol:register, audit:sign, etc.).
#
known_cmds=()
while IFS= read -r line; do
  # Skip blank lines, group headers (end with colon-only), flag lines (--)
  trimmed="${line#"${line%%[! ]*}"}"   # ltrim
  [[ -z "$trimmed" ]] && continue
  [[ "$trimmed" == --* ]] && continue
  [[ "$trimmed" == [A-Z]* ]] && continue  # section headers
  # First word of the trimmed line is the subcommand name
  word="${trimmed%% *}"
  # Must contain only valid command chars
  if [[ "$word" =~ ^[a-z][a-z0-9:_-]*$ ]]; then
    known_cmds+=("$word")
  fi
done < <("${BINARY_PATH}" --help 2>&1 || true)

# Also harvest subcommands from the help of every known top-level command.
# This lets us validate two-level references like `cache status`, `session save`.
declare -A known_subcmds  # key = "parent sub"
for cmd in "${known_cmds[@]}"; do
  while IFS= read -r line; do
    trimmed="${line#"${line%%[! ]*}"}"
    [[ -z "$trimmed" ]] && continue
    [[ "$trimmed" == --* ]] && continue
    [[ "$trimmed" == [A-Z]* ]] && continue
    word="${trimmed%% *}"
    if [[ "$word" =~ ^[a-z][a-z0-9:_-]*$ && "$word" != "$cmd" ]]; then
      known_subcmds["${cmd} ${word}"]=1
    fi
  done < <("${BINARY_PATH}" "${cmd}" --help 2>&1 || true)
done

echo "[INFO] Known top-level commands: ${known_cmds[*]:-<none>}"

# ── Patterns that are valid in README but are NOT subcommand names ─────────────
#
# These are fragments that appear right after `glassbox ` in a code block but
# should be treated as conceptual / placeholder text, not real commands.
readonly -a ALLOWED_NON_COMMANDS=(
  "--"         # global flags used standalone: glassbox --help, --version
  "<"          # glassbox <transaction-hash>
)

# ── Extract every `glassbox ...` invocation from README code blocks ────────────
#
# We only look inside fenced code blocks (``` ... ```) to avoid false-positives
# from inline backtick references in prose.

in_block=0
errors=()

while IFS= read -r line; do
  # Toggle code-block state on triple-backtick lines
  if [[ "$line" =~ ^\`\`\` ]]; then
    (( in_block = 1 - in_block ))
    continue
  fi
  [[ "$in_block" -eq 0 ]] && continue

  # Match lines that start with optional whitespace then "glassbox"
  if [[ "$line" =~ (^|[[:space:]])glassbox[[:space:]] ]]; then
    # Extract the token immediately after "glassbox "
    rest="${line#*glassbox }"
    rest="${rest%%[[:space:]]*}"   # first whitespace-delimited token

    # Skip empty, flag-only, or allowed-placeholder tokens
    skip=0
    for allowed in "${ALLOWED_NON_COMMANDS[@]}"; do
      [[ "$rest" == "$allowed"* ]] && skip=1 && break
    done
    [[ "$skip" -eq 1 ]] && continue
    [[ -z "$rest" ]] && continue

    # Check against known top-level commands
    found=0
    for cmd in "${known_cmds[@]}"; do
      [[ "$rest" == "$cmd" || "$rest" == "$cmd:"* ]] && found=1 && break
    done

    if [[ "$found" -eq 0 ]]; then
      errors+=("Unknown command reference: 'glassbox ${rest}' (line: ${line})")
    fi
  fi
done < "${README_PATH}"

# ── Report ─────────────────────────────────────────────────────────────────────
echo ""
if [[ "${#errors[@]}" -eq 0 ]]; then
  echo "[OK] All glassbox command references in README.md are valid."
  exit 0
else
  echo "[FAIL] ${#errors[@]} unknown command reference(s) found in README.md:"
  for e in "${errors[@]}"; do
    echo "  - ${e}"
  done
  echo ""
  echo "Fix: either correct the command name in README.md or add the new"
  echo "     subcommand to the CLI (internal/cmd/) and re-run this script."
  exit 1
fi
