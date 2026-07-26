// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package progress

import "time"

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
	e.sink.Emit(ev)
}

// OperationID returns the operation ID shared by all events from this emitter.
func (e *Emitter) OperationID() string {
	return e.sink.OperationID()
}
