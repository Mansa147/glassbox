// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

// Package health provides liveness, readiness, and detailed health check
// endpoints suitable for supervisors and container orchestration systems
// (e.g., Kubernetes, systemd, Docker Compose).
//
// Endpoint contract:
//
//   - GET /healthz/live  — liveness probe. Always 200 while the process is up.
//     Never depends on external services so a temporary RPC or cache outage
//     does not cause a container restart.
//
//   - GET /healthz/ready — readiness probe. 200 when all required subsystems
//     are available; 503 with a JSON body listing unavailable components when
//     one or more checks fail.
//
//   - GET /healthz       — detailed status. Always 200; returns a versioned
//     JSON object with per-component status, protocol version, uptime, and
//     timestamp. Intended for dashboards and operators — not for probes.
//
// Handlers are bound to a caller-supplied *http.ServeMux so they cannot be
// accidentally registered on the default mux.
//
// Checks are read-only and lightweight; they never trigger replay work.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// StatusValue is the string status carried in JSON responses.
type StatusValue string

const (
	// StatusOK means the component is operating normally.
	StatusOK StatusValue = "ok"
	// StatusDegraded means the component is available but impaired.
	StatusDegraded StatusValue = "degraded"
	// StatusUnavailable means the component cannot be reached.
	StatusUnavailable StatusValue = "unavailable"
	// StatusUnknown means the component has not been checked yet.
	StatusUnknown StatusValue = "unknown"
)

// schemaVersion is embedded in every /healthz response so consumers can
// detect breaking changes without inspecting field presence.
const schemaVersion = "1"

// Checker is a named health check function. It must return quickly (well
// under one second) and must not trigger any simulation or replay work.
type Checker interface {
	// Name returns the stable component identifier used in JSON output.
	Name() string
	// Check performs the health check. A nil error means healthy.
	Check(ctx context.Context) error
}

// CheckerFunc adapts a plain function to the Checker interface.
type CheckerFunc struct {
	name string
	fn   func(ctx context.Context) error
}

// NewChecker constructs a CheckerFunc with the given name and check function.
func NewChecker(name string, fn func(ctx context.Context) error) *CheckerFunc {
	return &CheckerFunc{name: name, fn: fn}
}

// Name returns the component name.
func (c *CheckerFunc) Name() string { return c.name }

// Check invokes the underlying function.
func (c *CheckerFunc) Check(ctx context.Context) error { return c.fn(ctx) }

// ComponentStatus is the per-component portion of a health response.
type ComponentStatus struct {
	// Status is one of "ok", "degraded", or "unavailable".
	Status StatusValue `json:"status"`
	// Message is a human-readable explanation when status != "ok". Empty otherwise.
	Message string `json:"message,omitempty"`
}

// ReadinessResponse is the JSON body returned by /healthz/ready.
type ReadinessResponse struct {
	// SchemaVersion enables consumers to detect format changes.
	SchemaVersion string `json:"schema_version"`
	// Status is the overall readiness status.
	Status StatusValue `json:"status"`
	// Components holds individual check results keyed by component name.
	Components map[string]ComponentStatus `json:"components"`
	// CheckedAt is the RFC 3339 timestamp of this evaluation.
	CheckedAt string `json:"checked_at"`
}

// DetailedResponse is the JSON body returned by /healthz.
type DetailedResponse struct {
	// SchemaVersion enables consumers to detect format changes.
	SchemaVersion string `json:"schema_version"`
	// Status is the overall health status.
	Status StatusValue `json:"status"`
	// Components holds individual check results keyed by component name.
	Components map[string]ComponentStatus `json:"components"`
	// ProtocolVersion is the Soroban protocol version detected by the daemon,
	// or 0 when unknown.
	ProtocolVersion int `json:"protocol_version"`
	// UptimeSeconds is the number of seconds since the Handler was created.
	UptimeSeconds int64 `json:"uptime_seconds"`
	// CheckedAt is the RFC 3339 timestamp of this evaluation.
	CheckedAt string `json:"checked_at"`
}

// Handler holds the registered checkers and serves health endpoints.
// It is safe for concurrent use.
type Handler struct {
	mu              sync.RWMutex
	checkers        []Checker
	protocolVersion int
	startTime       time.Time
	// checkTimeout limits the time each Checker.Check call may take.
	// Defaults to 5 seconds.
	checkTimeout time.Duration
}

// NewHandler returns a Handler with no registered checkers.
func NewHandler() *Handler {
	return &Handler{
		startTime:    time.Now(),
		checkTimeout: 5 * time.Second,
	}
}

// Register adds a Checker to the handler. Duplicate names are allowed but
// each check is run independently; callers should ensure names are unique.
func (h *Handler) Register(c Checker) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkers = append(h.checkers, c)
}

// SetProtocolVersion stores the current Soroban protocol version for reporting
// in /healthz. Call this whenever the daemon detects a version change.
func (h *Handler) SetProtocolVersion(v int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.protocolVersion = v
}

// Mount registers the three health endpoints on mux:
//
//	GET /healthz/live
//	GET /healthz/ready
//	GET /healthz
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/healthz/live", h.handleLive)
	mux.HandleFunc("/healthz/ready", h.handleReady)
	mux.HandleFunc("/healthz", h.handleDetailed)
}

// handleLive serves GET /healthz/live.
// Always returns 200 while the process is running.
func (h *Handler) handleLive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok","schema_version":"` + schemaVersion + `"}`))
}

// handleReady serves GET /healthz/ready.
// Returns 200 when all checkers pass; 503 with details when any fail.
func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	results, overall := h.runChecks(r.Context())
	resp := ReadinessResponse{
		SchemaVersion: schemaVersion,
		Status:        overall,
		Components:    results,
		CheckedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	if overall != StatusOK {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(resp)
}

// handleDetailed serves GET /healthz.
// Always returns 200 with full component details, uptime, and protocol version.
func (h *Handler) handleDetailed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	results, overall := h.runChecks(r.Context())
	h.mu.RLock()
	protoVer := h.protocolVersion
	uptime := int64(time.Since(h.startTime).Seconds())
	h.mu.RUnlock()

	resp := DetailedResponse{
		SchemaVersion:   schemaVersion,
		Status:          overall,
		Components:      results,
		ProtocolVersion: protoVer,
		UptimeSeconds:   uptime,
		CheckedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(resp)
}

// runChecks executes all registered checkers concurrently (up to checkTimeout
// each) and returns the per-component map and the overall status.
func (h *Handler) runChecks(ctx context.Context) (map[string]ComponentStatus, StatusValue) {
	h.mu.RLock()
	checkers := make([]Checker, len(h.checkers))
	copy(checkers, h.checkers)
	timeout := h.checkTimeout
	h.mu.RUnlock()

	if len(checkers) == 0 {
		return map[string]ComponentStatus{}, StatusOK
	}

	type result struct {
		name string
		cs   ComponentStatus
	}

	results := make(chan result, len(checkers))

	for _, c := range checkers {
		c := c
		go func() {
			checkCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			err := c.Check(checkCtx)
			if err != nil {
				results <- result{
					name: c.Name(),
					cs:   ComponentStatus{Status: StatusUnavailable, Message: err.Error()},
				}
			} else {
				results <- result{
					name: c.Name(),
					cs:   ComponentStatus{Status: StatusOK},
				}
			}
		}()
	}

	components := make(map[string]ComponentStatus, len(checkers))
	for i := 0; i < len(checkers); i++ {
		r := <-results
		components[r.name] = r.cs
	}

	overall := StatusOK
	for _, cs := range components {
		if cs.Status != StatusOK {
			overall = StatusUnavailable
			break
		}
	}
	return components, overall
}
