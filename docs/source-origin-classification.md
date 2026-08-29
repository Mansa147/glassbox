# Source Origin Classification

Glassbox classifies every DWARF-resolved source location into one of four
origin classes so trace viewers, formatters, and automation scripts can tell
user-authored code apart from machine-generated build output and external
dependencies — without parsing raw file paths manually.

---

## Origin classes

| Class | JSON value | Display label | Meaning |
|---|---|---|---|
| User | `"user"` | *(none)* | Developer-authored source under the project root. No label is added; user frames are the expected happy path. |
| Generated | `"generated"` | `[generated]` | Machine-produced build output: Rust WASM artifacts (`target/`, `.wasm`), macro-expanded code, proc-macro output. The file may not exist in the workspace. |
| External | `"external"` | `[external]` | Source from an external crate or dependency: Cargo registry paths (`.cargo/registry`), Cargo git checkouts, or any absolute path outside the project root. |
| Unknown | `"unknown"` | `[unknown origin]` | Classification could not be determined, or the path was empty. Used for backward compatibility with traces produced before this field existed. |

---

## Where it appears

### JSON trace export

Every `ExecutionState.source_ref` object in a trace JSON export now carries
an `origin_class` field:

```json
{
  "source_ref": {
    "file": "src/lib.rs",
    "line": 42,
    "column": 8,
    "origin_class": "user"
  }
}
```

```json
{
  "source_ref": {
    "file": "target/wasm32-unknown-unknown/release/build/my_crate/out/generated.rs",
    "line": 5,
    "column": 1,
    "origin_class": "generated"
  }
}
```

The field is omitted when the value is `"user"` or empty (via `omitempty`) to
keep the common case compact. Parsers should treat a missing field as `"user"`.

### Terminal trace output (formatter and split pane)

Generated and external frames are annotated inline after the source location:

```
source: target/wasm32-unknown-unknown/release/build/foo.rs:5  [generated]
source: /home/user/.cargo/registry/src/serde-1.0.0/src/lib.rs:200  [external]
source: src/lib.rs:42
```

User frames (`"user"`) and unknown frames (`""`) produce no extra annotation.

### Trap diagnostics (`FormatTrapInfo`)

The same label appears on the `Location:` line of a trap report:

```
⊗ Trap Detected: memory_out_of_bounds

⚠ Error: memory access out of bounds

📌 Location: target/wasm32-unknown-unknown/release/my_contract.wasm:0  [generated]

🔧 Function: my_contract::transfer
```

---

## Classification rules

The classifier in `internal/sourcemap/origin.go` applies these rules in
priority order. The first match wins.

| Priority | Pattern | Class |
|---|---|---|
| 1 | Path is empty | `unknown` |
| 2 | Contains `/.cargo/registry` or `/.cargo/git` | `external` |
| 3 | Ends with `.wasm` or contains `target/wasm32` | `generated` |
| 4 | Contains `/target/` or starts with `target/` | `generated` |
| 5 | Matches a caller-supplied `ExtraBuildDirs` entry | `generated` |
| 6 | Matches a caller-supplied `ExtraExternalPrefixes` entry | `external` |
| 7 | Absolute path outside configured `ProjectRoot` | `external` |
| 8 | Everything else | `user` |

The rules are conservative: a path that does not match any build-output or
dependency pattern is classified as `user` so developer frames are never
accidentally hidden or mislabelled.

---

## Using the classifier in Go

```go
import "github.com/dotandev/glassbox/internal/sourcemap"

// One-shot classification
class := sourcemap.ClassifyPath("/project/src/lib.rs", "/project")
// → sourcemap.OriginUser

// Reusable classifier (cheaper for many frames)
c := sourcemap.NewClassifier(sourcemap.ClassifierOptions{
    ProjectRoot:    "/project",
    ExtraBuildDirs: []string{"_generated/"},
})
class = c.Classify("_generated/tokens.rs")
// → sourcemap.OriginGenerated

// Display label
fmt.Println(class.Label()) // → "[generated]"
```

### Populating `SourceRef.OriginClass` during source mapping

When the source-mapping pipeline produces a `SourceRef`, it should populate
`OriginClass` with the result of `Classifier.Classify`:

```go
ref := &trace.SourceRef{
    File:        resolvedFile,
    Line:        resolvedLine,
    Column:      resolvedCol,
    OriginClass: string(sourcemap.ClassifyPath(resolvedFile, projectRoot)),
}
```

### Populating `TrapInfo.OriginClass`

`TrapDetector.DetectTrap` should set `OriginClass` after resolving the fault
location:

```go
trap.OriginClass = string(sourcemap.ClassifyPath(trap.SourceLocation.File, projectRoot))
```

---

## TypeScript (VS Code extension)

The same logic is implemented in `vscode-extension/src/sourceMap.ts`:

```typescript
import { classifyPath, originLabelText } from './sourceMap';

const origin = classifyPath(frame.file, workspaceRoot);
const label  = originLabelText(origin); // "[generated]", "[external]", or ""
```

The extension uses the classification to:
- Annotate generated and external steps in the `Glassbox: Open Source Location`
  command with a VS Code information message before navigating.
- Preserve navigation to all frames — generated and external frames are
  resolved and opened, not hidden.

---

## Related files

| File | Purpose |
|---|---|
| `internal/sourcemap/origin.go` | `OriginClass` type, `Classifier`, `ClassifyPath` |
| `internal/sourcemap/origin_test.go` | 22 tests covering all classification rules |
| `internal/trace/splitpane.go` | `SourceRef.OriginClass` field definition |
| `internal/trace/formatter.go` | `sourceRefOriginLabel` helper; label in formatter output |
| `internal/trace/trap.go` | `TrapInfo.OriginClass` field; label in `FormatTrapInfo` |
| `internal/ui/trace_view.go` | Label in detail-pane `source` row |
| `vscode-extension/src/sourceMap.ts` | `classifyPath`, `originLabelText` for the extension |
| `docs/json-output-automation.md` | `origin_class` field in the JSON schema reference |
