# Trace Filtering

Large traces are difficult to inspect when you need to focus on a specific
contract, source location, event category, or failure path. The trace filter
reduces noise without changing the underlying evidence.

## Principles

- **Filters are AND-combined.** All specified criteria must match for a step
  to be included. Omitting a flag means "no restriction" for that dimension.
- **Original step IDs are preserved.** Filtered output always uses the same
  step numbers as the source trace. Filtering never renumbers steps.
- **Empty matches are successful results.** Zero matched steps is not an
  error — it is a deterministic, repeatable result.
- **The source trace is never mutated.** `ApplyFilter` returns a
  `FilteredTrace` view; the original `ExecutionTrace` is unchanged.
- **Active filters are always in the output header** so the result is
  self-describing.

## Available filters

| Flag / field | Type | Description |
|---|---|---|
| `--contract` | regex | Match `ContractID` |
| `--function` | regex | Match `Function` name |
| `--event-type` | enum | `trap`, `contract_call`, `host_function`, `auth` |
| `--severity` | enum | `error`, `warning`, `info`, `all` |
| `--source-file` | substring | Match `SourceFile` path |
| `--step-min` | int | Minimum step index (inclusive, 0-based) |
| `--step-max` | int | Maximum step index (inclusive, 0-based) |
| `--line-min` | int | Minimum source line number (inclusive) |
| `--line-max` | int | Maximum source line number (inclusive) |
| `--exclude` | bool | Invert: include steps that do **not** match |

### Source line range notes

- Steps with `SourceLine == 0` (line information not recorded, e.g. steps
  without DWARF debug info) **never** match a non-zero line range. This
  avoids false positives on steps that lack source mapping.
- `--line-min` and `--line-max` can be used independently (open-ended ranges
  are fine).

### Exclude mode

`--exclude` inverts the filter result. Combined with any other flag, it
means "show me everything **except** steps matching these criteria":

```sh
# Show everything except host_function noise
glassbox trace filter tx.json --event-type host_function --exclude
```

`--exclude` requires at least one other filter flag — `--exclude` alone
would exclude every step, which is almost certainly a mistake.

## CLI usage

```
glassbox trace filter <trace-file> [flags]
```

### Output formats

| `--format` | Description |
|---|---|
| `text` (default) | Plain text with active-filter header |
| `json` | Machine-readable JSON with `filter_summary` block |

Use `--output <file>` to write to a file instead of stdout.

### Examples

```sh
# All steps for a specific contract
glassbox trace filter tx.json --contract CAAAA

# Only error steps, as JSON for CI consumption
glassbox trace filter tx.json --severity error --format json

# Steps in a source line range
glassbox trace filter tx.json --source-file token.rs --line-min 40 --line-max 80

# All traps only
glassbox trace filter tx.json --event-type trap

# Combine contract and function (AND)
glassbox trace filter tx.json --contract CAAAA --function "transfer.*"

# Everything except a specific contract
glassbox trace filter tx.json --contract CNOISE --exclude

# Save filtered result to a file
glassbox trace filter tx.json --contract CAAAA --output ca_steps.json --format json
```

## JSON output format

```json
{
  "filter_summary": {
    "total_steps": 1200,
    "matched_count": 47,
    "match_ratio": 0.039,
    "active_filters": {
      "contract_id": "CAAAA",
      "severity": "error"
    },
    "exclude_mode": false
  },
  "matched_steps": [
    {
      "original_step": 14,
      "has_parent": true,
      "parent_step": 12,
      "state": { ... }
    }
  ]
}
```

- `original_step` is always the unmodified step index from the source trace.
- `has_parent` / `parent_step` provide call-nesting context so readers can
  understand the hierarchy even in a filtered view.
- `active_filters` lists only the criteria that were actually set.

## Text output format

```
Glassbox Filtered Trace
=======================

Filter summary:
  Matched:   47 / 1200 steps (3.9%)
  Filters:
    contract_id   = "CAAAA" (regex)
    severity      = "error"

Matched steps:
--------------

Step 14: contract_call
  Contract:  CAAAA
  Function:  transfer
  Source:    token.rs:55
  Error:     trap: integer overflow
```

## Programmatic API

```go
// Build an expression
expr := &trace.FilterExpression{
    ContractID: "CAAAA",  // compiled as regex on Validate()
    Severity:   trace.FilterSeverityError,
    LineMin:    40,
    LineMax:    80,
}
if err := expr.Validate(); err != nil {
    // invalid regex, bad severity, line_min > line_max, etc.
}

// Apply — never mutates the source trace
ft, err := trace.ApplyFilter(executionTrace, expr)

// Render
text, _ := trace.RenderFilteredText(ft)
jsonBytes, _ := trace.RenderFilteredJSON(ft)

// Compose filters with AND
combined := expr1.And(expr2)

// Metadata
meta := trace.FilterMetadataFromTrace(ft)
fmt.Printf("%d/%d steps matched (%.1f%%)\n",
    meta.MatchedCount, meta.TotalSteps, meta.MatchRatio*100)
```

## Implementation locations

| File | Purpose |
|---|---|
| `internal/trace/filter.go` | `FilterExpression`, `ApplyFilter`, `FilteredTrace`, `FilterMetadata` |
| `internal/trace/filter_render.go` | `RenderFilteredText`, `RenderFilteredJSON` |
| `internal/cmd/trace_filter.go` | `glassbox trace filter` CLI command |
| `internal/trace/filter_test.go` | Existing filter tests |
| `internal/trace/filter_extended_test.go` | Line-range, Exclude, nested calls, empty results, render tests |
