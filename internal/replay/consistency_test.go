// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package replay_test

import (
	"strings"
	"testing"

	"github.com/dotandev/glassbox/internal/replay"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func meta(source, network string, seq, proto uint32, keys ...string) replay.InputMetadata {
	return replay.InputMetadata{
		Source:                source,
		Network:               network,
		LedgerSequence:        seq,
		ProtocolVersion:       proto,
		RequiredFootprintKeys: keys,
	}
}

func stateKeys(keys ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[k] = struct{}{}
	}
	return m
}

// ── valid inputs ─────────────────────────────────────────────────────────────

func TestValidateConsistency_AllMatch(t *testing.T) {
	inputs := []replay.InputMetadata{
		meta("transaction", "testnet", 100, 22),
		meta("ledger state", "testnet", 100, 22),
		meta("RPC", "testnet", 100, 22),
	}
	report, err := replay.ValidateConsistency(inputs, nil)
	require.NoError(t, err)
	assert.True(t, report.OK)
	assert.Empty(t, report.Mismatches)
}

func TestValidateConsistency_SingleInput(t *testing.T) {
	inputs := []replay.InputMetadata{
		meta("transaction", "mainnet", 200, 21),
	}
	report, err := replay.ValidateConsistency(inputs, nil)
	require.NoError(t, err)
	assert.True(t, report.OK)
}

func TestValidateConsistency_MissingFieldsSkipped(t *testing.T) {
	// Zero values are treated as "not available" and skipped.
	inputs := []replay.InputMetadata{
		{Source: "transaction", Network: "mainnet", LedgerSequence: 0, ProtocolVersion: 0},
		{Source: "ledger state", Network: "mainnet", LedgerSequence: 0, ProtocolVersion: 0},
	}
	report, err := replay.ValidateConsistency(inputs, nil)
	require.NoError(t, err)
	assert.True(t, report.OK)
}

func TestValidateConsistency_FootprintPresent(t *testing.T) {
	inputs := []replay.InputMetadata{
		meta("transaction", "testnet", 1, 22, "key_aaa", "key_bbb"),
	}
	opts := &replay.ValidateConsistencyOptions{
		LedgerStateKeys: stateKeys("key_aaa", "key_bbb", "key_ccc"),
	}
	report, err := replay.ValidateConsistency(inputs, opts)
	require.NoError(t, err)
	assert.True(t, report.OK)
}

// ── sequence mismatch ─────────────────────────────────────────────────────────

func TestValidateConsistency_SequenceMismatch(t *testing.T) {
	inputs := []replay.InputMetadata{
		meta("transaction", "testnet", 100, 22),
		meta("ledger state", "testnet", 101, 22), // different sequence
	}
	report, err := replay.ValidateConsistency(inputs, nil)
	require.Error(t, err)
	assert.False(t, report.OK)

	found := false
	for _, m := range report.Mismatches {
		if m.Kind == replay.MismatchLedgerSequence {
			found = true
			assert.Contains(t, m.Description, "ledger_sequence")
			assert.Contains(t, m.Values["transaction"], "100")
			assert.Contains(t, m.Values["ledger state"], "101")
		}
	}
	assert.True(t, found, "expected MismatchLedgerSequence mismatch")

	// Error message must identify the mismatched field
	ce := replay.AsConsistencyError(err)
	require.NotNil(t, ce)
	assert.Contains(t, err.Error(), "ledger_sequence")
}

// ── network mismatch ──────────────────────────────────────────────────────────

func TestValidateConsistency_NetworkMismatch(t *testing.T) {
	inputs := []replay.InputMetadata{
		meta("transaction", "testnet", 50, 22),
		meta("RPC loader", "mainnet", 50, 22), // different network
	}
	report, err := replay.ValidateConsistency(inputs, nil)
	require.Error(t, err)
	assert.False(t, report.OK)

	found := false
	for _, m := range report.Mismatches {
		if m.Kind == replay.MismatchNetwork {
			found = true
			assert.Equal(t, "testnet", m.Values["transaction"])
			assert.Equal(t, "mainnet", m.Values["RPC loader"])
		}
	}
	assert.True(t, found, "expected MismatchNetwork mismatch")
}

// ── protocol version mismatch ─────────────────────────────────────────────────

func TestValidateConsistency_ProtocolVersionMismatch(t *testing.T) {
	inputs := []replay.InputMetadata{
		meta("transaction", "testnet", 50, 22),
		meta("ledger state", "testnet", 50, 20), // different protocol version
	}
	report, err := replay.ValidateConsistency(inputs, nil)
	require.Error(t, err)
	assert.False(t, report.OK)

	found := false
	for _, m := range report.Mismatches {
		if m.Kind == replay.MismatchProtocolVersion {
			found = true
			assert.Contains(t, m.Values["transaction"], "22")
			assert.Contains(t, m.Values["ledger state"], "20")
		}
	}
	assert.True(t, found, "expected MismatchProtocolVersion mismatch")
}

// ── multiple mismatches reported together ─────────────────────────────────────

func TestValidateConsistency_MultipleMismatchesReportedTogether(t *testing.T) {
	inputs := []replay.InputMetadata{
		meta("transaction", "testnet", 100, 22),
		meta("ledger state", "mainnet", 200, 20), // all three fields differ
	}
	report, err := replay.ValidateConsistency(inputs, nil)
	require.Error(t, err)
	assert.False(t, report.OK)
	// All three mismatches must be in the report, not just the first one found.
	assert.GreaterOrEqual(t, len(report.Mismatches), 3)

	kinds := make(map[replay.MismatchKind]bool)
	for _, m := range report.Mismatches {
		kinds[m.Kind] = true
	}
	assert.True(t, kinds[replay.MismatchLedgerSequence], "missing ledger_sequence mismatch")
	assert.True(t, kinds[replay.MismatchNetwork], "missing network mismatch")
	assert.True(t, kinds[replay.MismatchProtocolVersion], "missing protocol_version mismatch")

	// Error message must list all mismatch fields
	errMsg := err.Error()
	assert.Contains(t, errMsg, "ledger_sequence")
	assert.Contains(t, errMsg, "network")
	assert.Contains(t, errMsg, "protocol_version")
}

// ── missing footprint entry ───────────────────────────────────────────────────

func TestValidateConsistency_MissingFootprintEntry(t *testing.T) {
	inputs := []replay.InputMetadata{
		meta("transaction", "testnet", 10, 22, "key_present", "key_absent"),
	}
	opts := &replay.ValidateConsistencyOptions{
		LedgerStateKeys: stateKeys("key_present"),
	}
	report, err := replay.ValidateConsistency(inputs, opts)
	require.Error(t, err)
	assert.False(t, report.OK)

	found := false
	for _, m := range report.Mismatches {
		if m.Kind == replay.MismatchMissingFootprint {
			found = true
			assert.Equal(t, "key_absent", m.MissingKey)
		}
	}
	assert.True(t, found, "expected MismatchMissingFootprint mismatch")
}

func TestValidateConsistency_MultipleFootprintMismatches(t *testing.T) {
	inputs := []replay.InputMetadata{
		meta("transaction", "testnet", 10, 22, "k1", "k2", "k3"),
	}
	opts := &replay.ValidateConsistencyOptions{
		LedgerStateKeys: stateKeys("k1"), // k2 and k3 are absent
	}
	report, err := replay.ValidateConsistency(inputs, opts)
	require.Error(t, err)

	missingCount := 0
	for _, m := range report.Mismatches {
		if m.Kind == replay.MismatchMissingFootprint {
			missingCount++
		}
	}
	assert.Equal(t, 2, missingCount, "expected 2 missing footprint entries")
}

// ── diagnostic override ───────────────────────────────────────────────────────

func TestValidateConsistency_DiagnosticOverride_NoError(t *testing.T) {
	inputs := []replay.InputMetadata{
		meta("transaction", "testnet", 100, 22),
		meta("ledger state", "mainnet", 200, 20),
	}
	opts := &replay.ValidateConsistencyOptions{
		DiagnosticOverride: true,
	}
	report, err := replay.ValidateConsistency(inputs, opts)
	require.NoError(t, err, "diagnostic override must suppress the error")
	assert.False(t, report.OK, "OK must still be false when mismatches exist")
	assert.True(t, report.DiagnosticOverride, "DiagnosticOverride flag must be set in the report")
	assert.NotEmpty(t, report.Mismatches, "mismatches must still be recorded")
}

// ── report helpers ────────────────────────────────────────────────────────────

func TestConsistencyReport_MismatchSummary(t *testing.T) {
	inputs := []replay.InputMetadata{
		meta("tx", "testnet", 1, 22),
		meta("state", "mainnet", 2, 20),
	}
	report, _ := replay.ValidateConsistency(inputs, nil)
	summary := report.MismatchSummary()
	assert.NotEmpty(t, summary)
	assert.True(t, strings.Contains(summary, "mismatch"))
}

func TestConsistencyReport_MismatchSummary_OK(t *testing.T) {
	inputs := []replay.InputMetadata{
		meta("tx", "testnet", 1, 22),
		meta("state", "testnet", 1, 22),
	}
	report, _ := replay.ValidateConsistency(inputs, nil)
	assert.Equal(t, "all inputs consistent", report.MismatchSummary())
}

// ── error identification helpers ──────────────────────────────────────────────

func TestIsConsistencyError(t *testing.T) {
	inputs := []replay.InputMetadata{
		meta("tx", "testnet", 1, 22),
		meta("state", "mainnet", 1, 22),
	}
	_, err := replay.ValidateConsistency(inputs, nil)
	require.Error(t, err)
	assert.True(t, replay.IsConsistencyError(err))
	assert.NotNil(t, replay.AsConsistencyError(err))
}

func TestErrorContainsRemediationHint(t *testing.T) {
	inputs := []replay.InputMetadata{
		meta("tx", "testnet", 1, 22),
		meta("state", "mainnet", 2, 20),
	}
	_, err := replay.ValidateConsistency(inputs, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), DiagnosticOverrideName())
}

// DiagnosticOverrideName is accessible from the test to verify the constant is exported.
func DiagnosticOverrideName() string { return replay.DiagnosticOverrideName }
