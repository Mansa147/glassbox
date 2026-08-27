# PR #795: Source Mapping Confidence System

## Summary

This PR implements a comprehensive source mapping confidence system that differentiates between exact, approximate, and inferred source locations. The system provides documented confidence levels for every emitted source mapping, visibly distinguishes low-confidence mappings with explanations, and enables JSON consumers to filter or act on confidence without parsing text.

## Problem

Presenting all source mappings with equal confidence risks sending developers to the wrong line during failure investigation. Different mapping mechanisms (DWARF debug info, heuristics, fallbacks) have varying levels of reliability, but users have no way to distinguish between them.

## Solution

### Architecture

The implementation adds confidence metadata throughout the source mapping pipeline:

1. **Confidence Levels**: 5-tier system (Exact, High, Medium, Low, Unknown)
2. **Reason Codes**: 12 specific codes explaining why each confidence level was assigned
3. **Typed Metadata**: Structured confidence information with context
4. **Backward Compatibility**: Optional fields that default to nil for existing code

### Integration

- **DWARF Package** (`internal/dwarf/`):
  - Created `confidence.go` with confidence types and functions
  - Integrated confidence into `SourceLocation` struct
  - Added confidence calculation based on DWARF data quality
  - Path normalization diagnostics affect confidence

- **Trace Package** (`internal/trace/`):
  - Created `confidence.go` as alias to dwarf confidence types
  - Added confidence to `SourceRef` struct in `splitpane.go`
  - Enhanced `ExecutionState` with confidence fields in `navigation.go`
  - Updated export formatting to display confidence in HTML, Markdown, and text
  - Added formatter support for confidence display

- **Sourcemap Package** (`internal/sourcemap/`):
  - Enhanced `FallbackResult` with `DetailedConfidence` field
  - Updated `mapping_confidence.go` with detailed confidence types
  - Added `DetailedConfidence` structure with level, reason, and context
  - Integrated confidence calculation in `applyConfidence()`

### Features

- **5 Confidence Levels**: Exact, High, Medium, Low, Unknown
- **12 Reason Codes**: Covering DWARF quality, heuristics, path normalization, etc.
- **Helper Methods**: `IsHighConfidence()`, `IsLowConfidence()`, `Description()`
- **JSON Filtering**: Methods to filter traces by confidence level and reason codes
- **Export Display**: Confidence shown in HTML, Markdown, and text exports
- **Backward Compatible**: All confidence fields are optional with nil defaults

## Test Coverage

Comprehensive test suites in multiple packages:

- **DWARF Package**: `confidence.go` provides core confidence types and tests
- **Trace Package**: `confidence_test.go` tests confidence integration
- **Sourcemap Package**: `mapping_confidence_test.go` tests mapping confidence
- **Filtering**: `filtering.go` and `filtering_test.go` test JSON filtering capabilities

Test fixtures for different confidence levels:
- Exact DWARF mapping with full precision
- High confidence with line but no column
- Medium confidence from inline expansion
- Low confidence from heuristic matching
- Unknown confidence from missing debug info

## Acceptance Criteria Verification

### ✅ Every emitted source mapping has a documented confidence level
- **Implementation**: All source location types (`dwarf.SourceLocation`, `trace.SourceRef`, `sourcemap.FallbackResult`) include optional confidence fields
- **Documentation**: 5 confidence levels (Exact, High, Medium, Low, Unknown) with 12 specific reason codes
- **Helper Methods**: `IsHighConfidence()`, `IsLowConfidence()`, `Description()` for confidence assessment
- **Integration**: Confidence calculated and propagated through DWARF parsing → sourcemap resolution → trace decoding

### ✅ Low-confidence mappings are visibly distinguished and explain why
- **HTML Export**: Confidence badges and tooltips show level and reason
- **Markdown Export**: Confidence information formatted alongside source locations
- **Text Export**: Plain text confidence display with reason codes
- **Explanations**: Each reason code has human-readable descriptions (e.g., "heuristic_match" → "Determined through heuristic pattern matching")
- **Visual Distinction**: Different confidence levels have distinct styling in formatted output

### ✅ JSON consumers can filter or act on confidence without parsing text
- **Structured JSON**: `confidence_level` (string), `confidence_reason` (string), `confidence_context` (optional string)
- **Filtering Methods**:
  - `FilterByConfidence(minLevel)` - Filter by minimum confidence level
  - `FilterByConfidenceReason(reasons...)` - Filter by specific reason codes
  - `GetHighConfidenceStates()` - Get only high/exact confidence states
  - `GetLowConfidenceStates()` - Get only low/unknown confidence states
- **Summary Methods**:
  - `GetConfidenceSummary()` - Count states by confidence level
  - `GetReasonSummary()` - Count states by reason code
- **Programmatic Access**: All confidence fields are first-class JSON properties with clear types

## Files Changed

### New Files
- `internal/dwarf/confidence.go` - Core confidence types and functions
- `internal/sourcemap/mapping_confidence_test.go` - Mapping confidence tests
- `internal/trace/filtering.go` - JSON filtering capabilities
- `internal/trace/filtering_test.go` - Filtering tests

### Modified Files
- `internal/dwarf/parser.go` - Added confidence to SourceLocation, integrated with DWARF parsing
- `internal/trace/splitpane.go` - Added confidence to SourceRef, JSON tags
- `internal/trace/navigation.go` - Added confidence fields to ExecutionState
- `internal/trace/export.go` - Added confidence display in export formats
- `internal/trace/formatter.go` - Added confidence display in formatted output
- `internal/sourcemap/fallback.go` - Added DetailedConfidence to FallbackResult
- `internal/sourcemap/mapping_confidence.go` - Added detailed confidence types
- Various test files updated for backward compatibility

## Breaking Changes

None. The implementation is fully backward compatible:
- All confidence fields are optional pointers that default to nil
- Existing code without confidence continues to work unchanged
- JSON serialization omits nil confidence fields by default
- Tests have been updated to include nil confidence values for compatibility

## Configuration Example

```go
// Create a source reference with confidence
sourceRef := &trace.SourceRef{
    File:       "token.rs",
    Line:       42,
    Column:     7,
    Function:   "transfer",
    Confidence: &trace.Confidence{
        Level:   trace.ConfidenceExact,
        Reason:  trace.ReasonDWARFExact,
        Context: "verified with column precision",
    },
}

// Filter trace by confidence level
highConfidenceTrace := trace.FilterByConfidence(trace.ConfidenceHigh)

// Get confidence summary
summary := trace.GetConfidenceSummary()
fmt.Printf("Exact: %d, High: %d, Medium: %d, Low: %d\n",
    summary[trace.ConfidenceExact],
    summary[trace.ConfidenceHigh],
    summary[trace.ConfidenceMedium],
    summary[trace.ConfidenceLow])
```

## Future Enhancements

- Confidence-aware UI components (visual distinction in IDE)
- Confidence thresholds for automated systems
- Integration with path normalization confidence impacts
- Additional reason codes for edge cases
- Performance optimizations for large-scale filtering

## Related
closes #795