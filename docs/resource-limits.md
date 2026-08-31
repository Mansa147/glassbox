# Resource Limits for HTTP Response Fetching

## Overview

Large envelopes, ledger entries, or trace payloads can exhaust memory when fetched from a misbehaving endpoint. This module implements explicit and adjustable resource limits for HTTP response fetching to prevent memory exhaustion.

## Problem

When interacting with misbehaving or malicious RPC endpoints, oversized responses can:
- Exhaust available memory
- Cause application crashes
- Enable denial-of-service attacks
- Mask the root cause of failures with generic errors

## Solution

Implement per-response and aggregate fetch limits with:
- Bounded readers that enforce limits before buffering data
- Clear failure codes for different limit violations
- Configuration and CLI overrides for legitimate large transactions
- Diagnostics showing observed vs configured sizes
- Machine-readable error output that remains small

## Architecture

### Bounded Readers

The `BoundedReader` wraps an `io.Reader` and enforces a byte limit:
- Clamps read operations to never exceed limit+1 bytes
- Returns `PayloadTooLargeError` as soon as the limit is exceeded
- Tracks total bytes consumed for diagnostics

### Aggregate Tracking

The `AggregateTracker` tracks total bytes across all requests:
- Thread-safe tracking of cumulative bytes fetched
- Returns `AggregateLimitExceededError` when aggregate limit is exceeded
- Provides statistics (total bytes, request count, limit)

### Error Types

#### PayloadTooLargeError

Returned when a single response exceeds the per-response limit:
```go
type PayloadTooLargeError struct {
    Code        string // "ERR_PAYLOAD_TOO_LARGE"
    Operation  string // RPC operation name
    ReadBytes  int64  // Bytes actually read
    Limit      int64  // Configured limit
    Configured int64  // Original configured limit
}
```

#### AggregateLimitExceededError

Returned when total bytes across all requests exceed the aggregate limit:
```go
type AggregateLimitExceededError struct {
    Code             string // "ERR_AGGREGATE_LIMIT_EXCEEDED"
    TotalBytesRead  int64  // Total bytes across all requests
    AggregateLimit  int64  // Configured aggregate limit
    ConfiguredLimit int64  // Original configured limit
    RequestCount    int    // Number of requests made
}
```

## Default Limits

| Limit Type | Default Value | Description |
|------------|---------------|-------------|
| `DefaultResponsePayloadLimit` | 32 MiB | Maximum bytes per single RPC response |
| `DefaultAggregateFetchLimit` | 512 MiB | Maximum total bytes across all responses |
| `DefaultEnvelopeLimit` | 1 MiB | Maximum size for transaction envelopes |
| `DefaultLedgerEntriesLimit` | 1 MiB | Maximum size for ledger entries responses |

## Configuration

### CLI Flags

```bash
# Set per-response limit (default: 32 MiB)
glassbox debug --rpc-response-limit 64000000 abc123...

# Set aggregate limit (default: 512 MiB)
glassbox debug --rpc-aggregate-limit 1073741824 abc123...

# Use both limits together
glassbox debug --rpc-response-limit 64000000 --rpc-aggregate-limit 1073741824 abc123...
```

### Programmatic Configuration

```go
import "github.com/dotandev/glassbox/internal/rpc"

// Set per-response limit to 64 MiB
client, err := rpc.NewClient(
    rpc.WithNetwork(rpc.Mainnet),
    rpc.WithResponsePayloadLimit(64 * 1024 * 1024),
)

// Set aggregate limit to 1 GiB
client, err := rpc.NewClient(
    rpc.WithNetwork(rpc.Mainnet),
    rpc.WithAggregateFetchLimit(1024 * 1024 * 1024),
)
```

## Error Messages

### Per-Response Limit Exceeded

```
rpc: getLedgerEntries response exceeded size limit: read 40.0 MiB (41943040 bytes), limit 32.0 MiB (33554432 bytes) — reduce the request scope (fewer ledger keys, smaller footprint) or raise the limit with --rpc-response-limit
```

### Aggregate Limit Exceeded

```
rpc: aggregate fetch limit exceeded: read 600.0 MiB (629145600 bytes) across 15 requests, limit 512.0 MiB (536870912 bytes) — reduce the total number of operations or raise the limit with --rpc-aggregate-limit
```

## JSON Error Output

Errors are serialized to compact JSON for machine-readable handling:

```json
{
  "code": "ERR_PAYLOAD_TOO_LARGE",
  "operation": "getLedgerEntries",
  "read_bytes": 41943040,
  "limit": 33554432,
  "configured_limit": 33554432
}
```

```json
{
  "code": "ERR_AGGREGATE_LIMIT_EXCEEDED",
  "total_bytes_read": 629145600,
  "aggregate_limit": 536870912,
  "configured_limit": 536870912,
  "request_count": 15
}
```

**Note**: JSON output is intentionally compact (< 500 bytes) and does not include response content.

## Error Codes

| Code | Description |
|------|-------------|
| `ERR_PAYLOAD_TOO_LARGE` | Single response exceeded per-response limit |
| `ERR_AGGREGATE_LIMIT_EXCEEDED` | Total bytes exceeded aggregate limit |
| `ERR_ENVELOPE_TOO_LARGE` | Transaction envelope exceeded envelope limit |
| `ERR_LEDGER_ENTRIES_TOO_LARGE` | Ledger entries response exceeded entries limit |

## Operational Tuning

### When to Increase Limits

Increase limits when:
- Working with legitimate large transactions (e.g., complex contracts)
- Fetching many ledger entries in a single request
- Debugging contracts with large state or trace payloads

### Recommended Limits by Use Case

| Use Case | Per-Response | Aggregate |
|----------|--------------|-----------|
| Typical debugging | 32 MiB (default) | 512 MiB (default) |
| Large contracts | 64 MiB | 1 GiB |
| Batch operations | 128 MiB | 2 GiB |
| Stress testing | 256 MiB | 4 GiB |

### Safety Guidelines

1. **Never disable limits**: Use 0 to use defaults, not to disable enforcement
2. **Monitor aggregate usage**: Check aggregate stats before long-running operations
3. **Use request batching**: Break large requests into smaller chunks when possible
4. **Validate endpoints**: Use trusted RPC endpoints in production

## Implementation Details

### BoundedReader Behavior

- Reads are clamped to never exceed `limit + 1` bytes
- Error is returned immediately when limit is exceeded
- `BytesRead()` returns total bytes consumed (may exceed limit by 1)
- Zero limit is treated as unlimited (equivalent to `io.ReadAll`)

### AggregateTracker Behavior

- Thread-safe via mutex locking
- Records each request's byte count
- Checks aggregate limit after each request
- Provides real-time statistics via `GetStats()`

### Integration with HTTP Layer

The bounded reader is integrated into the HTTP response body reading:

```go
func (c *Client) readResponseBody(body io.Reader, op string) ([]byte, error) {
    limit := c.ResponsePayloadLimit
    if limit <= 0 {
        limit = DefaultResponsePayloadLimit
    }
    
    var tracker *AggregateTracker
    if c.aggregateTracker != nil {
        tracker = c.aggregateTracker
    }
    
    return ReadBoundedWithTracker(body, limit, op, tracker)
}
```

## Testing

### Test Coverage

Tests cover:
- Exact boundary behavior (responses at exactly the limit)
- One-byte-over scenarios (responses exceeding limit by 1 byte)
- Compressed responses (gzip-compressed payloads)
- Multiple requests with aggregate tracking
- Concurrent access safety
- JSON serialization and size limits
- Error message formatting
- Default limit values

### Running Tests

```bash
# Run all bounded reader tests
go test ./internal/rpc -run BoundedReader

# Run aggregate tracking tests
go test ./internal/rpc -run Aggregate

# Run compression tests
go test ./internal/rpc -run Compressed
```

## Performance Considerations

- **Memory**: Bounded readers add minimal overhead (single allocation for limit tracking)
- **CPU**: Hashing and comparison are O(1) per read operation
- **Locking**: Aggregate tracker uses mutex for thread safety (negligible for typical workloads)
- **Network**: Limits are enforced before full response buffering (no unnecessary data transfer)

## Security Considerations

### Memory Exhaustion Prevention

- Limits are enforced before data is fully buffered
- Bounded readers clamp individual read operations
- Aggregate tracking prevents cumulative memory exhaustion

### Response Content Exclusion

- Error messages do not include response content
- JSON output is limited to metadata (byte counts, limits)
- Remediation hints guide users without exposing sensitive data

### Denial-of-Service Mitigation

- Per-response limits prevent single large responses
- Aggregate limits prevent many small responses
- Limits are applied before buffering (early rejection)

## Troubleshooting

### Common Issues

**Issue**: Legitimate large transaction is rejected
- **Solution**: Increase per-response limit with `--rpc-response-limit`

**Issue**: Aggregate limit exceeded during batch operations
- **Solution**: Increase aggregate limit with `--rpc-aggregate-limit` or reduce batch size

**Issue**: Limits are too restrictive for specific use case
- **Solution**: Adjust limits via CLI flags or programmatic configuration

**Issue**: Unclear which limit was exceeded
- **Solution**: Check error code and message for specific limit type

### Diagnostics

Query aggregate statistics:

```go
totalBytes, requestCount, limit := client.aggregateTracker.GetStats()
fmt.Printf("Total: %d bytes across %d requests (limit: %d)", 
    totalBytes, requestCount, limit)
```

## Future Enhancements

Potential improvements:
- Add per-operation specific limits (e.g., lower limit for getHealth)
- Implement adaptive limits based on historical usage
- Add metrics/telemetry for limit violations
- Provide UI integration for limit configuration
- Add warnings when approaching aggregate limit