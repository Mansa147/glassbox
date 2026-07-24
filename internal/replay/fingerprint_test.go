// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package replay_test

import (
	"testing"

	"github.com/dotandev/glassbox/internal/replay"
	"github.com/dotandev/glassbox/internal/simulator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func proto(v uint32) *uint32 { return &v }
func mem(v uint64) *uint64   { return &v }

func baseRequest() *simulator.SimulationRequest {
	return &simulator.SimulationRequest{
		EnvelopeXdr:   "AAAA",
		ResultMetaXdr: "BBBB",
		LedgerEntries: map[string]string{
			"key1": "val1",
			"key2": "val2",
		},
		LedgerSequence:  100,
		ProtocolVersion: proto(22),
	}
}

func baseResponse() *simulator.SimulationResponse {
	return &simulator.SimulationResponse{
		Status: "success",
		Events: []string{"event1", "event2"},
	}
}

// ── input fingerprint ─────────────────────────────────────────────────────────

func TestComputeInputFingerprint_Deterministic(t *testing.T) {
	req := baseRequest()
	fp1, err := replay.ComputeInputFingerprint(req)
	require.NoError(t, err)
	fp2, err := replay.ComputeInputFingerprint(req)
	require.NoError(t, err)
	assert.Equal(t, fp1, fp2, "same request must produce same fingerprint")
}

func TestComputeInputFingerprint_LedgerEntryOrderIndependent(t *testing.T) {
	// Build two requests with the same entries but inserted in different order.
	req1 := baseRequest()
	req1.LedgerEntries = map[string]string{"key_b": "val_b", "key_a": "val_a"}

	req2 := baseRequest()
	req2.LedgerEntries = map[string]string{"key_a": "val_a", "key_b": "val_b"}

	fp1, err := replay.ComputeInputFingerprint(req1)
	require.NoError(t, err)
	fp2, err := replay.ComputeInputFingerprint(req2)
	require.NoError(t, err)
	assert.Equal(t, fp1, fp2, "map iteration order must not affect the input fingerprint")
}

func TestComputeInputFingerprint_TimestampExcluded(t *testing.T) {
	req1 := baseRequest()
	req1.Timestamp = 1000000

	req2 := baseRequest()
	req2.Timestamp = 9999999 // different volatile timestamp

	fp1, err := replay.ComputeInputFingerprint(req1)
	require.NoError(t, err)
	fp2, err := replay.ComputeInputFingerprint(req2)
	require.NoError(t, err)
	assert.Equal(t, fp1, fp2, "volatile timestamp must not affect the input fingerprint")
}

func TestComputeInputFingerprint_EnvelopeChange(t *testing.T) {
	req1 := baseRequest()
	req2 := baseRequest()
	req2.EnvelopeXdr = "ZZZZ" // mutation

	fp1, err := replay.ComputeInputFingerprint(req1)
	require.NoError(t, err)
	fp2, err := replay.ComputeInputFingerprint(req2)
	require.NoError(t, err)
	assert.NotEqual(t, fp1, fp2, "envelope change must change the input fingerprint")
}

func TestComputeInputFingerprint_LedgerEntryMutation(t *testing.T) {
	req1 := baseRequest()
	req2 := baseRequest()
	req2.LedgerEntries["key1"] = "MUTATED"

	fp1, err := replay.ComputeInputFingerprint(req1)
	require.NoError(t, err)
	fp2, err := replay.ComputeInputFingerprint(req2)
	require.NoError(t, err)
	assert.NotEqual(t, fp1, fp2, "ledger entry value change must change the input fingerprint")
}

func TestComputeInputFingerprint_NilRequest(t *testing.T) {
	_, err := replay.ComputeInputFingerprint(nil)
	assert.Error(t, err)
}

// ── sim config fingerprint ────────────────────────────────────────────────────

func TestComputeSimConfigFingerprint_Deterministic(t *testing.T) {
	req := baseRequest()
	fp1, err := replay.ComputeSimConfigFingerprint(req)
	require.NoError(t, err)
	fp2, err := replay.ComputeSimConfigFingerprint(req)
	require.NoError(t, err)
	assert.Equal(t, fp1, fp2)
}

func TestComputeSimConfigFingerprint_IndependentFromInput(t *testing.T) {
	req1 := baseRequest()
	req2 := baseRequest()
	req2.EnvelopeXdr = "DIFFERENT_ENVELOPE" // input change

	cfgFP1, err := replay.ComputeSimConfigFingerprint(req1)
	require.NoError(t, err)
	cfgFP2, err := replay.ComputeSimConfigFingerprint(req2)
	require.NoError(t, err)
	assert.Equal(t, cfgFP1, cfgFP2, "input change must not affect the SimConfig fingerprint")

	// But input fingerprints must differ
	inFP1, _ := replay.ComputeInputFingerprint(req1)
	inFP2, _ := replay.ComputeInputFingerprint(req2)
	assert.NotEqual(t, inFP1, inFP2)
}

func TestComputeSimConfigFingerprint_MemoryLimitChange(t *testing.T) {
	req1 := baseRequest()
	req1.MemoryLimit = mem(64 * 1024 * 1024)

	req2 := baseRequest()
	req2.MemoryLimit = mem(128 * 1024 * 1024)

	fp1, err := replay.ComputeSimConfigFingerprint(req1)
	require.NoError(t, err)
	fp2, err := replay.ComputeSimConfigFingerprint(req2)
	require.NoError(t, err)
	assert.NotEqual(t, fp1, fp2, "memory limit change must change the SimConfig fingerprint")
}

// ── output trace fingerprint ──────────────────────────────────────────────────

func TestComputeOutputTraceFingerprint_Deterministic(t *testing.T) {
	resp := baseResponse()
	fp1, err := replay.ComputeOutputTraceFingerprint(resp)
	require.NoError(t, err)
	fp2, err := replay.ComputeOutputTraceFingerprint(resp)
	require.NoError(t, err)
	assert.Equal(t, fp1, fp2)
}

func TestComputeOutputTraceFingerprint_BudgetExcluded(t *testing.T) {
	resp1 := baseResponse()
	resp1.BudgetUsage = &simulator.BudgetUsage{
		CPUInstructions: 1000,
		MemoryBytes:     500,
	}
	resp2 := baseResponse()
	resp2.BudgetUsage = &simulator.BudgetUsage{
		CPUInstructions: 9999, // different volatile budget
		MemoryBytes:     8888,
	}

	fp1, err := replay.ComputeOutputTraceFingerprint(resp1)
	require.NoError(t, err)
	fp2, err := replay.ComputeOutputTraceFingerprint(resp2)
	require.NoError(t, err)
	assert.Equal(t, fp1, fp2, "volatile budget usage must not affect the output trace fingerprint")
}

func TestComputeOutputTraceFingerprint_EventChange(t *testing.T) {
	resp1 := baseResponse()
	resp2 := baseResponse()
	resp2.Events = append(resp2.Events, "extra_event")

	fp1, _ := replay.ComputeOutputTraceFingerprint(resp1)
	fp2, _ := replay.ComputeOutputTraceFingerprint(resp2)
	assert.NotEqual(t, fp1, fp2, "event addition must change the output trace fingerprint")
}

func TestComputeOutputTraceFingerprint_NilResponse(t *testing.T) {
	_, err := replay.ComputeOutputTraceFingerprint(nil)
	assert.Error(t, err)
}

// ── ComputeFingerprints ───────────────────────────────────────────────────────

func TestComputeFingerprints_AllThree(t *testing.T) {
	req := baseRequest()
	resp := baseResponse()

	fp, err := replay.ComputeFingerprints(req, resp)
	require.NoError(t, err)
	assert.NotEmpty(t, fp.Input)
	assert.NotEmpty(t, fp.SimConfig)
	assert.NotEmpty(t, fp.OutputTrace)
}

func TestComputeFingerprints_NilRequest(t *testing.T) {
	resp := baseResponse()
	fp, err := replay.ComputeFingerprints(nil, resp)
	assert.Error(t, err)
	assert.Equal(t, "nil", fp.Input)
	assert.Equal(t, "nil", fp.SimConfig)
	assert.NotEmpty(t, fp.OutputTrace) // response is fine
}

// ── CompareFingerprints ───────────────────────────────────────────────────────

func TestCompareFingerprints_Identical(t *testing.T) {
	req := baseRequest()
	resp := baseResponse()

	fp1, err := replay.ComputeFingerprints(req, resp)
	require.NoError(t, err)
	fp2, err := replay.ComputeFingerprints(req, resp)
	require.NoError(t, err)

	diff := replay.CompareFingerprints(fp1, fp2)
	assert.False(t, diff.HasDifference)
	assert.Contains(t, diff.Explanation, "match")
}

func TestCompareFingerprints_InputDiffers(t *testing.T) {
	req1 := baseRequest()
	req2 := baseRequest()
	req2.EnvelopeXdr = "DIFFERENT"
	resp := baseResponse()

	fp1, _ := replay.ComputeFingerprints(req1, resp)
	fp2, _ := replay.ComputeFingerprints(req2, resp)

	diff := replay.CompareFingerprints(fp1, fp2)
	assert.True(t, diff.HasDifference)
	assert.True(t, diff.InputDiffers)
	assert.False(t, diff.SimConfigDiffers)
	assert.False(t, diff.OutputTraceDiffers)
	assert.Contains(t, diff.Explanation, "input")
}

func TestCompareFingerprints_OutputDiffers(t *testing.T) {
	req := baseRequest()
	resp1 := baseResponse()
	resp2 := baseResponse()
	resp2.Status = "error"
	resp2.Error = "contract panicked"

	fp1, _ := replay.ComputeFingerprints(req, resp1)
	fp2, _ := replay.ComputeFingerprints(req, resp2)

	diff := replay.CompareFingerprints(fp1, fp2)
	assert.True(t, diff.HasDifference)
	assert.False(t, diff.InputDiffers)
	assert.True(t, diff.OutputTraceDiffers)
	assert.Contains(t, diff.Explanation, "output_trace")
}

func TestCompareFingerprints_ExplanationIdentifiesDifferingField(t *testing.T) {
	fp1 := replay.ReplayFingerprints{
		Input:       "aaaaaa",
		SimConfig:   "bbbbbb",
		OutputTrace: "cccccc",
	}
	fp2 := replay.ReplayFingerprints{
		Input:       "aaaaaa",
		SimConfig:   "XXXXXX", // only this differs
		OutputTrace: "cccccc",
	}
	diff := replay.CompareFingerprints(fp1, fp2)
	assert.True(t, diff.HasDifference)
	assert.True(t, diff.SimConfigDiffers)
	assert.False(t, diff.InputDiffers)
	assert.Contains(t, diff.Explanation, "sim_config")
}
