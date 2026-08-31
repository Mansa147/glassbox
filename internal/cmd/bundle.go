// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/dotandev/glassbox/internal/bundle"
	"github.com/dotandev/glassbox/internal/config"
	"github.com/dotandev/glassbox/internal/errors"
	"github.com/dotandev/glassbox/internal/rpc"
	"github.com/dotandev/glassbox/internal/simulator"
	"github.com/dotandev/glassbox/internal/version"
	"github.com/spf13/cobra"
)

// networkPassphraseFor returns the network passphrase for the given built-in
// network name.  Falls back to an empty string for unknown networks (callers
// should validate the network name before calling this).
func networkPassphraseFor(network string) string {
	switch rpc.Network(strings.ToLower(network)) {
	case rpc.Testnet:
		return rpc.TestnetConfig.NetworkPassphrase
	case rpc.Mainnet:
		return rpc.MainnetConfig.NetworkPassphrase
	case rpc.Futurenet:
		return rpc.FuturenetConfig.NetworkPassphrase
	default:
		// Custom network — try to load from config
		if cfg, err := config.GetCustomNetwork(network); err == nil && cfg != nil {
			return cfg.NetworkPassphrase
		}
		return ""
	}
}

// ── bundle command group ──────────────────────────────────────────────────────

var debugBundleCmd = &cobra.Command{
	Use:     "bundle",
	Short:   "Create and verify debug bundles for offline replay",
	Long: `Manage portable debug bundles for offline Soroban transaction replay.

A bundle packages everything needed to replay a transaction without network
access: the transaction envelope, result metadata, ledger state snapshot,
network identity, protocol version, and provenance metadata.

Bundles are JSON files with per-member checksums for integrity verification.
They explicitly do not contain provider credentials (RPC tokens, API keys).`,
}

// ── bundle create ─────────────────────────────────────────────────────────────

var (
	bundleCreateNetwork string
	bundleCreateRPCURL  string
	bundleCreateRPCToken string
	bundleCreateOutput  string
	bundleCreateSimPath string
	bundleCreateJSON    bool
)

var debugBundleCreateCmd = &cobra.Command{
	Use:     "create <transaction-hash>",
	Short:   "Create a debug bundle from a live transaction",
	GroupID: "utility",
	Long: `Fetch a transaction and its ledger state from the network, then package
everything into a portable bundle file for offline replay.

The bundle contains:
  - Transaction envelope XDR
  - Transaction result metadata XDR
  - Full ledger state snapshot (all referenced entries)
  - Network identity (name + passphrase)
  - Protocol version
  - Provenance metadata (tx hash, ledger sequence, fetch time)

Bundles do NOT contain RPC tokens, API keys, or private keys.

Examples:
  glassbox debug bundle create <tx-hash>
  glassbox debug bundle create <tx-hash> --network testnet --output my-bundle.json
  glassbox debug bundle create <tx-hash> --rpc https://soroban-testnet.stellar.org`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if err := rpc.ValidateTransactionHash(args[0]); err != nil {
			return errors.WrapValidationError(fmt.Sprintf("invalid transaction hash: %v", err))
		}
		switch rpc.Network(bundleCreateNetwork) {
		case rpc.Testnet, rpc.Mainnet, rpc.Futurenet:
		default:
			return errors.WrapInvalidNetwork(bundleCreateNetwork)
		}
		return nil
	},
	RunE: runDebugBundleCreate,
}

func init() {
	debugBundleCreateCmd.Flags().StringVarP(&bundleCreateNetwork, "network", "n", string(rpc.Testnet),
		"Stellar network (testnet, mainnet, futurenet)")
	debugBundleCreateCmd.Flags().StringVar(&bundleCreateRPCURL, "rpc-url", "",
		"Custom Soroban RPC URL")
	debugBundleCreateCmd.Flags().StringVar(&bundleCreateRPCToken, "rpc-token", "",
		"RPC authentication token (or GLASSBOX_RPC_TOKEN env var)")
	debugBundleCreateCmd.Flags().StringVarP(&bundleCreateOutput, "output", "o", "",
		"Output path for the bundle file (default: glassbox-bundle-<tx-hash>.json)")
	debugBundleCreateCmd.Flags().StringVar(&bundleCreateSimPath, "sim-path", "",
		"Path to glassbox-sim binary (overrides auto-discovery)")
	debugBundleCreateCmd.Flags().BoolVar(&bundleCreateJSON, "json", false,
		"Output bundle metadata as JSON instead of human-readable text")

	_ = debugBundleCreateCmd.RegisterFlagCompletionFunc("network", completeNetworkFlag)
	debugBundleCmd.AddCommand(debugBundleCreateCmd)
}

func runDebugBundleCreate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	txHash := args[0]
	out := cmd.OutOrStdout()

	if !bundleCreateJSON {
		fmt.Fprintf(out, "Creating debug bundle for transaction %s\n", txHash)
		fmt.Fprintf(out, "Network: %s\n\n", bundleCreateNetwork)
	}

	// ── Build RPC client ──────────────────────────────────────────────────────
	token := bundleCreateRPCToken
	if token == "" {
		token = os.Getenv("GLASSBOX_RPC_TOKEN")
	}
	if token == "" {
		if cfg, err := config.Load(); err == nil && cfg.RPCToken != "" {
			token = cfg.RPCToken
		}
	}

	opts, err := networkClientOptions(bundleCreateNetwork)
	if err != nil {
		return errors.WrapValidationError(fmt.Sprintf("failed to build client options: %v", err))
	}
	opts = append(opts, rpc.WithToken(token))
	if bundleCreateRPCURL != "" {
		opts = append(opts, rpc.WithAltURLs(splitTrimmed(bundleCreateRPCURL)))
	} else {
		if cfg, err := config.Load(); err == nil {
			if len(cfg.RpcUrls) > 0 {
				opts = append(opts, rpc.WithAltURLs(cfg.RpcUrls))
			} else if cfg.RpcUrl != "" {
				opts = append(opts, rpc.WithHorizonURL(cfg.RpcUrl))
			}
		}
	}

	client, err := rpc.NewClient(opts...)
	if err != nil {
		return errors.WrapValidationError(fmt.Sprintf("failed to create RPC client: %v", err))
	}

	// ── Fetch transaction ────────────────────────────────────────────────────
	if !bundleCreateJSON {
		fmt.Fprintln(out, "Fetching transaction...")
	}
	txResp, err := client.GetTransaction(ctx, txHash)
	if err != nil {
		return errors.WrapRPCConnectionFailed(err)
	}

	// ── Fetch ledger header for sequence + protocol version ──────────────────
	// TransactionResponse does not carry the ledger sequence directly, so we
	// attempt a best-effort fetch via the latest ledger.  If this fails, the
	// bundle is still created with sequence 0 and a default protocol version.
	var ledgerSeq uint32
	var protoVersion uint32
	if latestSeq, seqErr := client.GetLatestLedgerSequence(ctx); seqErr == nil && latestSeq > 0 {
		ledgerHeader, ledgerErr := client.GetLedgerHeader(ctx, uint32(latestSeq))
		if ledgerErr != nil {
			if !bundleCreateJSON {
				fmt.Fprintf(out, "[WARN] Could not fetch ledger header: %v\n", ledgerErr)
			}
		} else {
			ledgerSeq = ledgerHeader.Sequence
			protoVersion = ledgerHeader.ProtocolVersion
		}
	} else if !bundleCreateJSON {
		fmt.Fprintf(out, "[WARN] Could not determine latest ledger sequence: %v\n", seqErr)
	}

	// ── Extract ledger state ─────────────────────────────────────────────────
	if !bundleCreateJSON {
		fmt.Fprintln(out, "Extracting ledger state...")
	}
	ledgerState, err := extractBundleLedgerState(ctx, client, txResp)
	if err != nil {
		return errors.WrapValidationError(fmt.Sprintf("failed to extract ledger state: %v", err))
	}

	// ── Resolve protocol version from simulator if not in ledger header ──────
	if protoVersion == 0 {
		protoVersion = simulator.LatestVersion()
	}

	// ── Build network identity ────────────────────────────────────────────────
	netIdentity := bundle.NetworkIdentity{
		Name:       bundleCreateNetwork,
		Passphrase: networkPassphraseFor(bundleCreateNetwork),
	}

	// ── Create bundle ─────────────────────────────────────────────────────────
	m := bundle.New(
		version.Version,
		txHash,
		ledgerSeq,
		protoVersion,
		netIdentity,
		txResp.EnvelopeXdr,
		txResp.ResultMetaXdr,
		ledgerState,
	)

	// ── Determine output path ─────────────────────────────────────────────────
	outputPath := bundleCreateOutput
	if outputPath == "" {
		outputPath = bundle.SuggestedFilename(txHash)
	}

	if err := m.SaveToFile(outputPath); err != nil {
		return errors.WrapValidationError(fmt.Sprintf("failed to save bundle: %v", err))
	}

	// ── Output ────────────────────────────────────────────────────────────────
	if bundleCreateJSON {
		result := map[string]interface{}{
			"path":             outputPath,
			"tx_hash":          txHash,
			"network":          bundleCreateNetwork,
			"ledger_sequence":  ledgerSeq,
			"protocol_version": protoVersion,
			"ledger_entries":   len(ledgerState),
			"checksums":        m.Checksums,
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "[OK] Bundle created: %s\n", outputPath)
	fmt.Fprintf(out, "  Transaction     : %s\n", txHash)
	fmt.Fprintf(out, "  Network         : %s\n", bundleCreateNetwork)
	fmt.Fprintf(out, "  Ledger sequence : %d\n", ledgerSeq)
	fmt.Fprintf(out, "  Protocol version: %d\n", protoVersion)
	fmt.Fprintf(out, "  Ledger entries  : %d\n", len(ledgerState))
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Use 'glassbox debug bundle verify %s' to confirm integrity.\n", outputPath)
	fmt.Fprintf(out, "Use 'glassbox debug --bundle %s' to replay offline.\n", outputPath)
	return nil
}

// ── bundle verify ─────────────────────────────────────────────────────────────

var (
	bundleVerifyJSON bool
)

var debugBundleVerifyCmd = &cobra.Command{
	Use:   "verify <bundle-file>",
	Short: "Verify the integrity of a debug bundle",
	Long: `Verify that a debug bundle has not been modified or corrupted since creation.

This command re-computes the SHA-256 checksum of each bundle member and
compares it to the stored value.  Each mismatched or missing member is
reported individually so you know exactly which field was affected.

Examples:
  glassbox debug bundle verify glassbox-bundle-abc123.json
  glassbox debug bundle verify my-bundle.json --json`,
	Args: cobra.ExactArgs(1),
	RunE: runDebugBundleVerify,
}

func init() {
	debugBundleVerifyCmd.Flags().BoolVar(&bundleVerifyJSON, "json", false,
		"Output verification result as JSON")

	debugBundleCmd.AddCommand(debugBundleVerifyCmd)
}

func runDebugBundleVerify(cmd *cobra.Command, args []string) error {
	path := args[0]
	out := cmd.OutOrStdout()

	m, err := bundle.LoadFromFile(path)
	if err != nil {
		// LoadFromFile already validates checksums; surface per-field errors.
		if bundle.IsChecksumMismatch(err) {
			if bundleVerifyJSON {
				result := map[string]interface{}{
					"path":  path,
					"valid": false,
					"error": err.Error(),
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			fmt.Fprintf(out, "[FAIL] Bundle verification failed: %s\n", path)
			fmt.Fprintln(out, err.Error())
			return errors.WrapValidationError("bundle integrity check failed")
		}
		if bundle.IsValidationError(err) {
			return errors.WrapValidationError(err.Error())
		}
		return errors.WrapValidationError(fmt.Sprintf("failed to load bundle: %v", err))
	}

	report := m.Verify()

	if bundleVerifyJSON {
		result := map[string]interface{}{
			"path":         path,
			"valid":        report.OK,
			"tx_hash":      m.Provenance.TxHash,
			"network":      m.Network.Name,
			"fetched_at":   m.Provenance.FetchedAt,
			"field_errors": report.FieldErrors,
			"missing":      report.MissingMembers,
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Fprintf(out, "Bundle Verification: %s\n", path)
	fmt.Fprintln(out, strings.Repeat("─", 60))
	fmt.Fprintf(out, "  Transaction : %s\n", m.Provenance.TxHash)
	fmt.Fprintf(out, "  Network     : %s\n", m.Network.Name)
	fmt.Fprintf(out, "  Fetched at  : %s\n", m.Provenance.FetchedAt.Format("2006-01-02T15:04:05Z"))
	fmt.Fprintf(out, "  Protocol    : %d\n", m.Provenance.ProtocolVersion)
	fmt.Fprintf(out, "  Ledger seq  : %d\n", m.Provenance.LedgerSequence)
	fmt.Fprintln(out)

	for member, checksum := range m.Checksums {
		if _, failed := report.FieldErrors[member]; failed {
			fmt.Fprintf(out, "  [FAIL] %-20s %s\n", member, checksum)
		} else {
			fmt.Fprintf(out, "  [OK]   %-20s %s\n", member, checksum)
		}
	}

	if len(report.MissingMembers) > 0 {
		for _, m := range report.MissingMembers {
			fmt.Fprintf(out, "  [MISS] %-20s (no checksum stored)\n", m)
		}
	}

	fmt.Fprintln(out)
	if report.OK {
		fmt.Fprintln(out, "Result: VALID — bundle integrity confirmed.")
		return nil
	}

	for member, desc := range report.FieldErrors {
		fmt.Fprintf(out, "Error [%s]: %s\n", member, desc)
	}
	fmt.Fprintln(out, "Result: INVALID — bundle integrity check failed.")
	return errors.WrapValidationError("bundle integrity check failed")
}

// ── registration ──────────────────────────────────────────────────────────────

func init() {
	// Register the bundle sub-commands under the debug command.
	// debugCmd is defined in debug.go.
	// We use a lazy registration approach via an init function so that the
	// debugCmd variable is available when this file is processed.
	registerBundleSubCommands()
}

// registerBundleSubCommands wires the bundle command group under the debug
// command.  Calling it from init() ensures deterministic registration order.
func registerBundleSubCommands() {
	// Guard against double-registration in case init() is called twice in tests.
	for _, sub := range debugCmd.Commands() {
		if sub.Use == "bundle" {
			return
		}
	}
	debugCmd.AddCommand(debugBundleCmd)
}

// ── private helpers ───────────────────────────────────────────────────────────

// extractBundleLedgerState extracts ledger entries from result metadata,
// supplementing any missing entries from the RPC.
func extractBundleLedgerState(ctx context.Context, client *rpc.Client, txResp *rpc.TransactionResponse) (map[string]string, error) {
	if txResp == nil {
		return nil, fmt.Errorf("nil transaction response")
	}

	// Extract from result metadata first.
	state, extractErr := rpc.ExtractLedgerEntriesFromMeta(txResp.ResultMetaXdr)
	if extractErr != nil {
		// Fall back to a live fetch if extraction fails.
		keys, keysErr := extractLedgerKeys(txResp.ResultMetaXdr)
		if keysErr != nil {
			return nil, fmt.Errorf("cannot extract ledger keys: %w", keysErr)
		}
		var fetchErr error
		state, fetchErr = client.GetLedgerEntries(ctx, keys)
		if fetchErr != nil {
			return nil, fmt.Errorf("ledger entry fetch failed: %w", fetchErr)
		}
	}

	if len(state) == 0 {
		return nil, fmt.Errorf("no ledger entries found for this transaction; the result metadata may be empty")
	}

	return state, nil
}
