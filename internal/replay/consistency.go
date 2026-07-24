// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

// Package replay provides snapshot registry, ledger validation, and the
// cross-input consistency validator used before simulator startup.
package replay

import (
	"fmt"
	"strings"
)

// InputMetadata carries the ledger-level identity fields extracted from the
// three replay inputs (transaction envelope, footprint/ledger state, and the
// result metadata / RPC loader).  All fields are optional; a zero-value field
// means "not available" and is skipped during comparison.
type InputMetadata struct {
	// Source names the origin of this metadata (e.g. "transaction", "ledger state", "RPC").
	Source string

	// LedgerSequence is the ledger sequence this input was sourced from.
	LedgerSequence uint32

	// Network identifies the Stellar network (testnet, mainnet, futurenet).
	Network string

	// ProtocolVersion is the Soroban protocol version associated with this input.
	ProtocolVersion uint32

	// RequiredFootprintKeys is the set of XDR ledger-key strings the transaction
	// reads or writes.  When non-nil it is used to verify that all required
	// entries are present in the provided ledger state.
	RequiredFootprintKeys []string
}

// MismatchKind identifies which field of the inputs is inconsistent.
type MismatchKind string

const (
	MismatchLedgerSequence  MismatchKind = "ledger_sequence"
	MismatchNetwork         MismatchKind = "network"
	MismatchProtocolVersion MismatchKind = "protocol_version"
	MismatchMissingFootprint MismatchKind = "missing_footprint_entry"
)

// Mismatch describes one detected inconsistency between replay inputs.
type Mismatch struct {
	// Kind is the stable identifier for the mismatched field.
	Kind MismatchKind

	// Field is the human-readable field name.
	Field string

	// Values is a map from source name to the value observed at that source.
	Values map[string]string

	// MissingKey is set only for MismatchMissingFootprint entries and contains
	// the XDR ledger key that is required but absent from the ledger state.
	MissingKey string

	// Description is a one-sentence explanation suitable for user output.
	Description string
}

// ConsistencyReport is the output of ValidateConsistency.
type ConsistencyReport struct {
	// OK is true when no mismatches were found.
	OK bool

	// Mismatches lists every detected inconsistency.
	Mismatches []Mismatch

	// DiagnosticOverride is true when validation ran in diagnostic mode
	// (GLASSBOX_ALLOW_MIXED_INPUTS=1 or WithDiagnosticOverride option).
	// When true, the simulator is still started even though mismatches exist.
	DiagnosticOverride bool
}

// ConsistencyError is returned by ValidateConsistency when the report contains
// mismatches and diagnostic override is not enabled.
type ConsistencyError struct {
	Report *ConsistencyReport
}

func (e *ConsistencyError) Error() string {
	return formatReport(e.Report)
}

// IsConsistencyError reports whether err is a *ConsistencyError.
func IsConsistencyError(err error) bool {
	_, ok := err.(*ConsistencyError)
	return ok
}

// AsConsistencyError returns the *ConsistencyError if err is one, or nil.
func AsConsistencyError(err error) *ConsistencyError {
	if ce, ok := err.(*ConsistencyError); ok {
		return ce
	}
	return nil
}

// ValidateConsistencyOptions configures the behaviour of ValidateConsistency.
type ValidateConsistencyOptions struct {
	// DiagnosticOverride disables the hard failure so mismatches are reported
	// but do not prevent the simulator from starting.  Use this only for
	// advanced debugging.  The named constant DiagnosticOverrideName appears in
	// error messages so users know how to enable it.
	DiagnosticOverride bool

	// LedgerStateKeys is the full set of XDR ledger-key strings present in the
	// provided ledger state snapshot.  When non-nil, footprint completeness is
	// checked.
	LedgerStateKeys map[string]struct{}
}

// DiagnosticOverrideName is the environment variable name that enables the
// diagnostic override.  It is surfaced in error messages so users can find it.
const DiagnosticOverrideName = "GLASSBOX_ALLOW_MIXED_INPUTS"

// ValidateConsistency checks that all provided InputMetadata agree on ledger
// sequence, network, and protocol version, and that every required footprint
// key is present in the ledger state.
//
// When opts is nil, strict default behaviour applies: any mismatch causes a
// *ConsistencyError to be returned and the simulator must not be started.
//
// When opts.DiagnosticOverride is true, mismatches are recorded but the
// returned error is nil, allowing the caller to proceed with a degraded replay.
// The report's DiagnosticOverride field will be true so callers can log a
// prominent warning.
//
// All detected mismatches are collected before returning so the user sees the
// complete picture in one error message.
func ValidateConsistency(inputs []InputMetadata, opts *ValidateConsistencyOptions) (*ConsistencyReport, error) {
	if opts == nil {
		opts = &ValidateConsistencyOptions{}
	}

	report := &ConsistencyReport{
		DiagnosticOverride: opts.DiagnosticOverride,
	}

	// ── 1. Ledger sequence consistency ───────────────────────────────────────
	seqMismatch := checkFieldConsistency(inputs, "ledger_sequence", MismatchLedgerSequence,
		func(m InputMetadata) (string, bool) {
			if m.LedgerSequence == 0 {
				return "", false
			}
			return fmt.Sprintf("%d", m.LedgerSequence), true
		},
	)
	report.Mismatches = append(report.Mismatches, seqMismatch...)

	// ── 2. Network consistency ───────────────────────────────────────────────
	netMismatch := checkFieldConsistency(inputs, "network", MismatchNetwork,
		func(m InputMetadata) (string, bool) {
			if m.Network == "" {
				return "", false
			}
			return m.Network, true
		},
	)
	report.Mismatches = append(report.Mismatches, netMismatch...)

	// ── 3. Protocol version consistency ─────────────────────────────────────
	protoMismatch := checkFieldConsistency(inputs, "protocol_version", MismatchProtocolVersion,
		func(m InputMetadata) (string, bool) {
			if m.ProtocolVersion == 0 {
				return "", false
			}
			return fmt.Sprintf("%d", m.ProtocolVersion), true
		},
	)
	report.Mismatches = append(report.Mismatches, protoMismatch...)

	// ── 4. Footprint completeness ────────────────────────────────────────────
	if opts.LedgerStateKeys != nil {
		for _, meta := range inputs {
			for _, key := range meta.RequiredFootprintKeys {
				if _, present := opts.LedgerStateKeys[key]; !present {
					short := key
					if len(short) > 40 {
						short = short[:40] + "…"
					}
					report.Mismatches = append(report.Mismatches, Mismatch{
						Kind:       MismatchMissingFootprint,
						Field:      "footprint_entry",
						MissingKey: key,
						Values:     map[string]string{meta.Source: key},
						Description: fmt.Sprintf(
							"footprint entry required by %q is absent from the ledger state: %s",
							meta.Source, short,
						),
					})
				}
			}
		}
	}

	report.OK = len(report.Mismatches) == 0

	if !report.OK && !opts.DiagnosticOverride {
		return report, &ConsistencyError{Report: report}
	}

	return report, nil
}

// checkFieldConsistency compares the value of a single field across all inputs
// and returns a Mismatch when two or more non-zero values differ.
func checkFieldConsistency(
	inputs []InputMetadata,
	field string,
	kind MismatchKind,
	extract func(InputMetadata) (string, bool),
) []Mismatch {
	values := make(map[string]string) // source → value
	var canonical string
	divergent := false

	for _, m := range inputs {
		v, ok := extract(m)
		if !ok {
			continue
		}
		values[m.Source] = v
		if canonical == "" {
			canonical = v
		} else if v != canonical {
			divergent = true
		}
	}

	if !divergent || len(values) < 2 {
		return nil
	}

	// Build a readable description listing all observed values.
	var parts []string
	for src, val := range values {
		parts = append(parts, fmt.Sprintf("%s=%s", src, val))
	}
	desc := fmt.Sprintf("%s mismatch across inputs: %s", field, strings.Join(parts, ", "))

	return []Mismatch{{
		Kind:        kind,
		Field:       field,
		Values:      values,
		Description: desc,
	}}
}

// formatReport produces the human-readable multi-mismatch message included in
// ConsistencyError.Error().
func formatReport(r *ConsistencyReport) string {
	if r == nil || r.OK {
		return "consistency validation passed"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"replay inputs are inconsistent (%d mismatch(es)) — simulator startup aborted:\n",
		len(r.Mismatches),
	))

	for i, m := range r.Mismatches {
		sb.WriteString(fmt.Sprintf("  %d. [%s] %s\n", i+1, m.Kind, m.Description))
		if m.MissingKey != "" {
			sb.WriteString(fmt.Sprintf("     Missing key: %s\n", m.MissingKey))
		}
	}

	sb.WriteString("\nRemediation:\n")
	sb.WriteString("  • Ensure the transaction, ledger state, and footprint all originate from\n")
	sb.WriteString("    the same ledger sequence and network.\n")
	sb.WriteString("  • Re-fetch the ledger state with 'glassbox debug <tx-hash>' to guarantee\n")
	sb.WriteString("    a consistent snapshot.\n")
	sb.WriteString(fmt.Sprintf("  • Set %s=1 to bypass this check for advanced diagnostics\n", DiagnosticOverrideName))
	sb.WriteString("    (diagnostic override; mismatches will be logged but not fatal).\n")

	return sb.String()
}

// MismatchSummary returns a one-line human-readable summary of the report
// suitable for diagnostic log output.
func (r *ConsistencyReport) MismatchSummary() string {
	if r == nil || r.OK {
		return "all inputs consistent"
	}
	kinds := make([]string, 0, len(r.Mismatches))
	for _, m := range r.Mismatches {
		kinds = append(kinds, string(m.Kind))
	}
	return fmt.Sprintf("%d mismatch(es): %s", len(r.Mismatches), strings.Join(kinds, ", "))
}
