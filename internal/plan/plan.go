// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

// Package plan defines the ExecutionPlan model: a structured, deterministic
// description of everything a Glassbox command will do before it actually does
// it. Plans are built after validation but before any side effects (network,
// signing, process, file mutation), making --plan / --dry-run safe to run in
// any environment.
//
// Design goals:
//   - Deterministic: the same configuration always produces the same plan.
//   - Side-effect free: building a plan touches only resolved configuration,
//     not live network endpoints or the filesystem.
//   - Secret-safe: all secret values (tokens, private keys, PINs) are
//     redacted in both text and JSON renderings.
//   - Inspectable: the plan shows which signing provider, output paths, and
//     RPC endpoints the actual execution will use.
package plan

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// NetworkRequest describes one outbound HTTP/RPC call the command will make.
type NetworkRequest struct {
	// Method is the JSON-RPC or HTTP method name (e.g. "getTransaction",
	// "getLedgerEntries", "simulateTransaction").
	Method string `json:"method"`
	// Endpoint is the base URL (token/key redacted).
	Endpoint string `json:"endpoint"`
	// Purpose is a short human-readable description of why this call is made.
	Purpose string `json:"purpose"`
}

// FileOperation describes a file that will be read from or written to.
type FileOperation struct {
	// Path is the absolute or relative filesystem path.
	Path string `json:"path"`
	// Op is the operation type: "read", "write", or "append".
	Op string `json:"op"`
	// Description provides context about what the file contains.
	Description string `json:"description"`
}

// SimulatorInvocation describes how the Rust simulator will be invoked.
type SimulatorInvocation struct {
	// BinaryPath is the resolved path to the simulator binary.
	BinaryPath string `json:"binary_path"`
	// Mode is "local" for WASM replay, "network" for tx simulation.
	Mode string `json:"mode"`
	// Arguments lists the logical arguments passed (e.g. wasm path, mock args).
	Arguments []string `json:"arguments,omitempty"`
}

// SigningOperation describes a signing action the command will perform.
type SigningOperation struct {
	// Provider is the signing provider name (e.g. "software", "pkcs11").
	Provider string `json:"provider"`
	// KeyIdentifier is a human-safe identifier for the key (fingerprint,
	// label, or KMS ARN) — never the raw private key material.
	KeyIdentifier string `json:"key_identifier,omitempty"`
	// Algorithm is the signing algorithm (e.g. "ed25519").
	Algorithm string `json:"algorithm,omitempty"`
	// Purpose describes what artifact is being signed.
	Purpose string `json:"purpose"`
}

// OutputDestination describes where a command will write its results.
type OutputDestination struct {
	// Kind is the destination type: "file", "stdout", "ipfs", "arweave".
	Kind string `json:"kind"`
	// Path is the filesystem path (Kind == "file") or the gateway URL.
	Path string `json:"path,omitempty"`
	// Description provides context about the output format/content.
	Description string `json:"description"`
}

// ExecutionPlan is the complete plan model for a Glassbox command execution.
// It is rendered before any side effects occur when --plan / --dry-run is set.
type ExecutionPlan struct {
	// Command is the CLI command being planned (e.g. "debug", "audit:sign").
	Command string `json:"command"`
	// Network is the Stellar network name (testnet, mainnet, futurenet).
	Network string `json:"network,omitempty"`
	// BuildTime is when this plan was constructed (ISO-8601 UTC).
	BuildTime time.Time `json:"build_time"`
	// NetworkRequests lists all outbound RPC/HTTP calls in execution order.
	NetworkRequests []NetworkRequest `json:"network_requests,omitempty"`
	// Files lists all file read/write operations in execution order.
	Files []FileOperation `json:"files,omitempty"`
	// Simulator describes the simulator invocation (nil if not used).
	Simulator *SimulatorInvocation `json:"simulator,omitempty"`
	// Signing describes any signing operations (nil if none).
	Signing *SigningOperation `json:"signing,omitempty"`
	// Outputs lists the final output destinations.
	Outputs []OutputDestination `json:"outputs"`
	// PinnedProvider is non-empty when an RPC endpoint is pinned for replay.
	PinnedProvider string `json:"pinned_provider,omitempty"`
	// Notes contains human-readable warnings or informational messages about
	// the plan (e.g. "cache disabled by --no-cache").
	Notes []string `json:"notes,omitempty"`
}

// New creates an empty ExecutionPlan for the given command.
func New(command string) *ExecutionPlan {
	return &ExecutionPlan{
		Command:   command,
		BuildTime: time.Now().UTC(),
	}
}

// AddNetworkRequest appends a network request to the plan.
func (p *ExecutionPlan) AddNetworkRequest(method, endpoint, purpose string) {
	p.NetworkRequests = append(p.NetworkRequests, NetworkRequest{
		Method:   method,
		Endpoint: redactURL(endpoint),
		Purpose:  purpose,
	})
}

// AddFile appends a file operation to the plan.
func (p *ExecutionPlan) AddFile(op, path, description string) {
	p.Files = append(p.Files, FileOperation{
		Op:          op,
		Path:        path,
		Description: description,
	})
}

// SetSimulator sets the simulator invocation details.
func (p *ExecutionPlan) SetSimulator(binaryPath, mode string, args []string) {
	p.Simulator = &SimulatorInvocation{
		BinaryPath: binaryPath,
		Mode:       mode,
		Arguments:  args,
	}
}

// SetSigning sets the signing operation details, redacting any key material.
func (p *ExecutionPlan) SetSigning(provider, keyIdentifier, algorithm, purpose string) {
	p.Signing = &SigningOperation{
		Provider:      provider,
		KeyIdentifier: redactKeyIdentifier(keyIdentifier),
		Algorithm:     algorithm,
		Purpose:       purpose,
	}
}

// AddOutput appends an output destination to the plan.
func (p *ExecutionPlan) AddOutput(kind, path, description string) {
	p.Outputs = append(p.Outputs, OutputDestination{
		Kind:        kind,
		Path:        path,
		Description: description,
	})
}

// AddNote appends a human-readable note to the plan.
func (p *ExecutionPlan) AddNote(format string, args ...interface{}) {
	if len(args) == 0 {
		p.Notes = append(p.Notes, format)
	} else {
		p.Notes = append(p.Notes, fmt.Sprintf(format, args...))
	}
}

// RenderText returns a deterministic, human-readable text rendering of the plan.
// Secrets are redacted. The output is stable across runs with identical inputs.
func (p *ExecutionPlan) RenderText() string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Execution Plan — %s\n", p.Command)
	fmt.Fprintf(&sb, "Built at: %s\n", p.BuildTime.Format(time.RFC3339))
	if p.Network != "" {
		fmt.Fprintf(&sb, "Network:  %s\n", p.Network)
	}
	if p.PinnedProvider != "" {
		fmt.Fprintf(&sb, "Pinned provider: %s (replay mode — failover disabled)\n", p.PinnedProvider)
	}
	sb.WriteString("\n")

	if len(p.NetworkRequests) > 0 {
		fmt.Fprintf(&sb, "Network requests (%d):\n", len(p.NetworkRequests))
		for i, r := range p.NetworkRequests {
			fmt.Fprintf(&sb, "  [%d] %-28s %s\n", i+1, r.Method, r.Endpoint)
			fmt.Fprintf(&sb, "      %s\n", r.Purpose)
		}
		sb.WriteString("\n")
	}

	if len(p.Files) > 0 {
		fmt.Fprintf(&sb, "File operations (%d):\n", len(p.Files))
		for _, f := range p.Files {
			fmt.Fprintf(&sb, "  %-6s %s — %s\n", strings.ToUpper(f.Op), f.Path, f.Description)
		}
		sb.WriteString("\n")
	}

	if p.Simulator != nil {
		fmt.Fprintf(&sb, "Simulator:\n")
		fmt.Fprintf(&sb, "  Binary: %s\n", p.Simulator.BinaryPath)
		fmt.Fprintf(&sb, "  Mode:   %s\n", p.Simulator.Mode)
		if len(p.Simulator.Arguments) > 0 {
			fmt.Fprintf(&sb, "  Args:   %s\n", strings.Join(p.Simulator.Arguments, " "))
		}
		sb.WriteString("\n")
	}

	if p.Signing != nil {
		fmt.Fprintf(&sb, "Signing:\n")
		fmt.Fprintf(&sb, "  Provider: %s\n", p.Signing.Provider)
		if p.Signing.KeyIdentifier != "" {
			fmt.Fprintf(&sb, "  Key:      %s\n", p.Signing.KeyIdentifier)
		}
		if p.Signing.Algorithm != "" {
			fmt.Fprintf(&sb, "  Algo:     %s\n", p.Signing.Algorithm)
		}
		fmt.Fprintf(&sb, "  Purpose:  %s\n", p.Signing.Purpose)
		sb.WriteString("\n")
	}

	if len(p.Outputs) > 0 {
		fmt.Fprintf(&sb, "Outputs (%d):\n", len(p.Outputs))
		for _, o := range p.Outputs {
			if o.Path != "" {
				fmt.Fprintf(&sb, "  %-8s %s — %s\n", o.Kind, o.Path, o.Description)
			} else {
				fmt.Fprintf(&sb, "  %-8s %s\n", o.Kind, o.Description)
			}
		}
		sb.WriteString("\n")
	}

	if len(p.Notes) > 0 {
		fmt.Fprintf(&sb, "Notes:\n")
		for _, n := range p.Notes {
			fmt.Fprintf(&sb, "  ! %s\n", n)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("This is a dry-run plan. No network calls, signing, simulator invocations,\n")
	sb.WriteString("or file writes will be performed.\n")
	return sb.String()
}

// RenderJSON returns a deterministic JSON rendering of the plan.
// Secrets are redacted. The JSON is stable (sorted keys via encoding/json).
func (p *ExecutionPlan) RenderJSON() (string, error) {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
