// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package replay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/dotandev/glassbox/internal/simulator"
)

// ReplayFingerprints holds the three independent fingerprints computed for a
// single replay run.  Any mutation of the corresponding input or configuration
// changes exactly the relevant fingerprint, leaving the others unchanged.
type ReplayFingerprints struct {
	// Input is the SHA-256 hex digest of the normalized replay inputs
	// (EnvelopeXdr, sorted LedgerEntries, network, protocol version,
	// ledger sequence).  Timestamps and volatile fields are excluded.
	Input string `json:"input"`

	// SimConfig is the SHA-256 hex digest of the normalized simulator
	// configuration (protocol version, memory limit, auth trace options,
	// resource calibration, sandbox policy, custom auth config).  Request
	// fields that are part of the input (EnvelopeXdr, LedgerEntries) are
	// excluded to keep the two fingerprints independent.
	SimConfig string `json:"sim_config"`

	// OutputTrace is the SHA-256 hex digest of the normalized output trace
	// structure (events slice, diagnostic events, status).  Volatile fields
	// (timestamps, budget instruction counts) are excluded so that two
	// semantically equivalent runs produce the same output fingerprint.
	OutputTrace string `json:"output_trace"`
}

// inputCanon is the stable canonical representation of replay inputs.
// Fields excluded: Timestamp (volatile clock), WasmPath (path, not content),
// MockArgs, ControlCommand, RewindStep, ForkParams, HarnessReset,
// CoverageLCOVPath, EnableCoverage, Profile, NoCache.
type inputCanon struct {
	EnvelopeXdr     string            `json:"envelope_xdr"`
	ResultMetaXdr   string            `json:"result_meta_xdr"`
	LedgerEntries   []ledgerEntryPair `json:"ledger_entries"`
	LedgerSequence  uint32            `json:"ledger_sequence,omitempty"`
	ProtocolVersion *uint32           `json:"protocol_version,omitempty"`
}

// ledgerEntryPair is a sorted representation of a single ledger entry.
type ledgerEntryPair struct {
	Key   string `json:"k"`
	Value string `json:"v"`
}

// simConfigCanon is the stable canonical representation of simulator config.
// The fields that are part of the input (EnvelopeXdr, LedgerEntries, etc.)
// are intentionally excluded to keep Input and SimConfig independent.
type simConfigCanon struct {
	ProtocolVersion           *uint32                `json:"protocol_version,omitempty"`
	MemoryLimit               *uint64                `json:"memory_limit,omitempty"`
	MockBaseFee               *uint32                `json:"mock_base_fee,omitempty"`
	EnableOptimizationAdvisor bool                   `json:"enable_optimization_advisor,omitempty"`
	EnableSnapshots           bool                   `json:"enable_snapshots,omitempty"`
	SandboxMode               bool                   `json:"sandbox_mode,omitempty"`
	AllowedHostFunctions      []string               `json:"allowed_host_functions,omitempty"`
	ResourceCalibration       *simulator.ResourceCalibration `json:"resource_calibration,omitempty"`
	AuthTraceOpts             *simulator.AuthTraceOptions    `json:"auth_trace_opts,omitempty"`
	CustomAuthCfg             map[string]interface{} `json:"custom_auth_cfg,omitempty"`
	SandboxNativeTokenCapStroops *uint64             `json:"sandbox_native_token_cap_stroops,omitempty"`
}

// outputTraceCanon is the stable canonical representation of a simulation
// output trace.  Volatile fields excluded: ProtocolVersion (set by runner,
// not deterministic across versions), Flamegraph (rendering artifact),
// LCOVReport/LCOVReportPath (path-dependent), LinearMemoryDump (large blob),
// WasmOffset, SourceLocation, StackTrace (debug info, not trace semantics),
// BudgetUsage (instruction counts vary with VM implementation).
type outputTraceCanon struct {
	Status           string                    `json:"status"`
	Error            string                    `json:"error,omitempty"`
	Events           []string                  `json:"events,omitempty"`
	DiagnosticEvents []diagEventCanon          `json:"diagnostic_events,omitempty"`
	CategorizedEvents []catEventCanon          `json:"categorized_events,omitempty"`
}

type diagEventCanon struct {
	EventType                string   `json:"event_type"`
	ContractID               *string  `json:"contract_id,omitempty"`
	Topics                   []string `json:"topics,omitempty"`
	Data                     string   `json:"data,omitempty"`
	InSuccessfulContractCall bool     `json:"in_successful_contract_call,omitempty"`
}

type catEventCanon struct {
	Category string `json:"category"`
	Data     string `json:"data,omitempty"`
}

// ComputeInputFingerprint computes the Input fingerprint for a *SimulationRequest.
// Equivalent requests produce identical fingerprints; any mutation of a
// non-volatile field changes the fingerprint.
func ComputeInputFingerprint(req *simulator.SimulationRequest) (string, error) {
	if req == nil {
		return "", fmt.Errorf("nil SimulationRequest")
	}

	canon := inputCanon{
		EnvelopeXdr:     req.EnvelopeXdr,
		ResultMetaXdr:   req.ResultMetaXdr,
		LedgerSequence:  req.LedgerSequence,
		ProtocolVersion: req.ProtocolVersion,
	}

	// Sort ledger entries by key for determinism.
	if len(req.LedgerEntries) > 0 {
		pairs := make([]ledgerEntryPair, 0, len(req.LedgerEntries))
		for k, v := range req.LedgerEntries {
			pairs = append(pairs, ledgerEntryPair{Key: k, Value: v})
		}
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].Key < pairs[j].Key })
		canon.LedgerEntries = pairs
	}

	return hashCanonical(canon)
}

// ComputeSimConfigFingerprint computes the SimConfig fingerprint for a
// *SimulationRequest.  Only configuration fields are included; volatile and
// input fields are excluded.
func ComputeSimConfigFingerprint(req *simulator.SimulationRequest) (string, error) {
	if req == nil {
		return "", fmt.Errorf("nil SimulationRequest")
	}

	cfg := simConfigCanon{
		ProtocolVersion:              req.ProtocolVersion,
		MemoryLimit:                  req.MemoryLimit,
		MockBaseFee:                  req.MockBaseFee,
		EnableOptimizationAdvisor:    req.EnableOptimizationAdvisor,
		EnableSnapshots:              req.EnableSnapshots,
		SandboxMode:                  req.SandboxMode,
		AllowedHostFunctions:         req.AllowedHostFunctions,
		ResourceCalibration:          req.ResourceCalibration,
		AuthTraceOpts:                req.AuthTraceOpts,
		CustomAuthCfg:                req.CustomAuthCfg,
		SandboxNativeTokenCapStroops: req.SandboxNativeTokenCapStroops,
	}

	// Sort AllowedHostFunctions for determinism.
	if len(cfg.AllowedHostFunctions) > 0 {
		sorted := make([]string, len(cfg.AllowedHostFunctions))
		copy(sorted, cfg.AllowedHostFunctions)
		sort.Strings(sorted)
		cfg.AllowedHostFunctions = sorted
	}

	return hashCanonical(cfg)
}

// ComputeOutputTraceFingerprint computes the OutputTrace fingerprint for a
// *SimulationResponse.  Volatile fields (budget, protocol version, source
// locations, flamegraph, memory dumps) are excluded.
func ComputeOutputTraceFingerprint(resp *simulator.SimulationResponse) (string, error) {
	if resp == nil {
		return "", fmt.Errorf("nil SimulationResponse")
	}

	canon := outputTraceCanon{
		Status: resp.Status,
		Error:  resp.Error,
		Events: resp.Events,
	}

	for _, de := range resp.DiagnosticEvents {
		deCanon := diagEventCanon{
			EventType:                de.EventType,
			ContractID:               de.ContractID,
			Topics:                   de.Topics,
			Data:                     de.Data,
			InSuccessfulContractCall: de.InSuccessfulContractCall,
		}
		canon.DiagnosticEvents = append(canon.DiagnosticEvents, deCanon)
	}

	for _, ce := range resp.CategorizedEvents {
		ceCanon := catEventCanon{
			Category: ce.Category,
			Data:     ce.Data,
		}
		canon.CategorizedEvents = append(canon.CategorizedEvents, ceCanon)
	}

	return hashCanonical(canon)
}

// ComputeFingerprints computes all three fingerprints for a replay run in one
// call.  Either req or resp may be nil (the corresponding fingerprint will be
// an error string fingerprint of "nil").
func ComputeFingerprints(req *simulator.SimulationRequest, resp *simulator.SimulationResponse) (ReplayFingerprints, error) {
	var fp ReplayFingerprints
	var firstErr error

	inputFP, err := ComputeInputFingerprint(req)
	if err != nil {
		inputFP = "nil"
		if firstErr == nil {
			firstErr = fmt.Errorf("input fingerprint: %w", err)
		}
	}
	fp.Input = inputFP

	cfgFP, err := ComputeSimConfigFingerprint(req)
	if err != nil {
		cfgFP = "nil"
		if firstErr == nil {
			firstErr = fmt.Errorf("sim_config fingerprint: %w", err)
		}
	}
	fp.SimConfig = cfgFP

	outFP, err := ComputeOutputTraceFingerprint(resp)
	if err != nil {
		outFP = "nil"
		if firstErr == nil {
			firstErr = fmt.Errorf("output_trace fingerprint: %w", err)
		}
	}
	fp.OutputTrace = outFP

	return fp, firstErr
}

// FingerprintDiff describes which fingerprint(s) differ between two runs.
type FingerprintDiff struct {
	// InputDiffers is true when the Input fingerprints differ.
	InputDiffers bool
	// SimConfigDiffers is true when the SimConfig fingerprints differ.
	SimConfigDiffers bool
	// OutputTraceDiffers is true when the OutputTrace fingerprints differ.
	OutputTraceDiffers bool
	// HasDifference is true when any fingerprint differs.
	HasDifference bool
	// Explanation is a human-readable summary of which fingerprint(s) differ.
	Explanation string
}

// CompareFingerprints returns a FingerprintDiff describing how two sets of
// fingerprints differ.  When the sets are identical HasDifference is false and
// Explanation says so.
func CompareFingerprints(a, b ReplayFingerprints) FingerprintDiff {
	diff := FingerprintDiff{
		InputDiffers:       a.Input != b.Input,
		SimConfigDiffers:   a.SimConfig != b.SimConfig,
		OutputTraceDiffers: a.OutputTrace != b.OutputTrace,
	}
	diff.HasDifference = diff.InputDiffers || diff.SimConfigDiffers || diff.OutputTraceDiffers

	if !diff.HasDifference {
		diff.Explanation = "all fingerprints match — runs are equivalent"
		return diff
	}

	var parts []string
	if diff.InputDiffers {
		parts = append(parts, fmt.Sprintf("input (%s vs %s)", shortHash(a.Input), shortHash(b.Input)))
	}
	if diff.SimConfigDiffers {
		parts = append(parts, fmt.Sprintf("sim_config (%s vs %s)", shortHash(a.SimConfig), shortHash(b.SimConfig)))
	}
	if diff.OutputTraceDiffers {
		parts = append(parts, fmt.Sprintf("output_trace (%s vs %s)", shortHash(a.OutputTrace), shortHash(b.OutputTrace)))
	}

	diff.Explanation = fmt.Sprintf("fingerprint(s) differ: %s", joinStrings(parts, ", "))
	return diff
}

// ── internal helpers ──────────────────────────────────────────────────────────

// hashCanonical marshals v to canonical JSON and returns the SHA-256 hex digest.
func hashCanonical(v interface{}) (string, error) {
	data, err := marshalCanonicalReplay(v)
	if err != nil {
		return "", fmt.Errorf("fingerprint marshal: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// marshalCanonicalReplay produces deterministic JSON with sorted object keys.
// It is a self-contained copy of the canonical JSON logic used elsewhere in
// the codebase so the replay package has no dependency on internal/cmd.
func marshalCanonicalReplay(v interface{}) ([]byte, error) {
	// Convert to generic interface{} via standard JSON first.
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var intermediate interface{}
	if err := json.Unmarshal(raw, &intermediate); err != nil {
		return nil, err
	}
	return encodeCanonicalValue(intermediate)
}

func encodeCanonicalValue(v interface{}) ([]byte, error) {
	switch val := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		buf := make([]byte, 0, 64)
		buf = append(buf, '{')
		for i, k := range keys {
			if i > 0 {
				buf = append(buf, ',')
			}
			keyBytes, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			buf = append(buf, keyBytes...)
			buf = append(buf, ':')
			vBytes, err := encodeCanonicalValue(val[k])
			if err != nil {
				return nil, err
			}
			buf = append(buf, vBytes...)
		}
		buf = append(buf, '}')
		return buf, nil

	case []interface{}:
		buf := make([]byte, 0, 32)
		buf = append(buf, '[')
		for i, item := range val {
			if i > 0 {
				buf = append(buf, ',')
			}
			itemBytes, err := encodeCanonicalValue(item)
			if err != nil {
				return nil, err
			}
			buf = append(buf, itemBytes...)
		}
		buf = append(buf, ']')
		return buf, nil

	default:
		return json.Marshal(val)
	}
}

func shortHash(h string) string {
	if len(h) <= 12 {
		return h
	}
	return h[:8] + "…" + h[len(h)-4:]
}

func joinStrings(parts []string, sep string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}
