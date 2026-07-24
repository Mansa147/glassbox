// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/dotandev/glassbox/internal/config"
	"github.com/dotandev/glassbox/internal/deeplink"
	"github.com/dotandev/glassbox/internal/localization"
	"github.com/dotandev/glassbox/internal/logger"
	"github.com/dotandev/glassbox/internal/protocolreg"
	"github.com/dotandev/glassbox/internal/shutdown"
	"github.com/dotandev/glassbox/internal/telemetry"
	"github.com/dotandev/glassbox/internal/trace"
	"github.com/dotandev/glassbox/internal/updater"
	"github.com/dotandev/glassbox/internal/version"
	"github.com/dotandev/glassbox/internal/visualizer"
	"github.com/spf13/cobra"
)

// Global flag variables
var (
	TimestampFlag int64
	WindowFlag int64
	ProfileFlag bool
	ProfileFormatFlag string
	DeepLinkFlag string
	VersionFlag bool
	LogLevelFlag string
	VerboseFlag  bool
	NoColorFlag  bool

	AuditLogPathFlag string
	AuditLogProviderFlag string
	AuditLogSoftwareKey string
	AuditLogPKCS11Module string
	AuditLogPKCS11PIN string
	AuditLogPKCS11TokenLabel string
	AuditLogPKCS11KeyLabel string
	AuditLogPKCS11KeyIDHex string
	AuditLogMetadata []string

	// ConfigPassphraseFlag is the passphrase used to decrypt an encrypted config file.
	// It can also be supplied via GLASSBOX_CONFIG_PASSPHRASE.
	ConfigPassphraseFlag string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "glassbox",
	Short: "Soroban smart contract debugger and transaction analyzer",
	Long: `Glassbox is a specialized developer tool for the Stellar network that helps you
debug failed Soroban transactions and analyze smart contract execution.

Key features:
  • Debug failed transactions with detailed error traces
  • Simulate transaction execution locally
  • Track token flows and contract events
  • Manage debugging sessions for complex workflows
  • Cache transaction data for offline analysis
  • Local WASM replay for rapid contract development

Examples:
  Glassbox debug abc123...def                    Debug a transaction
  Glassbox debug --network testnet abc123...def  Debug on testnet
  Glassbox debug --wasm ./contract.wasm          Test contract locally
  Glassbox session list                          View saved sessions
  Glassbox cache status                          Check cache usage

Get started with 'Glassbox debug --help' or visit the documentation.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if VersionFlag {
			fmt.Println(version.Version)
			os.Exit(0)
		}

		// Disable ANSI colours when --no-color is set or GLASSBOX_NO_COLOR is in the
		// environment. NO_COLOR (https://no-color.org) is also honoured by each
		// subsystem individually; this block provides the global CLI-flag path.
		if NoColorFlag || os.Getenv("GLASSBOX_NO_COLOR") != "" {
			_ = os.Setenv("NO_COLOR", "1") // propagate to child processes
			visualizer.SetNoColor(true)
			trace.SetNoColor(true)
		}

		// Apply log verbosity from CLI flags before any subsystem initialises.
		// --verbose is a shorthand for --log-level=debug.
		if VerboseFlag {
			logger.SetLevel(logger.ParseLevel("debug"))
		} else if LogLevelFlag != "" {
			logger.SetLevel(logger.ParseLevel(LogLevelFlag))
		}

		// Handle deep link probe invocation before anything else.
		// The doctor command triggers this to verify OS dispatch works.
		if DeepLinkFlag != "" {
			return handleDeepLinkProbe(DeepLinkFlag)
		}

		// Load localizations
		if err := localization.LoadTranslations(); err != nil {
			return err
		}

		// Show "Upgrade available" banner from last run's cached check (non-blocking)
		updater.ShowBannerFromCache(version.Version)
		// Ping version endpoint asynchronously for next run
		checkForUpdatesAsync()

		if TelemetryFlag {
			telemetry.RecordCommandUsage(cmd.Context(), cmd.CommandPath())
		}

		return nil
	},
	SilenceUsage:  true,
	SilenceErrors: true,
	Version:       version.Version,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	coordinator := shutdown.NewCoordinator()
	setShutdownCoordinator(coordinator)
	defer clearShutdownCoordinator()

	err := executeWithSignals(ctx, stop, sigCh, coordinator, func(execCtx context.Context) error {
		return rootCmd.ExecuteContext(execCtx)
	})

	if AuditLogPathFlag != "" {
		if auditErr := writeOperationAuditLog(os.Args, err); auditErr != nil && err == nil {
			err = auditErr
		}
	}

	return err
}

var forceExit = os.Exit

func executeWithSignals(
	ctx context.Context,
	stop context.CancelFunc,
	sigCh <-chan os.Signal,
	coordinator *shutdown.Coordinator,
	execFn func(context.Context) error,
) error {
	var interrupted atomic.Bool
	shutdownDone := make(chan struct{})

	go func() {
		defer close(shutdownDone)
		for {
			select {
			case <-ctx.Done():
				return
			case <-sigCh:
				if interrupted.CompareAndSwap(false, true) {
					stop()
					shutdownComplete := make(chan struct{})
					go func() {
						runShutdownHooksWithTimeout(coordinator, shutdownTimeout)
						close(shutdownComplete)
					}()
					select {
					case <-shutdownComplete:
					case <-sigCh:
						forceExit(InterruptExitCode)
					}
					return
				}
				forceExit(InterruptExitCode)
			}
		}
	}()

	err := execFn(ctx)
	stop()
	<-shutdownDone

	if interrupted.Load() {
		_ = err
		return ErrInterrupted
	}

	return err
}

func applyTelemetryConfig(cfg *config.Config) {
	if !rootCmd.PersistentFlags().Lookup("telemetry").Changed {
		TelemetryFlag = cfg.Telemetry
	}
	if !rootCmd.PersistentFlags().Lookup("telemetry-anonymized").Changed {
		TelemetryAnonymizedFlag = cfg.TelemetryAnonymized
	}
}

// checkForUpdatesAsync runs the update check in a goroutine to not block CLI startup
func init() {
	rootCmd.PersistentFlags().StringVar(&AuditLogPathFlag, "audit-log", "", "Write a signed operation audit log JSON file")
	rootCmd.PersistentFlags().StringVar(&AuditLogProviderFlag, "audit-log-provider", "", "Audit signing provider to use (software, pkcs11)")
	rootCmd.PersistentFlags().StringVar(&AuditLogSoftwareKey, "audit-log-software-private-key", "", "PKCS#8 PEM Ed25519 private key for CLI operation auditing")
	rootCmd.PersistentFlags().StringVar(&AuditLogPKCS11Module, "audit-log-pkcs11-module", "", "PKCS#11 shared library path for CLI operation auditing")
	rootCmd.PersistentFlags().StringVar(&AuditLogPKCS11PIN, "audit-log-pkcs11-pin", "", "PKCS#11 PIN for CLI operation auditing")
	rootCmd.PersistentFlags().StringVar(&AuditLogPKCS11TokenLabel, "audit-log-pkcs11-token-label", "", "PKCS#11 token label for CLI operation auditing")
	rootCmd.PersistentFlags().StringVar(&AuditLogPKCS11KeyLabel, "audit-log-pkcs11-key-label", "", "PKCS#11 key label for CLI operation auditing")
	rootCmd.PersistentFlags().StringVar(&AuditLogPKCS11KeyIDHex, "audit-log-pkcs11-key-id", "", "PKCS#11 key ID hex for CLI operation auditing")
	rootCmd.PersistentFlags().StringArrayVar(&AuditLogMetadata, "audit-log-meta", nil, "Additional metadata entries for CLI operation audit logs in key=value form")

	rootCmd.PersistentFlags().StringVar(&ConfigPassphraseFlag, "config-passphrase", "",
		"Passphrase to decrypt an encrypted config file (or set GLASSBOX_CONFIG_PASSPHRASE)")
	_ = rootCmd.PersistentFlags().MarkHidden("config-passphrase") // sensitive; hidden from default help
}

func checkForUpdatesAsync() {
	// Run update check in background goroutine
	go func() {
		// Use the Version variable from version package
		checker := updater.NewChecker(version.Version)
		checker.CheckForUpdates()
	}()
}

// handleDeepLinkProbe processes a glassbox:// URL dispatched by the OS or by the
// doctor probe.
//
// Recognised paths:
//   - "doctor-probe"          — exits 0 immediately (used by the doctor check)
//   - "debug/<txhash>?..."    — delegates to the protocol:handle dispatcher which
//     re-invokes the binary with the full "debug" sub-command and validated flags
func handleDeepLinkProbe(rawURL string) error {
	if !strings.HasPrefix(rawURL, deeplink.Scheme+"://") {
		return fmt.Errorf("unrecognised deep link scheme: %s", rawURL)
	}

	// Strip the scheme prefix to get the host+path portion for simple matching.
	rest := strings.TrimPrefix(rawURL, deeplink.Scheme+"://")

	switch {
	case rest == "doctor-probe":
		// Intentional no-op: the doctor check just needs exit code 0.
		os.Exit(0)

	case strings.HasPrefix(rest, "debug/") || rest == "debug":
		// Delegate to the protocol handler which validates the URI and re-invokes
		// the binary with the correct "debug" sub-command arguments.
		return dispatchDebugDeepLink(rawURL)

	default:
		return fmt.Errorf("unhandled deep link path %q: supported paths are debug/<txhash> and doctor-probe", rest)
	}

	return nil
}

// dispatchDebugDeepLink validates a glassbox://debug/... URI and re-invokes the
// current binary as "glassbox debug <hash> --network <n> [--op <i>] [--view <v>]".
// This keeps the deep-link handler thin and reuses all validation in ParseDebugURI.
func dispatchDebugDeepLink(rawURL string) error {
	// Import is done via the protocolreg package already used in protocol.go.
	// We call the same ParseDebugURI used by protocol:handle so validation is
	// identical regardless of how the URI arrives (OS dispatch vs CLI flag).
	parsed, err := protocolreg.ParseDebugURI(rawURL)
	if err != nil {
		return fmt.Errorf("invalid deep link: %w", err)
	}

	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	debugArgs := []string{"debug", parsed.TransactionHash, "--network", parsed.Network}

	if parsed.Op != nil {
		debugArgs = append(debugArgs, "--op", fmt.Sprintf("%d", *parsed.Op))
	}
	if parsed.View != "" {
		debugArgs = append(debugArgs, "--view", parsed.View)
	}

	child := exec.CommandContext(context.Background(), executablePath, debugArgs...)
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	return child.Run()
}

func init() {
	// Root command initialization
	rootCmd.PersistentFlags().Int64Var(
		&TimestampFlag,
		"timestamp",
		0,
		"Override the ledger header timestamp (Unix epoch)",
	)

	rootCmd.PersistentFlags().Int64Var(
		&WindowFlag,
		"window",
		0,
		"Run range simulation across a time window (seconds)",
	)

	rootCmd.PersistentFlags().BoolVar(
		&ProfileFlag,
		"profile",
		false,
		"Enable CPU/Memory profiling and generate a flamegraph",
	)

	rootCmd.PersistentFlags().StringVar(
		&ProfileFormatFlag,
		"profile-format",
		"html",
		"Flamegraph export format: 'html' (interactive) or 'svg' (raw)",
	)

	rootCmd.PersistentFlags().StringVar(
		&LogLevelFlag,
		"log-level",
		"",
		"Set log verbosity level (trace, debug, info, warn, error)",
	)

	rootCmd.PersistentFlags().BoolVar(
		&VerboseFlag,
		"verbose",
		false,
		"Enable verbose output (equivalent to --log-level=debug)",
	)

	rootCmd.PersistentFlags().BoolVar(
		&NoColorFlag,
		"no-color",
		false,
		"Disable ANSI color output (also honoured via NO_COLOR or GLASSBOX_NO_COLOR env vars)",
	)

	rootCmd.PersistentFlags().BoolVar(
		&TelemetryFlag,
		"telemetry",
		false,
		"Opt in to anonymized command usage telemetry",
	)

	rootCmd.PersistentFlags().BoolVar(
		&TelemetryAnonymizedFlag,
		"telemetry-anonymized",
		true,
		"Send command usage telemetry in anonymized mode",
	)

	rootCmd.PersistentFlags().StringVar(
		&DeepLinkFlag,
		"deep-link",
		"",
		"Handle an glassbox:// deep link URL (used internally by the doctor probe)",
	)
	rootCmd.PersistentFlags().BoolVarP(
		&VersionFlag,
		"version",
		"V",
		false,
		"Print Glassbox version",
	)
	// Hide from normal help output; it is an internal dispatch mechanism.
	_ = rootCmd.PersistentFlags().MarkHidden("deep-link")

	// Define command groups for better organization
	rootCmd.AddGroup(&cobra.Group{
		ID:    "core",
		Title: "Core Debugging Commands:",
	})
	rootCmd.AddGroup(&cobra.Group{
		ID:    "testing",
		Title: "Testing & Validation Commands:",
	})
	rootCmd.AddGroup(&cobra.Group{
		ID:    "management",
		Title: "Session & Cache Management:",
	})
	rootCmd.AddGroup(&cobra.Group{
		ID:    "development",
		Title: "Development Tools:",
	})
	rootCmd.AddGroup(&cobra.Group{
		ID:    "utility",
		Title: "Utility Commands:",
	})

	// Register commands
	rootCmd.AddCommand(statsCmd)

	// Register completion for persistent (root-level) enum flags so every
	// subcommand inherits correct suggestions for --log-level and --profile-format.
	_ = rootCmd.RegisterFlagCompletionFunc("log-level", completeLogLevelFlag)
	_ = rootCmd.RegisterFlagCompletionFunc("profile-format", completeProfileFormatFlag)
}
