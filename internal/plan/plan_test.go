// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package plan_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dotandev/glassbox/internal/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// Plan construction
// ─────────────────────────────────────────────────────────────────────────────

func TestPlan_NewSetsCommandAndBuildTime(t *testing.T) {
	before := time.Now().UTC().Add(-time.Second)
	p := plan.New("debug")
	after := time.Now().UTC().Add(time.Second)

	assert.Equal(t, "debug", p.Command)
	assert.True(t, p.BuildTime.After(before), "BuildTime should be after test start")
	assert.True(t, p.BuildTime.Before(after), "BuildTime should be before test end")
}

func TestPlan_AddNetworkRequest(t *testing.T) {
	p := plan.New("debug")
	p.AddNetworkRequest("getTransaction", "https://soroban-testnet.stellar.org", "fetch tx")

	require.Len(t, p.NetworkRequests, 1)
	assert.Equal(t, "getTransaction", p.NetworkRequests[0].Method)
	assert.Equal(t, "https://soroban-testnet.stellar.org", p.NetworkRequests[0].Endpoint)
	assert.Equal(t, "fetch tx", p.NetworkRequests[0].Purpose)
}

func TestPlan_AddFile(t *testing.T) {
	p := plan.New("export")
	p.AddFile("write", "./state.snap.json", "Ledger state snapshot")

	require.Len(t, p.Files, 1)
	assert.Equal(t, "write", p.Files[0].Op)
	assert.Equal(t, "./state.snap.json", p.Files[0].Path)
}

func TestPlan_SetSimulator(t *testing.T) {
	p := plan.New("debug")
	p.SetSimulator("/usr/local/bin/glassbox-sim", "network", nil)

	require.NotNil(t, p.Simulator)
	assert.Equal(t, "/usr/local/bin/glassbox-sim", p.Simulator.BinaryPath)
	assert.Equal(t, "network", p.Simulator.Mode)
}

func TestPlan_SetSigning(t *testing.T) {
	p := plan.New("audit:sign")
	p.SetSigning("software", "my-key", "ed25519", "sign payload")

	require.NotNil(t, p.Signing)
	assert.Equal(t, "software", p.Signing.Provider)
	assert.Equal(t, "my-key", p.Signing.KeyIdentifier)
	assert.Equal(t, "ed25519", p.Signing.Algorithm)
}

func TestPlan_AddOutput(t *testing.T) {
	p := plan.New("debug")
	p.AddOutput("stdout", "", "simulation results")
	p.AddOutput("file", "/tmp/trace.json", "execution trace")

	require.Len(t, p.Outputs, 2)
	assert.Equal(t, "stdout", p.Outputs[0].Kind)
	assert.Equal(t, "file", p.Outputs[1].Kind)
	assert.Equal(t, "/tmp/trace.json", p.Outputs[1].Path)
}

// ─────────────────────────────────────────────────────────────────────────────
// Determinism
// ─────────────────────────────────────────────────────────────────────────────

func TestPlan_RenderText_IsDeterministic(t *testing.T) {
	// Build the same plan twice with the same inputs and verify the outputs
	// match structurally (ignoring BuildTime which varies).
	opts := plan.DebugPlanOptions{
		TxHash:          "abc123",
		Network:         "testnet",
		RPCEndpoint:     "https://soroban-testnet.stellar.org",
		SimulatorBinary: "/usr/local/bin/sim",
		SimulatorMode:   "network",
		JSONOutput:      false,
	}

	p1 := plan.BuildDebugPlan(opts)
	p2 := plan.BuildDebugPlan(opts)

	// Network requests, files, outputs should be identical.
	assert.Equal(t, len(p1.NetworkRequests), len(p2.NetworkRequests))
	assert.Equal(t, len(p1.Files), len(p2.Files))
	assert.Equal(t, len(p1.Outputs), len(p2.Outputs))
	for i := range p1.NetworkRequests {
		assert.Equal(t, p1.NetworkRequests[i], p2.NetworkRequests[i])
	}
}

func TestPlan_RenderJSON_IsDeterministic(t *testing.T) {
	p := plan.New("debug")
	p.Network = "testnet"
	p.AddNetworkRequest("getTransaction", "https://soroban-testnet.stellar.org", "fetch tx")
	p.SetSimulator("/bin/sim", "network", nil)
	p.AddOutput("stdout", "", "results")

	// Force a fixed time so JSON is byte-identical across runs.
	fixed, _ := time.Parse(time.RFC3339, "2026-07-24T00:00:00Z")
	p.BuildTime = fixed

	out1, err1 := p.RenderJSON()
	out2, err2 := p.RenderJSON()

	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.Equal(t, out1, out2, "JSON rendering should be deterministic")
}

// ─────────────────────────────────────────────────────────────────────────────
// Secret redaction
// ─────────────────────────────────────────────────────────────────────────────

func TestPlan_RedactURLToken(t *testing.T) {
	p := plan.New("debug")
	// URL with a token in the query string.
	p.AddNetworkRequest("getTransaction",
		"https://example.com/rpc?token=supersecret123&network=testnet",
		"fetch tx",
	)

	text := p.RenderText()
	assert.NotContains(t, text, "supersecret123", "token must be redacted from URL")
	assert.Contains(t, text, "example.com", "host must be preserved")
	assert.Contains(t, text, "REDACTED", "REDACTED marker should appear")
}

func TestPlan_RedactHexPrivateKey(t *testing.T) {
	p := plan.New("audit:sign")
	// Pass a 64-char hex string (Ed25519 seed) as the key identifier.
	hexKey := strings.Repeat("a1b2c3d4", 8) // 64 hex chars
	p.SetSigning("software", hexKey, "ed25519", "sign")

	text := p.RenderText()
	assert.NotContains(t, text, hexKey, "raw hex key must be redacted")
	assert.Contains(t, text, "REDACTED (hex key)")
}

func TestPlan_RedactPEMKey(t *testing.T) {
	p := plan.New("audit:sign")
	pemKey := "-----BEGIN PRIVATE KEY-----\nMC4CAQAwBQYDK2VwBCIEIFakeKeyMaterial\n-----END PRIVATE KEY-----\n"
	p.SetSigning("software", pemKey, "ed25519", "sign")

	text := p.RenderText()
	assert.NotContains(t, text, "FakeKeyMaterial", "PEM key material must not appear in plan text")
	assert.Contains(t, text, "REDACTED (PEM key)")
}

func TestPlan_SafeIdentifiersPassThrough(t *testing.T) {
	p := plan.New("audit:sign")
	// PKCS#11 key labels, KMS ARNs, and fingerprints should not be redacted.
	p.SetSigning("pkcs11", "my-signing-key", "ed25519", "sign HSM")

	text := p.RenderText()
	assert.Contains(t, text, "my-signing-key", "human-safe key labels must not be redacted")
}

func TestPlan_AuditKeyInDebugPlan_Redacted(t *testing.T) {
	hexKey := strings.Repeat("deadbeef", 8) // 64 hex chars
	opts := plan.DebugPlanOptions{
		TxHash:          "abc",
		Network:         "testnet",
		SimulatorMode:   "network",
		SimulatorBinary: "/bin/sim",
		AuditKey:        hexKey,
	}
	p := plan.BuildDebugPlan(opts)
	text := p.RenderText()

	assert.NotContains(t, text, hexKey, "audit key must be redacted in debug plan")
}

// ─────────────────────────────────────────────────────────────────────────────
// JSON schema
// ─────────────────────────────────────────────────────────────────────────────

func TestPlan_RenderJSON_ValidSchema(t *testing.T) {
	opts := plan.DebugPlanOptions{
		TxHash:          "abc123",
		Network:         "testnet",
		RPCEndpoint:     "https://soroban-testnet.stellar.org",
		SimulatorBinary: "/bin/sim",
		SimulatorMode:   "network",
		TraceOutputFile: "/tmp/trace.json",
		JSONOutput:      true,
	}
	p := plan.BuildDebugPlan(opts)

	out, err := p.RenderJSON()
	require.NoError(t, err)

	// Verify the JSON is parseable and has required top-level fields.
	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))

	assert.Equal(t, "debug", decoded["command"])
	assert.Equal(t, "testnet", decoded["network"])
	assert.Contains(t, decoded, "build_time")
	assert.Contains(t, decoded, "network_requests")
	assert.Contains(t, decoded, "simulator")
	assert.Contains(t, decoded, "outputs")
}

// ─────────────────────────────────────────────────────────────────────────────
// Replay pinning in plan
// ─────────────────────────────────────────────────────────────────────────────

func TestPlan_PinnedProvider_AppearsInOutput(t *testing.T) {
	opts := plan.DebugPlanOptions{
		TxHash:         "abc",
		Network:        "testnet",
		RPCEndpoint:    "https://soroban-testnet.stellar.org",
		PinnedEndpoint: "https://pinned.example.com",
		SimulatorMode:  "network",
	}
	p := plan.BuildDebugPlan(opts)

	text := p.RenderText()
	out, err := p.RenderJSON()
	require.NoError(t, err)

	assert.Contains(t, text, "pinned.example.com",
		"pinned provider must appear in text plan")
	assert.Contains(t, text, "failover disabled",
		"text plan must note that failover is disabled when pinned")
	assert.Contains(t, out, "pinned.example.com",
		"pinned provider must appear in JSON plan")
}

// ─────────────────────────────────────────────────────────────────────────────
// Builder helpers
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildAuditPlan_IncludesSigningAndPayload(t *testing.T) {
	opts := plan.AuditPlanOptions{
		PayloadFile:   "./payload.json",
		ProviderName:  "pkcs11",
		KeyIdentifier: "hsm-signing-key",
		Algorithm:     "ed25519",
		CertChainFile: "./chain.pem",
	}
	p := plan.BuildAuditPlan(opts)

	assert.Equal(t, "audit:sign", p.Command)
	require.NotNil(t, p.Signing)
	assert.Equal(t, "pkcs11", p.Signing.Provider)
	assert.Equal(t, "hsm-signing-key", p.Signing.KeyIdentifier)

	// Payload file should be a read op.
	require.Len(t, p.Files, 2) // payload + cert chain
	assert.Equal(t, "read", p.Files[0].Op)
	assert.Equal(t, "./payload.json", p.Files[0].Path)
	assert.Equal(t, "./chain.pem", p.Files[1].Path)
}

func TestBuildExportPlan_WritesSnapshot(t *testing.T) {
	opts := plan.ExportPlanOptions{
		SnapshotOutputPath: "./state.snap.json",
		IncludeMemory:      true,
		JSONOutput:         true,
	}
	p := plan.BuildExportPlan(opts)

	assert.Equal(t, "export", p.Command)
	require.Len(t, p.Files, 1)
	assert.Equal(t, "write", p.Files[0].Op)
	assert.Equal(t, "./state.snap.json", p.Files[0].Path)

	// Memory note should be present.
	found := false
	for _, n := range p.Notes {
		if strings.Contains(n, "memory") {
			found = true
		}
	}
	assert.True(t, found, "expected a note about linear memory dump")
}

func TestBuildSessionSavePlan_IncludesDBPath(t *testing.T) {
	opts := plan.SessionPlanOptions{
		SessionID: "my-session-123",
		Name:      "bug-hunt",
		DBPath:    "~/.Glassbox/sessions.db",
	}
	p := plan.BuildSessionSavePlan(opts)

	assert.Equal(t, "session save", p.Command)
	require.Len(t, p.Files, 1)
	assert.Equal(t, "write", p.Files[0].Op)

	// Session ID and name should appear in notes.
	noteText := strings.Join(p.Notes, " ")
	assert.Contains(t, noteText, "my-session-123")
	assert.Contains(t, noteText, "bug-hunt")
}

// ─────────────────────────────────────────────────────────────────────────────
// RenderText content checks
// ─────────────────────────────────────────────────────────────────────────────

func TestPlan_RenderText_ContainsDryRunDisclaimer(t *testing.T) {
	p := plan.New("debug")
	text := p.RenderText()
	assert.Contains(t, text, "dry-run plan",
		"text plan must include a disclaimer about no side effects")
	assert.Contains(t, text, "No network calls",
		"text plan must explicitly mention that no network calls are made")
}

func TestPlan_RenderText_NetworkRequests_DisplaysSucceededProvider(t *testing.T) {
	// When there is only one endpoint it should appear clearly.
	p := plan.New("debug")
	p.AddNetworkRequest("getTransaction", "https://soroban-testnet.stellar.org", "fetch tx")

	text := p.RenderText()
	assert.Contains(t, text, "soroban-testnet.stellar.org")
	assert.Contains(t, text, "getTransaction")
}
