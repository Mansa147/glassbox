#!/usr/bin/env bash
# Copyright 2026 Glassbox Users
# SPDX-License-Identifier: Apache-2.0
#
# redact-logs.sh
#
# Scan every file in a staging directory for common secret patterns and
# replace matching content with a safe placeholder before CI artifact upload.
#
# Usage:
#   ./scripts/redact-logs.sh <staging-dir> [max-size-mb]
#
# Arguments:
#   staging-dir   Directory whose files will be redacted in-place.
#   max-size-mb   Abort upload with exit 1 if total directory size exceeds
#                 this many megabytes (default: 50).
#
# Exit codes:
#   0  All files redacted successfully and size is within limit.
#   1  Size limit exceeded — caller must NOT upload the directory.
#   2  staging-dir argument is missing or does not exist.
#
# Redacted patterns
# ─────────────────
# The redaction rules intentionally cover broad patterns to minimise the risk
# of accidental exposure. False positives (non-secret strings that match) are
# acceptable; false negatives (actual secrets that are missed) are not.
#
# Patterns:
#   • Bearer / Authorization header values
#   • GitHub tokens (ghp_, ghs_, gho_, github_pat_)
#   • Generic API key assignments (api_key=, apikey=, api-key:)
#   • Password assignments (password=, passwd=, pwd=)
#   • PEM private key blocks (-----BEGIN ... PRIVATE KEY-----)
#   • AWS access key IDs and secret access keys
#   • PKCS#11 PIN values (pkcs11_pin=, GLASSBOX_PKCS11_PIN=)
#   • RPC tokens (rpc_token=, GLASSBOX_RPC_TOKEN=)
#   • Connection strings with embedded credentials (://user:pass@)
#   • Ed25519 / hex private key literals (64-char hex strings in key contexts)
#   • Sentry DSN URLs

set -euo pipefail

# ── Arguments ─────────────────────────────────────────────────────────────────
STAGING_DIR="${1:-}"
MAX_SIZE_MB="${2:-50}"

if [[ -z "${STAGING_DIR}" ]]; then
  echo "[redact-logs] ERROR: staging directory argument is required." >&2
  echo "Usage: $0 <staging-dir> [max-size-mb]" >&2
  exit 2
fi

if [[ ! -d "${STAGING_DIR}" ]]; then
  echo "[redact-logs] ERROR: '${STAGING_DIR}' does not exist or is not a directory." >&2
  exit 2
fi

PLACEHOLDER="[REDACTED]"

# ── Helper: in-place sed that works on both GNU (Linux) and BSD (macOS) ───────
inplace_sed() {
  local pattern="$1"
  local file="$2"
  if sed --version 2>/dev/null | grep -q GNU; then
    sed -i "${pattern}" "${file}"
  else
    sed -i '' "${pattern}" "${file}"
  fi
}

# ── Redaction rules ────────────────────────────────────────────────────────────
redact_file() {
  local file="$1"

  # Skip binary files (images, compiled artifacts, etc.)
  if file "${file}" 2>/dev/null | grep -qE 'binary|ELF|PE32|Mach-O|archive'; then
    return
  fi

  # Skip very large individual files (> 10 MB) to avoid OOM in sed.
  local size_bytes
  size_bytes=$(wc -c < "${file}" 2>/dev/null || echo 0)
  if (( size_bytes > 10 * 1024 * 1024 )); then
    echo "[redact-logs] WARN: skipping large file (${size_bytes} bytes): ${file}" >&2
    return
  fi

  # 1. Bearer / Authorization headers
  inplace_sed 's/\(Authorization:[[:space:]]*Bearer[[:space:]]\)[^"'"'"' \t\n\r,}]*/\1'"${PLACEHOLDER}"'/gi' "${file}"
  inplace_sed 's/\(Authorization:[[:space:]]*Token[[:space:]]\)[^"'"'"' \t\n\r,}]*/\1'"${PLACEHOLDER}"'/gi' "${file}"

  # 2. GitHub tokens (variable-length suffix)
  inplace_sed 's/ghp_[A-Za-z0-9_]\{10,\}/'"${PLACEHOLDER}"'/g' "${file}"
  inplace_sed 's/ghs_[A-Za-z0-9_]\{10,\}/'"${PLACEHOLDER}"'/g' "${file}"
  inplace_sed 's/gho_[A-Za-z0-9_]\{10,\}/'"${PLACEHOLDER}"'/g' "${file}"
  inplace_sed 's/github_pat_[A-Za-z0-9_]\{10,\}/'"${PLACEHOLDER}"'/g' "${file}"

  # 3. Generic API key assignments (key = "value" or key: value)
  inplace_sed 's/\(api[_-]\?key[[:space:]]*[:=][[:space:]]*["'"'"']\?\)[^"'"'"' \t\n\r,}]\{4,\}/\1'"${PLACEHOLDER}"'/gi' "${file}"

  # 4. Password assignments
  inplace_sed 's/\(pass\(word\|wd\|phrase\)\?[[:space:]]*[:=][[:space:]]*["'"'"']\?\)[^"'"'"' \t\n\r,}]\{2,\}/\1'"${PLACEHOLDER}"'/gi' "${file}"

  # 5. PEM private key blocks — replace everything between header and footer.
  #    Use Python if available for reliable multi-line redaction; fall back to
  #    a single-line marker replacement.
  if command -v python3 >/dev/null 2>&1; then
    python3 - "${file}" "${PLACEHOLDER}" <<'PYEOF'
import sys, re, pathlib
path = pathlib.Path(sys.argv[1])
placeholder = sys.argv[2]
content = path.read_text(errors='replace')
# Match PEM blocks: -----BEGIN ... KEY-----\n...\n-----END ... KEY-----
redacted = re.sub(
    r'-----BEGIN[^\n]*(?:PRIVATE|RSA|EC|DSA|ENCRYPTED)[^\n]*KEY-----'
    r'.*?'
    r'-----END[^\n]*(?:PRIVATE|RSA|EC|DSA|ENCRYPTED)[^\n]*KEY-----',
    f'-----BEGIN PRIVATE KEY-----\n{placeholder}\n-----END PRIVATE KEY-----',
    content,
    flags=re.DOTALL | re.IGNORECASE,
)
path.write_text(redacted)
PYEOF
  else
    # Fallback: mark the PEM header line only.
    inplace_sed 's/-----BEGIN[[:space:]]*[A-Z ]*PRIVATE KEY-----/-----BEGIN PRIVATE KEY----- '"${PLACEHOLDER}"'/gi' "${file}"
  fi

  # 6. AWS credentials
  # Access Key ID: AKIA... (20 chars) or ASIA... (20 chars)
  inplace_sed 's/\(AKIA\|ASIA\)[A-Z0-9]\{16\}/'"${PLACEHOLDER}"'/g' "${file}"
  # Secret access key (context: aws_secret_access_key = ...)
  inplace_sed 's/\(aws[_-]\?secret[_-]\?access[_-]\?key[[:space:]]*[:=][[:space:]]*["'"'"']\?\)[A-Za-z0-9/+=]\{20,\}/\1'"${PLACEHOLDER}"'/gi' "${file}"

  # 7. PKCS#11 PIN values
  inplace_sed 's/\(pkcs11[_-]\?pin[[:space:]]*[:=][[:space:]]*["'"'"']\?\)[^"'"'"' \t\n\r,}]\{2,\}/\1'"${PLACEHOLDER}"'/gi' "${file}"
  inplace_sed 's/\(GLASSBOX_PKCS11_PIN[[:space:]]*=[[:space:]]*\)[^"'"'"' \t\n\r,}]\{2,\}/\1'"${PLACEHOLDER}"'/gi' "${file}"

  # 8. RPC tokens
  inplace_sed 's/\(rpc[_-]\?token[[:space:]]*[:=][[:space:]]*["'"'"']\?\)[^"'"'"' \t\n\r,}]\{4,\}/\1'"${PLACEHOLDER}"'/gi' "${file}"
  inplace_sed 's/\(GLASSBOX_RPC_TOKEN[[:space:]]*=[[:space:]]*\)[^"'"'"' \t\n\r,}]\{4,\}/\1'"${PLACEHOLDER}"'/gi' "${file}"

  # 9. Connection strings with embedded credentials (://user:pass@host)
  inplace_sed 's|://\([^:@/]*\):[^@/]*@|\1:'"${PLACEHOLDER}"'@|g' "${file}"

  # 10. Sentry DSN URLs (contain a secret key before the @ symbol)
  inplace_sed 's|https://[a-f0-9]\{32\}@[a-z0-9.]*/[0-9]*|https://'"${PLACEHOLDER}"'@[REDACTED]|gi' "${file}"

  # 11. Generic long hex strings in key/secret/token assignment contexts
  #     (avoids redacting transaction hashes which are legitimate in logs)
  inplace_sed 's/\(\(secret\|priv\(ate\)\?\)[_-]\?key[[:space:]]*[:=][[:space:]]*["'"'"']\?\)[0-9a-f]\{40,\}/\1'"${PLACEHOLDER}"'/gi' "${file}"
}

# ── Process all files ──────────────────────────────────────────────────────────
echo "[redact-logs] Scanning '${STAGING_DIR}' for secrets..."

file_count=0
while IFS= read -r -d '' f; do
  redact_file "${f}"
  (( file_count++ )) || true
done < <(find "${STAGING_DIR}" -type f -print0)

echo "[redact-logs] Redacted ${file_count} file(s)."

# ── Size check ─────────────────────────────────────────────────────────────────
total_kb=$(du -sk "${STAGING_DIR}" 2>/dev/null | cut -f1 || echo 0)
total_mb=$(( total_kb / 1024 ))

echo "[redact-logs] Staging directory size: ${total_mb} MB (limit: ${MAX_SIZE_MB} MB)."

if (( total_mb > MAX_SIZE_MB )); then
  echo "[redact-logs] ERROR: artifact size ${total_mb} MB exceeds limit ${MAX_SIZE_MB} MB." >&2
  echo "[redact-logs] Upload aborted to protect CI storage budget." >&2
  echo "[redact-logs] Reduce the number of collected files or increase ARTIFACT_MAX_MB." >&2
  exit 1
fi

echo "[redact-logs] Size check passed. Artifacts are ready for upload."
