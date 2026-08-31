// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package testhelpers

import (
	"github.com/dotandev/glassbox/internal/simulator"
)

// SimRequestFixture is a minimal builder for simulator.SimulationRequest.
type SimRequestFixture struct {
	EnvelopeXdr   string
	ResultMetaXdr string
	LedgerEntries map[string]string
	Timestamp     int64
}

// NewSimRequestFixture creates a new simulation request fixture with stub defaults.
func NewSimRequestFixture() *SimRequestFixture {
	return &SimRequestFixture{
		EnvelopeXdr:   CanonicalEnvelopeXDR,
		ResultMetaXdr: CanonicalEnvelopeXDR,
		LedgerEntries: make(map[string]string),
		Timestamp:     1735689600, // 2026-01-01T00:00:00Z
	}
}

// WithEnvelope sets the envelope XDR.
func (f *SimRequestFixture) WithEnvelope(xdr string) *SimRequestFixture {
	f.EnvelopeXdr = xdr
	return f
}

// WithResultMeta sets the result meta XDR.
func (f *SimRequestFixture) WithResultMeta(xdr string) *SimRequestFixture {
	f.ResultMetaXdr = xdr
	return f
}

// WithLedgerEntry adds a single ledger entry (key → XDR value).
func (f *SimRequestFixture) WithLedgerEntry(key, xdr string) *SimRequestFixture {
	f.LedgerEntries[key] = xdr
	return f
}

// Build converts the fixture into a simulator.SimulationRequest.
func (f *SimRequestFixture) Build() *simulator.SimulationRequest {
	return &simulator.SimulationRequest{
		EnvelopeXdr:   f.EnvelopeXdr,
		ResultMetaXdr: f.ResultMetaXdr,
		LedgerEntries: f.LedgerEntries,
		Timestamp:     f.Timestamp,
	}
}

// SimResponseFixture is a minimal builder for simulator.SimulationResponse.
type SimResponseFixture struct {
	Status           string
	Error            string
	Events           []string
	DiagnosticEvents []simulator.DiagnosticEvent
	Logs             []string
	BudgetUsage      *simulator.BudgetUsage
}

// NewSimResponseFixture creates a new simulation response fixture.
func NewSimResponseFixture() *SimResponseFixture {
	return &SimResponseFixture{
		Status: "success",
		Events: []string{},
		Logs:   []string{},
	}
}

// WithError marks the response as failed with the given error message.
func (f *SimResponseFixture) WithError(msg string) *SimResponseFixture {
	f.Status = "error"
	f.Error = msg
	return f
}

// WithDiagnosticEvent appends a diagnostic event.
func (f *SimResponseFixture) WithDiagnosticEvent(eventType string, contractID *string) *SimResponseFixture {
	f.DiagnosticEvents = append(f.DiagnosticEvents, simulator.DiagnosticEvent{
		EventType:  eventType,
		ContractID: contractID,
		Topics:     []string{},
		Data:       "",
	})
	return f
}

// WithBudgetExhausted sets budget usage to indicate CPU exhaustion.
func (f *SimResponseFixture) WithBudgetExhausted() *SimResponseFixture {
	f.BudgetUsage = &simulator.BudgetUsage{
		CPUInstructions:    100000000,
		CPULimit:           100000000,
		CPUUsagePercent:    100.0,
		MemoryBytes:        1024,
		MemoryLimit:        41943040,
		MemoryUsagePercent: 0.002,
	}
	return f
}

// Build converts the fixture into a simulator.SimulationResponse.
func (f *SimResponseFixture) Build() *simulator.SimulationResponse {
	return &simulator.SimulationResponse{
		Status:           f.Status,
		Error:            f.Error,
		Events:           f.Events,
		DiagnosticEvents: f.DiagnosticEvents,
		Logs:             f.Logs,
		BudgetUsage:      f.BudgetUsage,
	}
}
