# Regression Test Guide

This guide explains how to add a structured regression test when you file or
fix a bug in Glassbox. Following this workflow ensures every regression is:

- **reproducible** — a minimal fixture captures the exact failure input
- **attributable** — a comment links the test to the original issue or PR
- **categorised** — the failure class is named, not just "it errored"
- **self-contained** — no live networks, real keys, or secrets involved

---

## Quick start (5-step recipe)

```
1. File (or find) a GitHub issue that describes the bug.
2. Create a fixture file in test/regression/fixtures/<layer>/.
3. Copy internal/cmd/regression_example_test.go as a starting point.
4. Write the test: Arrange → Act → Assert → name the failure class.
5. Add the fixture filename and issue number to the issue description.
```

That is it. The sections below explain each step in detail.

---

## 1. File a GitHub issue

Every regression test must trace back to an issue. If one does not exist,
open one first. Use this section in the issue body:

```markdown
## Regression fixture

- **Fixture directory**: `test/regression/fixtures/<layer>/`
- **Suggested filename**: `<layer>_<scenario>_issue<N>.<ext>`
- **Failure class**: <one-line description, e.g. "CPU budget exhaustion silently ignored">
- **Canonical input**: <paste minimal JSON or command here>
```

The fix PR must include both the fixture file and a test referencing it.
Reviewers will reject a bug fix that lacks a regression test.

---

## 2. Choose the right layer and fixture directory

| Layer | Directory | What goes here |
|-------|-----------|----------------|
| RPC | `test/regression/fixtures/rpc/` | Stellar RPC response JSON stubs |
| Replay | `test/regression/fixtures/replay/` | Snapshot registries and ledger-state maps |
| Trace | `test/regression/fixtures/trace/` | Serialised ExecutionTrace JSON |
| Source map | `test/regression/fixtures/sourcemap/` | Minimal WASM stubs and alias JSON |
| Session | `test/regression/fixtures/session/` | Session record JSON and SQLite dumps |
| Audit | `test/regression/fixtures/audit/` | Payloads, signed logs, TEST-ONLY keys |
| CLI | `test/regression/fixtures/cli/` | Expected output fragments and env files |

Each directory has its own `README.md` with layer-specific naming rules. Read
it before creating your fixture.

---

## 3. Create a minimal fixture

### Naming

```
<layer>_<scenario-slug>_<issue-or-pr-slug>.<ext>
```

Examples:

```
rpc_gettransaction_notfound_issue150.json
trace_empty_steps_pr319.trace.json
session_missing_txhash_issue230.session.json
audit_empty_payload_rejected.payload.json
```

### Canonical stub values

Always use these stubs unless the test explicitly exercises field validation:

| Field | Value |
|-------|-------|
| Transaction hash | `5c0a1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab` |
| Envelope XDR | `AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=` |
| Network | `testnet` |
| Timestamp | `2026-01-01T00:00:00Z` |
| Session ID prefix | `sess_test_` |

These constants are also available as Go package-level identifiers in
`internal/testhelpers`:

```go
testhelpers.CanonicalTxHash
testhelpers.CanonicalNetwork
testhelpers.CanonicalEnvelopeXDR
testhelpers.CanonicalTimestamp
```

### Minimal reproduction rule

Include **only the fields required to hit the code path under test**. Omit or
zero-value every other field. A fixture that reproduces the failure in 5 fields
is better than one that copies a full real-world response.

### Secret avoidance

Fixtures must **never** contain:

- Real private keys, PKCS#8 PEM blobs, HSM PINs, or KMS key ARNs
- Real transaction hashes from mainnet or testnet
- Real contract IDs from deployed contracts
- Bearer tokens, API keys, or RPC credentials

Keys in `test/regression/fixtures/audit/` must use the `testonly_` prefix in
both the filename and a comment at the top of the file.

---

## 4. Write the test

### File location

Unit and package-level regression tests live in the same package as the code
under test, named `*_regression_test.go` or appended to an existing
`regression_*_test.go` file.

Integration tests that require a built binary live in `integration/`.

### Template

Use `internal/cmd/regression_example_test.go` as the canonical starting point.
Copy the file, rename the functions, and fill in the details.

Every test must follow the **Arrange → Act → Assert** structure and carry a
`// Failure class:` comment explaining the category:

```go
// TestRegression_<Layer>_<Scenario>_<Observation> verifies that ...
//
// Original failure: <what the user saw / what was broken>. (Closes #<N>.)
func TestRegression_<Layer>_<Scenario>_<Observation>(t *testing.T) {
    // Failure class: <one-line category, e.g. "budget exhaustion silently swallowed">

    // 1. Arrange
    fixture := testhelpers.New<Layer>Fixture().<Options>().Build()

    // 2. Act
    result, err := SomeFunctionUnderTest(fixture)

    // 3. Assert
    if err == nil {
        t.Fatal("expected an error, got nil")
    }
    if !strings.Contains(err.Error(), "<expected substring>") {
        t.Errorf("error should mention '<expected substring>', got: %q", err.Error())
    }
}
```

### Using the testhelpers builders

All layer builders live in `internal/testhelpers/`. Import the package and use
fluent chaining:

```go
import "github.com/dotandev/glassbox/internal/testhelpers"

// RPC fixture
resp := testhelpers.NewRPCFixture().NotFound().Build()

// Simulator request
req := testhelpers.NewSimRequestFixture().
    WithEnvelope(myXDR).
    WithLedgerEntry("key1", "value1").
    Build()

// Simulator response with budget exhaustion
failResp := testhelpers.NewSimResponseFixture().
    WithError("ExceededInstructions").
    WithBudgetExhausted().
    Build()

// Trace with two events
tr := testhelpers.NewTraceFixture().
    AddContractCallEvent("CTEST...", "transfer").
    AddErrorEvent("CTEST...", "panic: out of resources").
    Build()

// Session missing TxHash (triggers validation failure)
sess := testhelpers.NewSessionFixture().MissingTxHash().Build()

// Audit payload
payload := testhelpers.NewAuditPayloadFixture().BuildString()
```

### Asserting on the failure class

Do not assert only that an error was returned. Assert that the **specific
failure class** was reproduced:

```go
// Bad — too generic
if err == nil {
    t.Fatal("expected error")
}

// Good — names the category
if err == nil {
    t.Fatal("CPU budget exhaustion must return an error, not success")
}
if !strings.Contains(strings.ToLower(err.Error()), "budget") {
    t.Errorf("error should mention 'budget' for CPU exhaustion, got: %q", err.Error())
}
```

### Running only regression tests

```bash
# Run all regression tests in internal/cmd
go test -run TestRegression ./internal/cmd/...

# Run a specific test
go test -run TestRegression_Session_MissingTxHash ./internal/cmd/...

# Run all integration regression tests
go test -run TestRegression ./integration/...
```

---

## 5. Update the issue

Once the PR is merged, add the fixture path and test name to the issue:

```
## Regression (fixed in PR #<N>)

- **Test**: `TestRegression_<Layer>_<Scenario>_<Observation>` in `internal/cmd/regression_example_test.go`
- **Fixture**: `test/regression/fixtures/<layer>/<filename>`
```

---

## Failure classes

Use one of the following standard failure-class names in the `// Failure class:`
comment. If none fits, add a new one and describe it here.

| Class | Description |
|-------|-------------|
| `rpc-not-found-masquerade` | NOT_FOUND presented as a connectivity error |
| `budget-exhaustion-silent` | CPU/memory exhaustion swallowed without a diagnostic |
| `empty-trace-silent-export` | Zero-step trace written to disk without an error |
| `late-validation` | Flag/input validated after an expensive operation instead of at startup |
| `missing-field-raw-error` | Required field absent but returned as a DB/marshal error instead of a user message |
| `empty-payload-signed` | Empty or blank input passed to signing without rejection |
| `stale-command-reference` | CLI example in docs uses a removed or renamed command/flag |
| `panic-on-bad-input` | Any `panic` reached from user-supplied input |
| `missing-remediation-hint` | Error returned with no `Fix:` or `Hint:` guidance |

---

## Checklist for reviewers

When reviewing a PR that fixes a bug, verify:

- [ ] A `TestRegression_*` test is present that reproduces the original failure
- [ ] The test has a `// Failure class:` comment
- [ ] The fixture file is in `test/regression/fixtures/<layer>/` and follows the naming convention
- [ ] No real secrets, hashes, or credentials appear in fixtures
- [ ] The test asserts on the failure category, not just "error != nil"
- [ ] The issue body has been updated with the fixture path and test name

---

## Related files

| File | Purpose |
|------|---------|
| `internal/cmd/regression_example_test.go` | Canonical template with one example per layer |
| `internal/testhelpers/` | Fixture builder package |
| `docs/printer-golden-tests.md` | Golden tests comparing trace printers across output modes |
| `test/regression/FIXTURES.md` | Naming rules, stub values, secret avoidance |
| `test/regression/fixtures/<layer>/README.md` | Layer-specific rules |
| `scripts/check-readme-commands.sh` | Detects stale command references in README |
