// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

// completion_helpers.go is the single source of truth for every enum-valued
// flag used by Glassbox commands.  Each exported slice and completeXxx
// function is referenced by RegisterFlagCompletionFunc calls in the
// individual command init() blocks.
//
// Rules:
//   - Completions must never perform network I/O (completeNetworkFlag is the
//     sole exception: it may read locally-cached custom network names, but
//     falls back cleanly when the config is unavailable).
//   - All completeXxx functions return cobra.ShellCompDirectiveNoFileComp so
//     the shell does not fall back to filename completion for enum flags.
//   - Add new enum tables here; reference them from the relevant command.
package cmd

import (
	"fmt"
	"sort"

	"github.com/dotandev/glassbox/internal/config"
	"github.com/spf13/cobra"
)

// ── Network values ────────────────────────────────────────────────────────────

// NetworkValues lists the built-in Stellar network names accepted by --network.
var NetworkValues = []string{"testnet", "mainnet", "futurenet"}

// InitNetworkValues adds "standalone" for commands that support local networks.
var InitNetworkValues = []string{"public", "testnet", "futurenet", "standalone"}

var networkAliases = []string{
	"testnet\tStellar test network",
	"mainnet\tStellar public network",
	"futurenet\tStellar future network",
}
var initNetworkAliases = []string{
	"public\tStellar public network",
	"testnet\tStellar test network",
	"futurenet\tStellar future network",
	"standalone\tLocal standalone network",
}

// ── Theme values ──────────────────────────────────────────────────────────────

// ThemeValues lists all accepted --theme flag values.
var ThemeValues = []string{
	"default",
	"dark",
	"light",
	"deuteranopia",
	"protanopia",
	"tritanopia",
	"high-contrast",
}

var themeNames = []string{
	"default\tStandard terminal colors",
	"dark\tDark terminal background",
	"light\tLight terminal background",
	"deuteranopia\tRed-green color blind friendly",
	"protanopia\tRed color blind friendly",
	"tritanopia\tBlue-yellow color blind friendly",
	"high-contrast\tHigh contrast for low-vision",
}

// ── XDR values ────────────────────────────────────────────────────────────────

// XDRFormatValues lists the accepted --format values for the xdr command.
var XDRFormatValues = []string{"json", "table"}

// XDRTypeValues lists the accepted --type values for the xdr command.
var XDRTypeValues = []string{"ledger-entry", "diagnostic-event"}

var xdrFormats = []string{"json\tJSON output", "table\tTabular output"}
var xdrTypes = []string{"ledger-entry\tLedger entry XDR", "diagnostic-event\tDiagnostic event XDR"}

// ── Report / export format values ─────────────────────────────────────────────

// ReportFormatValues lists the accepted --format values for the report command.
var ReportFormatValues = []string{"text", "json", "html", "pdf", "html,pdf"}

var reportFormats = []string{
	"text\tText diagnostic summary",
	"json\tJSON diagnostic summary",
	"html\tHTML report",
	"pdf\tPDF report",
	"html,pdf\tBoth HTML and PDF",
}

// TraceExportFormatValues lists the accepted --format/--export-format values
// for the trace command.
var TraceExportFormatValues = []string{"html", "markdown", "json", "text"}

// ExportFormatValues lists the accepted --format values for the export command.
var ExportFormatValues = []string{"text", "json"}

// ── Bindings runtime / spec-format ───────────────────────────────────────────

// BindingsRuntimeValues lists the accepted --runtime values for
// generate-bindings and check-bindings.
var BindingsRuntimeValues = []string{"node", "browser", "universal"}

// SpecFormatValues lists the accepted --spec-format values.
var SpecFormatValues = []string{"json", "xdr"}

// GeneralFormatValues covers text/json used by several commands.
var GeneralFormatValues = []string{"text", "json"}

var bindingsRuntimes = []string{
	"node\tNode.js (default)",
	"browser\tBrowser-safe (no Node imports)",
	"universal\tNode + browser + Electron",
}
var specFormats = []string{
	"json\tJSON ABI file",
	"xdr\tXDR ABI file",
}
var generalFormats = []string{
	"text\tHuman-readable text",
	"json\tMachine-readable JSON",
}

// ── Log level / verbosity ─────────────────────────────────────────────────────

// LogLevelValues lists the accepted --log-level values.
var LogLevelValues = []string{"trace", "debug", "info", "warn", "error"}

// TraceVerbosityValues lists the accepted --trace-verbosity values.
var TraceVerbosityValues = []string{"summary", "normal", "verbose"}

// ProfileFormatValues lists the accepted --profile-format values.
var ProfileFormatValues = []string{"html", "svg"}

var logLevels = []string{
	"trace\tMost verbose",
	"debug\tDebug and above",
	"info\tInformational and above (default)",
	"warn\tWarnings and errors only",
	"error\tErrors only",
}
var traceVerbosities = []string{
	"summary\tMinimal output",
	"normal\tStandard detail (default)",
	"verbose\tFull detail",
}
var profileFormats = []string{
	"html\tInteractive flamegraph",
	"svg\tRaw SVG",
}

// ── View modes ────────────────────────────────────────────────────────────────

// ViewModeValues lists accepted --view values for the debug command.
var ViewModeValues = []string{"trace", "flamegraph", "events", "auth", "budget", "storage"}

var viewModes = []string{
	"trace\tExecution trace viewer (default)",
	"flamegraph\tFlamegraph viewer",
	"events\tContract events",
	"auth\tAuthorization trace",
	"budget\tBudget usage breakdown",
	"storage\tLedger storage diff",
}

// ── Failover strategy ─────────────────────────────────────────────────────────

// FailoverStrategyValues lists accepted --failover-strategy values.
var FailoverStrategyValues = []string{"weighted", "sticky", "round_robin"}

var failoverStrategies = []string{
	"weighted\tProbabilistic selection by health score (default)",
	"sticky\tAlways use the single healthiest endpoint",
	"round_robin\tCycle through all healthy endpoints",
}

// ── Completion functions ───────────────────────────────────────────────────────

// completeNetworkFlag returns the built-in network names plus any locally
// configured custom networks.  It never performs network I/O.
func completeNetworkFlag(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	completions := append([]string{}, networkAliases...)
	networks, err := config.ListCustomNetworks()
	if err == nil && len(networks) > 0 {
		sort.Strings(networks)
		for _, name := range networks {
			completions = append(completions, fmt.Sprintf("%s\tSaved custom network", name))
		}
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

func completeInitNetworkFlag(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return initNetworkAliases, cobra.ShellCompDirectiveNoFileComp
}

func completeThemeFlag(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return themeNames, cobra.ShellCompDirectiveNoFileComp
}

func completeXDRFormatFlag(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return xdrFormats, cobra.ShellCompDirectiveNoFileComp
}

func completeXDRTypeFlag(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return xdrTypes, cobra.ShellCompDirectiveNoFileComp
}

func completeReportFormatFlag(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return reportFormats, cobra.ShellCompDirectiveNoFileComp
}

func completeTraceExportFormatFlag(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{"html\tInteractive HTML", "markdown\tMarkdown report", "json\tMachine-readable JSON", "text\tPlain text"}, cobra.ShellCompDirectiveNoFileComp
}

func completeExportFormatFlag(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return generalFormats, cobra.ShellCompDirectiveNoFileComp
}

func completeBindingsRuntimeFlag(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return bindingsRuntimes, cobra.ShellCompDirectiveNoFileComp
}

func completeSpecFormatFlag(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return specFormats, cobra.ShellCompDirectiveNoFileComp
}

func completeGeneralFormatFlag(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return generalFormats, cobra.ShellCompDirectiveNoFileComp
}

func completeLogLevelFlag(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return logLevels, cobra.ShellCompDirectiveNoFileComp
}

func completeTraceVerbosityFlag(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return traceVerbosities, cobra.ShellCompDirectiveNoFileComp
}

func completeProfileFormatFlag(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return profileFormats, cobra.ShellCompDirectiveNoFileComp
}

func completeViewModeFlag(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return viewModes, cobra.ShellCompDirectiveNoFileComp
}

func completeFailoverStrategyFlag(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return failoverStrategies, cobra.ShellCompDirectiveNoFileComp
}

// completeNoOp returns no completions and disables file completion.
// Use for flags that accept free-form strings with no known finite set.
func completeNoOp(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}
