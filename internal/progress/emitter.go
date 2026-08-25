// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package progress

import (
	"time"

	"github.com/dotandev/glassbox/internal/telemetry"
)

// Emitter is a convenience wrapper around a Sink that stamps events with the
// shared operation ID and current time, so call sites only supply phase,
// status, message, and optional metadata.
type Emitter struct {
	sink Sink
}

// NewEmitter wraps sink with helper methods.
func NewEmitter(sink Sink) *Emitter {
	return &Emitter{sink: sink}
}

// Start emits a StatusStart event for the given phase.
func (e *Emitter) Start(phase Phase, message string, meta ...map[string]interface{}) {
	ev := Event{
		OperationID: e.sink.OperationID(),
		Phase:       phase,
		Status:      StatusStart,
		Timestamp:   time.Now().UTC(),
		Message:     message,
	}
	if len(meta) > 0 {
		ev.Meta = meta[0]
	}
	
	// Validate event against telemetry registry before emitting
	if err := validateEvent(ev); err != nil {
		// Log validation error but still emit to avoid breaking existing behavior
		// TODO: Make this strict once all events are registered
		e.sink.Emit(ev)
		return
	}
	
	e.sink.Emit(ev)
}

// Complete emits a StatusComplete event for the given phase.
func (e *Emitter) Complete(phase Phase, message string, meta ...map[string]interface{}) {
	ev := Event{
		OperationID: e.sink.OperationID(),
		Phase:       phase,
		Status:      StatusComplete,
		Timestamp:   time.Now().UTC(),
		Message:     message,
	}
	if len(meta) > 0 {
		ev.Meta = meta[0]
	}
	
	if err := validateEvent(ev); err != nil {
		e.sink.Emit(ev)
		return
	}
	
	e.sink.Emit(ev)
}

// Error emits a StatusError event for the given phase.
// errorCode should be a stable snake_case identifier (e.g. "rpc_connection_failed").
func (e *Emitter) Error(phase Phase, message, errorCode string, meta ...map[string]interface{}) {
	ev := Event{
		OperationID: e.sink.OperationID(),
		Phase:       phase,
		Status:      StatusError,
		Timestamp:   time.Now().UTC(),
		Message:     message,
		ErrorCode:   errorCode,
	}
	if len(meta) > 0 {
		ev.Meta = meta[0]
	}
	
	if err := validateEvent(ev); err != nil {
		e.sink.Emit(ev)
		return
	}
	
	e.sink.Emit(ev)
}

// Skip emits a StatusSkipped event for the given phase.
func (e *Emitter) Skip(phase Phase, message string, meta ...map[string]interface{}) {
	ev := Event{
		OperationID: e.sink.OperationID(),
		Phase:       phase,
		Status:      StatusSkipped,
		Timestamp:   time.Now().UTC(),
		Message:     message,
	}
	if len(meta) > 0 {
		ev.Meta = meta[0]
	}
	
	if err := validateEvent(ev); err != nil {
		e.sink.Emit(ev)
		return
	}
	
	e.sink.Emit(ev)
}

// OperationID returns the operation ID shared by all events from this emitter.
func (e *Emitter) OperationID() string {
	return e.sink.OperationID()
}

// validateEvent converts an Event to a map and validates it against the telemetry registry.
func validateEvent(ev Event) error {
	// Convert Event to map for validation
	payload := map[string]interface{}{
		"operation_id": ev.OperationID,
		"phase":        string(ev.Phase),
		"status":       string(ev.Status),
		"timestamp":    ev.Timestamp.UTC().Format(time.RFC3339Nano),
	}
	if ev.Message != "" {
		payload["message"] = ev.Message
	}
	if ev.ErrorCode != "" {
		payload["error_code"] = ev.ErrorCode
	}
	if ev.Meta != nil {
		payload["meta"] = ev.Meta
	}
	
	// Validate against telemetry registry
	return telemetry.Validate("debug.progress", payload)
}
