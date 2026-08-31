# Running the Validation Suite

This file documents how to run the scheduled validation suite locally and in CI.

Prerequisites (local)
- Go toolchain installed and on PATH (matched to `go.mod`).
- Node.js (v20 recommended) and `npm` available for TypeScript artifacts.
- Bash and `python3` available for the runner scripts.

Local run

```bash
# Build the Go binary
go build -o bin/glassbox ./cmd/glassbox

# Install Node deps (if needed) and build TS artifacts
npm ci --prefer-offline
npm run build --if-present

# Make scripts executable then run the validation runner
chmod +x scripts/run-validation-suite.sh scripts/generate-compat-report.sh
GLASSBOX_BINARY=./bin/glassbox scripts/run-validation-suite.sh

# After a run, artifacts are written to ci-artifacts/validation/
# You can aggregate compatibility reports across runs:
scripts/generate-compat-report.sh ci-artifacts/validation ci-artifacts/validation/compatibility_report.md
```

CI notes
- The GitHub workflow `.github/workflows/validation-suite.yml` runs the suite
  quarterly on `ubuntu-latest` and `windows-latest`. Artifacts are uploaded
  with a 30-day retention. The workflow will run in hosted runners that include
  the required toolchains.

Troubleshooting
- If `go` or `bash` are not available locally, install them or run the suite
  inside a CI environment or container that matches the workflow (Ubuntu).
