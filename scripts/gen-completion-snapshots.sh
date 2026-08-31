#!/usr/bin/env bash
# gen-completion-snapshots.sh
# Regenerate shell completion golden files used by completion_snapshot_test.go.
#
# Usage (from repo root):
#   ./scripts/gen-completion-snapshots.sh
#
# Or via go test:
#   go test ./internal/cmd -run TestCompletionSnapshot -update
set -euo pipefail

cd "$(dirname "$0")/.."
echo "Regenerating completion snapshots..."
go test ./internal/cmd/... -run "TestCompletionSnapshot" -update -count=1
echo "Done. Files written to internal/cmd/testdata/completion/"
