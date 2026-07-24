.PHONY: build test lint lint-strict lint-unused test-unused validate-ci validate-interface clean
.PHONY: rust-lint rust-lint-strict rust-test rust-build lint-all-strict
.PHONY: build test lint validate-errors clean bench bench-rpc bench-sim bench-replay bench-sourcemap bench-profile bench-perf-regression
.PHONY: fmt fmt-go fmt-rust pre-commit
.PHONY: release release-linux release-darwin release-windows package verify-release ts-build
.PHONY: mutation-test mutation-test-report mutation-test-ci mutation-test-install

# Build variables
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT_SHA?=$(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "unknown")
DIST_DIR?=dist/release

# Go build flags — inject version metadata at link time
GO_LDFLAGS=-ldflags "-s -w \
  -X 'github.com/dotandev/glassbox/internal/version.Version=$(VERSION)' \
  -X 'github.com/dotandev/glassbox/internal/version.CommitSHA=$(COMMIT_SHA)' \
  -X 'github.com/dotandev/glassbox/internal/version.BuildDate=$(BUILD_DATE)'"

# Build the main binary
build:
	go build $(GO_LDFLAGS) -o bin/glassbox ./cmd/glassbox

# Build for release (optimized, stripped)
build-release:
	go build $(GO_LDFLAGS) -o bin/glassbox ./cmd/glassbox

# ──────────────────────────────────────────────
# Cross-compilation targets
# ──────────────────────────────────────────────

release-linux:
	@mkdir -p $(DIST_DIR)
	GOOS=linux   GOARCH=amd64  CGO_ENABLED=0 go build $(GO_LDFLAGS) -o $(DIST_DIR)/glassbox-linux-amd64   ./cmd/glassbox
	GOOS=linux   GOARCH=arm64  CGO_ENABLED=0 go build $(GO_LDFLAGS) -o $(DIST_DIR)/glassbox-linux-arm64   ./cmd/glassbox
	@echo "Linux binaries built in $(DIST_DIR)"

release-darwin:
	@mkdir -p $(DIST_DIR)
	GOOS=darwin  GOARCH=amd64  CGO_ENABLED=0 go build $(GO_LDFLAGS) -o $(DIST_DIR)/glassbox-darwin-amd64  ./cmd/glassbox
	GOOS=darwin  GOARCH=arm64  CGO_ENABLED=0 go build $(GO_LDFLAGS) -o $(DIST_DIR)/glassbox-darwin-arm64  ./cmd/glassbox
	@echo "macOS binaries built in $(DIST_DIR)"

release-windows:
	@mkdir -p $(DIST_DIR)
	GOOS=windows GOARCH=amd64  CGO_ENABLED=0 go build $(GO_LDFLAGS) -o $(DIST_DIR)/glassbox-windows-amd64.exe ./cmd/glassbox
	@echo "Windows binary built in $(DIST_DIR)"

# Build TypeScript/Node artifacts
ts-build:
	npm ci --prefer-offline
	npm run build
	@echo "TypeScript artifacts built in dist/"

# Build all release targets
release: release-linux release-darwin release-windows ts-build
	@echo "All release targets built"

# ──────────────────────────────────────────────
# Packaging: checksums + archives
# ──────────────────────────────────────────────

# Produce per-binary SHA-256 checksums and zip/tar archives.
# Requires: sha256sum (Linux) or shasum -a 256 (macOS), zip, tar.
package: release
	@echo "Packaging release artifacts..."
	@cd $(DIST_DIR) && \
	  for f in glassbox-linux-* glassbox-darwin-* glassbox-windows-*; do \
	    [ -f "$$f" ] || continue; \
	    echo "  archiving $$f"; \
	    case "$$f" in \
	      *.exe) zip "$$f.zip" "$$f" ;; \
	      *)     tar czf "$$f.tar.gz" "$$f" ;; \
	    esac; \
	  done
	@echo "  generating checksums..."
	@cd $(DIST_DIR) && \
	  if command -v sha256sum >/dev/null 2>&1; then \
	    sha256sum *.tar.gz *.zip 2>/dev/null > checksums.sha256 || true; \
	  else \
	    shasum -a 256 *.tar.gz *.zip 2>/dev/null > checksums.sha256 || true; \
	  fi
	@echo "  writing version metadata..."
	@printf 'version=%s\ncommit=%s\nbuild_date=%s\n' \
	  "$(VERSION)" "$(COMMIT_SHA)" "$(BUILD_DATE)" > $(DIST_DIR)/version.txt
	@echo "Package complete. Artifacts in $(DIST_DIR):"
	@ls -lh $(DIST_DIR)

# Smoke-test the produced binaries
verify-release:
	@bash scripts/verify-release.sh $(DIST_DIR)

# Check binary sizes against thresholds
size-check:
	@bash scripts/check_binary_size.sh

# Run tests
test:
	go test ./...

# Run full linter suite
lint:
	golangci-lint run

# Run strict linting (fail on all warnings)
lint-strict:
	@echo "Running strict Go linting..."
	@golangci-lint run --config=.golangci.yml --max-issues-per-linter=0 --max-same-issues=0
	@go vet ./...
	@echo " Strict linting passed"

# Run unused code detection
lint-unused:
	./scripts/lint-unused.sh

# Test unused code detection setup
test-unused:
	./scripts/test-unused-detection.sh

# Validate CI/CD configuration
validate-ci:
	./scripts/validate-ci.sh
# Validate error standardization
validate-errors:
	./scripts/validate-errors.sh

# Validate interface implementation
validate-interface:
	./scripts/validate-interface.sh

# Clean build artifacts
clean:
	rm -rf bin/
	go clean -cache

# Install dependencies
deps:
	go mod tidy
	go mod download

# Run all benchmarks (RPC, replay, sourcemap, simulator)
bench:
	go test -bench=. -benchmem ./internal/rpc ./internal/replay ./internal/sourcemap ./internal/simulator

# Run RPC benchmarks only
bench-rpc:
	go test -bench=. -benchmem ./internal/rpc

# Run simulator benchmarks only
bench-sim:
	go test -bench=. -benchmem ./internal/simulator

# Run replay benchmarks only
bench-replay:
	go test -bench=. -benchmem ./internal/replay

# Run sourcemap benchmarks only
bench-sourcemap:
	go test -bench=. -benchmem ./internal/sourcemap

# Run benchmarks with CPU profiling
bench-profile:
	go test -bench=. -benchmem -cpuprofile=cpu.prof ./internal/rpc ./internal/replay ./internal/sourcemap ./internal/simulator

# Run performance regression tests for simulator
bench-perf-regression:
	@echo "Running performance regression tests..."
	@go test -v -run 'TestPerfRegression' ./internal/simulator/...
	@echo " Performance regression tests passed"

# Rust simulator targets
.PHONY: rust-lint rust-lint-strict rust-test rust-build

# Run Rust linting
rust-lint:
	cd simulator && cargo clippy --all-targets --all-features

# Run strict Rust linting (fail on all warnings)
rust-lint-strict:
	@echo "Running strict Rust linting..."
	@cd simulator && cargo clippy --all-targets --all-features -- \
		-D warnings \
		-D clippy::all \
		-D unused-variables \
		-D unused-imports \
		-D unused-mut \
		-D dead-code \
		-D unused-assignments \
		-W clippy::pedantic \
		-W clippy::nursery
	@echo " Strict Rust linting passed"

# Run Rust tests
rust-test:
	cd simulator && cargo test --verbose

# Build Rust simulator
rust-build:
	cd simulator && cargo build --verbose

# Run all strict linting (Go + Rust)
lint-all-strict: lint-strict rust-lint-strict
	@echo " All strict linting passed"

# ──────────────────────────────────────────────
# Formatting targets
# ──────────────────────────────────────────────

# Format Go files (gofmt + goimports)
fmt-go:
	@echo "Formatting Go files..."
	@gofmt -w .
	@if command -v goimports >/dev/null 2>&1; then \
		goimports -w .; \
	else \
		echo "⚠  goimports not found. Install: go install golang.org/x/tools/cmd/goimports@latest"; \
	fi
	@echo "✓ Go formatting done"

# Format Rust files (cargo fmt)
fmt-rust:
	@echo "Formatting Rust files..."
	@cd simulator && cargo fmt
	@echo "✓ Rust formatting done"

# Format everything (Go + Rust)
fmt: fmt-go fmt-rust
	@echo "✓ All formatting done"

# ──────────────────────────────────────────────
# Pre-commit setup
# ──────────────────────────────────────────────

# Install pre-commit hooks
pre-commit:
	@echo "Setting up pre-commit hooks..."
	@if command -v pre-commit >/dev/null 2>&1; then \
		pre-commit install; \
		echo "✓ Pre-commit hooks installed"; \
	else \
		echo "⚠  pre-commit not found. Install: pip install pre-commit"; \
		exit 1; \
	fi

# ──────────────────────────────────────────────
# Mutation testing (gremlins)
#
# gremlins is a Go mutation testing tool that runs the existing test suite
# against small code mutations to find untested branches.
# https://github.com/go-gremlins/gremlins
#
# Focused package scope — only packages whose validation logic is worth
# mutation-testing (config, trace export, session integrity, audit validation).
# Expanding this set is cheap; shrinking it after the fact is painful.
#
# Agreed mutation score threshold: 70 % (documented in docs/ci-artifacts.md).
# ──────────────────────────────────────────────

# Packages in scope for mutation testing.
# One per line so diffs are easy to review when scope changes.
MUTATION_PACKAGES := \
	./internal/cmd/... \
	./internal/session/... \
	./internal/trace/... \
	./internal/simulator/...

# HTML and JSON report output directory.
MUTATION_REPORT_DIR ?= mutation-report

# Minimum mutation score (0-100).  Jobs fail below this threshold.
MUTATION_THRESHOLD ?= 70

# Install gremlins from the pinned version used in CI.
# Pin to a specific version so contributors get the same results locally.
GREMLINS_VERSION ?= v0.5.1

mutation-test-install:
	@echo "Installing gremlins $(GREMLINS_VERSION)..."
	@go install github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION)
	@echo "gremlins installed: $$(gremlins --version 2>&1 || true)"

# Run mutation tests interactively.  Produces an HTML report in
# $(MUTATION_REPORT_DIR)/ that can be opened in a browser.
#
# Usage:
#   make mutation-test                  # use default packages and threshold
#   make mutation-test MUTATION_THRESHOLD=60
#   make mutation-test MUTATION_PACKAGES=./internal/session/...
mutation-test: mutation-test-install
	@echo "Running mutation tests..."
	@echo "  Scope     : $(MUTATION_PACKAGES)"
	@echo "  Threshold : $(MUTATION_THRESHOLD)%"
	@echo "  Report    : $(MUTATION_REPORT_DIR)/"
	@mkdir -p "$(MUTATION_REPORT_DIR)"
	@gremlins unleash \
		--threshold=$(MUTATION_THRESHOLD) \
		--output=$(MUTATION_REPORT_DIR)/gremlins-report.json \
		$(MUTATION_PACKAGES) \
		2>&1 | tee "$(MUTATION_REPORT_DIR)/gremlins.log"
	@echo "Mutation test complete.  Report: $(MUTATION_REPORT_DIR)/gremlins-report.json"

# CI-mode mutation test: machine-readable JSON output only, no TTY spinner,
# exits non-zero when the mutation score falls below MUTATION_THRESHOLD.
# Called by .github/workflows/mutation-test.yml.
mutation-test-ci: mutation-test-install
	@mkdir -p "$(MUTATION_REPORT_DIR)"
	gremlins unleash \
		--threshold=$(MUTATION_THRESHOLD) \
		--output=$(MUTATION_REPORT_DIR)/gremlins-report.json \
		--silent \
		$(MUTATION_PACKAGES) \
		2>&1 | tee "$(MUTATION_REPORT_DIR)/gremlins.log"

# Generate a human-readable summary from the last JSON report.
# Prints surviving mutants grouped by package so contributors know where to
# add tests.  Requires the JSON report to exist (run mutation-test first).
mutation-test-report:
	@if [ ! -f "$(MUTATION_REPORT_DIR)/gremlins-report.json" ]; then \
		echo "No report found.  Run 'make mutation-test' first."; \
		exit 1; \
	fi
	@echo "=== Mutation Test Summary ==="
	@python3 - "$(MUTATION_REPORT_DIR)/gremlins-report.json" "$(MUTATION_THRESHOLD)" <<'PYEOF'
import json, sys, collections

report_path = sys.argv[1]
threshold   = int(sys.argv[2])

with open(report_path) as f:
    data = json.load(f)

total    = data.get("total_mutants", 0)
killed   = data.get("killed",        0)
survived = data.get("survived",      0)
score    = round(killed / total * 100, 1) if total else 0.0

print(f"  Total mutants : {total}")
print(f"  Killed        : {killed}")
print(f"  Survived      : {survived}")
print(f"  Score         : {score}% (threshold: {threshold}%)")
print()

if survived:
    by_pkg = collections.defaultdict(list)
    for m in data.get("mutants", []):
        if m.get("status") == "SURVIVED":
            pkg = m.get("package", "unknown")
            by_pkg[pkg].append(m)
    print("Surviving mutants by package:")
    for pkg in sorted(by_pkg):
        print(f"  {pkg} ({len(by_pkg[pkg])} mutant(s))")
        for m in by_pkg[pkg][:5]:
            print(f"    • {m.get('file','?')}:{m.get('line','?')} — {m.get('mutation_type','?')}")
        if len(by_pkg[pkg]) > 5:
            print(f"    … and {len(by_pkg[pkg])-5} more (see full report)")

if score < threshold:
    print(f"\nFAIL: mutation score {score}% is below threshold {threshold}%")
    sys.exit(1)
else:
    print(f"\nPASS: mutation score {score}% meets threshold {threshold}%")
PYEOF
