// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package rpc_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dotandev/glassbox/internal/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

func fastPoolConfig() rpc.PoolConfig {
	cfg := rpc.DefaultPoolConfig()
	cfg.InitialBackoff = 1 * time.Millisecond
	cfg.MaxBackoff = 5 * time.Millisecond
	cfg.RequestDeadline = 5 * time.Second
	return cfg
}

// newCountingServer returns an httptest.Server that responds with the given
// status code on every request and counts the total calls.
func newCountingServer(t *testing.T, statusCode int) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var count atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(statusCode)
	}))
	t.Cleanup(srv.Close)
	return srv, &count
}

// ────────────────────────────────────────────────────────────────────────────
// Tests
// ────────────────────────────────────────────────────────────────────────────

// TestProviderPool_SingleProviderSuccess verifies the happy-path single
// provider case: the fn is called once and the succeeded URL is set.
func TestProviderPool_SingleProviderSuccess(t *testing.T) {
	cfg := fastPoolConfig()
	pool := rpc.NewProviderPool([]string{"https://example.com"}, cfg)

	called := 0
	diag, err := pool.Do(context.Background(), func(_ context.Context, url string) (int, error) {
		called++
		assert.Equal(t, "https://example.com", url)
		return http.StatusOK, nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, called)
	assert.Equal(t, "https://example.com", diag.SucceededURL)
	assert.Len(t, diag.Attempts, 1)
	assert.Nil(t, diag.Attempts[0].Err)
}

// TestProviderPool_FailoverToSecondProvider verifies that a retryable failure
// on the first provider causes the pool to attempt the second provider.
func TestProviderPool_FailoverToSecondProvider(t *testing.T) {
	cfg := fastPoolConfig()
	cfg.MaxRetries = 3
	cfg.DegradedThreshold = 1
	cfg.DownThreshold = 3

	first := "https://first.example.com"
	second := "https://second.example.com"
	pool := rpc.NewProviderPool([]string{first, second}, cfg)

	calls := map[string]int{}
	diag, err := pool.Do(context.Background(), func(_ context.Context, url string) (int, error) {
		calls[url]++
		if url == first {
			return http.StatusServiceUnavailable, fmt.Errorf("service unavailable")
		}
		return http.StatusOK, nil
	})

	require.NoError(t, err)
	assert.Equal(t, second, diag.SucceededURL)
	assert.GreaterOrEqual(t, calls[first], 1, "first provider should have been tried")
	assert.GreaterOrEqual(t, calls[second], 1, "second provider should have been tried")
}

// TestProviderPool_AllProvidersFail verifies that when every provider returns a
// retryable error, an AllNodesFailedError is eventually returned.
func TestProviderPool_AllProvidersFail(t *testing.T) {
	cfg := fastPoolConfig()
	cfg.MaxRetries = 4

	urls := []string{"https://a.example.com", "https://b.example.com"}
	pool := rpc.NewProviderPool(urls, cfg)

	diag, err := pool.Do(context.Background(), func(_ context.Context, url string) (int, error) {
		return http.StatusServiceUnavailable, fmt.Errorf("all down")
	})

	require.Error(t, err)

	var allFailed *rpc.AllNodesFailedError
	assert.True(t, errors.As(err, &allFailed), "expected AllNodesFailedError, got %T: %v", err, err)
	assert.Greater(t, len(diag.Attempts), 0, "at least one attempt should be recorded")
	assert.Empty(t, diag.SucceededURL)
}

// TestProviderPool_NonRetryableErrorNoDuplicate verifies that a non-retryable
// HTTP status (e.g. 400 Bad Request) does NOT cause the pool to try other
// providers — it returns immediately.
func TestProviderPool_NonRetryableErrorNoDuplicate(t *testing.T) {
	cfg := fastPoolConfig()
	cfg.MaxRetries = 5

	urls := []string{"https://a.example.com", "https://b.example.com"}
	pool := rpc.NewProviderPool(urls, cfg)

	totalCalls := 0
	diag, err := pool.Do(context.Background(), func(_ context.Context, url string) (int, error) {
		totalCalls++
		// 400 Bad Request is not retryable.
		return http.StatusBadRequest, fmt.Errorf("bad request")
	})

	require.Error(t, err)
	// Only the first provider should have been attempted once.
	assert.Equal(t, 1, totalCalls, "non-retryable errors must not cause duplicate requests")
	assert.Empty(t, diag.SucceededURL)
	assert.Len(t, diag.Attempts, 1)
	assert.False(t, diag.Attempts[0].Retryable)
}

// TestProviderPool_RateLimitIsRetryable verifies that HTTP 429 is classified
// as retryable and causes failover to the next provider.
func TestProviderPool_RateLimitIsRetryable(t *testing.T) {
	cfg := fastPoolConfig()
	cfg.MaxRetries = 4
	cfg.DegradedThreshold = 1

	primary := "https://primary.example.com"
	fallback := "https://fallback.example.com"
	pool := rpc.NewProviderPool([]string{primary, fallback}, cfg)

	calls := map[string]int{}
	diag, err := pool.Do(context.Background(), func(_ context.Context, url string) (int, error) {
		calls[url]++
		if url == primary {
			return http.StatusTooManyRequests, fmt.Errorf("rate limited")
		}
		return http.StatusOK, nil
	})

	require.NoError(t, err)
	assert.Equal(t, fallback, diag.SucceededURL)
	assert.True(t, diag.Attempts[0].Retryable, "429 should be classified as retryable")
}

// TestProviderPool_TimeoutIsRetryable verifies that a context.DeadlineExceeded
// from a per-attempt deadline is classified as retryable.
func TestProviderPool_TimeoutIsRetryable(t *testing.T) {
	cfg := fastPoolConfig()
	cfg.MaxRetries = 4
	cfg.RequestDeadline = 10 * time.Millisecond // very short per-attempt deadline

	primary := "https://slow.example.com"
	fast := "https://fast.example.com"
	pool := rpc.NewProviderPool([]string{primary, fast}, cfg)

	calls := map[string]int{}
	diag, err := pool.Do(context.Background(), func(ctx context.Context, url string) (int, error) {
		calls[url]++
		if url == primary {
			// Simulate a slow provider that exceeds the per-attempt deadline.
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(200 * time.Millisecond):
				return http.StatusOK, nil
			}
		}
		return http.StatusOK, nil
	})

	require.NoError(t, err, "should succeed via fallback")
	assert.Equal(t, fast, diag.SucceededURL)
	// The primary attempt should be marked retryable (deadline exceeded).
	require.Greater(t, len(diag.Attempts), 1)
	assert.True(t, diag.Attempts[0].Retryable)
}

// TestProviderPool_PinnedModeSuccess verifies that a pinned pool routes
// all requests to the pinned URL and reports the correct succeeded URL.
func TestProviderPool_PinnedModeSuccess(t *testing.T) {
	cfg := fastPoolConfig()
	cfg.PinnedURL = "https://pinned.example.com"

	pool := rpc.NewProviderPool([]string{"https://pinned.example.com", "https://other.example.com"}, cfg)

	assert.True(t, pool.IsPinned())
	assert.Equal(t, "https://pinned.example.com", pool.PinnedURL())

	callURLs := []string{}
	diag, err := pool.Do(context.Background(), func(_ context.Context, url string) (int, error) {
		callURLs = append(callURLs, url)
		return http.StatusOK, nil
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"https://pinned.example.com"}, callURLs,
		"pinned pool must only call the pinned URL")
	assert.Equal(t, "https://pinned.example.com", diag.SucceededURL)
}

// TestProviderPool_PinnedModeFailNoFallback verifies that when the pinned
// provider fails, the pool does NOT fall back to other providers — it
// returns an error immediately, preserving replay determinism.
func TestProviderPool_PinnedModeFailNoFallback(t *testing.T) {
	cfg := fastPoolConfig()
	cfg.PinnedURL = "https://pinned.example.com"

	pool := rpc.NewProviderPool(
		[]string{"https://pinned.example.com", "https://fallback.example.com"},
		cfg,
	)

	callURLs := []string{}
	diag, err := pool.Do(context.Background(), func(_ context.Context, url string) (int, error) {
		callURLs = append(callURLs, url)
		return http.StatusServiceUnavailable, fmt.Errorf("pinned down")
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pinned provider")
	assert.Contains(t, err.Error(), "replay pinning disables silent switching",
		"error must explicitly mention that silent switching is disabled")
	assert.Equal(t, []string{"https://pinned.example.com"}, callURLs,
		"fallback must NOT be called when pinned")
	assert.Len(t, diag.Attempts, 1)
}

// TestProviderPool_HealthStateTransitions verifies that consecutive failures
// move a provider through Healthy → Degraded → Down transitions.
func TestProviderPool_HealthStateTransitions(t *testing.T) {
	cfg := fastPoolConfig()
	cfg.DegradedThreshold = 2
	cfg.DownThreshold = 4

	pool := rpc.NewProviderPool([]string{"https://a.example.com"}, cfg)

	// Initial state.
	states := pool.ProviderStates()
	require.Len(t, states, 1)
	assert.Equal(t, rpc.ProviderStatusHealthy, states[0].Status)

	pool.RecordFailure("https://a.example.com")
	states = pool.ProviderStates()
	assert.Equal(t, rpc.ProviderStatusHealthy, states[0].Status, "1 failure should not degrade")

	pool.RecordFailure("https://a.example.com")
	states = pool.ProviderStates()
	assert.Equal(t, rpc.ProviderStatusDegraded, states[0].Status, "2 failures should degrade")

	pool.RecordFailure("https://a.example.com")
	pool.RecordFailure("https://a.example.com")
	states = pool.ProviderStates()
	assert.Equal(t, rpc.ProviderStatusDown, states[0].Status, "4 failures should mark down")

	// Recovery after success.
	pool.RecordSuccess("https://a.example.com")
	states = pool.ProviderStates()
	assert.Equal(t, rpc.ProviderStatusHealthy, states[0].Status, "success should restore healthy")
	assert.Equal(t, 0, states[0].ConsecutiveFailures)
}

// TestProviderPool_FormatDiagnostics_Output verifies that FormatDiagnostics
// includes the succeeded URL and attempt count in its output.
func TestProviderPool_FormatDiagnostics_Output(t *testing.T) {
	diag := rpc.AttemptDiagnostics{
		Attempts: []rpc.AttemptRecord{
			{URL: "https://a.example.com", Err: fmt.Errorf("timeout"), Retryable: true, Latency: 50 * time.Millisecond, HTTPStatusCode: 0},
			{URL: "https://b.example.com", Err: nil, Latency: 20 * time.Millisecond, HTTPStatusCode: 200},
		},
		SucceededURL:  "https://b.example.com",
		TotalDuration: 80 * time.Millisecond,
	}

	out := rpc.FormatDiagnostics(diag)
	assert.Contains(t, out, "2 attempt(s)")
	assert.Contains(t, out, "https://a.example.com")
	assert.Contains(t, out, "https://b.example.com")
	assert.Contains(t, out, "Succeeded via: https://b.example.com")
	assert.Contains(t, out, "retryable")
}

// TestProviderPool_ParentContextCancellation verifies that the pool respects
// parent context cancellation and does not schedule further attempts.
func TestProviderPool_ParentContextCancellation(t *testing.T) {
	cfg := fastPoolConfig()
	cfg.MaxRetries = 10

	pool := rpc.NewProviderPool([]string{"https://a.example.com", "https://b.example.com"}, cfg)

	ctx, cancel := context.WithCancel(context.Background())

	callCount := 0
	_, err := pool.Do(ctx, func(_ context.Context, url string) (int, error) {
		callCount++
		cancel() // cancel after the first attempt
		return http.StatusServiceUnavailable, fmt.Errorf("unavailable")
	})

	require.Error(t, err)
	// After the first attempt the context is cancelled; the pool should stop.
	assert.LessOrEqual(t, callCount, 2,
		"pool must stop scheduling attempts after parent context is cancelled")
}

// TestProviderPool_LiveHTTPServer exercises the pool against real httptest servers.
func TestProviderPool_LiveHTTPServer(t *testing.T) {
	srv1, count1 := newCountingServer(t, http.StatusServiceUnavailable)
	srv2, count2 := newCountingServer(t, http.StatusOK)

	cfg := fastPoolConfig()
	cfg.MaxRetries = 4
	cfg.DegradedThreshold = 1
	pool := rpc.NewProviderPool([]string{srv1.URL, srv2.URL}, cfg)

	diag, err := pool.Do(context.Background(), func(ctx context.Context, url string) (int, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, httpErr := http.DefaultClient.Do(req)
		if httpErr != nil {
			return 0, httpErr
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 500 {
			return resp.StatusCode, fmt.Errorf("server error: %d", resp.StatusCode)
		}
		return resp.StatusCode, nil
	})

	require.NoError(t, err)
	assert.Equal(t, srv2.URL, diag.SucceededURL)
	assert.GreaterOrEqual(t, count1.Load(), int64(1), "first server should have been tried")
	assert.GreaterOrEqual(t, count2.Load(), int64(1), "second server should have been tried")
}
