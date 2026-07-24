// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package plan

// DebugPlanOptions holds the resolved configuration needed to build an
// execution plan for the `debug` command. All values come from resolved
// flags / config — never from live network calls.
type DebugPlanOptions struct {
	// TxHash is the transaction hash being debugged (empty in WASM mode).
	TxHash string
	// Network is the Stellar network name.
	Network string
	// RPCEndpoint is the primary Soroban RPC URL (token already resolved).
	RPCEndpoint string
	// AdditionalEndpoints holds any failover URLs configured.
	AdditionalEndpoints []string
	// PinnedEndpoint is set when --pin-endpoint was passed.
	PinnedEndpoint string
	// SimulatorBinary is the resolved path to the simulator binary.
	SimulatorBinary string
	// SimulatorMode is "local" (WASM) or "network" (tx simulation).
	SimulatorMode string
	// WasmPath is set in WASM replay mode.
	WasmPath string
	// SnapshotPath is set when --snapshot is provided.
	SnapshotPath string
	// SaveSnapshotsPath is set when --save-snapshots is provided.
	SaveSnapshotsPath string
	// LoadSnapshotsPath is set when --load-snapshots is provided.
	LoadSnapshotsPath string
	// TraceOutputFile is set when --trace-output is provided.
	TraceOutputFile string
	// AuditKey is set when --audit-key is passed (raw value; will be redacted).
	AuditKey string
	// PublishIPFS is true when --publish-ipfs is set.
	PublishIPFS bool
	// IPFSNode is the IPFS node URL.
	IPFSNode string
	// PublishArweave is true when --publish-arweave is set.
	PublishArweave bool
	// ArweaveGateway is the Arweave gateway URL.
	ArweaveGateway string
	// ExportSVG is the path for SVG call graph export.
	ExportSVG string
	// CacheDisabled is true when --no-cache was set.
	CacheDisabled bool
	// JSONOutput is true when --json or --format=json was set.
	JSONOutput bool
}

// BuildDebugPlan constructs an ExecutionPlan for the `debug` command.
func BuildDebugPlan(opts DebugPlanOptions) *ExecutionPlan {
	p := New("debug")
	p.Network = opts.Network

	if opts.PinnedEndpoint != "" {
		p.PinnedProvider = opts.PinnedEndpoint
	}

	// Network requests.
	if opts.SimulatorMode == "network" {
		endpoint := opts.RPCEndpoint
		if endpoint == "" {
			endpoint = "(default " + opts.Network + " endpoint)"
		}
		p.AddNetworkRequest("getTransaction", endpoint,
			"Fetch transaction envelope and result metadata")
		if opts.SnapshotPath == "" && opts.LoadSnapshotsPath == "" {
			p.AddNetworkRequest("getLedgerEntries", endpoint,
				"Fetch ledger state entries for simulation")
		}
	}

	// Additional failover endpoints.
	for _, ep := range opts.AdditionalEndpoints {
		p.AddNote("Failover endpoint configured: %s", ep)
	}

	// File operations.
	if opts.SnapshotPath != "" {
		p.AddFile("read", opts.SnapshotPath, "Ledger state snapshot for replay")
	}
	if opts.LoadSnapshotsPath != "" {
		p.AddFile("read", opts.LoadSnapshotsPath, "Snapshot registry for time-travel replay")
	}
	if opts.WasmPath != "" {
		p.AddFile("read", opts.WasmPath, "Wasm binary for local replay")
	}
	if opts.SaveSnapshotsPath != "" {
		p.AddFile("write", opts.SaveSnapshotsPath, "Output snapshot registry")
	}
	if opts.ExportSVG != "" {
		p.AddFile("write", opts.ExportSVG, "Call graph SVG export")
	}

	// Simulator.
	simMode := opts.SimulatorMode
	if simMode == "" {
		simMode = "network"
	}
	var simArgs []string
	if opts.WasmPath != "" {
		simArgs = append(simArgs, "--wasm", opts.WasmPath)
	}
	binary := opts.SimulatorBinary
	if binary == "" {
		binary = "(auto-detected)"
	}
	p.SetSimulator(binary, simMode, simArgs)

	// Signing (audit trail).
	if opts.AuditKey != "" {
		p.SetSigning("software", opts.AuditKey, "ed25519", "Sign audit trail before publishing")
	}

	// Outputs.
	if opts.JSONOutput {
		p.AddOutput("stdout", "", "JSON simulation results")
	} else {
		p.AddOutput("stdout", "", "Text simulation results")
	}
	if opts.TraceOutputFile != "" {
		p.AddFile("write", opts.TraceOutputFile, "Execution trace export")
		p.AddOutput("file", opts.TraceOutputFile, "Execution trace (JSON)")
	}
	if opts.PublishIPFS {
		node := opts.IPFSNode
		if node == "" {
			node = "(public gateway)"
		}
		p.AddOutput("ipfs", node, "Signed audit trail published to IPFS")
	}
	if opts.PublishArweave {
		gw := opts.ArweaveGateway
		if gw == "" {
			gw = "(default gateway)"
		}
		p.AddOutput("arweave", gw, "Signed audit trail published to Arweave")
	}

	if opts.CacheDisabled {
		p.AddNote("Ledger entry cache disabled (--no-cache)")
	}

	return p
}

// AuditPlanOptions holds the resolved configuration for the `audit:sign` command.
type AuditPlanOptions struct {
	// PayloadFile is the path to the payload file, or "" if --payload was used.
	PayloadFile string
	// ProviderName is the resolved signing provider (e.g. "software", "pkcs11").
	ProviderName string
	// KeyIdentifier is a safe identifier for the key (label, ARN, fingerprint).
	KeyIdentifier string
	// Algorithm is the signing algorithm.
	Algorithm string
	// CertChainFile is the path to the certificate chain PEM, if set.
	CertChainFile string
}

// BuildAuditPlan constructs an ExecutionPlan for the `audit:sign` command.
func BuildAuditPlan(opts AuditPlanOptions) *ExecutionPlan {
	p := New("audit:sign")

	if opts.PayloadFile != "" {
		p.AddFile("read", opts.PayloadFile, "JSON payload to be signed")
	}

	p.SetSigning(opts.ProviderName, opts.KeyIdentifier, opts.Algorithm,
		"Sign payload and produce a signed audit log")

	if opts.CertChainFile != "" {
		p.AddFile("read", opts.CertChainFile, "PEM certificate chain for provenance")
	}

	p.AddOutput("stdout", "", "Signed audit log JSON")
	return p
}

// ExportPlanOptions holds the resolved configuration for the `export` command.
type ExportPlanOptions struct {
	// SnapshotOutputPath is the file that will be written.
	SnapshotOutputPath string
	// IncludeMemory is true when --include-memory was set.
	IncludeMemory bool
	// JSONOutput is true when --format=json was set.
	JSONOutput bool
}

// BuildExportPlan constructs an ExecutionPlan for the `export` command.
func BuildExportPlan(opts ExportPlanOptions) *ExecutionPlan {
	p := New("export")

	if opts.SnapshotOutputPath != "" {
		p.AddFile("write", opts.SnapshotOutputPath, "Ledger state snapshot JSON")
		p.AddOutput("file", opts.SnapshotOutputPath, "Snapshot (JSON)")
	}
	if opts.IncludeMemory {
		p.AddNote("Linear memory dump will be included in the snapshot")
	}
	if opts.JSONOutput {
		p.AddOutput("stdout", "", "Export summary (JSON)")
	} else {
		p.AddOutput("stdout", "", "Export summary (text)")
	}
	return p
}

// SessionPlanOptions holds the resolved configuration for the `session save` command.
type SessionPlanOptions struct {
	// SessionID is the session ID (or "(auto-generated)").
	SessionID string
	// Name is the optional session name.
	Name string
	// DBPath is the path to the SQLite session store.
	DBPath string
}

// BuildSessionSavePlan constructs an ExecutionPlan for the `session save` command.
func BuildSessionSavePlan(opts SessionPlanOptions) *ExecutionPlan {
	p := New("session save")

	if opts.DBPath != "" {
		p.AddFile("write", opts.DBPath, "Session store (SQLite)")
	}

	sessionID := opts.SessionID
	if sessionID == "" {
		sessionID = "(auto-generated)"
	}
	p.AddNote("Session ID: %s", sessionID)
	if opts.Name != "" {
		p.AddNote("Session name: %s", opts.Name)
	}
	p.AddOutput("stdout", "", "Confirmation with session ID")
	return p
}
