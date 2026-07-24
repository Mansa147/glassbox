// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dotandev/glassbox/internal/abi"
	"github.com/dotandev/glassbox/internal/bindings"
	"github.com/dotandev/glassbox/internal/clioutput"
	"github.com/spf13/cobra"
)

var (
	bindingsOutput          string
	bindingsContractID      string
	bindingsNetwork         string
	bindingsPackage         string
	bindingsDebugMeta       bool
	bindingsWasmSrcPath     string
	bindingsRuntime         string
	bindingsSpecFile        string
	bindingsSpecFormat      string
	bindingsNoEmbedMetadata bool
	bindingsJSONFlag        bool
	bindingsFormatFlag      string
)

// generateBindingsOutput is the structured result for --json / --format json.
type generateBindingsOutput struct {
	Package  string   `json:"package"`
	Output   string   `json:"output"`
	Runtime  string   `json:"runtime"`
	Files    []string `json:"files"`
	DebugMeta bool    `json:"debug_metadata"`
}

var generateBindingsCmd = &cobra.Command{
	Use:   "generate-bindings [wasm-file]",
	Short: "Generate TypeScript bindings for a Soroban smart contract",
	Long: `Generate strongly-typed TypeScript client bindings from a Soroban smart contract.

Spec sources (mutually exclusive):
  <wasm-file>          Extract the contract spec from a compiled WASM binary.
  --spec-file <path>   Load the contract spec from a JSON or XDR ABI file instead
                       of requiring on-chain contract code discovery.

Runtime targets (--runtime):
  node       (default) Node.js – uses child_process and Node transports.
  browser    Browser-safe – uses the global fetch API; no Node-only imports.
  universal  Emits environment-detection code that works in Node, browser,
             and Electron renderer processes.

Generated files:
  types.ts               – TypeScript interfaces, enums, and error classes
  metadata.ts            – Per-function ABI descriptors with source-location hints
  client.ts              – Strongly-typed async client class
  Glassbox-integration.ts – ErstSimulator wrapper for local simulation
  index.ts               – Barrel export
  package.json           – npm package manifest
  README.md              – Usage documentation

Examples:
  glassbox generate-bindings contract.wasm
  glassbox generate-bindings --output ./src/bindings --package my-contract contract.wasm
  glassbox generate-bindings --runtime browser --output ./src/bindings contract.wasm
  glassbox generate-bindings --spec-file contract-abi.json --package my-contract
  glassbox generate-bindings --spec-file contract.xdr --spec-format xdr --package my-contract
  glassbox generate-bindings --debug-metadata --wasm-source ./contract.wasm contract.wasm`,
	Args: cobra.MaximumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		return validateGenerateBindingsFlags(bindingsNetwork, args, bindingsOutput, bindingsRuntime, bindingsSpecFile, bindingsSpecFormat)
	},
	RunE: runGenerateBindings,
}

func runGenerateBindings(_ *cobra.Command, args []string) error {
	if bindingsOutput == "" {
		bindingsOutput = "."
	}

	var specBytes []byte
	var specFormat abi.ImportFormat
	var wasmBytes []byte

	switch {
	case bindingsSpecFile != "":
		// External ABI/spec file – no WASM needed.
		var err error
		specBytes, err = os.ReadFile(bindingsSpecFile)
		if err != nil {
			return fmt.Errorf("failed to read spec file: %w", err)
		}
		switch strings.ToLower(bindingsSpecFormat) {
		case "json":
			specFormat = abi.ImportFormatJSON
		case "xdr":
			specFormat = abi.ImportFormatXDR
		default:
			// Auto-detect.
			specFormat = abi.DetectFormat(specBytes)
		}
		if bindingsPackage == "" {
			base := filepath.Base(bindingsSpecFile)
			bindingsPackage = base[:len(base)-len(filepath.Ext(base))]
		}

	case len(args) == 1:
		// Classic WASM path.
		var err error
		wasmBytes, err = os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("failed to read WASM file: %w", err)
		}
		if bindingsPackage == "" {
			base := filepath.Base(args[0])
			bindingsPackage = base[:len(base)-len(filepath.Ext(base))]
		}

	default:
		return fmt.Errorf("provide either a wasm-file argument or --spec-file")
	}

	wasmSrc := bindingsWasmSrcPath
	if wasmSrc == "" && bindingsDebugMeta && len(args) == 1 {
		wasmSrc = args[0]
	}

	config := bindings.GeneratorConfig{
		WasmBytes:               wasmBytes,
		SpecBytes:               specBytes,
		SpecFormat:              specFormat,
		OutputDir:               bindingsOutput,
		PackageName:             bindingsPackage,
		ContractID:              bindingsContractID,
		Network:                 bindingsNetwork,
		RuntimeTarget:           bindings.RuntimeTarget(strings.ToLower(bindingsRuntime)),
		IncludeDebugMeta:        bindingsDebugMeta,
		WasmSourcePath:          wasmSrc,
		NoEmbedArtifactMetadata: bindingsNoEmbedMetadata,
	}

	generator := bindings.NewGenerator(config)
	files, err := generator.Generate()
	if err != nil {
		return fmt.Errorf("failed to generate bindings: %w", err)
	}

	written := make([]string, 0, len(files))
	for _, file := range files {
		fullPath := filepath.Join(bindingsOutput, file.Path)

		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}

		if err := os.WriteFile(fullPath, []byte(file.Content), 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", fullPath, err)
		}
		written = append(written, fullPath)

		if !clioutput.WantsJSON(bindingsJSONFlag, bindingsFormatFlag) {
			fmt.Printf("Generated: %s\n", fullPath)
		}
	}

	if clioutput.WantsJSON(bindingsJSONFlag, bindingsFormatFlag) {
		return clioutput.WriteStdout("generate-bindings", generateBindingsOutput{
			Package:   bindingsPackage,
			Output:    bindingsOutput,
			Runtime:   bindingsRuntime,
			Files:     written,
			DebugMeta: bindingsDebugMeta,
		})
	}

	fmt.Printf("\n[OK] TypeScript bindings generated successfully\n")
	fmt.Printf("Package: %s\n", bindingsPackage)
	fmt.Printf("Output:  %s\n", bindingsOutput)
	fmt.Printf("Runtime: %s\n", bindingsRuntime)
	if bindingsDebugMeta {
		fmt.Println("Debug metadata: enabled (metadata.ts + withDebugMetadata option)")
	}
	if bindingsNoEmbedMetadata {
		fmt.Println("Artifact metadata: disabled (staleness detection unavailable)")
	} else {
		fmt.Println("Artifact metadata: embedded (use `glassbox check-bindings` to detect stale artifacts)")
	}

	return nil
}

func init() {
	generateBindingsCmd.Flags().StringVarP(&bindingsOutput, "output", "o", "",
		"Output directory (defaults to current directory)")
	generateBindingsCmd.Flags().StringVarP(&bindingsPackage, "package", "p", "",
		"Package name (defaults to input filename without extension)")
	generateBindingsCmd.Flags().StringVar(&bindingsContractID, "contract-id", "",
		"Contract ID for network calls")
	generateBindingsCmd.Flags().StringVarP(&bindingsNetwork, "network", "n", "testnet",
		"Stellar network (testnet, mainnet, futurenet)")
	generateBindingsCmd.Flags().BoolVar(&bindingsDebugMeta, "debug-metadata", false,
		"Generate ABI metadata objects and withDebugMetadata() wrappers")
	generateBindingsCmd.Flags().StringVar(&bindingsWasmSrcPath, "wasm-source", "",
		"Source path hint embedded in debug metadata (defaults to the WASM file path when --debug-metadata is set)")
	generateBindingsCmd.Flags().StringVar(&bindingsRuntime, "runtime", "node",
		"Target runtime environment: node (default), browser, or universal")
	generateBindingsCmd.Flags().StringVar(&bindingsSpecFile, "spec-file", "",
		"Path to an external contract ABI/spec file (JSON or XDR). When set, no WASM file is required.")
	generateBindingsCmd.Flags().StringVar(&bindingsSpecFormat, "spec-format", "",
		"Format of --spec-file: json or xdr (auto-detected when omitted)")
	generateBindingsCmd.Flags().BoolVar(&bindingsNoEmbedMetadata, "no-embed-metadata", false,
		"Disable embedding @glassbox-bindings-meta headers in generated files (disables staleness detection)")
	generateBindingsCmd.Flags().BoolVar(&bindingsJSONFlag, "json", false,
		"Output generation summary as machine-readable JSON")
	generateBindingsCmd.Flags().StringVar(&bindingsFormatFlag, "format", "text",
		"Output format: text or json")

	_ = generateBindingsCmd.RegisterFlagCompletionFunc("network", completeNetworkFlag)
	_ = generateBindingsCmd.RegisterFlagCompletionFunc("runtime", completeBindingsRuntimeFlag)
	_ = generateBindingsCmd.RegisterFlagCompletionFunc("spec-format", completeSpecFormatFlag)
	_ = generateBindingsCmd.RegisterFlagCompletionFunc("format", completeGeneralFormatFlag)

	rootCmd.AddCommand(generateBindingsCmd)
}
