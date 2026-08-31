# gen-completion-snapshots.ps1
# Regenerate shell completion golden files used by completion_snapshot_test.go.
#
# Usage (from repo root):
#   .\scripts\gen-completion-snapshots.ps1
#
# Or via go test:
#   go test ./internal/cmd -run TestCompletionSnapshot -update

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot

try {
    Write-Host "Regenerating completion snapshots..."
    go test ./internal/cmd/... -run "TestCompletionSnapshot" -update -count=1
    Write-Host "Done. Files written to internal/cmd/testdata/completion/"
} finally {
    Pop-Location
}
