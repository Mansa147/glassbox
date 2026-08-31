// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package testhelpers

import (
	"time"

	"github.com/dotandev/glassbox/internal/trace"
)

// TraceFixture is a minimal builder for trace.ExecutionTrace used in
// regression tests. It produces well-formed traces without requiring a real
// simulator run.
type TraceFixture struct {
	TxHash string
	states []trace.ExecutionState
}

// NewTraceFixture creates a new trace fixture seeded with the canonical stub hash.
func NewTraceFixture() *TraceFixture {
	return &TraceFixture{TxHash: CanonicalTxHash}
}

// WithTxHash overrides the transaction hash.
func (f *TraceFixture) WithTxHash(hash string) *TraceFixture {
	f.TxHash = hash
	return f
}

// AddContractCallEvent appends a contract_call diagnostic event step.
func (f *TraceFixture) AddContractCallEvent(contractID, function string) *TraceFixture {
	f.states = append(f.states, trace.ExecutionState{
		Step:       len(f.states),
		Operation:  "contract_call",
		EventType:  "contract_call",
		ContractID: contractID,
		Function:   function,
		Timestamp:  time.Now(),
		Arguments:  []interface{}{},
	})
	return f
}

// AddErrorEvent appends a step representing a contract execution failure.
func (f *TraceFixture) AddErrorEvent(contractID, errMsg string) *TraceFixture {
	f.states = append(f.states, trace.ExecutionState{
		Step:       len(f.states),
		Operation:  "error",
		EventType:  "error",
		ContractID: contractID,
		Error:      errMsg,
		Timestamp:  time.Now(),
	})
	return f
}

// AddStep appends a generic step to the trace.
func (f *TraceFixture) AddStep(operation, eventType string) *TraceFixture {
	f.states = append(f.states, trace.ExecutionState{
		Step:      len(f.states),
		Operation: operation,
		EventType: eventType,
		Timestamp: time.Now(),
	})
	return f
}

// Build converts the fixture into a trace.ExecutionTrace.
// If no steps were added the returned trace has zero states, which is useful
// for testing the "empty trace" error path.
func (f *TraceFixture) Build() *trace.ExecutionTrace {
	now := time.Now()
	t := trace.NewExecutionTrace(f.TxHash, trace.DefaultSnapshotInterval)
	t.StartTime = now
	t.EndTime = now.Add(time.Millisecond)
	for _, s := range f.states {
		t.AddState(s)
	}
	return t
}
