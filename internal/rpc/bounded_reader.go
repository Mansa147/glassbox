// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package rpc

import (
	"fmt"
	"io"
)

// DefaultResponsePayloadLimit is the default maximum number of bytes allowed
// for a single Soroban RPC response body (32 MiB).
const DefaultResponsePayloadLimit int64 = 32 * 1024 * 1024

// PayloadTooLargeError is returned when a response body exceeds the configured
// byte limit. It names the operation and the effective limit so callers can
// surface an actionable remediation hint.
type PayloadTooLargeError struct {
	Operation string
	ReadBytes int64
	Limit     int64
}

func (e *PayloadTooLargeError) Error() string {
	return fmt.Sprintf(
		"rpc: %s response exceeded size limit: read %s, limit %s — "+
			"reduce the request scope (fewer ledger keys, smaller footprint) "+
			"or raise the limit with --rpc-response-limit",
		e.Operation,
		formatBytes(e.ReadBytes),
		formatBytes(e.Limit),
	)
}

// BoundedReader wraps an io.Reader and returns a PayloadTooLargeError once
// more than limit bytes have been consumed. A limit of 0 disables enforcement.
type BoundedReader struct {
	r     io.Reader
	limit int64
	n     int64 // total bytes consumed so far
	op    string
}

// NewBoundedReader returns a BoundedReader that caps reads at limit bytes.
// op is embedded in the error message for diagnostics.
func NewBoundedReader(r io.Reader, limit int64, op string) *BoundedReader {
	return &BoundedReader{r: r, limit: limit, op: op}
}

// Read implements io.Reader. It returns PayloadTooLargeError as soon as the
// cumulative byte count exceeds limit.
func (b *BoundedReader) Read(p []byte) (int, error) {
	if b.limit > 0 && b.n >= b.limit {
		return 0, &PayloadTooLargeError{Operation: b.op, ReadBytes: b.n, Limit: b.limit}
	}
	// Clamp the slice so a single Read can never exceed limit+1 bytes.
	if b.limit > 0 {
		if remaining := b.limit - b.n + 1; int64(len(p)) > remaining {
			p = p[:remaining]
		}
	}
	nn, err := b.r.Read(p)
	b.n += int64(nn)
	if b.limit > 0 && b.n > b.limit {
		return nn, &PayloadTooLargeError{Operation: b.op, ReadBytes: b.n, Limit: b.limit}
	}
	return nn, err
}

// BytesRead returns the total number of bytes consumed so far.
func (b *BoundedReader) BytesRead() int64 { return b.n }

// ReadBounded reads all bytes from r up to limit, returning PayloadTooLargeError
// when the body exceeds the limit. op is included in the error for diagnostics.
// A limit ≤ 0 is treated as unlimited (equivalent to io.ReadAll).
func ReadBounded(r io.Reader, limit int64, op string) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(r)
	}
	return io.ReadAll(NewBoundedReader(r, limit, op))
}

// readResponseBody reads resp.Body up to the client's configured response
// payload limit. The operation name is included in any oversize error.
func (c *Client) readResponseBody(body io.Reader, op string) ([]byte, error) {
	limit := c.ResponsePayloadLimit
	if limit <= 0 {
		limit = DefaultResponsePayloadLimit
	}
	return ReadBounded(body, limit, op)
}
